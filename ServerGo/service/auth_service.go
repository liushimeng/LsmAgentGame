// Package service contains the business logic used by api and ws handlers.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/models"
	"LsmAgentGame/util"

	"gorm.io/gorm"
)

// AgentBypassAccounts is the whitelist of agent / automation-test accounts
// that skip CAPTCHA verification on /api/auth/login.
//
// 重要:此白名单只在 cfg.Server.DevMode=true 时生效。生产部署必须显式设置
// dev_mode=false,否则白名单失效 → CAPTCHA 全员强制。
//
// 白名单来源:由 cfg.Server.AgentBypassAccounts (LsmAgentGame.conf 配置)
// 注入;空配置时回退到本常量作为开发模式兜底。**严禁**在生产 conf 中写入
// 真实账号名,推荐使用 dev_mode 关闭以彻底禁用此旁路。
//
// 匹配大小写敏感 — 仅注册时的精确字符串可旁路。新增自动化账号时除更新
// AgentBypassAccounts 默认值外,必须同步 docs/通用功能/测试账号凭证.md 与
// CLAUDE.md §21。
var AgentBypassAccounts = map[string]struct{}{
	"test19082jauishf8": {}, // legacy single-account seed
	"test_01":           {},
	"test_02":           {},
	"test_03":           {},
	"test_04":           {},
}

// IsAgentBypassAccount reports whether the given login account string is on
// the CAPTCHA-bypass whitelist **AND** cfg.Server.DevMode is true. It is the
// single source of truth used by AuthService.Login; expose it so callers
// (e.g. frontend detector, future tooling) can reuse the same predicate.
//
// 当 DevMode=false 时此函数永远返回 false,即白名单整体失效。
func (s *AuthService) IsAgentBypassAccount(account string) bool {
	if s == nil || s.cfg == nil || !s.cfg.Server.DevMode {
		return false
	}
	_, ok := AgentBypassAccounts[account]
	return ok
}

// IsAgentBypassAccountGlobal 是旧式全局函数,保留用于无法访问 AuthService
// 实例的调用点(如测试 / 旧前端 detector)。它**不**检查 DevMode — 调用者
// 必须自行保证仅在开发模式下使用。新代码一律改用 AuthService.IsAgentBypassAccount。
func IsAgentBypassAccountGlobal(account string) bool {
	_, ok := AgentBypassAccounts[account]
	return ok
}

// AgentBypassAccount is retained for backwards-compatibility with older
// call sites and tests. New code should use AuthService.IsAgentBypassAccount().
const AgentBypassAccount = "test19082jauishf8"

// AuthService is the user-account service.
type AuthService struct {
	db       *gorm.DB
	cfg      *config.Config
	captcha  *util.CaptchaStore // may be nil; AuthAPI wires it before any login happens
	wallets  *WalletService     // may be nil when constructed via NewAuthService; callers set via SetWalletService
}

// NewAuthService builds an AuthService.
//
// captcha may be nil; callers can either pass it here or set it later via
// SetCaptchaStore. When nil, real (non-agent) login attempts are refused
// at the boundary with ErrAuthCaptchaMissing as defense-in-depth.
func NewAuthService(db *gorm.DB, cfg *config.Config, captcha *util.CaptchaStore) *AuthService {
	return &AuthService{db: db, cfg: cfg, captcha: captcha}
}

// SetWalletService wires the wallet service so that registration seeds the
// wallet and login can credit the daily reward. Must be called before any
// register/login call.
func (s *AuthService) SetWalletService(ws *WalletService) {
	s.wallets = ws
}

// SetCaptchaStore attaches the captcha store after construction.
// Used by main.go during wiring and by tests that don't need one at build time.
func (s *AuthService) SetCaptchaStore(cs *util.CaptchaStore) {
	s.captcha = cs
}

// RegisterInput is the payload for register.
//
// Invitation model (CLAUDE.md §14 chat + invite refactor 2026-06):
//
//   - Every user has a personal invite code stored as MyInviteCode. The code
//     is generated at registration and is unique across the platform.
//   - On the registration form the user supplies ONE field: the personal
//     invite code of an existing user (ReferrerCode). That code resolves
//     to a ReferrerUserID and credits the referrer's referral_count.
//   - There is no admin-managed gate code: any logged-in user who shares
//     their MyInviteCode can be a referrer.
type RegisterInput struct {
	Account      string
	Password     string
	Nickname     string
	Phone        string
	Email        string
	ReferrerCode string // the inviter's personal code (their MyInviteCode)
}

// LoginInput is the payload for login. Provide Account OR Phone (phone wins
// when both are set), plus Password. Real users must also supply a valid
// CaptchaID/CaptchaAnswer — accounts in AgentBypassAccounts skip that
// requirement (see docs/测试账号凭证.md §6 & §7.2).
type LoginInput struct {
	Account       string
	Phone         string
	Password      string
	CaptchaID     string
	CaptchaAnswer string
	IP            string
	UA            string
}

// AuthResponse is the payload returned by login / register / refresh.
//
// CookieValue is the AES-256-GCM-encrypted auth cookie body. It is NOT
// serialized into JSON (`json:"-"`); the API layer writes it to Set-Cookie.
//
// MyInviteCode is the freshly-issued personal invite code for this user.
// It is also visible on the profile page, but echoing it on register lets
// the registration UI surface "your invite code" without a second round-trip.
//
// UserType is the user's role (1=normal, 2=admin, 3=super admin).
//
// DailyRewardClaimed and DailyRewardAmount surface the UTC+8 daily login
// bonus state so the frontend can toast the reward without a second call.
type AuthResponse struct {
	UserID              string        `json:"user_id"`
	Token               string        `json:"token"`
	ExpiresAt           int64         `json:"expires_at"`
	Language            string        `json:"language"`
	UserType            models.UserType `json:"user_type"`
	MyInviteCode        string        `json:"my_invite_code"`
	CookieValue         string        `json:"-"`
	DailyRewardClaimed  bool          `json:"daily_reward_claimed"`
	DailyRewardAmount   int64         `json:"daily_reward_amount"`
}

// Register creates a new user and returns the freshly issued token.
//
// Invitation model: registration requires ONLY an inviter's personal invite
// code (their MyInviteCode). We resolve the inviter and atomically credit
// their referral_count inside the same transaction that inserts the user row,
// so concurrent registrations cannot race the counter.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*AuthResponse, error) {
	if in.Account == "" || in.Password == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if strings.TrimSpace(in.ReferrerCode) == "" {
		return nil, errcode.Code(errcode.ErrAuthReferrerMissing)
	}
	// Uniqueness checks (read-only — the unique index is the source of truth,
	// but failing fast with a clear error code keeps the API clean).
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
		Where("account = ?", in.Account).Count(&count).Error; err != nil {
		return nil, errcode.Code(errcode.ErrDB)
	}
	if count > 0 {
		return nil, errcode.Code(errcode.ErrAuthAccountTaken)
	}
	// Nickname: default to account if empty, then validate uniqueness.
	nickname := strings.TrimSpace(in.Nickname)
	if nickname == "" {
		nickname = in.Account
	}
	if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
		Where("nickname = ?", nickname).Count(&count).Error; err != nil {
		return nil, errcode.Code(errcode.ErrDB)
	}
	if count > 0 {
		return nil, errcode.Code(errcode.ErrAuthNicknameTaken)
	}

	if in.Email != "" {
		if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
			Where("email = ? AND email <> ''", in.Email).Count(&count).Error; err != nil {
			return nil, errcode.Code(errcode.ErrDB)
		}
		if count > 0 {
			return nil, errcode.Code(errcode.ErrAuthEmailTaken)
		}
	}
	if in.Phone != "" {
		if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
			Where("phone = ? AND phone <> ''", in.Phone).Count(&count).Error; err != nil {
			return nil, errcode.Code(errcode.ErrDB)
		}
		if count > 0 {
			return nil, errcode.Code(errcode.ErrAuthPhoneTaken)
		}
	}

	hash, err := util.HashPassword(in.Password)
	if err != nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}

	// Generate this user's own personal invite code (others register with it).
	// Retry on the rare unique-key collision.
	var myCode string
	for attempt := 0; attempt < 3; attempt++ {
		c, err := util.NewInviteCode()
		if err != nil {
			return nil, errcode.Code(errcode.ErrInternal)
		}
		myCode = c
		var dup int64
		if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
			Where("my_invite_code = ?", myCode).Count(&dup).Error; err != nil {
			return nil, errcode.Code(errcode.ErrDB)
		}
		if dup == 0 {
			break
		}
	}

	referrerCode := strings.TrimSpace(in.ReferrerCode)

	// Resolve referrer + insert user + credit referral_count atomically.
	var user models.TLsmGameUser
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Resolve the inviter by their personal invite code.
		var referrer models.TLsmGameUser
		if err := tx.WithContext(ctx).
			Where("my_invite_code = ?", referrerCode).
			First(&referrer).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errcode.Code(errcode.ErrAuthReferrerInvalid)
			}
			return errcode.Code(errcode.ErrDB)
		}

		user = models.TLsmGameUser{
			ID:             util.NewUUID(),
			Account:        in.Account,
			Nickname:       nickname,
			PasswordHash:   hash,
			Phone:          in.Phone,
			Email:          in.Email,
			MyInviteCode:   myCode,
			ReferrerUserID: referrer.ID,
			Language:       "zh-CN",
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}

		// Credit the referrer: +1 to their referral_count.
		if err := tx.WithContext(ctx).Model(&models.TLsmGameUser{}).
			Where("id = ?", referrer.ID).
			Update("referral_count", gorm.Expr("referral_count + 1")).Error; err != nil {
			return errcode.Code(errcode.ErrDB)
		}

		// Seed the wallet in the SAME transaction. This guarantees that a user
		// row always has a matching wallet row — either both exist or neither
		// does. Sub-service call bypasses the outer svc.wallets DB handle and
		// uses the transaction directly.
		if s.wallets != nil {
			wallet := models.TLsmGameWallet{
				ID:        util.NewUUID(),
				UserID:    user.ID,
				Balance:   DefaultInitialBalance,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			if err := tx.Create(&wallet).Error; err != nil {
				return errcode.Code(errcode.ErrDB)
			}
			// register_bonus ledger row so the 1000 starting coins are auditable
			if err := s.wallets.writeTx(tx.Statement.Context, tx, user.ID,
				string(TxTypeRegisterBonus), DefaultInitialBalance,
				DefaultInitialBalance, "", "", "", "注册奖励"); err != nil {
				return errcode.Code(errcode.ErrWalletTxFailed)
			}
		}
		return nil
	})
	if err != nil {
		ce := errcode.AsError(err)
		return nil, ce
	}
	return s.issueTokenAndCookie(&user)
}

// RootInviteCode is the well-known personal invite code owned by the seeded
// root user. Because registration requires a referrer's personal code, a fresh
// database needs at least one user whose code new registrants can use. New
// users may register against this code until the user base grows.
//
// 默认值由 main.go 在启动时根据 cfg.RootInviteCode 设置;若 cfg 未提供,
// 启动器会随机生成一个并通过日志输出一次。源码常量仅作为开发模式兜底
// 默认值(占位符),不应出现在生产构建中。
var RootInviteCode = "ROOT_INVITE_CODE_FROM_CONFIG_OR_RANDOM"

// SeedRootUserIfEmpty creates a genesis "root" account on a fresh database so
// the referrer-gated registration flow has a valid starting referrer code.
// inviteCode 为空时回退到 RootInviteCode 全局变量。它是一个 no-op(非首次启动)。
func (s *AuthService) SeedRootUserIfEmpty(ctx context.Context, account, password, inviteCode string) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).Count(&count).Error; err != nil {
		return false, errcode.Code(errcode.ErrDB)
	}
	if count > 0 {
		return false, nil
	}
	if inviteCode == "" {
		inviteCode = RootInviteCode
	}
	hash, err := util.HashPassword(password)
	if err != nil {
		return false, errcode.Code(errcode.ErrInternal)
	}
	root := models.TLsmGameUser{
		ID:           util.NewUUID(),
		Account:      account,
		Nickname:     account,
		PasswordHash: hash,
		MyInviteCode: inviteCode,
		Language:     "zh-CN",
	}
	if err := s.db.WithContext(ctx).Create(&root).Error; err != nil {
		return false, errcode.Code(errcode.ErrDB)
	}
	return true, nil
}

// Login verifies credentials and returns a fresh token + 48h cookie.
//
// Behavior matrix:
//   - Account OR Phone required; phone wins when both are supplied.
//   - Accounts in AgentBypassAccounts skip CAPTCHA verification; everyone
//     else must supply matching CaptchaID/CaptchaAnswer (otherwise
//     10301/10302/10303).
//   - bcrypt password verification is always required, even for the bypass.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (*AuthResponse, error) {
	in.Account = strings.TrimSpace(in.Account)
	in.Phone = strings.TrimSpace(in.Phone)
	if in.Account == "" && in.Phone == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if in.Password == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}

	// CAPTCHA gate. Whitelisted agent / automation accounts always skip
	// (但仅在 cfg.Server.DevMode=true 时);every other caller must supply
	// matching CaptchaID/CaptchaAnswer. 详见 AgentBypassAccounts 注释。
	if !s.IsAgentBypassAccount(in.Account) {
		if s.captcha == nil {
			return nil, errcode.Code(errcode.ErrAuthCaptchaMissing)
		}
		switch s.captcha.Verify(in.CaptchaID, in.CaptchaAnswer) {
		case util.CaptchaMissing:
			return nil, errcode.Code(errcode.ErrAuthCaptchaMissing)
		case util.CaptchaExpired:
			return nil, errcode.Code(errcode.ErrAuthCaptchaExpired)
		case util.CaptchaWrong:
			return nil, errcode.Code(errcode.ErrAuthCaptchaWrong)
		}
	}

	// Look the user up. Phone takes precedence when both are provided.
	var user models.TLsmGameUser
	q := s.db.WithContext(ctx).Model(&models.TLsmGameUser{})
	switch {
	case in.Phone != "":
		q = q.Where("phone = ? AND phone <> ''", in.Phone)
	default:
		q = q.Where("account = ?", in.Account)
	}
	err := q.First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.Code(errcode.ErrAuthAccountNotFound)
		}
		return nil, errcode.Code(errcode.ErrDB)
	}
	if err := util.VerifyPassword(user.PasswordHash, in.Password); err != nil {
		return nil, errcode.Code(errcode.ErrAuthPasswordWrong)
	}
	now := time.Now()
	s.db.WithContext(ctx).Model(&user).Update("last_login_at", &now)

	// Best-effort daily login bonus. The unique key on
	// (user_id, reward_date) in t_lsm_game_daily_reward makes this idempotent
	// across WS reconnect / page refresh / multiple clients — only one credit
	// per UTC+8 calendar day ever succeeds.
	var dailyRewardClaimed bool
	var dailyRewardAmount int64
	if s.wallets != nil {
		balanceAfter, err := s.wallets.ClaimDailyReward(ctx, user.ID, now)
		if err == nil && balanceAfter > 0 {
			dailyRewardClaimed = true
			dailyRewardAmount = DefaultDailyLoginReward
		}
	}

	resp, err := s.issueTokenAndCookie(&user)
	if err != nil {
		return nil, err
	}
	resp.DailyRewardClaimed = dailyRewardClaimed
	resp.DailyRewardAmount = dailyRewardAmount
	return resp, nil
}

// Refresh re-issues a token for an already-authenticated user and rolls the
// 48h cookie forward.
func (s *AuthService) Refresh(ctx context.Context, userID string) (*AuthResponse, error) {
	var user models.TLsmGameUser
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.Code(errcode.ErrAuthAccountNotFound)
		}
		return nil, errcode.Code(errcode.ErrDB)
	}
	return s.issueTokenAndCookie(&user)
}

// issueTokenAndCookie signs a session, persists it, and builds the
// AES-GCM-encrypted 48h cookie payload alongside the JWT. The user's
// preferred language is echoed back so the client can sync UI locale.
func (s *AuthService) issueTokenAndCookie(user *models.TLsmGameUser) (*AuthResponse, error) {
	userID := user.ID
	ttl := time.Duration(s.cfg.JWT.TTLSeconds) * time.Second
	tok, exp, err := util.IssueToken(userID, s.cfg.JWT.Secret, s.cfg.JWT.Issuer, ttl)
	if err != nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	sess := models.TLsmGameSession{
		ID:        util.NewUUID(),
		UserID:    userID,
		Token:     tok,
		ExpiresAt: exp,
	}
	// Best-effort session persistence — do not fail login if the audit row can't be written.
	_ = s.db.Create(&sess).Error

	// 48h encrypted cookie payload (separate TTL from JWT by design so that
	// the JWT can be shortened later without losing the cookie).
	cookieTTL := time.Duration(s.cfg.Cookie.TTLSeconds) * time.Second
	plain := util.EncodeCookiePayload(userID, cookieTTL, time.Now())
	cookieValue, err := util.EncryptCookie(plain, s.cfg.Cookie.Secret)
	if err != nil {
		return nil, errcode.Code(errcode.ErrInternal)
	}
	lang := user.Language
	if lang == "" {
		lang = "zh-CN"
	}
	return &AuthResponse{
		UserID:       userID,
		Token:        tok,
		ExpiresAt:    exp.Unix(),
		Language:     lang,
		UserType:     user.UserType,
		MyInviteCode: user.MyInviteCode,
		CookieValue:  cookieValue,
	}, nil
}
