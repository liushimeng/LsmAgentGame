package wwplayer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// §20260813-04 U5 —— 「私有字段声明了却从不接线」的通用机制化防线。
//
// # 为什么需要这条断言（而 §20260812-04 U6 的 6 条不够）
//
// §130「声明了却从不接线」已复发 **七次**：
//
//	1  §130           AgentJudge.Provider 字段声明但从不注入
//	2  §132           WolfTeammateHint 定义了却从未被调用
//	3  §134           RoleGuard 在卡池但引擎/工具/UI 三层缺失
//	4  §135           HunterPendingFrom=="wolf" 分支无生产置位点
//	5  §20260811-08   PerSeatPOV 7 字段硬编码为空 + 结算奖励只接 1/4 路径
//	6  §20260812-04   SystemBlock.CacheControl 从未赋值 + MemoryInjectRunes 4 赋值 0 读取
//	7  §20260813-04   steeringQueue / toolHooks 完整实现但零 setter（本次）
//
// §20260812-04 U6 写了 wiring_lint_test.go 的 6 条断言，但它们都是
// **针对当时具体缺陷的专项断言**（块函数有调用点 / SkipPhaseAction 有 case /
// 夜间私有字段有读取点）。本次三处缺陷 **一条都没咬到** ——
// 因为 lint 断言的是「实例」而不是「模式」。
//
// 本次三处缺陷的共同形状：
//
//	Agent struct 声明了引用类型私有字段 → 有读取点（或连读取点都没有）
//	                                    → 但没有任何生产 setter / 构造赋值
//
// # 对标 Hermes
//
// Hermes 用 ContextEngine 的 @abstractmethod 在 **语言层面** 杜绝此类缺陷：
// 不实现抽象方法就无法实例化。Go 没有 abstract，interface 不约束字段，
// 编译器的「未使用变量」检查也不覆盖 struct 字段。
// **语言不保证的，必须靠 CI 保证。**
//
// # 判据
//
// Agent struct 的每个引用类型私有字段（指针 / 接口 / map / slice / chan / func）
// 必须满足以下之一，否则判为死字段：
//
//	① 存在 func (a *Agent) SetXxx(...) 形式的 setter
//	② 在构造函数（NewWithRoom / New / newAgent…）内被赋值
//	③ 在 exemptAgentFields 白名单内（每条必须附非空理由）
//
// 值类型字段（int / string / bool / time.Duration / sync.Mutex …）不在扫描范围：
// 它们零值可用，且大多是 §130 不适用的配置项。引用类型字段的零值 nil
// 才会导致「整条代码路径静默失效」—— 正是本教训的核心。

// exemptAgentFields 是不需要 setter 的 Agent 私有引用字段白名单。
// **每条必须附非空理由** —— 空理由等于没审查。
var exemptAgentFields = map[string]string{
	// sync 原语：零值可用，不存在「未接线」概念
	"mu": "sync.Mutex 零值可用",

	// 构造函数内 make/new，且生命周期与 Agent 绑定，无需外部替换
	"events":   "构造函数内 make(chan AgentEvent)，Agent 生命周期私有",
	"memory":   "构造函数内 NewMemoryWithWolfHint，Agent 生命周期私有",
	"stopOnce": "sync.Once 零值可用",

	// 由 Run(ctx, runner, rp) 参数注入而非 setter —— 属于「构造式注入」变体
	"runner": "Run() 参数注入，非 setter 模式",
	"rp":     "Run() 参数注入的 RolePhase 回调，非 setter 模式",

	// 2026-08-13 §20260813-05 U5 — Provider Cache 字节稳定。
	// systemPromptBytes 在 NewWithRoom 构造期一次性写入 (BuildSystemPromptBytes 冻结
	// 字节快照),后续不再修改。invariant I11 在发请求前只读比对。nil = 旧行为不变。
	"systemPromptBytes": "U5 字节稳定:构造期一次性写入,运行时仅 invariant 读取比对",
}

// TestWiringLintField_AgentPrivateRefFieldsHaveSetter 断言 Agent struct 的每个
// 引用类型私有字段都有生产 setter、构造赋值，或白名单豁免。
//
// **这条 lint 在编写时（U1/U2 修复前）会立刻咬到 steeringQueue 与 toolHooks** ——
// 那正是它有效的证明（§20260812-04 教训 3：能咬到作者本人的 lint 才是有效的 lint）。
func TestWiringLintField_AgentPrivateRefFieldsHaveSetter(t *testing.T) {
	fset := token.NewFileSet()
	agentPath := filepath.Join("agent.go")
	f, err := parser.ParseFile(fset, agentPath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", agentPath, err)
	}

	fields := agentPrivateRefFields(t, f)
	if len(fields) == 0 {
		t.Fatal("未从 Agent struct 提取到任何引用类型私有字段，lint 会假通过")
	}

	// 收集本包所有非测试源码，用于搜索 setter / 构造赋值。
	srcs := packageSources(t)

	var dead []string
	for _, name := range fields {
		if reason, ok := exemptAgentFields[name]; ok {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("字段 %q 在白名单内但理由为空 —— 空理由等于没审查", name)
			}
			continue
		}
		if hasProductionWiring(srcs, name) {
			continue
		}
		dead = append(dead, name)
	}

	if len(dead) > 0 {
		sort.Strings(dead)
		t.Errorf(`§130 第 N 次复发 —— 以下 Agent 私有引用字段没有任何生产 setter 或构造赋值，
恒为 nil，其所有读取分支静默失效：

    %s

修复方式（三选一）：
  ① 补 func (a *Agent) Set<Field>(...) 并在 room_agent.go StartAgentsLocked 调用
  ② 在构造函数内赋值
  ③ 若确实不需要，删除该字段及其实现文件，或加入 exemptAgentFields 并写明理由

不要仅仅补 setter 而不补生产调用点 —— 那只是把死字段变成死 setter。`,
			strings.Join(dead, "\n    "))
	}
}

// agentPrivateRefFields 从 AST 提取 Agent struct 的引用类型私有字段名。
func agentPrivateRefFields(t *testing.T, f *ast.File) []string {
	t.Helper()
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name == nil || ts.Name.Name != "Agent" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || st.Fields == nil {
			return true
		}
		for _, fld := range st.Fields.List {
			if !isRefType(fld.Type) {
				continue
			}
			for _, nm := range fld.Names {
				if nm == nil || nm.Name == "" {
					continue
				}
				// 只看私有字段：首字母小写。导出字段由调用方直接赋值，
				// 不适用 setter 判据。
				if r := rune(nm.Name[0]); r >= 'a' && r <= 'z' {
					out = append(out, nm.Name)
				}
			}
		}
		return false
	})
	return out
}

// isRefType 判断是否为引用类型（零值 nil，未接线即静默失效）。
// 值类型（int/string/bool/struct）零值可用，不在本 lint 范围。
func isRefType(e ast.Expr) bool {
	switch tv := e.(type) {
	case *ast.StarExpr, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType:
		return true
	case *ast.ArrayType:
		// slice（无固定长度）是引用类型；定长数组是值类型
		return tv.Len == nil
	case *ast.SelectorExpr:
		// 形如 sync.RWMutex / time.Time —— 值类型，跳过。
		// 真正的引用型跨包类型（如 *pkg.T）会走 StarExpr 分支。
		return false
	}
	return false
}

// packageSources 读取本包（wwplayer）所有非测试 .go 源码。
func packageSources(t *testing.T) map[string]string {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob 失败: %v", err)
	}
	out := map[string]string{}
	for _, p := range matches {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		out[p] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("未读到本包任何非测试源码，lint 会假通过")
	}
	return out
}

// hasProductionWiring 判断字段是否有生产 setter 或构造赋值。
func hasProductionWiring(srcs map[string]string, field string) bool {
	exported := strings.ToUpper(field[:1]) + field[1:]
	// ① setter：func (a *Agent) SetXxx / WithXxx
	setterPats := []string{
		"func (a *Agent) Set" + exported + "(",
		"func (a *Agent) With" + exported + "(",
	}
	// ② 构造赋值：a.field = / field: （struct literal）
	//    "a.field =" 覆盖构造函数内显式赋值；
	//    "field:" 覆盖 &Agent{field: ...} 字面量形式。
	assignPats := []string{
		"a." + field + " = ",
		"a." + field + " =\t",
		field + ": ",
	}

	for _, src := range srcs {
		for _, p := range setterPats {
			if strings.Contains(src, p) {
				return true
			}
		}
	}
	// 构造赋值只在构造函数附近才算 —— 简化处理：整包搜索，
	// 但排除「仅在 setter 内赋值」的情况（那种情况 setter 已被上面命中）。
	for name, src := range srcs {
		if !strings.Contains(name, "agent.go") {
			continue
		}
		for _, p := range assignPats {
			if strings.Contains(src, p) {
				return true
			}
		}
	}
	return false
}

