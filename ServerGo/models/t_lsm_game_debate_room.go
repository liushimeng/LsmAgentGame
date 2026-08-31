// Package models — 辩论比赛持久化表(2026-08-31 §20260831-08)。
//
// 设计依据:docs/辩论比赛/03-辩论比赛房间创建与配置设计.md §8(数据库设计)+
// docs/辩论比赛/06-辩论比赛公平性与评审系统设计.md §9(历史统计落库)。
//
// 按 CLAUDE.md §3:models/ 目录下 GORM 模型文件使用 t_lsm_game_*.go 前缀,
// 本文件是该前缀唯一允许目录下的辩论比赛 5 张表合一:
//
//	t_lsm_game_debate_room        房间记录(比赛结束 upsert,复盘主表)
//	t_lsm_game_debate_speech      发言记录(onSpeech 钩子异步写入)
//	t_lsm_game_debate_score       评审记录(onJudgeScore 钩子异步写入,一裁判×一队一行)
//	t_lsm_game_debate_model_stats 模型胜率统计(比赛结束 UPSERT 累加,重启不清零)
//	t_lsm_game_debate_topic       自定义辩题(管理员 POST /topics 落库)
//
// 写入方:ServerGo/game/debate/persistence.go(钩子接线);读取方:
// ServerGo/api/debate_api.go(history / topics 端点)。
package models

// TLsmGameDebateRoom 辩论房间记录(docs/辩论比赛/03 §8.1 + §20260831-08 扩展)。
//
// 一行 = 一场辩论比赛的最终快照。比赛正常结束(评审结果产出)时 upsert 全字段;
// 比赛被强制终止(StopGame,无评审结果)时 upsert 且 IsAbnormal=true。
// team_config / phase_config / judge_config / spectator_config / result
// 均为 JSON 字符串(由 game/debate 包序列化,内容分别为
// []debate.TeamConfig / debate.PhaseConfig / []debate.JudgeConfig /
// debate.SpectatorConfig / debate.DebateResult)。
type TLsmGameDebateRoom struct {
	ID              string `gorm:"type:varchar(64);primaryKey"                             json:"id"`
	TopicID         string `gorm:"type:varchar(64);not null"                               json:"topic_id"`
	TopicText       string `gorm:"type:varchar(500);not null"                              json:"topic_text"`
	TopicType       string `gorm:"type:varchar(32);not null"                               json:"topic_type"`
	Mode            string `gorm:"type:varchar(32);not null"                               json:"mode"`
	TeamConfig      string `gorm:"type:json"                                               json:"team_config"`
	PhaseConfig     string `gorm:"type:json"                                               json:"phase_config"`
	JudgeConfig     string `gorm:"type:json"                                               json:"judge_config"`
	SpectatorConfig string `gorm:"type:json"                                               json:"spectator_config"`
	Status          string `gorm:"type:varchar(32);not null"                               json:"status"`
	CurrentPhase    string `gorm:"type:varchar(32);not null"                               json:"current_phase"`
	CreatedBy       string `gorm:"type:varchar(64);not null"                               json:"created_by"`
	CreatedAt       int64  `gorm:"not null"                                                json:"created_at"`
	StartedAt       int64  `gorm:"not null"                                                json:"started_at"`
	// FinishedAt 是 GET /api/games/debate/history 的排序键(>0 = 已结束);
	// 与 (Status, FinishedAt) 组成复合索引 idx_debate_room_finished。
	FinishedAt        int64  `gorm:"not null;index:idx_debate_room_finished"           json:"finished_at"`
	WinnerTeamID      int    `gorm:"not null"                                                json:"winner_team_id"`
	BestDebaterSeat   int    `gorm:"not null"                                                json:"best_debater_seat"`
	BestDebaterTeamID int    `gorm:"not null"                                                json:"best_debater_team_id"`
	Result            string `gorm:"type:json"                                               json:"result"`
	IsAbnormal        bool   `gorm:"not null"                                                json:"is_abnormal"`
}

// TableName pins the SQL table name.
func (TLsmGameDebateRoom) TableName() string { return "t_lsm_game_debate_room" }

// TLsmGameDebateSpeech 发言记录(docs/辩论比赛/03 §8.2)。
//
// 由 onSpeech 钩子异步写入;ID 复用引擎内 Speech.ID("sp_<ms>_<rand>"),
// 兜底 "<room_id>:s<ms>"。References 为 JSON 字符串数组。
type TLsmGameDebateSpeech struct {
	ID              string `gorm:"type:varchar(64);primaryKey"                             json:"id"`
	RoomID          string `gorm:"type:varchar(64);not null;index:idx_debate_speech_room" json:"room_id"`
	Phase           string `gorm:"type:varchar(32);not null"                               json:"phase"`
	TeamID          int    `gorm:"not null"                                                json:"team_id"`
	Seat            int    `gorm:"not null"                                                json:"seat"`
	Stance          string `gorm:"type:varchar(32);not null"                               json:"stance"`
	SpeakerName     string `gorm:"type:varchar(64);not null"                               json:"speaker_name"`
	Role            string `gorm:"type:varchar(32);not null"                               json:"role"`
	Content         string `gorm:"type:text"                                               json:"content"`
	WordCount       int    `gorm:"not null"                                                json:"word_count"`
	References      string `gorm:"type:json"                                               json:"references"`
	InternalThought string `gorm:"type:text"                                               json:"internal_thought"`
	ModelKey        string `gorm:"type:varchar(64);not null"                               json:"model_key"`
	DurationMs      int    `gorm:"not null"                                                json:"duration_ms"`
	CreatedAt       int64  `gorm:"not null;index:idx_debate_speech_room"                   json:"created_at"` // unix 毫秒(与 Speech.Timestamp 对齐)
}

// TableName pins the SQL table name.
func (TLsmGameDebateSpeech) TableName() string { return "t_lsm_game_debate_speech" }

// TLsmGameDebateScore 评审记录(docs/辩论比赛/03 §8.3)。
//
// 一行 = 一名裁判对一支队伍的评分(JudgeScore.Rankings 展开写入)。
// ID 确定性生成 "<room_id>:j<judge_id>:t<team_id>",重复评分 upsert 幂等覆盖。
type TLsmGameDebateScore struct {
	ID                    string  `gorm:"type:varchar(64);primaryKey"                       json:"id"`
	RoomID                string  `gorm:"type:varchar(64);not null;index:idx_debate_score_room" json:"room_id"`
	JudgeID               int     `gorm:"not null"                                          json:"judge_id"`
	JudgeModelKey         string  `gorm:"type:varchar(64);not null"                         json:"judge_model_key"`
	TeamID                int     `gorm:"not null"                                          json:"team_id"`
	ArgumentQuality       int     `gorm:"not null"                                          json:"argument_quality"`
	LogicRigor            int     `gorm:"not null"                                          json:"logic_rigor"`
	LanguageExpression    int     `gorm:"not null"                                          json:"language_expression"`
	TeamCoordination      int     `gorm:"not null"                                          json:"team_coordination"`
	RebuttalEffectiveness int     `gorm:"not null"                                          json:"rebuttal_effectiveness"`
	TotalScore            float64 `gorm:"not null"                                          json:"total_score"`
	Comment               string  `gorm:"type:text"                                         json:"comment"`
	BestDebaterSeat       int     `gorm:"not null"                                          json:"best_debater_seat"`
	WinnerTeamID          int     `gorm:"not null"                                          json:"winner_team_id"`
	OverallComment        string  `gorm:"type:text"                                         json:"overall_comment"`
	IsFallback            bool    `gorm:"not null"                                          json:"is_fallback"`
	CreatedAt             int64   `gorm:"not null"                                          json:"created_at"` // unix 毫秒
}

// TableName pins the SQL table name.
func (TLsmGameDebateScore) TableName() string { return "t_lsm_game_debate_score" }

// TLsmGameDebateModelStats 模型胜率统计落库(docs/辩论比赛/06 §9.1)。
//
// 替代 §20260831-06 进程内 statsStore 的「重启清零」缺陷:model_key 主键,
// 每局结束 UPSERT 原子累加(total_games = total_games + ?)。
// 启动时由 AttachPersistence 全量回读到 statsStore,GET /api/games/debate/stats
// 逻辑不变(仍读进程内快照)。
type TLsmGameDebateModelStats struct {
	ModelKey         string  `gorm:"type:varchar(64);primaryKey"               json:"model_key"`
	TotalGames       int     `gorm:"not null"                                  json:"total_games"`
	WinCount         int     `gorm:"not null"                                  json:"win_count"`
	BestDebaterCount int     `gorm:"not null"                                  json:"best_debater_count"`
	ScoreSum         float64 `gorm:"not null"                                  json:"score_sum"`
	UpdatedAt        int64   `gorm:"not null"                                  json:"updated_at"` // unix 秒
}

// TableName pins the SQL table name.
func (TLsmGameDebateModelStats) TableName() string { return "t_lsm_game_debate_model_stats" }

// TLsmGameDebateTopic 自定义辩题(§20260831-08,docs/辩论比赛/03 §2.4)。
//
// 仅管理员经 POST /api/games/debate/topics 写入(IsOfficial 恒 false);
// GET /api/games/debate/topics 返回「内置题(cards.go) + 本表」合并列表。
// ID 形如 "custom_<rand>",由 debate.NewCustomTopicID 生成。
type TLsmGameDebateTopic struct {
	ID          string `gorm:"type:varchar(64);primaryKey"                    json:"id"`
	Text        string `gorm:"type:varchar(500);not null"                     json:"text"`
	Type        string `gorm:"type:varchar(32);not null"                      json:"type"`
	Category    string `gorm:"type:varchar(64);not null"                      json:"category"`
	ProPosition string `gorm:"type:varchar(500);not null"                     json:"pro_position"`
	ConPosition string `gorm:"type:varchar(500);not null"                     json:"con_position"`
	Background  string `gorm:"type:text"                                      json:"background"`
	Keywords    string `gorm:"type:json"                                      json:"keywords"` // JSON 字符串数组
	Difficulty  int    `gorm:"not null"                                       json:"difficulty"`
	CreatedBy   string `gorm:"type:varchar(64);not null"                      json:"created_by"`
	CreatedAt   int64  `gorm:"not null;index"                                 json:"created_at"` // unix 秒
	IsOfficial  bool   `gorm:"not null;default:false"                         json:"is_official"`
}

// TableName pins the SQL table name.
func (TLsmGameDebateTopic) TableName() string { return "t_lsm_game_debate_topic" }
