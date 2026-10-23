// Package citychrome — mission_log.go: 采集任务遥测环形日志(批次77 §3.2;
// 批次78 §3.3 增 Visited 站点尝试轨迹 + MissionStats 健康度聚合)。
//
// MissionRecord 记录一次 mission 的规划/步数/结果摘要;Agent 在 RunMission
// 入口 Append(running)、出口同 ID 覆盖为 ok/fail;Service 经 MissionLog()
// 快照出口供 HTTP /api/virtual_city/realtime_board 的 data.missions[],
// 经 Stats() 出口供同响应的 data.stats(成功率/平均步数/平均耗时)。
// WS game.realtime_feed 帧结构不动(missions/stats 只随 HTTP 快照回读,§3.4)。
package citychrome

import (
	"strings"
	"sync"
)

// missionLogCap 是环形缓冲容量(批次77 §3.2:≤20 条)。
const missionLogCap = 20

// missionErrorCap / missionPlanCap 是 Error/Plan 入库 rune 截断上限
// (批次77 §3.2:Error ≤300、Plan ≤200;截断用包内 truncate 统一口径)。
const (
	missionErrorCap = 300
	missionPlanCap  = 200
)

// 批次78 §3.3:Visited(站点尝试轨迹)的入库口径。
const (
	// missionVisitedCap 是 Visited 的条数上限(≤6 条)。
	missionVisitedCap = 6
	// missionVisitedRuneCap 是 Visited 单条 URL 的 rune 上限(≤120)。
	missionVisitedRuneCap = 120
)

// Mission status 取值(批次77 §3.2)。
const (
	MissionStatusRunning = "running"
	MissionStatusOK      = "ok"
	MissionStatusFail    = "fail"
)

// MissionRecord 是一次采集 mission 的遥测记录(批次77 §3.2;字段名与 JSON
// tag 逐字对齐契约,前端 types/virtualCityRealtime.ts 同构)。
type MissionRecord struct {
	ID         string `json:"id"`          // "<category>-<startedAtUnixMilli>"
	Category   string `json:"category"`
	StartedAt  int64  `json:"started_at"`  // Unix 秒
	DurationMs int64  `json:"duration_ms"`
	Status     string `json:"status"`      // running | ok | fail
	Source     string `json:"source"`      // 成功时 payload.Source
	StepsUsed  int    `json:"steps_used"`  // 实际 LLM 轮数
	MaxSteps   int    `json:"max_steps"`
	Plan       string `json:"plan"`        // web_plan 摘要(goal + steps 以 " → " 连接),≤200 rune
	Quotes     int    `json:"quotes"`      // 提交 payload.quotes 条数(已过校验)
	News       int    `json:"news"`        // 提交 payload.news 条数
	Error      string `json:"error"`       // 失败原因,≤300 rune
	// Visited 是本次 mission 访问过的 URL(批次78 §3.3;≤6 条,每条 ≤120 rune,
	// 新→旧去重)。看板「采集」子页的「站点尝试」列直出。
	Visited []string `json:"visited"`
}

// MissionStats 是采集健康度聚合(批次78 §3.3;HTTP 快照 data.stats 出口)。
// 成功率分母 = OK+Fail;AvgSteps / AvgDurationMs 只统计**成功**记录。
type MissionStats struct {
	Total       int     `json:"total"`         // 记录总数
	OK          int     `json:"ok"`            // 成功数
	Fail        int     `json:"fail"`          // 失败数
	Running     int     `json:"running"`       // 进行中数
	SuccessRate float64 `json:"success_rate"`  // 成功率 0..1(分母 = OK+Fail)
	AvgSteps    float64 `json:"avg_steps"`     // 成功记录平均步数
	AvgDuration int64   `json:"avg_duration_ms"` // 成功记录平均耗时(ms)
	LastError   string  `json:"last_error"`    // 最近一次失败原因(≤300 rune)
}

// MissionLog 是容量 20 的 MissionRecord 环形缓冲(mutex 保护;批次77 §3.2)。
// 记录按新→旧存放(头部插入);并发安全(调度器/手动刷新/HTTP 快照三路共读)。
type MissionLog struct {
	mu   sync.Mutex
	recs []MissionRecord // 新→旧
}

// NewMissionLog 构造空日志。
func NewMissionLog() *MissionLog { return &MissionLog{} }

// Append 追加一条记录;同 ID 已存在则原地覆盖更新(running → ok/fail 出口
// 翻状态用);超过容量 20 裁掉最旧。Error/Plan 按上限截断后入库;
// Visited 按 ≤6 条 / 每条 ≤120 rune / 新→旧去重 归一化(批次78 §3.3)。
func (l *MissionLog) Append(rec MissionRecord) {
	if l == nil {
		return
	}
	rec.Error = truncate(rec.Error, missionErrorCap)
	rec.Plan = truncate(rec.Plan, missionPlanCap)
	rec.Visited = normalizeVisited(rec.Visited)
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.recs {
		if l.recs[i].ID == rec.ID {
			l.recs[i] = rec
			return
		}
	}
	l.recs = append([]MissionRecord{rec}, l.recs...)
	if len(l.recs) > missionLogCap {
		l.recs = l.recs[:missionLogCap]
	}
}

// normalizeVisited 归一化站点尝试轨迹:新→旧去重、每条 ≤120 rune、最多 6 条。
// 入参顺序按「新→旧」(Toolset.VisitedURLs 的口径);输出保持该顺序。
func normalizeVisited(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, u := range in {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, truncate(u, missionVisitedRuneCap))
		if len(out) >= missionVisitedCap {
			break
		}
	}
	return out
}

// Snapshot 返回新→旧副本(调用方改动不影响内部状态;nil 接收者返回空切片)。
func (l *MissionLog) Snapshot() []MissionRecord {
	if l == nil {
		return []MissionRecord{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]MissionRecord, len(l.recs))
	copy(out, l.recs)
	return out
}

// LastOK 返回该类目最近一条 status=ok 记录的副本(无则 nil)。
// Agent 经验注入取此值(批次77 §3.3-2)。
func (l *MissionLog) LastOK(category string) *MissionRecord {
	return l.lastByStatus(category, MissionStatusOK)
}

// LastFail 返回该类目最近一条 status=fail 记录的副本(无则 nil)。
func (l *MissionLog) LastFail(category string) *MissionRecord {
	return l.lastByStatus(category, MissionStatusFail)
}

// lastByStatus 扫描(新→旧)取该类目首个命中状态的记录副本。
func (l *MissionLog) lastByStatus(category, status string) *MissionRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := range l.recs {
		if l.recs[i].Category == category && l.recs[i].Status == status {
			cp := l.recs[i]
			return &cp
		}
	}
	return nil
}

// Stats 返回采集健康度聚合(批次78 §3.3;基于环形缓冲内全部记录)。
//
// 口径:
//   - Total = 记录总数;OK/Fail/Running 按 status 计数;
//   - SuccessRate = OK / (OK+Fail)(分母为 0 时取 0);
//   - AvgSteps / AvgDurationMs **只统计成功记录**(失败 mission 的步数无意义);
//   - LastError = 最近一条 fail 记录的错误原因(记录按新→旧存放,故取首个
//     fail 的 Error),≤300 rune。
//
// nil 接收者返回零值 MissionStats(不 panic)。
func (l *MissionLog) Stats() MissionStats {
	if l == nil {
		return MissionStats{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	stats := MissionStats{Total: len(l.recs)}
	var stepSum, durSum float64
	for _, r := range l.recs {
		switch r.Status {
		case MissionStatusOK:
			stats.OK++
			stepSum += float64(r.StepsUsed)
			durSum += float64(r.DurationMs)
		case MissionStatusFail:
			stats.Fail++
		case MissionStatusRunning:
			stats.Running++
		}
	}
	if stats.OK+stats.Fail > 0 {
		stats.SuccessRate = float64(stats.OK) / float64(stats.OK+stats.Fail)
	}
	if stats.OK > 0 {
		stats.AvgSteps = stepSum / float64(stats.OK)
		stats.AvgDuration = int64(durSum / float64(stats.OK))
	}
	// 最近一次失败原因(新→旧 ⇒ 首个 fail)。
	for _, r := range l.recs {
		if r.Status == MissionStatusFail {
			stats.LastError = truncate(r.Error, missionErrorCap)
			break
		}
	}
	return stats
}
