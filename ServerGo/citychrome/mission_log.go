// Package citychrome — mission_log.go: 采集任务遥测环形日志(批次77 §3.2)。
//
// MissionRecord 记录一次 mission 的规划/步数/结果摘要;Agent 在 RunMission
// 入口 Append(running)、出口同 ID 覆盖为 ok/fail;Service 经 MissionLog()
// 快照出口供 HTTP /api/virtual_city/realtime_board 的 data.missions[]。
// WS game.realtime_feed 帧结构不动(missions 只随 HTTP 快照回读,§3.4)。
package citychrome

import "sync"

// missionLogCap 是环形缓冲容量(批次77 §3.2:≤20 条)。
const missionLogCap = 20

// missionErrorCap / missionPlanCap 是 Error/Plan 入库 rune 截断上限
// (批次77 §3.2:Error ≤300、Plan ≤200;截断用包内 truncate 统一口径)。
const (
	missionErrorCap = 300
	missionPlanCap  = 200
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
// 翻状态用);超过容量 20 裁掉最旧。Error/Plan 按上限截断后入库。
func (l *MissionLog) Append(rec MissionRecord) {
	if l == nil {
		return
	}
	rec.Error = truncate(rec.Error, missionErrorCap)
	rec.Plan = truncate(rec.Plan, missionPlanCap)
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
