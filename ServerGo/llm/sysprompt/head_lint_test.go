package sysprompt

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件是 §14.3 三段式头的**接线 lint**(§130「声明了却从不接线」模式):
//
//	① 三段式头的文本只能存在于 llm/sysprompt(含本 lint 自身)——
//	   任何 Agent / provider 手工拼接 "x-anthropic-billing-header" 都是漂移源;
//	② anthropic 与 openai 两个 Provider 都必须真的调用 sysprompt.EnsureHead ——
//	   "加了头却没有注入点"是本特性的头号回归形态。
//
// 扫描范围是 ServerGo/ 的非测试 .go 文件。

const (
	billingLiteral = "x-anthropic-billing-header:"
	// 允许出现计费头字面量的文件(唯一事实来源)。
	billingLiteralOwner = "llm/sysprompt/texts.go"
)

// injectionPoints 必须调用 EnsureHead 的 Provider 文件(注入点)。
var injectionPoints = []string{
	"llm/anthropic/anthropic.go",
	"llm/openai/convert.go",
}

func serverGoRoot(t *testing.T) string {
	t.Helper()
	// 测试工作目录 = ServerGo/llm/sysprompt ⇒ ServerGo = ../..
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve ServerGo root: %v", err)
	}
	return root
}

func walkProductionGo(t *testing.T, root string, fn func(rel string, body string)) {
	t.Helper()
	seen := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "static", "pb", ".git", "tmpPlan", "logs":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		seen++
		fn(rel, string(b))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if seen < 50 {
		t.Fatalf("只扫到 %d 个非测试 .go 文件,lint 会假通过(工作目录=%s)", seen, root)
	}
}

// TestLint_BillingHeaderTextHasSingleOwner: 计费头文本只在 sysprompt 定义。
func TestLint_BillingHeaderTextHasSingleOwner(t *testing.T) {
	root := serverGoRoot(t)
	walkProductionGo(t, root, func(rel, body string) {
		if rel == billingLiteralOwner {
			return
		}
		// 常量定义(本 lint 用 BillingHeaderPrefix)之外的裸字面量即为违规。
		// 注释里提到这个头(例如 provider 的头部说明)不算违规,只看代码行。
		if containsInCode(body, billingLiteral) && !strings.Contains(body, "BillingHeaderPrefix") {
			t.Errorf(`%s 手工拼接了 %q —— 三段式头只能由 llm/sysprompt 产出、由 Provider 注入`,
				filepath.Join("ServerGo", rel), billingLiteral)
		}
	})
}

// containsInCode 判断 needle 是否出现在**代码行**中(纯注释行被忽略)。
func containsInCode(body, needle string) bool {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") {
			continue
		}
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

// TestLint_ProvidersInjectHead: 每个 Provider 都调用 sysprompt.EnsureHead。
func TestLint_ProvidersInjectHead(t *testing.T) {
	root := serverGoRoot(t)
	found := map[string]bool{}
	walkProductionGo(t, root, func(rel, body string) {
		if strings.Contains(body, "sysprompt.EnsureHead(") {
			found[rel] = true
		}
	})
	for _, want := range injectionPoints {
		if !found[want] {
			t.Errorf("ServerGo/%s 未调用 sysprompt.EnsureHead —— 走该协议的 Agent 会漏掉三段式头", want)
		}
	}
}
