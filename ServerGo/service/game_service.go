package service

import (
	"LsmAgentGame/config"
	"LsmAgentGame/models"

	"gorm.io/gorm"
)

// GameService exposes the multi-game lobby catalog.
type GameService struct {
	db  *gorm.DB
	cfg *config.Config
}

// NewGameService builds a GameService.
func NewGameService(db *gorm.DB, cfg *config.Config) *GameService {
	return &GameService{db: db, cfg: cfg}
}

// GameInfo describes a game in the lobby.
type GameInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Category string `json:"category"` // "traditional" | "agent" — §20260819-02 大厅菜单分组
	Online   int    `json:"online"`
}

// ListGames returns the game catalog with live online counts.
//
// §20260819-02 大厅菜单分组: 6 款游戏按是否支持 Agent 分为两类。
//   - traditional: 象棋 / 国际象棋 / 军棋 / 斗地主
//   - agent:       德州扑克(R1 2026-08-19 接入 Agent + 多模型 Fisher-Yates)
//                   狼人杀(§15 13 人局 Agent)
// 前端按 category 分组渲染,AGENT 游戏置顶,带紫红高亮徽章。
func (s *GameService) ListGames() []GameInfo {
	games := []GameInfo{
		{ID: "lobby-xiangqi", Name: "中国象棋", Kind: "xiangqi", Category: "traditional"},
		{ID: "lobby-chess", Name: "国际象棋", Kind: "chess", Category: "traditional"},
		{ID: "lobby-junqi", Name: "中国军棋", Kind: "junqi", Category: "traditional"},
		{ID: "lobby-doudizhu", Name: "斗地主", Kind: "doudizhu", Category: "traditional"},
		{ID: "lobby-texasholdem", Name: "德州扑克", Kind: "texasholdem", Category: "agent"},
		{ID: "lobby-werewolf", Name: "狼人杀13人标准竞技局", Kind: "werewolf", Category: "agent"},
	}

	// Compute online counts from rooms + players.
	for i := range games {
		games[i].Online = s.onlineCount(games[i].Kind)
	}
	return games
}

// onlineCount sums CurrentCount across all open rooms of a given game kind.
func (s *GameService) onlineCount(kind string) int {
	var total int
	s.db.Model(&models.TLsmGameRoom{}).
		Where("game_kind = ? AND status = ?", kind, "open").
		Select("COALESCE(SUM(current_count), 0)").
		Scan(&total)
	return total
}
