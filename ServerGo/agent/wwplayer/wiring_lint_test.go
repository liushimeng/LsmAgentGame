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
	toolsSrc, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}

	// 提取 SkipPhaseAction 函数体内的所有 return "xxx"。
	//
	// 2026-08-14 §20260814-01 U4:改为**跨包内全文件搜索**,不再硬编码
	// "run.go"。原写法在本批次把 SkipPhaseAction 搬到 run_config.go
	// (§4 行数拆分)时立刻 t.Fatal「lint 失效」—— 这次是好事(lint 咬到了
	// 真实变化),但把「函数住在哪个文件」当成契约是脆的:下一次拆分又会
	// 报假失效,而修的人很可能直接把文件名换掉、甚至删掉这条断言。
	// 按符号搜索让 lint 只关心「函数存在且动作有派发」这个真正的不变量。
	var body string
	for _, src := range packageSources(t) {
		if b := funcBody(src, "func SkipPhaseAction("); b != "" {
			body = b
			break
		}
	}
	if body == "" {
		t.Fatal("未在本包任何文件中定位到 SkipPhaseAction 函数体，lint 失效")
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

// ─────────────────────────────────────────────────────────────────────────────
// §20260813-02 U5 — wiring lint 扩展(机制化防 §130 第七次复发)。
//
// 三条新断言覆盖「新增缓存 / 新增缓冲 / 新增配置 / 新增工具」四类死代码形态:
//
//	U7-A  Build*Cached / New*Buffer 导出构造函数必须有非测试生产调用点
//	U7-B  Agent 死字段(compactConfig 等)必须有 setter 且 setter 有生产调用点;
//	      已知死代码(steeringQueue/toolHooks)白名单豁免,截止 2026-09-13
//	U7-C  BuildTools 挂载的每个工具名必须同时有派发路径(switch case 或 registry)
// ─────────────────────────────────────────────────────────────────────────────

var cachedCtorDeclRe = regexp.MustCompile(`(?m)^func (Build[A-Za-z0-9]*Cached|New[A-Za-z0-9]*Buffer)\(`)

// TestWiring_U7_A_CachedCtorsHaveProductionCaller 断言:
// agent/ 与 game/werewolf/ 中所有 Build*Cached / New*Buffer 导出构造函数,
// 必须有 ≥1 个非测试生产调用点 —— ToolsCache(BuildToolsCached)就是靠这条
// 防止「缓存写好了但 run.go 从不调用」再次潜伏一个版本。
func TestWiring_U7_A_CachedCtorsHaveProductionCaller(t *testing.T) {
	src := repoFiles(t, "agent", "game/werewolf")

	declared := map[string]string{}
	for path, body := range src {
		for _, m := range cachedCtorDeclRe.FindAllStringSubmatch(body, -1) {
			declared[m[1]] = path
		}
	}
	if len(declared) == 0 {
		t.Fatal("未扫描到任何 Build*Cached/New*Buffer 声明,lint 失效")
	}

	// 已知未接线死代码白名单 —— ⚠️ 只允许变短。
	// 每项必须注明处置方向与截止日期,到期必须接线或删除。
	knownDead := map[string]string{
		// §20260813-02 U5:§20260813-01 新增,与 GameContext.RecentSpeeches 语义
		// 重叠度高,意图待确认;P1 评估接线或删除,截止 2026-09-13。
		"NewShortMemoryBuffer": "§20260813-01 新增,意图待确认,P1 评估接线或删除(截止 2026-09-13)",
	}

	for name, declPath := range declared {
		callers := 0
		for _, body := range src {
			for _, idx := range regexp.MustCompile(regexp.QuoteMeta(name)+`\(`).FindAllStringIndex(body, -1) {
				line := lineAt(body, idx[0])
				if strings.HasPrefix(strings.TrimSpace(line), "func "+name+"(") {
					continue // 声明
				}
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue // 注释
				}
				callers++
			}
		}
		if callers == 0 {
			if reason, ok := knownDead[name]; ok {
				t.Logf("[已知死代码] %s (%s) — %s", name, declPath, reason)
				continue
			}
			t.Errorf("缓存/缓冲构造函数 %s(声明于 %s)没有任何生产调用点 —— "+
				"这是 §130「声明了却从不接线」的缓存形态(U2 前的 BuildToolsCached)。",
				name, declPath)
		}
	}
}

// TestWiring_U7_B_AgentFieldsHaveWiredSetter 断言:
// Agent 的「配置类」字段必须有 setter 且 setter 有生产调用点 —— compactConfig
// 在 U1 之前就是「字段存在、触发判断存在、唯独没有 setter」导致整条压缩路径
// 静默失效整整两个版本。
func TestWiring_U7_B_AgentFieldsHaveWiredSetter(t *testing.T) {
	src := repoFiles(t, "agent", "game/werewolf")

	// 字段 → 必须的 setter 名。
	//
	// 2026-08-14 §20260814-01 U2 — 加入 difficultySpeakScale。
	// 为什么必须**显式**列进来:该字段是值类型 float64,而
	// wiring_lint_field_test.go 的 isRefType(:166-179)只覆盖指针/map/chan/
	// func/interface/slice,对值类型返回 false —— 那条 AST 泛化 lint
	// **抓不到它**。这正是 SpeakLimiterScale 能在 §20260812-04 U4 与
	// §20260813-04 U3 两轮同 struct 修复中都活下来的原因:
	// 两条 lint 的交集之外存在盲区,只能靠这张显式表补。
	mustWire := map[string]string{
		"compactConfig":        "SetCompactConfig",
		"difficultySpeakScale": "SetDifficultySpeakScale",
	}
	// 已知死字段白名单(无 setter / setter 无调用点)—— ⚠️ 只允许变短。
	//
	// 2026-08-14 §20260814-01 U2 — 清空。原有 steeringQueue / toolHooks
	// 两项已由 §20260813-04 U1/U2 真实接线(room_agent.go 的
	// SetSteeringQueue / SetToolHooks),白名单条目自那次起就是**过期的**。
	// 过期白名单比没有白名单更危险:它会让「已修好的字段」看起来仍是已知缺陷,
	// 下一个审计者会跳过它们。故本次一并移除,而非留着到 2026-09-13 截止日。
	knownDeadFields := map[string]string{}

	agentSrc, err := os.ReadFile("agent.go")
	if err != nil {
		t.Fatalf("read agent.go: %v", err)
	}
	for field, setter := range mustWire {
		if !strings.Contains(string(agentSrc), field) {
			t.Fatalf("字段 %s 在 agent.go 中不存在,lint 失效", field)
		}
		callers := 0
		for path, body := range src {
			if strings.HasSuffix(path, "run_compact.go") || strings.HasSuffix(path, "agent.go") {
				// setter 声明自身所在文件不计。
			}
			for _, idx := range regexp.MustCompile(regexp.QuoteMeta(setter)+`\(`).FindAllStringIndex(body, -1) {
				line := lineAt(body, idx[0])
				if strings.HasPrefix(strings.TrimSpace(line), "func (a *Agent) "+setter+"(") {
					continue // 声明
				}
				if strings.HasPrefix(strings.TrimSpace(line), "//") {
					continue
				}
				_ = path
				callers++
			}
		}
		if callers == 0 {
			t.Errorf("字段 %s 的 setter %s 没有任何生产调用点 —— "+
				"这正是 U1 修复前 compactConfig 的死状(Enabled 恒 false,触发判断永不生效)。",
				field, setter)
		}
	}
	for field, reason := range knownDeadFields {
		t.Logf("[已知死字段] %s — %s", field, reason)
	}
}

// TestWiring_U7_C_MountedToolsHaveDispatch 断言:
// BuildTools 挂载(add("x"))与 registry 注册(Name: "x")的每个工具名,
// 必须同时存在派发路径 —— dispatchToolInner 的 case "x",或 registry
// Dispatcher(DispatchToolByName)。chat_recall(U3)即被此断言保护:
// 只挂工具不写派发 = LLM 拿到 "unknown tool"(§97 双路径对照的机制化)。
func TestWiring_U7_C_MountedToolsHaveDispatch(t *testing.T) {
	toolsSrc, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}
	dispatch := string(toolsSrc)

	// 收集 BuildTools 内 add("x") 挂载名。
	mounted := map[string]bool{}
	for _, m := range regexp.MustCompile(`add\("([a-z_]+)"`).FindAllStringSubmatch(dispatch, -1) {
		mounted[m[1]] = true
	}
	if len(mounted) == 0 {
		t.Fatal("未解析到任何 add() 工具名,lint 失效")
	}

	// registry 注册的工具(Name: "x" + Dispatcher)走 DispatchToolByName,
	// 无需 switch case;收集这些名字作为合法派发证据。
	registryDispatched := map[string]bool{}
	for _, file := range []string{"tools_prop.go", "tools_wolf.go", "commitment_tools.go"} {
		body, rerr := os.ReadFile(file)
		if rerr != nil {
			continue
		}
		for _, m := range regexp.MustCompile(`Name:\s+"([a-z_]+)"`).FindAllStringSubmatch(string(body), -1) {
			registryDispatched[m[1]] = true
		}
	}

	for name := range mounted {
		hasCase := strings.Contains(dispatch, `case "`+name+`"`)
		if !hasCase && !registryDispatched[name] {
			t.Errorf("工具 %q 在 BuildTools 挂载,但 dispatchToolInner 无 case 且未走 registry 派发 —— "+
				"LLM 调用会拿到 \"unknown tool\"(§97 双路径对照)", name)
		}
	}
	// chat_recall 是本断言的「金丝雀」:必须显式存在,防止整个断言静默失效。
	if !mounted["chat_recall"] {
		t.Fatal("chat_recall 未在 BuildTools 挂载清单中(U3 接线回归)")
	}
	if !strings.Contains(dispatch, `case "chat_recall"`) {
		t.Fatal("chat_recall 未在 dispatchToolInner 派发(U3 接线回归)")
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
