package middleware

import (
	"time"

	"LsmWebGame/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RequestID injects an X-Request-ID header if missing, and stashes it in the context.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Writer.Header().Set("X-Request-ID", rid)
		c.Set("request_id", rid)
		c.Next()
	}
}

// Logging emits a single structured log line per request.
func Logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		uid, _ := c.Get("user_id")
		uidStr, _ := uid.(string)
		rid, _ := c.Get("request_id")
		ridStr, _ := rid.(string)
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", latency),
			zap.String("client_ip", c.ClientIP()),
			zap.String("request_id", ridStr),
		}
		if uidStr != "" {
			fields = append(fields, zap.String("user_id", uidStr))
		}
		if c.Writer.Status() >= 500 {
			logger.L().Error("http", fields...)
		} else {
			logger.L().Info("http", fields...)
		}
	}
}
