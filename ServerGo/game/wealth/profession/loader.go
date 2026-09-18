// Package profession — loader.go: 文档池懒加载器(P0 v1,2026-09-14 §财商流P0)。
//
// 契约: lag_docs/财商流游戏/已实现/03-Agent设计/财商流游戏-职业卡与加载器设计-v1.md §3。
// 三段式:
//   - 阶段 0(NewLoader):不读盘,仅记录根路径 + sync.Once。
//   - 阶段 1(buildIndex):首次抽卡 / HTTP professions 触发;walk 收集 *.md 相对
//     路径清单,**不解析 frontmatter**;结果缓存进程内。
//   - 阶段 2(Draw):均匀抽 n 张不重复 → 逐张读文件 → 解析 frontmatter
//     (gopkg.in/yaml.v3,形状容错见 frontmatter.go)→ 映射 Card → LRU 缓存(256)。
//
// 失败语义(2026-09-16 §文档池解析修复 修订):
//   - 单卡解析失败 → 跳过 + logger.Warn + parseFail 计数,并**从池中补抽**
//     (最多 drawRetryFactor×n 张候选),保证「文档池可用时不回退 curated」;
//   - 文档池整体不可用(根目录缺失 / 索引为空 / 候选耗尽)→ 回退精选补足;
//   - ForceIndex 后**同步**做一次 200 张采样自检,成功率 < 95% 打
//     logger.Error(线上可观测:形状漂移导致的批量解析失败第一时间暴露);
//   - seed 注入的 *rand.Rand 保证同 seed 同索引 → 同卡集(确定性)。
//
// 目录约定(硬约束): buildIndex 跳过所有 `_` / `.` 前缀目录(_框架、_交付说明
// 等为知识库框架文档,不是卡池)。**新增维度目录不得使用 `_` 前缀**,否则整棵
// 子树不入卡池。
package profession

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// lruCapacity 解析缓存上限(加载器文档 §3.1)。
const lruCapacity = 256

// drawRetryFactor 补抽倍率(2026-09-16 §文档池解析修复):单卡解析失败时最多
// 再试 (drawRetryFactor-1)×n 张候选。真实池失败率 ≈0.33%(income_monthly 与
// income_range 双缺的档案 stub),n=12 时 20×12=240 张候选足够把「回退 curated」
// 的概率压到 0.33%^240 ≈ 0;池本身不足 12 张时才会真正走到 curated 兜底。
const drawRetryFactor = 20

// selfCheckSample 是 ForceIndex 后采样自检的张数(可观测性,不影响抽卡路径)。
const selfCheckSample = 200

// selfCheckMinSuccessRate 采样解析成功率红线:低于此值打 logger.Error。
const selfCheckMinSuccessRate = 0.95

// Loader 是文档池懒加载器(并发安全)。
type Loader struct {
	root string // 文档池磁盘根

	mu         sync.RWMutex
	indexOnce  sync.Once
	indexPath  []string // 相对路径清单(排序稳定,保证同 seed 同抽卡)
	indexBuilt bool     // 阶段 1 是否已执行
	available  bool     // 根目录可读且非空
	lru        map[string]*lruEntry
	lruOrder   []string // 最近使用在尾
	parseFail  int      // 解析失败累计(跳过的卡数)

	// 采样自检(2026-09-16 §文档池解析修复):与 Draw 的 parseFail 分开计数,
	// 避免自检把「运行时抽卡跳过数」指标污染。
	selfCheckOnce sync.Once
	selfCheck     PoolSelfCheck
}

// PoolSelfCheck 是一次采样自检的结果(可观测性:形状漂移导致的批量解析失败
// 必须在日志/HTTP 里看得见,不能像旧实现那样静默回退 curated)。
type PoolSelfCheck struct {
	Sampled     int      // 采样张数
	Parsed      int      // 解析成功张数
	Valid       int      // 同时通过 Card.Validate() 的张数
	SuccessRate float64  // Parsed / Sampled
	ValidRate   float64  // Valid / Sampled
	Done        bool     // 是否已执行
	FirstErrors []string // 前 5 条失败原因(定位用)
}

type lruEntry struct {
	card Card
	err  error
}

// NewLoader 构造加载器(阶段 0:不读盘)。
func NewLoader(root string) *Loader {
	return &Loader{
		root: strings.TrimSpace(root),
		lru:  make(map[string]*lruEntry),
	}
}

// Root 返回根路径。
func (l *Loader) Root() string { return l.root }

// buildIndex 阶段 1:walk 根目录收集 *.md 相对路径(排序后缓存)。
func (l *Loader) buildIndex() {
	l.indexOnce.Do(func() {
		if l.root == "" {
			l.available = false
			return
		}
		var paths []string
		rootAbs, err := filepath.Abs(l.root)
		if err != nil {
			l.available = false
			return
		}
		err = filepath.WalkDir(l.root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // 单目录读失败跳过,不影响整体
			}
			if d.IsDir() {
				// 路径安全:跳过隐藏目录(_框架 等下划线前缀目录为框架文档,非卡池)。
				name := d.Name()
				if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
					if path != l.root {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return nil
			}
			// 拒绝符号链接逃逸与根前缀外路径(加载器文档 §3.1)。
			if !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
				return nil
			}
			rel, err := filepath.Rel(l.root, path)
			if err != nil {
				return nil
			}
			paths = append(paths, rel)
			return nil
		})
		sort.Strings(paths) // 稳定顺序 → 同 seed 同抽卡
		l.indexPath = paths
		l.available = err == nil && len(paths) > 0
		if !l.available {
			logger.L().Warn("wealth profession docs pool unavailable, will fall back to curated",
				zap.String("root", l.root),
				zap.Int("entries", len(paths)))
		}
	})
}

// PoolInfo 返回池状态(HTTP /api/games/wealth/professions 用)。
// total: -1 = 索引未建(available 按根目录存在性判断);≥0 = 索引条目数。
// indexed: 已解析进 LRU 的张数;parseFail: 累计跳过的解析失败卡数。
func (l *Loader) PoolInfo() (available bool, total, indexed, parseFail int) {
	l.mu.RLock()
	built, avail := l.indexBuilt, l.available
	indexed, parseFail = len(l.lru), l.parseFail
	total = -1
	if built {
		total = len(l.indexPath)
	}
	l.mu.RUnlock()
	if built {
		return avail, total, indexed, parseFail
	}
	// 索引未建:按根目录存在性判断(不触发 walk,HTTP 路径保持轻量)。
	if l.root != "" {
		if st, err := os.Stat(l.root); err == nil && st.IsDir() {
			return true, total, indexed, parseFail
		}
	}
	return false, total, indexed, parseFail
}

// ForceIndex 显式触发阶段 1(HTTP professions / 首次抽卡共用)。
// 索引完成后**同步**跑一次 200 张采样自检(200 张 + parseFile ≈30–60ms,远
// 小于 buildIndex walk 75k 卡的 ≈1s;同步避免 t.TempDir() 测试清理与 goroutine
// 争用导致「目录已删」误报,失败判定走 selfCheckMinSuccessRate,日志留前 5
// 条原因于 PoolSelfCheck.FirstErrors —— 2026-09-16 §文档池解析修复)。
func (l *Loader) ForceIndex() {
	l.buildIndex()
	l.mu.Lock()
	l.indexBuilt = true
	l.mu.Unlock()
	l.runSelfCheckOnce()
}

// runSelfCheckOnce 采样自检(sync.Once:整个进程每 Loader 只跑一次)。
func (l *Loader) runSelfCheckOnce() {
	l.selfCheckOnce.Do(func() {
		res := l.SelfCheckPool(selfCheckSample)
		if res.SuccessRate < selfCheckMinSuccessRate {
			logger.L().Error("wealth profession docs pool parse rate below threshold — 文档池卡形状可能已漂移,Draw 将大量回退 curated",
				zap.String("root", l.root),
				zap.Int("sampled", res.Sampled),
				zap.Int("parsed", res.Parsed),
				zap.Float64("success_rate", res.SuccessRate),
				zap.Float64("valid_rate", res.ValidRate),
				zap.Strings("first_errors", res.FirstErrors))
			return
		}
		logger.L().Info("wealth profession docs pool self-check ok",
			zap.String("root", l.root),
			zap.Int("sampled", res.Sampled),
			zap.Float64("success_rate", res.SuccessRate),
			zap.Float64("valid_rate", res.ValidRate))
	})
}

// SelfCheckPool 同步采样自检(供 main.go 启动期与集成测试调用)。
//
// 前置约定(死锁防护):调用方**必须**已 ForceIndex()。本方法内部**不再**
// 调 ForceIndex —— 否则会经 runSelfCheckOnce 递归进入 sync.Once.Do 的内部
// 互斥锁 → 自死锁(曾导致所有调用 ForceIndex 的测试/启动永久挂起)。
//
// 从索引里均匀取 sampleN 张(确定性:按索引序等距取样,不吃 rng),
// 逐张 parse + Card.Validate();结果写入 l.selfCheck 并返回。
// 自检直接走 parseFile(不经 LRU),避免把 256 条缓存被采样卡挤满。
func (l *Loader) SelfCheckPool(sampleN int) PoolSelfCheck {
	l.mu.RLock()
	total := len(l.indexPath)
	sample := make([]string, 0, sampleN)
	if total > 0 && sampleN > 0 {
		step := total / sampleN
		if step < 1 {
			step = 1
		}
		for i := 0; i < total && len(sample) < sampleN; i += step {
			sample = append(sample, l.indexPath[i])
		}
	}
	l.mu.RUnlock()

	res := PoolSelfCheck{Sampled: len(sample)}
	for _, rel := range sample {
		card, err := l.parseFile(rel)
		if err != nil {
			if len(res.FirstErrors) < 5 {
				res.FirstErrors = append(res.FirstErrors, rel+": "+err.Error())
			}
			continue
		}
		res.Parsed++
		if card.Validate() == nil {
			res.Valid++
		} else if len(res.FirstErrors) < 5 {
			res.FirstErrors = append(res.FirstErrors, rel+": validate failed")
		}
	}
	if res.Sampled > 0 {
		res.SuccessRate = float64(res.Parsed) / float64(res.Sampled)
		res.ValidRate = float64(res.Valid) / float64(res.Sampled)
	}
	res.Done = true
	l.mu.Lock()
	l.selfCheck = res
	l.mu.Unlock()
	return res
}

// SelfCheckResult 返回最近一次采样自检结果(未跑过则 Done=false)。
func (l *Loader) SelfCheckResult() PoolSelfCheck {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.selfCheck
}

// Draw 从文档池均匀抽 n 张不重复卡。
//
// 2026-09-16 §文档池解析修复:旧实现只试前 n 张候选,任一卡解析失败即掉进
// curated 回退 → 文档池卡与精选卡混发(且 curated 只有 10 张,n=12 时必然重复)。
// 现改为「失败即补抽」:候选上限 drawRetryFactor×n(不超过池大小),只有池真的
// 供不出 n 张时才回退精选补足(加载器文档 §3.1 失败语义)。
// rng 为注入的随机源(同 seed 同索引 → 同卡集,确定性单测)。
func (l *Loader) Draw(n int, rng *rand.Rand) []Card {
	if n <= 0 {
		return nil
	}
	l.ForceIndex()

	l.mu.RLock()
	pool := append([]string(nil), l.indexPath...)
	l.mu.RUnlock()

	out := make([]Card, 0, n)
	if len(pool) > 0 {
		// 候选上限:min(池大小, drawRetryFactor×n) —— 前缀 Fisher-Yates 部分洗牌,
		// 逐个解析,失败(数据缺陷卡/形状漂移)就继续吃下一个候选。
		candidates := n * drawRetryFactor
		if candidates > len(pool) {
			candidates = len(pool)
		}
		for i := 0; i < candidates; i++ {
			j := i + rng.Intn(len(pool)-i)
			pool[i], pool[j] = pool[j], pool[i]
		}
		for _, rel := range pool[:candidates] {
			if len(out) >= n {
				break
			}
			card, err := l.loadCard(rel)
			if err != nil {
				continue
			}
			out = append(out, card)
		}
		if len(out) < n {
			logger.L().Warn("wealth profession docs pool cannot supply requested card count, falling back to curated",
				zap.String("root", l.root),
				zap.Int("requested", n),
				zap.Int("from_docs", len(out)),
				zap.Int("pool_size", len(pool)))
		}
	}
	// 回退精选补足(仅当文档池供不出 n 张:根目录缺失 / 池太小 / 候选全失败)。
	if len(out) < n {
		curated := CuratedCards()
		perm := rng.Perm(len(curated))
		k := 0
		for len(out) < n && k < len(perm) {
			out = append(out, curated[perm[k]])
			k++
		}
		if len(out) < n {
			// 精选池也不足 n 张(理论上 curated ≥ MaxSeats=12,不该发生):
			// 允许重复取用,保证返回张数 = n,座位不会因零值卡变成空洞。
			for i := 0; len(out) < n && len(curated) > 0; i++ {
				out = append(out, curated[i%len(curated)])
			}
			logger.L().Error("wealth profession curated pool smaller than requested draw",
				zap.Int("requested", n), zap.Int("curated", len(curated)))
		}
	}
	return out
}

// loadCard 读单卡:LRU 命中免读盘;否则读文件 + 解析 frontmatter。
// 失败计数(parseFail)与 Warn 日志在此统一处理,Draw 的补抽循环不再重复计数。
func (l *Loader) loadCard(rel string) (Card, error) {
	l.mu.RLock()
	if e, ok := l.lru[rel]; ok {
		l.mu.RUnlock()
		l.touchLRU(rel)
		return e.card, e.err
	}
	l.mu.RUnlock()

	card, err := l.parseFile(rel)

	l.mu.Lock()
	l.lru[rel] = &lruEntry{card: card, err: err}
	l.lruOrder = append(l.lruOrder, rel)
	for len(l.lruOrder) > lruCapacity {
		oldest := l.lruOrder[0]
		l.lruOrder = l.lruOrder[1:]
		delete(l.lru, oldest)
	}
	if err != nil {
		l.parseFail++
	}
	l.mu.Unlock()
	if err != nil {
		logger.L().Warn("wealth profession card parse failed, skipped",
			zap.String("path", rel), zap.Error(err))
	}
	return card, err
}

func (l *Loader) touchLRU(rel string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, r := range l.lruOrder {
		if r == rel {
			l.lruOrder = append(append(l.lruOrder[:i:i], l.lruOrder[i+1:]...), rel)
			return
		}
	}
}

// parseFile 读文件并解析 frontmatter(首个 --- 块,YAML)。
func (l *Loader) parseFile(rel string) (Card, error) {
	full := filepath.Join(l.root, rel)
	data, err := os.ReadFile(full)
	if err != nil {
		return Card{}, fmt.Errorf("read %s: %w", rel, err)
	}
	fm := extractFrontmatter(string(data))
	if fm == nil {
		return Card{}, fmt.Errorf("no frontmatter in %s", rel)
	}
	raw, err := parseDocCard(fm)
	if err != nil {
		return Card{}, fmt.Errorf("yaml %s: %w", rel, err)
	}
	card, err := l.mapCard(raw, rel)
	if err != nil {
		return Card{}, fmt.Errorf("map %s: %w", rel, err)
	}
	return card, nil
}
