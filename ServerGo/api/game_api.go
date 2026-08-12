package api

import (
	"net/http"

	"LsmAgentGame/errcode"
	"LsmAgentGame/service"

	"github.com/gin-gonic/gin"
)

// GameAPI serves the lobby/game endpoints.
type GameAPI struct {
	svc *service.GameService
}

// NewGameAPI wires the handler.
func NewGameAPI(svc *service.GameService) *GameAPI {
	return &GameAPI{svc: svc}
}

// List GET /api/games — public, returns the lobby catalog.
func (a *GameAPI) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    a.svc.ListGames(),
	})
}
