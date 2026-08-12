// Package logger wraps zap to give the rest of the project a tiny, stable API.
//
// Use L() to obtain the global *zap.Logger. Init() is called once from main
// and configures the underlying core based on LsmAgentGame.conf.
package logger

import (
	"os"

	"LsmAgentGame/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var global *zap.Logger

// Init builds the global logger. It is safe to call exactly once at process start.
//
// BUG-WEREWOLF-P2-NEW-6 (Round 24): the previous version Tee'd every log
// line to BOTH stdout AND the configured file. Combined with the
// `nohup ... >> LsmAgentGame.log 2>&1` startup wrapper in
// `rebuild_restart_app.sh`, this caused every WARN/INFO line to be written
// twice (once as JSON via the Tee file sink, once as plain text via the
// stdout sink captured into the same log file). The fix keeps the stdout
// sink (so the wrapper still gets a copy), and only adds the file sink if
// it points to a DIFFERENT file than stdout — i.e. only when the operator
// explicitly wants a separate side-channel log. Setting `log.file` to a
// dedicated path like `/var/log/lsmwebgame.log` keeps the file Tee; leaving
// it at the default `./LsmAgentGame.log` (same as the wrapper's redirect)
// skips the file Tee and avoids the duplication.
func Init(cfg *config.Config) error {
	level := zapcore.InfoLevel
	_ = level.UnmarshalText([]byte(cfg.Log.Level))

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder

	cores := []zapcore.Core{
		zapcore.NewCore(zapcore.NewJSONEncoder(encoderCfg), zapcore.AddSync(os.Stdout), level),
	}
	if cfg.Log.File != "" && !isStdoutRedirectTarget(cfg.Log.File) {
		if f, err := os.OpenFile(cfg.Log.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			cores = append(cores, zapcore.NewCore(zapcore.NewJSONEncoder(encoderCfg), zapcore.AddSync(f), level))
		}
	}
	global = zap.New(zapcore.NewTee(cores...))
	return nil
}

// isStdoutRedirectTarget reports whether the path is the same file the
// process's stdout/stderr is being redirected to. Heuristic: resolve both
// paths via os.Stat and compare device+inode. If they match, adding a Tee
// would double-write every line. BUG-WEREWOLF-P2-NEW-6.
func isStdoutRedirectTarget(path string) bool {
	a, err := os.Stat(path)
	if err != nil {
		return false
	}
	b, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return os.SameFile(a, b)
}

// L returns the global logger. Init() must have been called first.
func L() *zap.Logger {
	if global == nil {
		global = zap.NewExample()
	}
	return global
}

// Sync flushes buffered log entries. Call before process exit.
func Sync() {
	if global != nil {
		_ = global.Sync()
	}
}
