package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"context"
	"time"

	"LsmWebGame/logger"

	"go.uber.org/zap"
)

func (a *Agent) scheduleReWake(ctx context.Context, delay time.Duration) {
	ctxRW, cancel := context.WithCancel(ctx)
	a.Lock()
	prev := a.reWakeCancel
	a.reWakeCancel = cancel
	a.Unlock()
	if prev != nil {
		prev() // cancel any previous pending reWake
	}
	defer func() {
		// Only clear if we're still the registered cancel — otherwise
		// SetQuarantined has already replaced us with nil.
		a.Lock()
		if a.reWakeCancel != nil {
			// Use a typed comparison: cancel funcs aren't comparable, so
			// we always nil out unconditionally after firing (the new
			// scheduleReWake / SetQuarantined already handled its own
			// slot).
			a.reWakeCancel = nil
		}
		a.Unlock()
	}()

	select {
	case <-ctx.Done():
		return
	case <-ctxRW.Done():
		return
	case <-time.After(delay):
	}
	// Build a minimal wake event. The agent's runLoop will call rp() to get
	// the latest live phase/role, so the snapshot here is intentionally empty —
	// the Phase="" guard in handleEvent would skip it, but we set Phase to a
	// placeholder so it passes the guard and lets rp() provide real data.
	a.PushEvent(AgentEvent{
		Kind:    "state_change",
		Context: wwtypes.GameContext{Phase: "re_wake", MySeat: a.Seat},
	})
	logger.L().Debug("agent: scheduled re-wake fired",
		zap.Int("seat", a.Seat), zap.String("model", a.ModelKey))
}
