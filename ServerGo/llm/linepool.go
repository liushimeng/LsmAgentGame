// Package llm — linepool.go: LLM 线路池（虚拟城市改造，2026-09-21）。
//
// 契约来源: lag_docs/财商流游戏/已实现/02-LLM线路池/
// 虚拟城市-LLM线路池与并发控制设计-v1.md §3。
//
// 语义: 一条「线路」= 一条可并行发起 LLM 调用的通道。t_lsm_game_llm_provider
// 每行的 concurrency_lines 表示该模型可同时占用的线路数;总线路数
// M = Σ enabled 行 lines,即全进程 Agent 调用大模型的全局并发上限。
//
// 实现: 预填充令牌通道(chan lineToken,容量 M,初始化时按平滑加权轮询序
// 预填)。Acquire 从通道取令牌(阻塞至有线路空闲或 ctx 超时);Release 把
// 令牌归还通道尾部。令牌内缓存已构造的 provider 实例 + 解密后的明文 key
// —— 二者均由 Registry 在写锁内产出(与 Registry.Get 的懒建路径复用同一
// 内部 helper newEndpointProviderLocked / sharedProviderForProtocol),
// 本文件只负责令牌调度,绝不自行构造 provider,避免两套构造漂移。
//
// Reload 语义: Registry.Reload() 重建行表后新建 LinePool 并原子替换指针;
// 在途租约持旧池令牌,Release 归还旧池(旧池令牌数不变、通道容量必有余位,
// 不阻塞不 panic),旧池随引用归零被 GC。
package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	types "LsmAgentGame/llm/types"
)

// ErrAllLinesBusy 在 Acquire 等待期间 ctx 被取消/超时(或池为空)时返回;
// 调用方应把它当作一次 LLM 失败走既有超时兜底(焦点座位 submit_month /
// 城市之声丢弃本条),而不是无限重试。
var ErrAllLinesBusy = errors.New("llm: all lines busy")

// concurrencyLinesLimits 是 concurrency_lines 的合法区间(闭区间)。
// DB 层只存值,API 层校验;registry / LinePool 加载时对越界值 clamp,
// 保证内存态不变量不被脏行破坏。
const (
	minConcurrencyLines = 1
	maxConcurrencyLines = 64
)

// clampConcurrencyLines 把线路数收敛到 [1,64]。<=0 视作缺省 1(旧行为),
// 超上限截断到 64,防止单行配置错误吃掉全局并发预算。
func clampConcurrencyLines(v int) int {
	if v < minConcurrencyLines {
		return minConcurrencyLines
	}
	if v > maxConcurrencyLines {
		return maxConcurrencyLines
	}
	return v
}

// LineSpec 是构建 LinePool 的一行输入,由 Registry 在写锁内从
// registeredProvider 产出(含已构造 provider 与解密 key)。导出仅因为
// LinePool 的构建入口需要跨文件可见;外部包不应自行构造。
type LineSpec struct {
	// ModelKey 是 registry 查找键(= t_lsm_game_llm_provider.model)。
	ModelKey string
	// Info 是 key-free 投影(含 concurrency_lines)。
	Info types.ModelInfo
	// Provider 是已构造的 provider(含 endpoint 覆盖;由 registry 复用
	// Get 懒建路径的同一 helper 产出)。
	Provider types.LLMProvider
	// APIKey 是解密后的明文 key,仅在租约生命周期内随 LineLease 传递,
	// 绝不落日志 / JSON。
	APIKey string
	// Lines 是该模型的线路数(clamp [1,64];NewLinePool 会再防御一次)。
	Lines int
}

// lineToken 是通道里流转的令牌:一份「模型 + provider + key」的不可变快照。
// Release 时原样归还。
type lineToken struct {
	modelKey string
	info     types.ModelInfo
	provider types.LLMProvider
	apiKey   string
	inFlight *atomic.Int64 // 指向该模型的在途计数器(与 LinePool.stat 计数器同源)
}

// LineLease 是一次 Acquire 拿到的线路租约。调用方用 Provider+APIKey 发起
// 本次 LLM 调用,完毕后必须 Release(幂等,重复调用安全)。
type LineLease struct {
	// ModelKey 是服务本次调用的模型 key。
	ModelKey string
	// Info 是 key-free 投影(含 concurrency_lines)。
	Info types.ModelInfo
	// Provider 是已构造的 provider(含 endpoint 覆盖)。
	Provider types.LLMProvider
	// APIKey 是解密后明文 key(仅本次调用内传递)。
	APIKey string

	pool     *LinePool
	token    lineToken
	released sync.Once
}

// Release 幂等归还线路令牌。首次调用把令牌放回通道尾部并递减在途计数;
// 后续调用是 no-op。归还永不为空等待 —— 令牌正被本租约持有,通道容量必有
// 空位(即使 Registry 已 Reload 换新池,旧池通道容量也不变)。
func (l *LineLease) Release() {
	if l == nil {
		return
	}
	l.released.Do(func() {
		l.token.inFlight.Add(-1)
		l.pool.tokens <- l.token
	})
}

// LineStat 是 Stats() 的一行:某模型的线路数与当前在途(已 Acquire 未
// Release)的调用数。供管理页 / 健康检查展示。
type LineStat struct {
	ModelKey string `json:"model_key"`
	Lines    int    `json:"lines"`
	InFlight int    `json:"in_flight"`
}

// lineStatEntry 是 Stats 的内部记账行。
type lineStatEntry struct {
	modelKey string
	lines    int
	inFlight *atomic.Int64
}

// LinePool 是 LLM 线路池:容量 M 的预填充令牌通道。构造后不可变(仅令牌在
// 通道内流转),并发安全;Registry.Reload 通过整体换池(原子替换指针)来
// 反映行表变化,而不是修改旧池。
type LinePool struct {
	tokens chan lineToken
	// total 是 Σ lines(M)。== cap(tokens)。0 表示无可线路(空池)。
	total int
	// stats 按 specs 顺序记账,与 NewLinePool 入参顺序一致(registry 侧
	// 按 model 排序,保证输出确定性)。
	stats []lineStatEntry
}

// emptyLinePool 是无可线路时的共享空池 —— Registry.LinePool() 在池尚未
// 构建 / 已被清空的极端路径返回它,消费方永远拿到非 nil *LinePool。
// Acquire 立即返回 ErrAllLinesBusy(等待一个不存在的线路毫无意义),
// Total() == 0。
var emptyLinePool = &LinePool{}

// NewLinePool 按行线路数构建预填充令牌通道。specs 中 Lines<=0 的行按 1
// 处理(clamp);全部为空时返回空池(total=0)。
//
// 预填顺序采用平滑加权轮询(interleave):A=3/B=2 产出 ABABA,而非 AAA BB
// 堆叠 —— 微观上交替放行,避免同一模型的线路被连续抢空。宏观权重只由
// 令牌数量(A×3、B×2)决定,与顺序无关。
func NewLinePool(specs []LineSpec) *LinePool {
	if len(specs) == 0 {
		return &LinePool{}
	}
	lines := make([]int, len(specs))
	total := 0
	for i, s := range specs {
		lines[i] = clampConcurrencyLines(s.Lines)
		total += lines[i]
	}
	if total == 0 {
		return &LinePool{}
	}
	p := &LinePool{
		tokens: make(chan lineToken, total),
		total:  total,
		stats:  make([]lineStatEntry, 0, len(specs)),
	}
	// 计数器先行建立(令牌与 stats 必须共享同一 *atomic.Int64)。
	counters := make([]*atomic.Int64, len(specs))
	for i := range specs {
		c := &atomic.Int64{}
		counters[i] = c
		p.stats = append(p.stats, lineStatEntry{
			modelKey: specs[i].ModelKey,
			lines:    lines[i],
			inFlight: c,
		})
	}
	// 平滑加权轮询预填:每一步选「(已填数+0.5)/lines」最小(即最欠配)的行。
	filled := make([]int, len(specs))
	for n := 0; n < total; n++ {
		best := -1
		for i := range specs {
			if filled[i] >= lines[i] {
				continue
			}
			if best == -1 ||
				(2*filled[i]+1)*lines[best] < (2*filled[best]+1)*lines[i] {
				best = i
			}
		}
		if best == -1 {
			// 不可达(total = Σ lines 保证每步都有欠配行);防御性兜底。
			break
		}
		p.tokens <- lineToken{
			modelKey: specs[best].ModelKey,
			info:     specs[best].Info,
			provider: specs[best].Provider,
			apiKey:   specs[best].APIKey,
			inFlight: counters[best],
		}
		filled[best]++
	}
	return p
}

// Total 返回总线路数 M(Σ enabled 行 lines)。无可用行返回 0。
func (p *LinePool) Total() int {
	if p == nil {
		return 0
	}
	return p.total
}

// Acquire 取一条线路。阻塞语义:
//   - 有空闲令牌 → 立即返回租约(令牌内含已构造 provider + 解密 key);
//   - 全忙 → 阻塞至任一 Release 归还令牌,或 ctx 被取消/超时;
//   - ctx 结束 / 池为空 → 返回 ErrAllLinesBusy(调用方走规则兜底)。
//
// select 双 case 就绪时 Go 随机选择,因此「ctx 已超时但恰逢令牌归还」时
// 可能返回成功 —— 这是良性竞争,两种结果对调用方都合法。
func (p *LinePool) Acquire(ctx context.Context) (*LineLease, error) {
	if p == nil || p.tokens == nil || p.total == 0 {
		// 空池:没有任何可等待的线路,立即按「全忙」处理,避免无 deadline
		// 的调用方永久挂死。
		return nil, ErrAllLinesBusy
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ErrAllLinesBusy
	case t := <-p.tokens:
		t.inFlight.Add(1)
		return &LineLease{
			ModelKey: t.modelKey,
			Info:     t.info,
			Provider: t.provider,
			APIKey:   t.apiKey,
			pool:     p,
			token:    t,
		}, nil
	}
}

// Stats 返回各模型的线路数与在途调用数(atomic 快照,构建顺序)。
// 供管理页 / 健康检查展示;空池返回 nil。
func (p *LinePool) Stats() []LineStat {
	if p == nil || len(p.stats) == 0 {
		return nil
	}
	out := make([]LineStat, 0, len(p.stats))
	for _, s := range p.stats {
		inFlight := s.inFlight.Load()
		if inFlight < 0 {
			inFlight = 0 // 防御:计数器不可能为负,钳到 0 展示
		}
		out = append(out, LineStat{
			ModelKey: s.modelKey,
			Lines:    s.lines,
			InFlight: int(inFlight),
		})
	}
	return out
}
