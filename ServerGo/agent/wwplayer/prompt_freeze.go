// Package wwplayer — prompt_freeze.go: System Prompt 字节稳定快照。
//
// 2026-08-13 §20260813-05 U5。借鉴 dsh agent.ts:465-470 的 request bytes 稳定路线:
// Provider cache 不下显式断点,而是让 system prompt + tools schema 在跨轮
// 间字节稳定,依赖 provider 自动 server-side KV cache 命中(Anthropic 5m-cache)。
//
// # 与 §20260814-02 U1 路线的关系
//
// §20260814-02 U1 (OpenCode 启发): 显式 4 断点 cache_policy
// 本 U5 (DSH 启发): 不下断点,依赖字节稳定
// 二者正交且目标一致:提升 cache hit rate。本次走 U5 路线,显式断点路线
// 留待 §20260814-02 U1 后续 commit。
//
// # 与 §14.1 的兼容性
//
// §14.1 CacheControl 字段保留 (向后兼容),但 BuildSystemPromptBytes 不再赋值;
// 注释 §14.1 文档后续会更新为「DSH 风格:不下断点 + 字节稳定」。
package wwplayer

import (
	"crypto/sha256"
	"encoding/json"
)

// BuildSystemPromptBytes 构造期一次性调用,返回冻结的 system prompt 字节快照。
//
// 字节稳定保证:
//   - 同一 selfPortrait / personality / personalityPresetKey / difficultyDirective
//     多次调用返回**完全相同**的 []byte (sha256 一致)。
//   - BuildSystemPrompt 内部依序拼接:身份 → 任务目标 → 规则 → persona → 自画像
//     → 难度指令;每次拼接都用 fmt.Sprintf + 常量字符串,无随机 / 无时间 / 无 seat
//     依赖 → 跨局字节稳定 → provider cache 自动命中。
//
// 返回值供 Agent.systemPromptBytes 字段保存,运行时 invariant I11 比对 req.System
// 字节与该快照,保证跨轮字节一致。
func BuildSystemPromptBytes(selfPortrait string, personality PersonalityVector, personalityPresetKey string, difficultyDirective string) []byte {
	blocks := BuildSystemPrompt(selfPortrait, personality, personalityPresetKey, difficultyDirective)
	// SystemBlock 没有自带 MarshalJSON,直接用 json.Marshal 序列化即可
	// (json 字段 tag 与 Anthropic wire 一致)。
	var total []byte
	for _, b := range blocks {
		j, _ := json.Marshal(b)
		total = append(total, j...)
	}
	return total
}

// HashSystemPromptBytes 返回 BuildSystemPromptBytes 的 sha256 hex,用于
// 跨构造期一致性断言(测试用)。
func HashSystemPromptBytes(selfPortrait string, personality PersonalityVector, personalityPresetKey string, difficultyDirective string) string {
	b := BuildSystemPromptBytes(selfPortrait, personality, personalityPresetKey, difficultyDirective)
	sum := sha256.Sum256(b)
	return hexEncode(sum[:])
}

// hexEncode 内部小工具,避免引入 encoding/hex 的 import 开销。
func hexEncode(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, x := range b {
		out[i*2] = hex[x>>4]
		out[i*2+1] = hex[x&0xf]
	}
	return string(out)
}