package service

import (
	"LsmWebGame/config"
	"LsmWebGame/models"

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
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Online int    `json:"online"`
}

// ListGames returns the game catalog with live online counts.
func (s *GameService) ListGames() []GameInfo {
	games := []GameInfo{
		{ID: "lobby-xiangqi", Name: "中国象棋", Kind: "xiangqi"},
		{ID: "lobby-chess", Name: "国际象棋", Kind: "chess"},
		{ID: "lobby-junqi", Name: "中国军棋", Kind: "junqi"},
		{ID: "lobby-doudizhu", Name: "斗地主", Kind: "doudizhu"},
		{ID: "lobby-texasholdem", Name: "德州扑克", Kind: "texasholdem"},
		{ID: "lobby-werewolf", Name: "狼人杀13人标准竞技局", Kind: "werewolf"},
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
