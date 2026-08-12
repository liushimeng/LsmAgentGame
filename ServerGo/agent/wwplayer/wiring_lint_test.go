package wwplayer

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// §20260812-04 U6 —— 「声明了却从不接线」的机制化防线。
//
// # 为什么需要这组测试
//
// CLAUDE.md 已把「声明了却从不接线」记为 §130 / §134 / §135 / §20260811-08
// **四次教训**，并在多处注释里写下自检条款。但本次（§20260812-04）审计仍新发现
// 12 处同类缺陷，其中最严重的一处（预言家查验结果从未进 prompt）潜伏了整整一个
// 版本 —— 因为**注释里的自检条款不会被执行**。
//
// §20260811-08 教训 (2) 原文就说过「注释里的自检条款必须转化为测试断言」，
// 而那一条本身当时也没有转化。本文件就是把它真正转化出来。
//
// # 三条断言分别覆盖本次发现的三类漏接
//
//	L1  XxxBlock() 函数无生产调用点   → EmotionStyleBlock / OthersEmotionBlock 等 5 处
//	L2  SkipPhaseAction 动作无派发 case → demon_hunter_hunt_skip
//	L3  GameContext 字段无 agent 侧读取 → MySeerCheck / WolfTarget（P0-1 本体）
//
// 三条都只扫源码文本，不依赖运行时，成本极低。

// repoFiles 读取指定相对目录下的所有非测试 .go 源码。
func repoFiles(t *testing.T, relDirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range relDirs {
		root := filepath.Join("..", "..", rel)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // 目录缺失不致命：跳过
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			out[path] = string(b)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(out) == 0 {
		t.Fatalf("未读到任何源码文件（relDirs=%v），lint 会假通过", relDirs)
	}
	return out
}

var blockFuncDeclRe = regexp.MustCompile(`(?m)^func ([A-Z][A-Za-z0-9_]*Block)\(`)

// U6-L1: 每个导出的 XxxBlock() 函数必须至少有一处**生产**调用点。
//
// 新增一个 prompt 块却忘记在 BuildUserPrompt/BuildSystemPrompt 里拼接时，
// 这条会立即失败 —— 这正是 EmotionStyleBlock / OthersEmotionBlock /
// PhasePromptHint 等 5 处死代码当初该被拦下的地方。
func TestWiring_U6_L1_AllBlockFuncsHaveProductionCaller(t *testing.T) {
	src := repoFiles(t, "agent")

	// 收集声明。
	declared := map[string]string{} // name -> 声明所在文件
	for path, body := range src {
		for _, m := range blockFuncDeclRe.FindAllStringSubmatch(body, -1) {
			declared[m[1]] = path
		}
	}
	if len(declared) == 0 {
		t.Fatal("未扫描到任何 XxxBlock 函数声明，lint 失效")
	}

	// 已知未接线、且已在文档中记录为待清理的死代码。
	// ⚠️ 这个白名单只允许变短，不允许变长 —— 新增块函数请直接接线。
	knownDead := map[string]string{
		"EmotionStyleBlock":  "§20260812-04 P1-4：情绪风格块未接线，待清理",
		"OthersEmotionBlock": "§20260812-04 P1-4：他人情绪感知整体未接线，待清理",
		"WolfTeammateHint":   "§132：实际走 identityPromptWithWolfHint 另一条路径",
		// judge_summary_bridge.go:13 与 judge_summary.go:10 两处注释都声称
		// 「BuildSystemPrompt 注入上一局记忆段」，但生产调用点为 0 ——
		// 教科书级的 §130：注释描述了一条不存在的接线。法官的跨局记忆实际
		// 走 JudgeModelMemories 下发前端，不经 system prompt。
		"LastGameMemoryBlock": "§20260812-04 P1-4：注释声称已接线但生产调用点为 0，待清理",
	}

	for name, declPath := range declared {
		callers := 0
		for path, body := range src {
			// 调用点计数：排除声明自身那一行。
			for _, idx := range regexp.MustCompile(regexp.QuoteMeta(name)+`\(`).FindAllStringIndex(body, -1) {
				line := lineAt(body, idx[0])
				if strings.HasPrefix(strings.TrimSpace(line), "func "+name+"(") {
					continue // 声明
				}
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue // 注释
				}
				_ = path
				callers++
			}
		}
		if callers == 0 {
			if reason, ok := knownDead[name]; ok {
				t.Logf("[已知死代码] %s (%s) — %s", name, declPath, reason)
				continue
			}
			t.Errorf("块函数 %s（声明于 %s）没有任何生产调用点 —— "+
				"这就是 §130/§134/§135 反复复发的「声明了却从不接线」。"+
				"请在 BuildUserPrompt/BuildSystemPrompt 中接线，或删除该函数。",
				name, declPath)
		}
	}
}

// U6-L2: SkipPhaseAction 返回的每个动作名，都必须在 dispatchToolInner 有对应 case。
//
// 缺失时 auto-skip 三条路径全部拿到 "unknown tool"（demon_hunter_hunt_skip
// 就是这样漏了整整一个版本），只能靠 manager 侧兜底。
func TestWiring_U6_L2_SkipActionsHaveDispatchCase(t *testing.T) {
	runSrc, err := os.ReadFile("run.go")
	if err != nil {
		t.Fatalf("read run.go: %v", err)
	}
	toolsSrc, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}

	// 提取 SkipPhaseAction 函数体内的所有 return "xxx"。
	body := funcBody(string(runSrc), "func SkipPhaseAction(")
	if body == "" {
		t.Fatal("未定位到 SkipPhaseAction 函数体，lint 失效")
	}
	actions := map[string]bool{}
	for _, m := range regexp.MustCompile(`return "([a-z_]+)"`).FindAllStringSubmatch(body, -1) {
		actions[m[1]] = true
	}
	if len(actions) == 0 {
		t.Fatal("SkipPhaseAction 未解析出任何动作名，lint 失效")
	}

	dispatch := string(toolsSrc)
	for action := range actions {
		if !strings.Contains(dispatch, `case "`+action+`"`) {
			t.Errorf("SkipPhaseAction 会返回 %q，但 tools.go 的派发表没有对应 case —— "+
				"auto-skip 会拿到 \"unknown tool\"。（§97 三处同步清单的第四处）", action)
		}
	}
}

// U6-L3: GameContext 中标注为「给 LLM 看」的关键字段，必须在 agent/ 下有读取点。
//
// 只覆盖夜间私有信息这一组 —— 它们是 P0-1 的本体，也是最高价值、
// 最容易被「引擎填了就以为完事」的一类字段。全字段扫描误报率太高，
// 这里刻意收窄到一份显式清单，保证零误报、可长期维持。
func TestWiring_U6_L3_NightPrivateFieldsAreRead(t *testing.T) {
	src := repoFiles(t, "agent")

	mustRead := []string{
		"MySeerCheck",
		"MySeerCheckFaction",
		"MySeerCheckHistory",
		"WolfTarget",
		"GuardLastProtect",
		"WitchAntidoteUsed",
		"WitchPoisonUsed",
	}
	for _, field := range mustRead {
		read := false
		for path, body := range src {
			// 跳过类型定义文件本身（那里只有声明）。
			if strings.HasSuffix(path, "wwtypes/context.go") {
				continue
			}
			if strings.Contains(body, "."+field) {
				read = true
				break
			}
		}
		if !read {
			t.Errorf("GameContext.%s 在 agent/ 下没有任何读取点 —— "+
				"引擎填充了但 LLM 永远看不到（这正是 P0-1：AI 预言家不知道查验结果、"+
				"AI 女巫不知道谁被刀）。请在 prompt 中渲染，或从 GameContext 删除该字段。",
				field)
		}
	}
}

// lineAt 返回 body 中 offset 所在的整行。
func lineAt(body string, offset int) string {
	start := strings.LastIndexByte(body[:offset], '\n') + 1
	end := strings.IndexByte(body[offset:], '\n')
	if end < 0 {
		return body[start:]
	}
	return body[start : offset+end]
}

// funcBody 返回以 sig 开头的函数体（按大括号配平截取）。找不到返回 ""。
func funcBody(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	depth := 0
	for j := 0; j < len(rest); j++ {
		switch rest[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:j+1]
			}
		}
	}
	return ""
}
