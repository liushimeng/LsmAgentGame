// Package vcplayer — rate.go: 每 Agent LLM 调用令牌桶(2026-09-26 §批次25,
// 25 文档 §3.3「LLM 调用节流」)。
//
// 契约:每个 City-Human(座位 Agent 或驱动轮次)平均每现实分钟 1–2 次 LLM。
// 实现为令牌桶:容量 2、每 minInterval(默认 30s,config
// virtual_city.agent_llm_min_interval_ms,clamp [5000,300000])补 1 枚、
// 初始满桶。月度决策 tool loop 每轮消耗 1 枚,桶空即中止本轮决策(走规则
// 回退意图 + submit_month 兜底,绝不无限重试);居民驱动层一次 RunMonth
// 逐条共享一个同参数桶(桶空条目直接丢弃,不重试)。
//
// 实现选型:sync.Mutex + 上次补充时间戳(调用点低频,无 time.Ticker 需求,
// 简单可靠)。now 可注入以便单测使用虚拟时钟。
package vcplayer

import (
	"sync"
	"time"
)

// defaultLLMMinInterval 令牌补充间隔缺省(与 config 归一值 30000ms 同源;
// NewAgent 构造期默认,manager 装配经 SetLLMRateLimit 覆盖为配置值)。
const defaultLLMMinInterval = 30 * time.Second

// tokenBucketCapacity 桶容量(契约:2)。
const tokenBucketCapacity = 2

// TokenBucket 每 Agent LLM 调用令牌桶。
type TokenBucket struct {
	capacity   int
	interval   time.Duration
	mu         sync.Mutex
	tokens     int
	lastRefill time.Time
	now        func() time.Time // 注入时钟(测试);nil → time.Now
}

// NewTokenBucket 构造满桶令牌桶(minInterval<=0 回落默认 30s)。
func NewTokenBucket(minInterval time.Duration) *TokenBucket {
	return NewTokenBucketWithClock(minInterval, nil)
}

// NewTokenBucketWithClock 构造注入时钟的令牌桶(单测用;now=nil 用真实时钟)。
func NewTokenBucketWithClock(minInterval time.Duration, now func() time.Time) *TokenBucket {
	if minInterval <= 0 {
		minInterval = defaultLLMMinInterval
	}
	if now == nil {
		now = time.Now
	}
	return &TokenBucket{
		capacity:   tokenBucketCapacity,
		interval:   minInterval,
		tokens:     tokenBucketCapacity,
		lastRefill: now(),
		now:        now,
	}
}

// Allow 取 1 枚令牌:按已流逝时间补充(每 interval 补 1 枚,补满容量为止),
// 桶空返回 false(调用方必须中止/丢弃本轮,绝不自旋重试)。
func (b *TokenBucket) Allow() bool {
	if b == nil {
		return true // 未装配限速(测试夹具):放行
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if b.interval > 0 {
		if refill := int(now.Sub(b.lastRefill) / b.interval); refill > 0 {
			b.tokens += refill
			if b.tokens > b.capacity {
				b.tokens = b.capacity
			}
			b.lastRefill = b.lastRefill.Add(time.Duration(refill) * b.interval)
		}
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// SetLLMRateLimit 覆盖 LLM 调用节流间隔(manager 装配 config
// virtual_city.agent_llm_min_interval_ms;nil-safe,interval<=0 回落默认)。
func (a *Agent) SetLLMRateLimit(interval time.Duration) {
	a.rate = NewTokenBucket(interval)
}

// rateAllowLLM 决策循环每轮 LLM 调用前取 1 枚令牌;桶空返回 false。
func (a *Agent) rateAllowLLM() bool {
	return a.rate.Allow()
}
