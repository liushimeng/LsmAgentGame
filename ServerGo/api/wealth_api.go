// Package api — wealth_api.go: GET /api/games/wealth/professions
// (2026-09-14 §财商流P0)。
//
// 契约: 协议契约 §6 — 返回 `{curated:[…], pool:{available,total,indexed}}`;
// curated 每项 = 精选手卡公开字段;pool 状态来自 profession.Loader.PoolInfo。
package api

import (
	"net/http"

	"LsmAgentGame/game/wealth/profession"

	"LsmAgentGame/errcode"

	"github.com/gin-gonic/gin"
)

// ProfessionAPI 是 GET /api/games/wealth/professions 的处理器。
type ProfessionAPI struct {
	loader *profession.Loader
}

// NewProfessionAPI 构造(loader 由 main.go 注入)。
func NewProfessionAPI(loader *profession.Loader) *ProfessionAPI {
	return &ProfessionAPI{loader: loader}
}

// CuratedCardPublic 是单张精选卡公开字段(协议 §6)。
type CuratedCardPublic struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Salary         int64    `json:"salary"`
	Expense        int64    `json:"expense"`
	Savings        int64    `json:"savings"`
	StartAge       int      `json:"start_age"`
	Energy         int      `json:"energy"`
	Network        int      `json:"network"`
	Cognition      int      `json:"cognition"`
	CreditScore    int      `json:"credit_score"`
	HomeDistrict   string   `json:"home_district"`
	RiskPreference string   `json:"risk_preference"`
	Personality    []string `json:"personality"`
	OpeningHook    string   `json:"opening_hook"`
	Goals          []string `json:"goals"`
}

// PoolInfoPublic 是池状态。
type PoolInfoPublic struct {
	Available bool `json:"available"`
	Total     int  `json:"total"`
	Indexed   int  `json:"indexed"`
}

// List 处理器。
func (a *ProfessionAPI) List(c *gin.Context) {
	if a.loader == nil {
		c.JSON(http.StatusOK, gin.H{
			"code": errcode.OK, "message": "ok",
			"data": gin.H{
				"curated": curatedPublic(),
				"pool":    PoolInfoPublic{Available: false, Total: -1},
			},
		})
		return
	}
	avail, total, indexed, _ := a.loader.PoolInfo()
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"curated": curatedPublic(),
			"pool":    PoolInfoPublic{Available: avail, Total: total, Indexed: indexed},
		},
	})
}

func curatedPublic() []CuratedCardPublic {
	cards := profession.CuratedCards()
	out := make([]CuratedCardPublic, len(cards))
	for i, c := range cards {
		out[i] = CuratedCardPublic{
			ID: c.ID, Title: c.Title,
			Salary: c.Salary, Expense: c.Expense, Savings: c.Savings,
			StartAge: c.StartAge, Energy: c.Energy, Network: c.Network, Cognition: c.Cognition,
			CreditScore:    c.CreditScore,
			HomeDistrict:   c.HomeDistrict,
			RiskPreference: c.RiskPreference,
			Personality:    c.Personality,
			OpeningHook:    c.OpeningHook,
			Goals:          c.Goals,
		}
	}
	return out
}