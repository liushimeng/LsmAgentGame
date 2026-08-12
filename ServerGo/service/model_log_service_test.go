// Package service — model_log_service_test.go.
//
// Coverage is intentionally limited to the nil-DB path (no MariaDB needed in
// CI). Every public method must return ErrInternal when gormDB is nil and
// ErrValidationFailed when an input id is empty. Real CRUD paths are covered
// by the integration suite (see service/wallet_service_test.go for the
// precedent).
package service

import (
	"context"
	"testing"
	"time"

	"LsmWebGame/errcode"
)

func TestModelLogService_NilDB(t *testing.T) {
	s := NewModelLogService(nil)
	ctx := context.Background()

	t.Run("ListProviderGames nil DB", func(t *testing.T) {
		_, err := s.ListProviderGames(ctx, "p1", 10, 0, time.Time{})
		assertErrCode(t, err, errcode.ErrInternal)
	})

	t.Run("GetGameLog nil DB", func(t *testing.T) {
		_, err := s.GetGameLog(ctx, "g1")
		assertErrCode(t, err, errcode.ErrInternal)
	})

	t.Run("ListGameMessages nil DB", func(t *testing.T) {
		_, err := s.ListGameMessages(ctx, "g1", 10, 0)
		assertErrCode(t, err, errcode.ErrInternal)
	})

	t.Run("ListGameActions nil DB", func(t *testing.T) {
		_, err := s.ListGameActions(ctx, "g1", 10, 0)
		assertErrCode(t, err, errcode.ErrInternal)
	})

	t.Run("GetBotWalletSummary nil DB", func(t *testing.T) {
		_, err := s.GetBotWalletSummary(ctx, "b1", 10)
		assertErrCode(t, err, errcode.ErrInternal)
	})
}

func TestModelLogService_EmptyInput(t *testing.T) {
	// Pass a non-nil but unmocked gormDB so we get to the input validation
	// branch (which executes before any SQL). We use a fake DB handle via
	// NewModelLogService(nil) — the empty-id branch fires regardless because
	// the validation check is up front. If the validation moves later we'd
	// catch the regression here.
	s := NewModelLogService(nil)
	ctx := context.Background()

	t.Run("ListProviderGames empty providerID", func(t *testing.T) {
		_, err := s.ListProviderGames(ctx, "", 10, 0, time.Time{})
		assertErrCode(t, err, errcode.ErrValidationFailed)
	})

	t.Run("GetGameLog empty id", func(t *testing.T) {
		_, err := s.GetGameLog(ctx, "")
		assertErrCode(t, err, errcode.ErrValidationFailed)
	})

	t.Run("ListGameMessages empty id", func(t *testing.T) {
		_, err := s.ListGameMessages(ctx, "", 10, 0)
		assertErrCode(t, err, errcode.ErrValidationFailed)
	})

	t.Run("ListGameActions empty id", func(t *testing.T) {
		_, err := s.ListGameActions(ctx, "", 10, 0)
		assertErrCode(t, err, errcode.ErrValidationFailed)
	})

	t.Run("GetBotWalletSummary empty bot id", func(t *testing.T) {
		_, err := s.GetBotWalletSummary(ctx, "", 10)
		assertErrCode(t, err, errcode.ErrValidationFailed)
	})
}
