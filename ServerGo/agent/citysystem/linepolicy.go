// Package citysystem — linepolicy.go: LLM 线路策略(2026-11-03 §批次81)。
//
// 与批次79 citychrome 的线路策略完全同构(方案文档 §3.6):
//
//	""        = 沿用 cfg.ModelKey(旧行为,零回归)
//	"random"  = 每轮从 registry.ListEnabled() 按 concurrency_lines 加权随机
//	其它      = 固定 model key
//
// 并发安全:建房链路(SetLinePolicy)写 / Run(resolveLineKey)读。
package citysystem

import (
	"math/rand"
	"strings"

	llmtypes "LsmAgentGame/llm/types"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// LinePolicyRandom 是线路策略的「随机」取值(建房弹窗默认)。
const LinePolicyRandom = "random"

// SetLinePolicy 设置 LLM 线路策略(建房链路透传)。
// "" = 沿用 cfg.ModelKey;"random" = 每轮随机;其它 = 固定 model key。
func (a *Agent) SetLinePolicy(line string) {
	line = strings.TrimSpace(line)
	a.lineMu.Lock()
	a.linePolicy = line
	a.lineMu.Unlock()
	logger.L().Info("citysystem line policy updated", zap.String("line", line))
}

// LinePolicy 返回当前线路策略(测试 / 诊断)。
func (a *Agent) LinePolicy() string {
	a.lineMu.RLock()
	defer a.lineMu.RUnlock()
	return a.linePolicy
}

// resolveLineKey 解析本轮使用的 model key:
//   - 固定策略(非空且非 random)→ 原样返回;
//   - 缺省 "" → cfg.ModelKey(旧行为,零回归);
//   - random → 从 registry.ListEnabled() 按 concurrency_lines 加权随机;
//     池空时回退 cfg.ModelKey(不因线路池空缺而整条通道失败)。
func (a *Agent) resolveLineKey() string {
	line := a.LinePolicy()
	if line != "" && !strings.EqualFold(line, LinePolicyRandom) {
		return line
	}
	if line == "" {
		return a.ModelKey
	}
	if a.registry != nil {
		if key := randomLineKey(a.registry.ListEnabled()); key != "" {
			return key
		}
	}
	return a.ModelKey
}

// randomLineKey 按线路数加权随机取一个 model key(权重 = concurrency_lines,
// ≤0 视作 1;空列表返回 "")。与 citychrome 同口径。
func randomLineKey(list []llmtypes.ModelInfo) string {
	total := 0
	for _, m := range list {
		total += lineWeight(m)
	}
	if total <= 0 {
		return ""
	}
	n := rand.Intn(total)
	for _, m := range list {
		w := lineWeight(m)
		if n < w {
			return m.Model
		}
		n -= w
	}
	return ""
}

// lineWeight 返回单模型的线路权重(concurrency_lines,≤0 → 1)。
func lineWeight(m llmtypes.ModelInfo) int {
	if m.ConcurrencyLines <= 0 {
		return 1
	}
	return m.ConcurrencyLines
}
