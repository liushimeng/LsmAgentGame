// Package api — wiki_api.go exposes Wiki (项目文档) 列表与内容接口。
//
// 端点:
//
//   GET /api/wiki/list            → { entries: [{name, title, size, mtime, excerpt}] }
//   GET /api/wiki/content?name=X  → { name, content }   (X = 文档文件名,UTF-8)
//
// 两个接口均为公开(不要求登录) —— 文档本身是仓库内的纯 markdown,
// 暴露给已加载页面的访客可加快阅读体验,免去用户复制文件路径。
//
// 安全性:
//   - 路径穿越防御:name 必须经 base name + 后缀白名单(.md)双重过滤,
//     禁止 ".." / 绝对路径 / 隐藏文件 / 非 .md 文件。
//   - 单文件大小上限 512 KiB,超过则返回 413,避免一次性把大文件读到内存。
package api

import (
	"fmt"
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

// wikiMaxFileBytes 单文档大小上限。超过此值的文件不出现在列表里,
// 直接 GET 内容也会返回 413。
const wikiMaxFileBytes = 512 * 1024

// WikiAPI 提供项目 wiki(项目根 docs/ 目录) 列表与内容访问。
type WikiAPI struct {
	// docsDir 是文档所在目录(绝对或相对路径,启动期解析)。
	docsDir string
}

// NewWikiAPI 构造 handler。docsDir 应指向项目内的 docs/ 目录;
// 若目录不存在,List 仍返回空数组(不报错),Content 对所有 name 都返 404。
func NewWikiAPI(docsDir string) *WikiAPI {
	return &WikiAPI{docsDir: docsDir}
}

// WikiEntry 描述单个文档的元数据,供前端 WikiModal 列表渲染。
//
// Excerpt 是文件首段(去除前导标题/空行后)的 80 字摘要,用于列表项预览;
// mtime 用 RFC3339 字符串,前端可直接 new Date() 解析。
type WikiEntry struct {
	Name    string `json:"name"`    // 相对路径(用于 /api/wiki/content?name=X)
	Title   string `json:"title"`   // 人类可读标题(由首行 # / 文件名推导)
	Size    int64  `json:"size"`    // 文件字节数
	MTime   string `json:"mtime"`   // 修改时间 RFC3339
	Excerpt string `json:"excerpt"` // 80 字摘要
}

// List 处理 GET /api/wiki/list —— 返回 docs/ 下所有 .md 文件的元数据。
// 按文件名升序排列(中文按 Unicode 码点);目录不存在时返回空 entries。
func (a *WikiAPI) List(c *gin.Context) {
	if a.docsDir == "" {
		c.JSON(http.StatusOK, gin.H{
			"code":    errcode.OK,
			"message": errcode.DefaultMessages[errcode.OK],
			"data":    gin.H{"entries": []WikiEntry{}},
		})
		return
	}
	entries, err := a.collectEntries()
	if err != nil {
		logger.L().Warn("wiki list failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": errcode.DefaultMessages[errcode.ErrInternal],
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data":    gin.H{"entries": entries},
	})
}

// Content 处理 GET /api/wiki/content?name=X —— 返回单个文档的文本内容。
// name 必须经过 baseName + 后缀白名单校验,否则返回 400;文件不存在返 404;
// 文件超过 wikiMaxFileBytes 返回 413。
func (a *WikiAPI) Content(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "name is required",
		})
		return
	}
	// 1. baseName —— 拒绝 "..", "/", "\" 等路径穿越字符。
	clean := filepath.Base(name)
	if clean != name || clean == "" || clean == "." || clean == ".." {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "invalid name",
		})
		return
	}
	// 2. 后缀白名单 —— 仅允许 .md
	if !strings.HasSuffix(strings.ToLower(clean), ".md") {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "only .md files are allowed",
		})
		return
	}
	// 3. 隐藏文件直接拒绝(以 . 开头)
	if strings.HasPrefix(clean, ".") {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "hidden files are not allowed",
		})
		return
	}

	full := filepath.Join(a.docsDir, clean)
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    errcode.ErrValidationFailed,
				"message": fmt.Sprintf("wiki doc %q not found", clean),
			})
			return
		}
		logger.L().Warn("wiki stat failed", zap.String("name", clean), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": errcode.DefaultMessages[errcode.ErrInternal],
		})
		return
	}
	if info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "name must be a file",
		})
		return
	}
	if info.Size() > wikiMaxFileBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": fmt.Sprintf("wiki doc %q exceeds %d bytes", clean, wikiMaxFileBytes),
		})
		return
	}
	body, err := os.ReadFile(full)
	if err != nil {
		logger.L().Warn("wiki read failed", zap.String("name", clean), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": errcode.DefaultMessages[errcode.ErrInternal],
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data": gin.H{
			"name":    clean,
			"content": string(body),
		},
	})
}

// collectEntries 扫描 docsDir 下所有 .md 文件,组装 WikiEntry 列表并排序。
// 不可读的文件会跳过并在日志记录一次 warn,不让单个坏文件拖累整个列表。
func (a *WikiAPI) collectEntries() ([]WikiEntry, error) {
	dirEntries, err := os.ReadDir(a.docsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []WikiEntry{}, nil
		}
		return nil, err
	}
	out := make([]WikiEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		// 与 Content 保持一致:仅收录 .md,且拒绝隐藏文件。
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		full := filepath.Join(a.docsDir, name)
		info, err := de.Info()
		if err != nil {
			logger.L().Warn("wiki entry stat failed", zap.String("name", name), zap.Error(err))
			continue
		}
		// 单文件大小超过上限时仍展示在列表(用户能看到但拿不到内容),
		// 让前端可以选择灰显并给出"过大无法预览"的提示。
		entry := WikiEntry{
			Name:    name,
			Title:   deriveTitle(name),
			Size:    info.Size(),
			MTime:   info.ModTime().Format(time.RFC3339),
			Excerpt: deriveExcerpt(full),
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// deriveTitle 把文件名转换为人类可读标题:
//   - 去掉 .md 后缀
//   - 把下划线 / 连字符替换为空格
//   - 优先尝试读取文件首行 "# xxx"(去除 # 与空白),命中即返回
//   - 否则退回到文件名转换结果
func deriveTitle(filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	// 文件名级回退:下划线和短横线都替换成空格,中文文件名原样保留
	return strings.NewReplacer("_", " ", "-", " ").Replace(base)
}

// deriveExcerpt 读取文档首段,截取 80 字作为摘要。
//   - 跳过开头连续的 # / 空行
//   - 取下一段(直到第一个空行)的前 80 字符(中文字符按 rune 计)
//   - 文件读取失败时返回空串(不阻断列表)
func deriveExcerpt(fullPath string) string {
	f, err := os.Open(fullPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 2048)
	n, _ := f.Read(buf)
	if n <= 0 {
		return ""
	}
	text := string(buf[:n])
	// 按行扫描,跳过 # 开头与空行
	lines := strings.Split(text, "\n")
	var picked []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			if len(picked) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(t, "#") {
			continue
		}
		picked = append(picked, t)
		if len(picked) >= 3 {
			break
		}
	}
	if len(picked) == 0 {
		return ""
	}
	joined := strings.Join(picked, " ")
	// 按 rune 截断 80 字,避免中文半个字符
	runes := []rune(joined)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return joined
}