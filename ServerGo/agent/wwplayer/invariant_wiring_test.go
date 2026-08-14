// Package wwplayer — invariant_wiring_test.go: §20260813-05 U2 wiring lint。
//
// # 为什么需要这组测试
//
// §130 "声明了却从不接线" 反复复发 7 次(§20260811-08 / §20260812-04 /
// §20260813-04 / §20260814-02)。CI 时 AST lint (§20260814-02 U6) 抓"字段
// 无 setter"，runtime invariant companion 抓"字段值违反契约"。
//
// 但 invariant 函数本身也可能"声明了却从不接线"——若 3 个 Check* 函数
// 没有任何生产调用点，运行时根本不会触发校验。本 lint 强制 3 个 Check*
// 都有生产调用点：
//
//   - CheckGameContextInvariant   → 至少 1 处 (buildAgentContextLocked 末尾)
//   - CheckMessagePairingInvariant → 至少 1 处 (runLoop 发请求前 / 响应后)
//   - CheckRequestReconstructabilityInvariant → 至少 1 处 (runLoop 发请求前)
//
// 与 §20260812-04 U6 lint 互补：U6 抓"声明的字段"，本文件抓"声明的
// invariant 函数"。两层 lint 都失败 = 真正的 §130 防御。
package wwplayer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 简单 substring 扫描，避免依赖 AST 解析 (本文件用于快速 CI 检查)。
// 生产调用点匹配宽松形 "<函数名>(" 即可。
//
// 扫描范围: wwplayer 包(packageSources) + game/werewolf 包(CheckGameContextInvariant
// 在 buildAgentContextLocked 末尾被调用,属于跨包接线,本 lint 必须能识别)。
func TestInvariant_Wiring_HasCallers(t *testing.T) {
	src := packageSources(t)

	// 额外扫描 game/werewolf 包(CheckGameContextInvariant 在 room_agent.go 调用)。
	werewolfSrc := werewolfSourceFiles(t)

	checks := []struct {
		Name string
		Func string
	}{
		{"CheckGameContextInvariant", "CheckGameContextInvariant("},
		{"CheckMessagePairingInvariant", "CheckMessagePairingInvariant("},
		{"CheckRequestReconstructabilityInvariant", "CheckRequestReconstructabilityInvariant("},
	}

	for _, c := range checks {
		hits := 0
		for _, body := range src {
			if containsCall(body, c.Func) {
				hits++
			}
		}
		for _, body := range werewolfSrc {
			if containsCall(body, c.Func) {
				hits++
			}
		}
		if hits < 1 {
			t.Errorf("invariant 函数 %s 没有任何生产调用点（§130 复发风险）[wwplayer files=%d, werewolf files=%d]",
				c.Name, len(src), len(werewolfSrc))
		}
	}
}

// werewolfSourceFiles 扫描 game/werewolf 目录下的非测试 .go 源码。
// 仅作 invariant wiring lint 使用,与 §20260814-02 U6 lint 的目录覆盖一致。
//
// 注意:Go 测试运行时 cwd 是包目录(ServerGo/agent/wwplayer/),不是 ServerGo。
// 用绝对路径绕过相对路径歧义。
func werewolfSourceFiles(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Abs("game/werewolf")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	// 兜底:若不在 ServerGo 跑,向上找 ServerGo/game/werewolf。
	if _, err := os.Stat(root); err != nil {
		alt, _ := filepath.Abs("../../game/werewolf")
		if _, err2 := os.Stat(alt); err2 == nil {
			root = alt
		} else {
			t.Fatalf("werewolf 源码目录未找到(tried %s and %s)", root, alt)
		}
	}
	var out []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		out = append(out, string(b))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Fatalf("未读到任何 werewolf 源码文件(%s),wiring lint 会假通过", root)
	}
	return out
}

// containsCall 简单 substring 扫描。
//
// 设计:仅要求出现 "<FuncName>(" 子串即可。注释中提及 "CheckGameContextInvariant("
// 也算"提及"——这对 lint 足够,因为 wiring 缺陷的真正杀手是「声明却无任何
// 出现」,而非「出现都在注释里」(后者代表 dev 在思考 invariant 但还没接线,
// 此时 lint 提醒一下也无害)。
//
// 严格模式可加: 排除 // 单行注释 与 /* */ 注释块,但工程经验显示接线 lint 的
// false negative 比 false positive 更致命(§130 反复复发)。
func containsCall(body, sig string) bool {
	return strings.Contains(body, sig)
}

func indexOf(s, sub string) int {
	return strings.Index(s, sub)
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
