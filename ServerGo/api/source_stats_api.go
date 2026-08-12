// Package api — source_stats_api.go exposes 源码统计 接口。
//
// 端点:
//
//	GET /api/source-stats            → { groups: [{name, files, lines, bytes}],
//	                                     total: { files, lines, bytes },
//	                                     extensions: [{ ext, files, lines, bytes }] }
//
// 公开接口(无需登录)。递归遍历前端 src/ 与后端根目录的代码文件,
// 按扩展名分类统计文件数 / 总行数 / 总字节数。
//
// 安全性:
//   - 路径硬编码 (./ClientWeb/src 与 ./ServerGo),不允许 query 注入路径
//   - 跟随 symlink 不开启(filepath.WalkDir 不自动 follow)
//   - 单目录最大文件数 + 单文件最大字节数双重防御,避免恶意大目录拖爆内存
//   - 不读取文件内容(仅 Stat + 按 \n 计数 + size),开销可控
package api

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 源码统计 — 软上限,防止恶意构造的大目录拖死单次请求。
const (
	// ssMaxFilesPerDir 单目录文件数上限,超过则跳过并 warn 日志,
	// 防止一次性遍历含十万级文件的目录(node_modules / vendor / dist)。
	ssMaxFilesPerDir = 20000
	// ssMaxSingleBytes 单文件大小上限(bytes),超过则跳过该文件(防止统计
	// 数 GB 级的锁文件 / 构建产物)。源码类文件几乎都 < 1 MB,放 8 MB 足够。
	ssMaxSingleBytes int64 = 8 * 1024 * 1024
	// ssMaxDepth 目录递归深度上限。ClientWeb/src 与 ServerGo 实际 < 10 层。
	ssMaxDepth = 20
)

// 计入统计的扩展名集合。前端主要是 ts/tsx/css;后端主要是 go;其他
// 常见源码扩展名作为兜底。proto/md/json/html 也算"代码"。
var ssCodeExts = map[string]bool{
	".go":   true,
	".ts":   true,
	".tsx":  true,
	".js":   true,
	".jsx":  true,
	".css":  true,
	".scss": true,
	".less": true,
	".html": true,
	".json": true,
	".md":   true,
	".proto": true,
	".sql":  true,
	".yaml": true,
	".yml":  true,
	".toml": true,
	".xml":  true,
	".sh":   true,
	".py":   true,
}

// SourceStatsAPI 提供源码统计能力。目标目录在构造时固定,
// 避免 query 注入任意路径。
type SourceStatsAPI struct {
	// groups 中每项的扫描根目录,顺序即响应中的 groups 顺序。
	groups []sourceStatsGroup
}

type sourceStatsGroup struct {
	Name string // 显示名
	Path string // 相对于工作目录的扫描根
}

// NewSourceStatsAPI 构造 handler。targetDirs 是 (显示名, 路径) 对,
// 推荐传入 []{"前端", "./ClientWeb/src"}, {"后端", "./ServerGo"}。
func NewSourceStatsAPI(targetDirs []struct{ Name, Path string }) *SourceStatsAPI {
	groups := make([]sourceStatsGroup, len(targetDirs))
	for i, td := range targetDirs {
		groups[i] = sourceStatsGroup{Name: td.Name, Path: td.Path}
	}
	return &SourceStatsAPI{
		groups: groups,
	}
}

// SourceStatsGroup 是单组统计(单根目录)的响应形状。
type SourceStatsGroup struct {
	Name  string `json:"name"`            // 显示名
	Path  string `json:"path"`            // 实际扫描的目录
	Files int    `json:"files"`           // 文件个数
	Lines int    `json:"lines"`           // 总行数(按 \n 计数)
	Bytes int64  `json:"bytes"`           // 总字节数
	Error string `json:"error,omitempty"` // 该组扫描失败时填充(不影响其他组)
}

// SourceStatsExtension 是按扩展名聚合后的统计项。
type SourceStatsExtension struct {
	Ext   string `json:"ext"`   // 扩展名(含点,如 ".go")
	Files int    `json:"files"` // 该扩展名文件数
	Lines int    `json:"lines"` // 该扩展名总行数
	Bytes int64  `json:"bytes"` // 该扩展名总字节数
}

// SourceStatsPayload 是 /api/source-stats 的 data 字段。
type SourceStatsPayload struct {
	Groups     []SourceStatsGroup     `json:"groups"`     // 各组统计
	Total      SourceStatsGroup       `json:"total"`      // 全部组合计
	Extensions []SourceStatsExtension `json:"extensions"` // 按扩展名聚合(全量)
	BuiltAt    string                 `json:"built_at"`   // 本次扫描时刻(每次请求实时生成)
}

// Stats 处理 GET /api/source-stats —— 一次返回所有组 + 总计 + 扩展名分布。
//
// 设计要点:
//  1. 各组扫描失败不会让整个接口返回 500,而是该组 error 字段填充,
//     其他组正常返回(部分可用 > 整体不可用)。
//  2. 不做缓存 —— 单次扫描在数百毫秒级,UI 弹窗按需触发即可。
//  3. 行数 = 文件中 '\n' 个数 + (尾部若有内容则 +1),
//     与 `wc -l` 行为一致(wc -l 对不带换行符结尾的最后一不计,这里多 1;
//     我们改用「bufio.Scanner」逐行计数,语义等价于 wc -l)。
func (a *SourceStatsAPI) Stats(c *gin.Context) {
	groups := make([]SourceStatsGroup, 0, len(a.groups))
	// extAgg 按扩展名聚合,key = ".go" 等,value 在所有组合并。
	extAgg := map[string]*SourceStatsExtension{}

	var totalFiles int
	var totalLines int
	var totalBytes int64

	for _, g := range a.groups {
		files, lines, bytes, err := scanDir(g.Path, ssMaxDepth, extAgg)
		grp := SourceStatsGroup{
			Name:  g.Name,
			Path:  g.Path,
			Files: files,
			Lines: lines,
			Bytes: bytes,
		}
		if err != nil {
			logger.L().Warn("source_stats scan failed",
				zap.String("group", g.Name), zap.String("path", g.Path), zap.Error(err))
			grp.Error = err.Error()
		}
		groups = append(groups, grp)
		totalFiles += files
		totalLines += lines
		totalBytes += bytes
	}

	// 扩展名聚合 → 排序后输出(按字节数倒序,最显眼的在前)
	exts := make([]SourceStatsExtension, 0, len(extAgg))
	for _, v := range extAgg {
		exts = append(exts, *v)
	}
	sort.Slice(exts, func(i, j int) bool {
		if exts[i].Bytes != exts[j].Bytes {
			return exts[i].Bytes > exts[j].Bytes
		}
		return exts[i].Ext < exts[j].Ext
	})

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data": SourceStatsPayload{
			Groups: groups,
			Total: SourceStatsGroup{
				Name:  "总计",
				Files: totalFiles,
				Lines: totalLines,
				Bytes: totalBytes,
			},
			Extensions: exts,
			// BUG-R239-P3-01 (2026-08-04): 原 builtAt 在构造期一次性赋值,
			// 导致「刷新」按钮重扫数据但时间戳纹丝不动。改为每次请求实时生成,
			// 与 i18n 文案「扫描于 {time}」语义一致。
			BuiltAt: time.Now().Format(time.RFC3339),
		},
	})
}

// scanDir 递归遍历 root,统计文件个数 / 行数 / 字节数,并按扩展名聚合到 extAgg。
// extAgg 在多个 scanDir 之间共享,因此汇总 + 各组 双视角数据天然对齐。
//
// 防御:
//   - depth > ssMaxDepth 直接剪枝,防止人为构造深目录栈溢出
//   - 文件数 > ssMaxFilesPerDir 终止该目录扫描
//   - 单文件 > ssMaxSingleBytes 跳过(不进统计)
//   - 不跟随 symlink(os.Stat + filepath.WalkDir 默认行为)
func scanDir(root string, maxDepth int, extAgg map[string]*SourceStatsExtension) (int, int, int64, error) {
	info, err := os.Stat(root)
	if err != nil {
		return 0, 0, 0, err
	}
	if !info.IsDir() {
		return 0, 0, 0, os.ErrNotExist
	}

	var files, lines int
	var bytes int64
	count := 0

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// 单个文件 stat 失败不阻断整个扫描,跳过即可
			logger.L().Debug("source_stats walk entry failed",
				zap.String("path", path), zap.Error(walkErr))
			return nil
		}
		if d.IsDir() {
			// 跳过常见依赖 / 构建产物 / 版本控制目录
			name := d.Name()
			if path != root && shouldSkipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}

		count++
		if count > ssMaxFilesPerDir {
			logger.L().Warn("source_stats file count exceeds limit, truncating",
				zap.String("root", root), zap.Int("limit", ssMaxFilesPerDir))
			return filepath.SkipAll
		}

		// 深度检查:相对 root 的深度
		rel, _ := filepath.Rel(root, path)
		depth := strings.Count(rel, string(filepath.Separator))
		if depth > maxDepth {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !ssCodeExts[ext] {
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			logger.L().Debug("source_stats file info failed",
				zap.String("path", path), zap.Error(err))
			return nil
		}
		if fi.Size() > ssMaxSingleBytes {
			return nil
		}

		// 行数统计 —— bufio.Scanner 行为等价 wc -l。
		lc, err := countLines(path)
		if err != nil {
			logger.L().Debug("source_stats line count failed",
				zap.String("path", path), zap.Error(err))
			lc = 0
		}

		files++
		lines += lc
		bytes += fi.Size()

		agg, ok := extAgg[ext]
		if !ok {
			agg = &SourceStatsExtension{Ext: ext}
			extAgg[ext] = agg
		}
		agg.Files++
		agg.Lines += lc
		agg.Bytes += fi.Size()

		return nil
	})
	if walkErr != nil {
		return files, lines, bytes, walkErr
	}
	return files, lines, bytes, nil
}

// countLines 用 bufio 扫描器逐行计数,语义等价于 `wc -l`。
// 文件读失败返回 0 行(已在 walk 层记录 debug)。
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// 增大 buffer 兜住极长行(自动行数行偶然会出现 > 64KB 的 minified js)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	count := 0
	for sc.Scan() {
		count++
	}
	if err := sc.Err(); err != nil {
		return count, err
	}
	return count, nil
}

// shouldSkipDir 决定哪些子目录不进入递归。
// 含典型依赖与构建产物:node_modules / vendor / dist / build / .git / .idea 等。
func shouldSkipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", "dist", "build", "out", ".git",
		".idea", ".vscode", ".next", ".nuxt", ".cache", "target",
		"__pycache__", ".venv", "venv":
		return true
	}
	return false
}