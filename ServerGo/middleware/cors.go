// Package middleware contains Gin middleware.
package middleware

import (
	"time"

	"LsmAgentGame/config"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS builds the CORS middleware from config. The allow-list is read from
// LsmAgentGame.conf → cors.allowed_origins. Do not hardcode origins here.
func CORS(cfg *config.Config) gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     cfg.CORS.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Request-ID"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
