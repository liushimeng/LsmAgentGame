package wwplayer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"LsmAgentGame/llm/anthropic"
)

func resolveModelName(modelKey string) string {
	if modelKey == "" {
		return "MeiTuan-model"
	}
	return modelKey
}

func buildMetadataUserID(a *Agent) string {
	account := fmt.Sprintf("bot:%s:%d", a.RoomID, a.Seat)
	if a.UserID != "" {
		// Truncated hash keeps the blob short while staying stable per bot.
		account = a.UserID
	}
	blob := fmt.Sprintf(`{"device_id":%q,"account_uuid":%q,"session_id":%q}`,
		fmt.Sprintf("bot:room-%s:seat-%d", a.RoomID, a.Seat),
		account,
		fmt.Sprintf("%s:%d", a.RoomID, a.Seat),
	)
	if len(blob) > 256 {
		// Defensive cap — should never trigger in practice.
		return blob[:256]
	}
	return blob
}

func containsSeat(alive []int, seat int) bool {
	for _, s := range alive {
		if s == seat {
			return true
		}
	}
	return false
}

func phaseAllowsPublicSpeech(phase string) bool {
	switch phase {
	case "PhasePreWolves", "pre_wolves",
		"PhaseSpeak", "speak",
		"PhaseVote", "vote",
		"PhaseSheriff", "sheriff",
		"PhaseHunterShoot", "hunter_shoot",
		"PhaseIdiotReveal", "idiot_reveal",
		"PhaseDeathLyric", "death_lyric",
		"PhaseRestartVote", "restart_vote":
		return true
	}
	return false
}

func isAnthropic429(err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*anthropic.Error); ok {
		return ae.HTTPStatus == 429
	}
	// network level: "rate limit" / "429" / "too many requests"
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests")
}

func isAnthropicTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timeout") {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return false
}

func isNetworkOrTimeoutTransient(err error) bool {
	if err == nil {
		return false
	}
	if isAnthropicTimeout(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	transient := []string{
		"connection refused",
		"connection reset",
		"reset by peer",
		"broken pipe",
		"no such host",
		"tls handshake timeout",
		"use of closed network connection",
		"context canceled",
	}
	for _, s := range transient {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func isModel400CircuitErr(err error) bool {
	if err == nil {
		return false
	}
	var ae *anthropic.Error
	if errors.As(err, &ae) && ae.Source == "model_400_circuit" {
		return true
	}
	return strings.Contains(err.Error(), "model_400_circuit")
}

// isModel429CircuitErr §20260810-15: 检测 429 限流熔断短路信号。
// 区分 isAnthropic429 —— 后者检测任意 429,前者只检测「熔断打开」信号。
func isModel429CircuitErr(err error) bool {
	if err == nil {
		return false
	}
	var ae *anthropic.Error
	if errors.As(err, &ae) && ae.Source == "model_429_circuit" {
		return true
	}
	return strings.Contains(err.Error(), "model_429_circuit")
}

// isEndpointBreakerErr 检测「端点熔断器打开」的短路错误。
//
// 2026-08-12 §20260812-04 U5 (P0-3) 新增。
//
// run_llm.go:63 构造该错误时 Source="breaker"、Message 含
// "endpoint breaker open (all endpoints)"。它与 model_400 / model_429 熔断
// 同属「上游暂时不可用、等冷却即可恢复」语义,但此前**没有任何 transient 特判**,
// 且其文案与 isNetworkOrTimeoutTransient 的子串表无一匹配 ——
// 于是成为唯一会累计 consecutiveFailures 的熔断信号,一次上游抖动即可让
// 全房 13 个 bot 批量 quarantine。
//
// 与同族 isModel4xxCircuitErr 保持一致:优先按结构化 Source 判定,
// 字符串匹配仅作为跨包装边界的兜底。
func isEndpointBreakerErr(err error) bool {
	if err == nil {
		return false
	}
	var ae *anthropic.Error
	if errors.As(err, &ae) && ae.Source == "breaker" {
		return true
	}
	return strings.Contains(err.Error(), "endpoint breaker open")
}

// isContextExceededError 检测 LLM 返回的 Context 超限错误。
// 2026-08-10 §20260810-14 新增:当 LLM 返回 400 "exceed max message tokens" 或
// 类似的 Context 超限错误时,调用激进压缩(50% 预算)确保快速回落到安全范围。
// 典型场景:DouBao 等小窗口模型累积大量历史后触发 400,普通 PruneByBytes
// 可能因预算设置过松而无法在单次调用中回落到安全范围。
//
// 典型错误信息:
//   - "Total tokens of image and text exceed max message tokens"
//   - "exceed max message tokens"
//   - "maximum context length exceeded"
func isContextExceededError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// DouBao / Anthropic 代理的典型错误
	if strings.Contains(msg, "exceed max message tokens") {
		return true
	}
	if strings.Contains(msg, "maximum context length exceeded") {
		return true
	}
	if strings.Contains(msg, "context length") && strings.Contains(msg, "exceed") {
		return true
	}
	// Anthropic HTTP 400 错误中包含 token 相关关键词
	if strings.Contains(msg, "400") && strings.Contains(msg, "token") && strings.Contains(msg, "exceed") {
		return true
	}
	return false
}
