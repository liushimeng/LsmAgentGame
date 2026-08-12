package service

import (
	"context"
	"errors"
	"strings"

	"LsmAgentGame/errcode"
	"LsmAgentGame/models"

	"gorm.io/gorm"
)

// DefaultLanguage is the fallback UI locale for users without an explicit
// preference. Must match the client's DEFAULT_LANG and the GORM column default
// on models.TLsmGameUser.Language.
const DefaultLanguage = "zh-CN"

// UserSortMode identifies which column the user list endpoint orders by.
// Keep the wire values stable — the client passes the same strings back.
type UserSortMode string

const (
	// UserSortByCreated orders by created_at DESC (newest first). Default.
	UserSortByCreated UserSortMode = "created_at"
	// UserSortByLastLogin orders by last_login_at DESC, with NULLs last so
	// accounts that have never logged in appear at the bottom.
	UserSortByLastLogin UserSortMode = "last_login_at"
)

// SupportedLanguages is the canonical set of locales the UI ships translations
// for. The user_service validates UpdateLanguage against this set; anything
// else is rejected with ErrValidationFailed.
var SupportedLanguages = map[string]bool{
	"zh-CN": true,
	"en":    true,
	"ja":    true,
}

// IsSupportedLanguage reports whether lang is one of the shipped locales.
func IsSupportedLanguage(lang string) bool {
	return SupportedLanguages[lang]
}

// UserService exposes per-user profile/preference operations.
type UserService struct {
	db *gorm.DB
}

// NewUserService builds a UserService.
func NewUserService(db *gorm.DB) *UserService {
	return &UserService{db: db}
}

// GetProfile loads the user row by id.
func (s *UserService) GetProfile(ctx context.Context, userID string) (*models.TLsmGameUser, error) {
	var user models.TLsmGameUser
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.Code(errcode.ErrAuthAccountNotFound)
		}
		return nil, errcode.Code(errcode.ErrDB)
	}
	if user.Language == "" {
		user.Language = DefaultLanguage
	}
	return &user, nil
}

// ReferredUser is a compact view of a user who registered using someone's
// personal invite code. Surfaced on the profile page.
type ReferredUser struct {
	UserID    string `json:"user_id"`
	Account   string `json:"account"`
	Nickname  string `json:"nickname"`
	CreatedAt int64  `json:"created_at"` // unix seconds
}

// ListReferrals returns the users who registered with the given user's personal
// invite code (i.e. rows whose referrer_user_id == userID), newest first.
func (s *UserService) ListReferrals(ctx context.Context, userID string) ([]ReferredUser, error) {
	var rows []models.TLsmGameUser
	if err := s.db.WithContext(ctx).
		Where("referrer_user_id = ?", userID).
		Order("created_at desc").
		Find(&rows).Error; err != nil {
		return nil, errcode.Code(errcode.ErrDB)
	}
	out := make([]ReferredUser, 0, len(rows))
	for _, r := range rows {
		out = append(out, ReferredUser{
			UserID:    r.ID,
			Account:   r.Account,
			Nickname:  r.Nickname,
			CreatedAt: r.CreatedAt.Unix(),
		})
	}
	return out, nil
}

// UpdateLanguage validates lang against SupportedLanguages and persists it.
func (s *UserService) UpdateLanguage(ctx context.Context, userID, lang string) error {
	if !IsSupportedLanguage(lang) {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	res := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
		Where("id = ?", userID).Update("language", lang)
	if res.Error != nil {
		return errcode.Code(errcode.ErrDB)
	}
	if res.RowsAffected == 0 {
		return errcode.Code(errcode.ErrAuthAccountNotFound)
	}
	return nil
}

// UpdateNickname validates and persists a new nickname. The nickname must be
// non-empty and unique across the platform.
func (s *UserService) UpdateNickname(ctx context.Context, userID, nickname string) error {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	// Check uniqueness (exclude current user).
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
		Where("nickname = ? AND id <> ?", nickname, userID).Count(&count).Error; err != nil {
		return errcode.Code(errcode.ErrDB)
	}
	if count > 0 {
		return errcode.Code(errcode.ErrAuthNicknameTaken)
	}
	res := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
		Where("id = ?", userID).Update("nickname", nickname)
	if res.Error != nil {
		return errcode.Code(errcode.ErrDB)
	}
	if res.RowsAffected == 0 {
		return errcode.Code(errcode.ErrAuthAccountNotFound)
	}
	return nil
}

// GetUserType returns the user type (role) for the given user ID.
func (s *UserService) GetUserType(ctx context.Context, userID string) (models.UserType, error) {
	var user models.TLsmGameUser
	if err := s.db.WithContext(ctx).Select("user_type").Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errcode.Code(errcode.ErrAuthAccountNotFound)
		}
		return 0, errcode.Code(errcode.ErrDB)
	}
	return user.UserType, nil
}

// AdminUserView is the JSON representation returned by ListAllUsers.
type AdminUserView struct {
	ID             string          `json:"id"`
	Account        string          `json:"account"`
	Nickname       string          `json:"nickname"`
	Phone          string          `json:"phone"`
	Email          string          `json:"email"`
	UserType       models.UserType `json:"user_type"`
	MyInviteCode   string          `json:"my_invite_code"`
	ReferralCount  int             `json:"referral_count"`
	ReferrerUserID string          `json:"referrer_user_id"`
	CreatedAt      int64           `json:"created_at"`
	LastLoginAt    *int64          `json:"last_login_at,omitempty"`
}

// ListAllUsers returns all users ordered by creation time (newest first).
//
// Kept for the legacy HTTP endpoint (api/admin_api.go). It delegates to
// ListAllUsersWithPaging with a generous limit so callers that expect the full
// list never see silent truncation.
func (s *UserService) ListAllUsers(ctx context.Context) ([]AdminUserView, error) {
	views, _, err := s.ListAllUsersWithPaging(ctx, 0, 1000, UserSortByCreated, 0, nil)
	return views, err
}

// ListAllUsersWithPaging returns one page of users ordered by the given sort
// mode, plus the total row count. skip is the offset, limit the page size.
//
// Parameter clamping (mirrors service/git_log_service.go):
//   - skip  < 0  -> 0
//   - limit <= 0 -> 20
//   - limit > 100 -> 100
//
// Sort modes:
//   - UserSortByCreated   → ORDER BY created_at DESC (default, legacy)
//   - UserSortByLastLogin → ORDER BY last_login_at IS NULL, last_login_at DESC
//     (NULLs last so accounts that have never logged in are pushed to the
//     bottom rather than appearing as "most recent" via MySQL's NULL ordering).
//
// Online filter (onlyOnline == 1 means online; -1 means offline; 0 means no filter):
//   - When onlyOnline != 0 the query is constrained to a fixed set of user IDs
//     (passed in by the caller from the live WS hub), so total reflects the
//     filtered set. The caller is responsible for ensuring the ID list is
//     non-empty; passing nil returns 0 rows / 0 total.
func (s *UserService) ListAllUsersWithPaging(
	ctx context.Context,
	skip, limit int,
	sort UserSortMode,
	onlyOnline int,
	onlineIDs []string,
) ([]AdminUserView, int, error) {
	if skip < 0 {
		skip = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Online filter prep. Build the ID slice we'll constrain the query to.
	//   onlyOnline == 1  → onlineIDs (caller passes hub.ConnectedUserIDs())
	//   onlyOnline == -1 → all IDs minus onlineIDs (offline users)
	//   onlyOnline == 0  → no filter
	var filterIDs []string
	if onlyOnline == 1 {
		filterIDs = onlineIDs
	} else if onlyOnline == -1 {
		// diff: allOnline - filterIDs; do this in Go instead of SQL to keep
		// the query plan simple and consistent with the online case.
		onlineSet := make(map[string]struct{}, len(onlineIDs))
		for _, id := range onlineIDs {
			onlineSet[id] = struct{}{}
		}
		// We need *all* user IDs to compute the offline set. Pull them with
		// a single minimal query.
		var allIDs []string
		if err := s.db.WithContext(ctx).
			Model(&models.TLsmGameUser{}).
			Pluck("id", &allIDs).Error; err != nil {
			return nil, 0, errcode.Code(errcode.ErrDB)
		}
		filterIDs = make([]string, 0, len(allIDs))
		for _, id := range allIDs {
			if _, isOnline := onlineSet[id]; isOnline {
				continue
			}
			filterIDs = append(filterIDs, id)
		}
	}

	var total int64
	if len(filterIDs) > 0 {
		if err := s.db.WithContext(ctx).
			Model(&models.TLsmGameUser{}).
			Where("id IN ?", filterIDs).
			Count(&total).Error; err != nil {
			return nil, 0, errcode.Code(errcode.ErrDB)
		}
	} else if onlyOnline != 0 {
		// Filter set is empty → 0 results, 0 total.
		return []AdminUserView{}, 0, nil
	} else {
		if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).Count(&total).Error; err != nil {
			return nil, 0, errcode.Code(errcode.ErrDB)
		}
	}
	if int64(skip) >= total {
		// Page beyond the end — return an empty page but the true total so the
		// client can clamp its page index.
		return []AdminUserView{}, int(total), nil
	}

	db := s.db.WithContext(ctx)
	switch sort {
	case UserSortByLastLogin:
		// "IS NULL" first so NULLs sort last (MySQL: 0 < 1), then DESC.
		db = db.Order("last_login_at IS NULL, last_login_at DESC")
	default:
		// Unknown / empty / UserSortByCreated — fall back to legacy ordering.
		db = db.Order("created_at DESC")
	}
	if len(filterIDs) > 0 {
		db = db.Where("id IN ?", filterIDs)
	}

	var users []models.TLsmGameUser
	if err := db.Limit(limit).Offset(skip).Find(&users).Error; err != nil {
		return nil, 0, errcode.Code(errcode.ErrDB)
	}

	out := make([]AdminUserView, 0, len(users))
	for _, u := range users {
		v := AdminUserView{
			ID:             u.ID,
			Account:        u.Account,
			Nickname:       u.Nickname,
			Phone:          u.Phone,
			Email:          u.Email,
			UserType:       u.UserType,
			MyInviteCode:   u.MyInviteCode,
			ReferralCount:  u.ReferralCount,
			ReferrerUserID: u.ReferrerUserID,
			CreatedAt:      u.CreatedAt.Unix(),
		}
		if u.LastLoginAt != nil {
			ts := u.LastLoginAt.Unix()
			v.LastLoginAt = &ts
		}
		out = append(out, v)
	}
	return out, int(total), nil
}

// DeleteUserWithRelatedData deletes a user and all their associated data.
//
// To avoid overwhelming the database IO subsystem (InnoDB redo-log fsyncs),
// the operation is split into two short transactions:
//
//   1. Clear referrer references (UPDATE on t_lsm_game_user).
//   2. Delete the user + chat messages + sessions + player records
//      (DELETEs across 4 tables, committed atomically).
//
// Room cleanup (adjusting room counts and removing empty rooms) is handled
// separately by the caller via RoomService.DeleteRoomsByUser, which runs its
// own transaction to keep lock contention minimal.
//
// The ctx passed here should have a generous timeout (≥30 s) — do NOT use a
// short per-request context, as a cancelled context mid-transaction forces an
// expensive InnoDB rollback.
func (s *UserService) DeleteUserWithRelatedData(ctx context.Context, userID string) error {
	// Guard: never delete a super admin through this path.
	var target models.TLsmGameUser
	if err := s.db.WithContext(ctx).Select("user_type").Where("id = ?", userID).First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.Code(errcode.ErrAuthAccountNotFound)
		}
		return errcode.Code(errcode.ErrDB)
	}
	if target.UserType >= models.UserTypeSuper {
		return errcode.Code(errcode.ErrPermissionDenied)
	}

	// ── Transaction 1: clear referrer references ────────────────────────
	// Small UPDATE, committed immediately to release the lock on
	// t_lsm_game_user before the larger DELETEs start.
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&models.TLsmGameUser{}).
			Where("referrer_user_id = ?", userID).
			Update("referrer_user_id", "").Error
	}); err != nil {
		return errcode.Code(errcode.ErrDB)
	}

	// ── Transaction 2: delete user + related rows ───────────────────────
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Order: dependent tables first, user table last.
		// DELETEs target indexed columns only (verified by EXPLAIN).

		if err := tx.Where("from_user_id = ?", userID).Delete(&models.TLsmGameChatMessage{}).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.TLsmGameSession{}).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.TLsmGamePlayer{}).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		if err := tx.Where("id = ?", userID).Delete(&models.TLsmGameUser{}).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}

		return nil
	})
}

// DemoteFromSuper revokes the super-admin role from targetID, downgrading
// them to a normal user. The caller is expected to have already verified:
//   - targetID != callerID (self-protection)
//   - target is currently online-free (the WS layer enforces this)
//   - target's current user_type is UserTypeSuper
//
// We additionally guard the UPDATE with `WHERE id = ? AND user_type = ?` so
// that a concurrent role change (e.g. another admin action racing) cannot
// silently demote a non-super user.
//
// Returns:
//   - (nil) on a successful downgrade.
//   - errcode.ErrAuthAccountNotFound when targetID doesn't exist.
//   - errcode.ErrPermissionDenied when targetID is not currently a super admin.
//   - errcode.ErrDB on any other database failure.
//
// This is intentionally a tiny, single-statement operation; we do not
// touch sessions, players, or rooms — those follow the demoted user's
// normal user_type enforcement on their next request.
func (s *UserService) DemoteFromSuper(ctx context.Context, targetID string) error {
	if targetID == "" {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	if s.db == nil {
		return errcode.Code(errcode.ErrInternal)
	}
	res := s.db.WithContext(ctx).
		Model(&models.TLsmGameUser{}).
		Where("id = ? AND user_type = ?", targetID, models.UserTypeSuper).
		Updates(map[string]any{"user_type": models.UserTypeNormal})
	if res.Error != nil {
		return errcode.Code(errcode.ErrDB)
	}
	if res.RowsAffected == 0 {
		// Distinguish "not found" from "not super". A separate COUNT query
		// is cheap (single-row index hit) and gives the handler a precise
		// error code instead of a generic "no change".
		var count int64
		if err := s.db.WithContext(ctx).
			Model(&models.TLsmGameUser{}).
			Where("id = ?", targetID).
			Count(&count).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}
		if count == 0 {
			return errcode.Code(errcode.ErrAuthAccountNotFound)
		}
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	return nil
}
