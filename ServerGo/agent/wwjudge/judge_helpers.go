// Package wwjudge — judge_helpers.go: 法官 Agent 局部 helper。
//
// 2026-08-06 §Agent 重构 Step 4 抽出。原 itoa / resolveModelName 在
// agent/memory.go / agent/run_helpers.go,搬到 wwplayer 后 wwjudge
// 不再可见。这两个函数是局部 helper,在 wwjudge 包独立保留一份
// (与 wwplayer 内部不共享,避免 wwjudge 反向 import wwplayer)。
package wwjudge

// itoa 是 strconv.Itoa 的轻量内联版,避免在热路径 import strconv。
// 与 wwplayer/memory.go、agentcore/chat_history.go 各自的 itoa 同形。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	out := string(buf[pos:])
	if neg {
		return "-" + out
	}
	return out
}

// resolveModelName 兜底空 modelKey,与 wwplayer/run_helpers.go 内同名函数同形。
// 独立保留避免跨包 import。
func resolveModelName(modelKey string) string {
	if modelKey == "" {
		return "MeiTuan-model"
	}
	return modelKey
}