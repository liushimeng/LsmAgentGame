// Package main 提供了 AgentWiringLint CLI 工具 (§20260814-02 / §20260814-02 U6)。
//
// AgentWiringLint 通过 Go AST 解析 ServerGo/agent/wwplayer/agent.go 中的 Agent struct,
// 对每个引用类型/结构体类型的非可忽略字段,扫描其在 ServerGo/ 仓库内是否至少有一个
// setter 或者构造赋值调用点。
//
// 目的:在 §130 第八次复发(任何"声明了却从不接线"模式下)给仓库加一个 CI 级别的硬守门。
//
// 设计动机:OpenCode 反思其 TS 缺少 Python ABC 那样的类型级约束,
//
//	本仓库也面临 Go interface 不约束 struct 私有字段的问题。
//	手工 grep 永远比新增字段的速度慢;AST 全字段扫描是根治办法。
//
// 用法:
//
//	go run ./cmd/wiringlint/        # 默认模式,人类可读
//	go run ./cmd/wiringlint/ -json   # 机器可读(JSON)
//	go run ./cmd/wiringlint/ -strict # 任一字段零 setter 即进程退出码=1
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FieldReport 单字段报告。
type FieldReport struct {
	Field       string   `json:"field"`
	Type        string   `json:"type"`
	SetterSites []string `json:"setter_sites,omitempty"`
	ConstructOK bool     `json:"construct_ok"`
	Note        string   `json:"note,omitempty"`
	Ignored     bool     `json:"ignored,omitempty"`
}

// Report 是 agent struct 的全字段扫描结果。
type Report struct {
	Target       string        `json:"target"`
	FieldsTotal  int           `json:"fields_total"`
	FieldsCheck  int           `json:"fields_check"`
	FieldsPassed int           `json:"fields_passed"`
	FieldsMissed int           `json:"fields_missed"`
	Fields       []FieldReport `json:"fields"`
}

var ignoreFieldPrefixes = []string{
	"_",
}

var ignoreFieldTypes = map[string]bool{
	"sync.Mutex":          true,
	"sync.RWMutex":        true,
	"sync.WaitGroup":      true,
	"sync.Once":           true,
	"time.Time":           true,
	"time.Duration":       true,
	"context.Context":     true,
	"context.CancelFunc":  true,
	"chan struct{}":       true,
	"map[string]string":   true,
	"map[string]any":      true,
	"map[int]int":         true,
	"map[int]uint64":      true,
	"map[int]bool":        true,
	"map[Seat]bool":       true,
	"map[Seat]*Agent":     true,
	"map[int]string":      true,
}

// isIgnoredField:字段名带"_"前缀或`//nolint:wiringlint`注释的字段被忽略。
func isIgnoredField(name string, f *ast.Field) bool {
	if strings.HasPrefix(name, "_") {
		return true
	}
	if f == nil {
		return false
	}
	if f.Comment != nil {
		for _, c := range f.Comment.List {
			if strings.Contains(c.Text, "nolint:wiringlint") {
				return true
			}
		}
	}
	if f.Doc != nil {
		for _, c := range f.Doc.List {
			if strings.Contains(c.Text, "nolint:wiringlint") {
				return true
			}
		}
	}
	return false
}

// extractFields 从 Agent struct 提取字段定义。
func extractFields(file *ast.File, structName string) map[string]*ast.Field {
	out := map[string]*ast.Field{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, sp := range gd.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok || ts.Name.Name != structName {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}
			for _, f := range st.Fields.List {
				if len(f.Names) == 0 {
					continue
				}
				name := f.Names[0].Name
				out[name] = f
			}
		}
	}
	return out
}

// typeNameOf 把 ast.Expr 渲染为可读类型名。
func typeNameOf(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeNameOf(t.X)
	case *ast.SelectorExpr:
		return typeNameOf(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + typeNameOf(t.Elt)
	case *ast.MapType:
		k := typeNameOf(t.Key)
		v := typeNameOf(t.Value)
		return "map[" + k + "]" + v
	case *ast.ChanType:
		return "chan " + typeNameOf(t.Value)
	case *ast.FuncType:
		return "func(...)"
	}
	return "<unknown>"
}

// scanSetterSites 扫描 root 全部 .go 文件,查找包含 ".FieldName" 引用的文件路径列表。
//
// 启发式简化:任何对 ".X" 的引用都视为"接线点"。
//
//	值调用 a.X = ... 、&a.X、a.X.field、a.X(idx) 都属于对字段的依赖。
//	甚至函数 a.X() 也算(因为依赖字段存在)。LLM setter 通常命名为 SetX。
func scanSetterSites(fieldName string, root string) []string {
	sites := []string{}
	target := "." + fieldName
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasPrefix(path, "cmd/wiringlint") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, "docs/") {
			return nil
		}
		if strings.HasPrefix(rel, "static/") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), target) {
			sites = append(sites, rel)
		}
		return nil
	})
	if len(sites) > 4 {
		return sites[:4]
	}
	return sites
}

// runLint 实施扫描并返回 Report。
func runLint(agentFile, structName, projectRoot string) (*Report, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, agentFile, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", agentFile, err)
	}
	fields := extractFields(f, structName)
	r := &Report{Target: structName}
	for _, name := range sortedKeys(fields) {
		fld := fields[name]
		typ := typeNameOf(fld.Type)
		pass := FieldReport{Field: name, Type: typ}

		if isIgnoredField(name, fld) {
			pass.Ignored = true
			pass.Note = "skipped by rule"
			r.Fields = append(r.Fields, pass)
			continue
		}

		if isPrimitiveType(typ) {
			pass.Ignored = true
			pass.Note = "primitive/bool/int/string"
			r.Fields = append(r.Fields, pass)
			continue
		}

		if ignoreFieldTypes[typ] {
			pass.Ignored = true
			pass.Note = "infra type"
			r.Fields = append(r.Fields, pass)
			continue
		}

		r.FieldsCheck++
		sites := scanSetterSites(name, projectRoot)
		pass.SetterSites = sites
		if len(sites) > 0 {
			pass.ConstructOK = true
			r.FieldsPassed++
		} else {
			r.FieldsMissed++
			pass.Note = "ZERO setter / construct sites in " + projectRoot + "/"
		}
		r.Fields = append(r.Fields, pass)
	}
	r.FieldsTotal = len(fields)
	return r, nil
}

func sortedKeys(m map[string]*ast.Field) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func isPrimitiveType(t string) bool {
	switch t {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64",
		"bool", "string", "byte", "rune":
		return true
	}
	return false
}

func main() {
	jsonOut := flag.Bool("json", false, "output as JSON")
	strict := flag.Bool("strict", false, "exit 1 if any field has zero setter sites")
	structName := flag.String("struct", "Agent", "struct name to lint")
	target := flag.String("file", "agent/wwplayer/agent.go", "agent struct file (relative to root)")
	root := flag.String("root", ".", "project root to scan")
	flag.Parse()

	absRoot, _ := filepath.Abs(*root)
	agentPath := *target
	if !filepath.IsAbs(agentPath) {
		agentPath = filepath.Join(absRoot, *target)
	}

	if _, err := os.Stat(agentPath); err != nil {
		fmt.Fprintf(os.Stderr, "wiringlint: target file %s not found (%v)\n", agentPath, err)
		os.Exit(2)
	}

	rep, err := runLint(agentPath, *structName, absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wiringlint: %v\n", err)
		os.Exit(2)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(rep)
	} else {
		fmt.Printf("== WiringLint report ==\n")
		fmt.Printf("target = %s\n", rep.Target)
		fmt.Printf("fields = %d, checked = %d, passed = %d, missed = %d\n\n",
			rep.FieldsTotal, rep.FieldsCheck, rep.FieldsPassed, rep.FieldsMissed)
		for _, f := range rep.Fields {
			if f.Ignored {
				continue
			}
			marker := "✓"
			if !f.ConstructOK {
				marker = "✗ MISSING"
			}
			fmt.Printf("  %s  %s : %s\n", marker, f.Field, f.Type)
			if len(f.SetterSites) > 0 {
				fmt.Printf("       used in:\n")
				for _, s := range f.SetterSites {
					fmt.Printf("         - %s\n", s)
				}
			}
			if f.Note != "" {
				fmt.Printf("       note: %s\n", f.Note)
			}
		}
	}

	if *strict && rep.FieldsMissed > 0 {
		os.Exit(1)
	}
}
