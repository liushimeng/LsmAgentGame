// Package profession — loader_batch.go: 批量抽路径与并行水合
// (2026-09-21 §虚拟城市居民档案锚定)。
//
// 契约: lag_docs/虚拟城市/已实现/03-Agent设计/虚拟城市-城市居民人物卡档案锚定设计-v1.md §3。
// 与 loader.go(Draw/单卡 LRU)同 package,复用 ForceIndex / parseFile:
//   - DrawPaths:仅索引洗牌,零文件 IO —— 城市锚定流水线第一步「先抽 n 条
//     相对路径,再异步水合」;
//   - PoolSize:文档池索引总数(≈10 万级,以实测为准);
//   - HydrateBatch:worker pool 并行「读文件 + 解析 frontmatter」,**绕过 LRU
//     直读**(避免 10 万级批量水合把单卡缓存 256 上限挤穿,挤掉 12 座位
//     对局正在用的热卡)。
//
// 失败语义:单卡失败按 loadCard 同款 Warn 日志 + parseFail 计数,跳过不中断
// 批量;调用方以「成功卡 + 失败路径」两个返回值自行兜底(锚定场景:池不足时
// 只锚定前 n_pool 名,其余居民保持合成数值)。
package profession

import (
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

const (
	// maxHydrateWorkers 批量水合并发上限(契约 §3 clamp [1,64])。
	maxHydrateWorkers = 64
	// hydrateProgressEvery 进度回调粒度:每完成 512 张回调一次;最终必然
	// 再回调一次 (total,total)(契约 §3)。
	hydrateProgressEvery = 512
)

// defaultHydrateWorkers 默认 worker 数:min(32, NumCPU×4)(契约 §3;
// IO 密集型,4×CPU 让单卡 0.3~0.8ms 的解析在 IO 等待间隙充分重叠)。
func defaultHydrateWorkers() int {
	n := runtime.NumCPU() * 4
	if n > 32 {
		n = 32
	}
	if n < 1 {
		n = 1
	}
	return n
}

// DrawPaths 返回 n 个不重复的相对路径(仅索引洗牌,零文件 IO)。
// 语义与 drawPairs 的前缀 Fisher-Yates 一致:n > 池大小时返回全池洗牌结果;
// 池为空/根目录缺失返回 nil。调用方负责后续水合(典型:HydrateBatch)。
// rng 为注入随机源(同 seed 同索引 → 同路径集,与 Draw 的确定性契约同构)。
func (l *Loader) DrawPaths(n int, rng *rand.Rand) []string {
	if n <= 0 || rng == nil {
		return nil
	}
	l.ForceIndex()

	l.mu.RLock()
	pool := append([]string(nil), l.indexPath...)
	l.mu.RUnlock()
	if len(pool) == 0 {
		return nil
	}
	k := n
	if k > len(pool) {
		k = len(pool)
	}
	// 前缀 Fisher-Yates(与 drawPairs 同款洗牌形状,仅候选数不同:
	// DrawPaths 抽 exactly n 条,不吃 drawRetryFactor 补抽倍率)。
	for i := 0; i < k; i++ {
		j := i + rng.Intn(len(pool)-i)
		pool[i], pool[j] = pool[j], pool[i]
	}
	return pool[:k]
}

// PoolSize 返回当前索引条数(未建索引时触发 ForceIndex;根目录缺失/为空
// 返回 0)。
func (l *Loader) PoolSize() int {
	l.ForceIndex()
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.indexPath)
}

// hydrateSlot 单条路径的水合结果槽(按输入下标写,保证输出与 paths 中
// 成功者顺序一致 —— 契约 §3 返回值语义;顺序确定后 workers=1 与 workers=8
// 结果逐位相同,单测可断言)。
type hydrateSlot struct {
	card Card
	err  error
}

// HydrateBatch 并行读取并解析 paths 对应的人物卡(绕过 LRU 直读,避免批量
// 水合把单卡缓存 256 上限挤穿)。workers clamp [1, 64],默认 min(32, NumCPU×4)。
// 返回:(成功卡(带 L1 域名,顺序与 paths 中成功者一致), 失败相对路径)。
// progress 非 nil 时每完成 512 张回调一次 (done, total);最终必然回调
// (total, total)。单卡失败:计 parseFail 同款 Warn 日志,跳过不中断。
// 并发安全、可重入(锁只触 loader 自身 mutex 与 atomic 计数)。
func (l *Loader) HydrateBatch(paths []string, workers int, progress func(done, total int)) ([]DomainCard, []string) {
	total := len(paths)
	if total == 0 {
		return nil, nil
	}
	if workers <= 0 {
		workers = defaultHydrateWorkers()
	}
	if workers > maxHydrateWorkers {
		workers = maxHydrateWorkers
	}
	if workers > total {
		workers = total
	}

	slots := make([]hydrateSlot, total)
	var done atomic.Int64
	tasks := make(chan int, total)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range tasks {
				card, err := l.parseFile(paths[i])
				slots[i] = hydrateSlot{card: card, err: err}
				if err != nil {
					// 与 loadCard 同款失败观测:parseFail 计数 + Warn 日志
					// (批量路径不写 LRU,失败卡不占缓存位)。
					l.mu.Lock()
					l.parseFail++
					l.mu.Unlock()
					logger.L().Warn("wealth profession card parse failed, skipped",
						zap.String("path", paths[i]), zap.Error(err))
				}
				if progress != nil {
					// AddInt64 返回自增后的唯一值 → 每个 512 倍数恰有一个
					// goroutine 观察到,进度回调不重不漏(最终由 wg.Wait
					// 之后的收尾回调兜底 (total,total))。
					if d := done.Add(1); d%hydrateProgressEvery == 0 {
						progress(int(d), total)
					}
				}
			}
		}()
	}
	for i := range paths {
		tasks <- i
	}
	close(tasks)
	wg.Wait()
	if progress != nil {
		progress(total, total)
	}

	cards := make([]DomainCard, 0, total)
	var failed []string
	for i := range paths {
		if slots[i].err != nil {
			failed = append(failed, paths[i])
			continue
		}
		cards = append(cards, DomainCard{Card: slots[i].card, Domain: domainOfPath(paths[i])})
	}
	return cards, failed
}
