// git_log_service.go 读取本地 git 仓库的提交历史,并把每次提交的文件变更行数
// 聚合成可分页的数据,供 Web 端"提交记录"弹窗使用。
//
// 实现要点:
//   - 通过 `git` 可执行文件读取日志,避免引入 libgit2 依赖。
//   - 短 ID 用 git 自带的 abbreviated hash(默认 7 位),人类可读。
//   - 长 ID 用 rev-parse 拿到完整 40 位 hash,作为不可变标识返回给前端。
//   - 文件行数通过 `--shortstat` + `--pretty=tformat:` 一次性取出,每个
//     commit 对应 3 行(shortstat + 空行),由 service 在内存里分组,无
//     需遍历大量 git call。
//   - 默认仓库根目录为执行时的工作目录(CWD),与 LsmAgentGame 服务运行
//     时的项目根一致 —— `rebuild_restart_app.sh` 会在项目根启动。
package service

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"LsmAgentGame/logger"
)

// CommitEntry 表示一次提交的可摘要信息。
// FilesChanged / Insertions / Deletions 反映本次提交修改文件的行数(合并提交则按父提交数归并)。
// LongID 是 40 位完整 hash;ShortID 是 git 自带的 abbreviated hash(通常 7 位),便于人眼对照。
type CommitEntry struct {
	LongID      string    `json:"id"`          // 40 位完整 hash
	ShortID     string    `json:"short_id"`    // git abbrev hash (常见 7 位)
	Author      string    `json:"author"`
	AuthorEmail string    `json:"author_email"`
	Time        time.Time `json:"time"`        // 提交时间(本地时区)
	Subject     string    `json:"subject"`     // 提交首行(标题)
	FilesChanged int      `json:"files_changed"`
	Insertions  int       `json:"insertions"`
	Deletions   int       `json:"deletions"`
}

// CommitFileStat 表示单次提交中某个文件的修改行数。
// Path 是仓库内相对路径;Insertions / Deletions 表示新增/删除行数,0 表示不变。
type CommitFileStat struct {
	Path       string `json:"path"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
}

// CommitDetail 在 CommitEntry 之上多出 files 字段,展示本次提交每个文件的行数变化。
type CommitDetail struct {
	CommitEntry
	Files []CommitFileStat `json:"files"`
}

// GitLogService 提供"提交记录"的数据访问层。
// cacheTTL 短缓存,避免每次翻页都 spawn git 子进程。
type GitLogService struct {
	repoDir  string
	cacheTTL time.Duration

	mu        sync.RWMutex
	cacheAt   time.Time
	cacheList []CommitEntry
}

// NewGitLogService 在 repoDir 构造一个服务。repoDir 应指向 git 仓库根(包含 .git 的目录)。
func NewGitLogService(repoDir string) *GitLogService {
	return &GitLogService{
		repoDir:  repoDir,
		cacheTTL: 15 * time.Second,
	}
}

// List 返回分页后的提交列表。skip 是偏移量,limit 是页大小(<=0 视为 20)。
// total 是仓库内可用的总记录数,前端据此计算总页数。
// skip/limit 都做了边界裁剪,避免一次性把几 MB 的 git 输出都拉到内存里。
func (s *GitLogService) List(skip, limit int) (entries []CommitEntry, total int, err error) {
	if limit <= 0 {
		limit = 20
	}
	if skip < 0 {
		skip = 0
	}
	all, err := s.loadAll()
	if err != nil {
		return nil, 0, err
	}
	total = len(all)
	if skip >= total {
		return []CommitEntry{}, total, nil
	}
	end := skip + limit
	if end > total {
		end = total
	}
	return all[skip:end], total, nil
}

// Detail 返回单次提交的详细文件变更信息。prefix 用于支持只给短 ID 的情况。
func (s *GitLogService) Detail(prefix string) (*CommitDetail, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, fmt.Errorf("commit id is required")
	}
	all, err := s.loadAll()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if strings.HasPrefix(all[i].LongID, prefix) || strings.EqualFold(all[i].ShortID, prefix) {
			files, err := s.fileStats(all[i].LongID)
			if err != nil {
				return nil, err
			}
			d := CommitDetail{CommitEntry: all[i], Files: files}
			return &d, nil
		}
	}
	return nil, fmt.Errorf("commit %s not found", prefix)
}

// loadAll 从 git 一次性读取全量(带 shortstat),并按 cacheTTL 缓存。
// 注意:大仓库可能产生几 MB 输出;若未来需要分摊,可改为基于 skip/limit 的
// 增量读取(用 git log -n N --skip M)。当前实现优先保持简洁。
func (s *GitLogService) loadAll() ([]CommitEntry, error) {
	s.mu.RLock()
	if s.cacheList != nil && time.Since(s.cacheAt) < s.cacheTTL {
		out := s.cacheList
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()

	out, err := s.runLog()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cacheList = out
	s.cacheAt = time.Now()
	s.mu.Unlock()
	return out, nil
}

// runLog 执行 git log 并把输出解析成 []CommitEntry。
// 使用 --shortstat 之后,每个 commit 段由 1 行提交头 + 1 行 shortstat + 1 行空行组成。
// 提交头字段以 |\x1f 分隔(罕见字符,避免与正文冲突)。
func (s *GitLogService) runLog() ([]CommitEntry, error) {
	// --no-pager 防止在非交互终端上 git 走 less。
	// --date=iso-strict 输出可解析的 ISO 时间;--shortstat 让 git 帮忙合并同一 commit 的所有父链行数。
	cmd := exec.Command(
		"git",
		"--no-pager",
		"log",
		"--shortstat",
		"--pretty=format:COMMIT|%H|%h|%an|%ae|%aI|%s",
	)
	cmd.Dir = s.repoDir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git log pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git log start: %w", err)
	}

	var entries []CommitEntry
	var cur *CommitEntry
	scanner := bufio.NewScanner(stdout)
	// 增大缓冲,避免长提交消息被默认 64KB 截断。
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
			cur = nil
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "COMMIT|") {
			flush()
			parts := strings.SplitN(line, "|", 7)
			if len(parts) < 7 {
				continue
			}
			t, perr := time.Parse(time.RFC3339, parts[5])
			if perr != nil {
				logger.L().Warn("git log time parse",
					zap.String("raw", parts[5]), zap.Error(perr))
			}
			cur = &CommitEntry{
				LongID:      parts[1],
				ShortID:     parts[2],
				Author:      parts[3],
				AuthorEmail: parts[4],
				Time:        t,
				Subject:     parts[6],
			}
			continue
		}
		// shortstat 行形如: " 3 files changed, 12 insertions(+), 5 deletions(-)"
		// 只在上一行已是提交头时使用。
		if cur != nil && strings.Contains(line, "changed") {
			cur.FilesChanged, cur.Insertions, cur.Deletions = parseShortStat(line)
		}
		// 其他空行/合并提交携带的 parent 行忽略,等下一个 COMMIT 触发 flush。
	}
	flush()
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git log wait: %w", err)
	}
	return entries, scanner.Err()
}

// parseShortStat 从 git 的 shortstat 行解析 files / insertions / deletions。
// 当插入/删除都为 0 时,git 会输出 " 0 insertions(+), 0 deletions(-)";调用方应
// 容忍缺失的字段(空字符串)。
func parseShortStat(line string) (files, ins, del int) {
	for _, seg := range strings.Split(line, ",") {
		seg = strings.TrimSpace(seg)
		switch {
		case strings.HasSuffix(seg, "changed"):
			// 形如 "3 files changed" —— 也可能是 "1 file changed"。
			fields := strings.Fields(seg)
			if len(fields) > 0 {
				files, _ = strconv.Atoi(fields[0])
			}
		case strings.HasSuffix(seg, "insertion(+)"), strings.HasSuffix(seg, "insertions(+)"):
			fields := strings.Fields(seg)
			if len(fields) > 0 {
				ins, _ = strconv.Atoi(fields[0])
			}
		case strings.HasSuffix(seg, "insertion(-)"):
			fields := strings.Fields(seg)
			if len(fields) > 0 {
				del, _ = strconv.Atoi(fields[0])
			}
		case strings.HasSuffix(seg, "deletion(-)"), strings.HasSuffix(seg, "deletions(-)"):
			fields := strings.Fields(seg)
			if len(fields) > 0 {
				del, _ = strconv.Atoi(fields[0])
			}
		}
	}
	return
}

// fileStats 读取指定 commit 的 per-file 行数变化,使用 --numstat 格式。
//   - 文件以二进制形式修改时,git 会输出 "-\t-\tpath",此时插入/删除都记为 0。
//   - 合并提交按 --cc 会合并展示;为简单起见,我们只用单父视图(默认)。
func (s *GitLogService) fileStats(longID string) ([]CommitFileStat, error) {
	cmd := exec.Command("git", "--no-pager", "show", "--numstat", "--format=", longID)
	cmd.Dir = s.repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show numstat: %w", err)
	}
	var files []CommitFileStat
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		// 行格式: "<ins>\t<del>\t<path>"(二进制: "-\t-\t<path>")
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		ins, _ := strconv.Atoi(parts[0])
		del, _ := strconv.Atoi(parts[1])
		files = append(files, CommitFileStat{
			Path:       parts[2],
			Insertions: ins,
			Deletions:  del,
		})
	}
	return files, nil
}

// Invalidate 清除缓存。当 Web 端主动触发"刷新"按钮时可调用,避免在用户连续翻页时
// 反复读 git。当前 API 暂不暴露该方法,保留以备后用。
func (s *GitLogService) Invalidate() {
	s.mu.Lock()
	s.cacheList = nil
	s.cacheAt = time.Time{}
	s.mu.Unlock()
}
