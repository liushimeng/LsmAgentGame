// Package ws — wallet WebSocket push.
//
// Clients never send wallet.* frames (all mutations go through HTTP), but the
// server pushes a `wallet.balance` envelope to every one of the user's
// connections when their balance changes, so all open tabs refresh.
//
//   server → client
//     wallet.balance  { balance, delta, reason }
//
// `reason` is one of the wallet.TxType constants (register_bonus, daily_login,
// win_reward, …) so the client can localise the toast. `delta` is signed.
package ws

import (
	"encoding/json"

	"LsmAgentGame/logger"
	"go.uber.org/zap"
)

// PushBalanceChange pushes a wallet.balance envelope to every connection owned
// by userID. Best-effort: a full send buffer is silently skipped so WS back-
// pressure never blocks wallet transactions.
func (h *Hub) PushBalanceChange(userID string, balance, delta int64, reason string) {
	payload, _ := json.Marshal(map[string]any{
		"balance": balance,
		"delta":   delta,
		"reason":  reason,
	})
	env := Envelope{Type: "wallet.balance", Payload: payload}
	h.BroadcastTo(userID, env)
	logger.L().Debug("wallet.balance pushed",
		zap.String("user_id", userID),
		zap.Int64("balance", balance),
		zap.Int64("delta", delta),
		zap.String("reason", reason))
}
