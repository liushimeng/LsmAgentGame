// Package service contains the business logic used by api and ws handlers.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/util"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 2026-08-25 安全加固：原 CAPTCHA 旁路白名单（AgentBypassAccounts /
// IsAgentBypassAccount / IsAgentBypassAccountGlobal / AgentBypassAccount 常量）
// 已全部删除。登录对**所有**账号强制 CAPTCHA，不存在任何绕过路径。
// 详见 tmpPlan/安全信息和加固-20260825-02.md（不入库）。

// AuthService is the user-account service.
type AuthService struct {
	db      *gorm.DB
	cfg     *config.Config
	captcha *util.CaptchaStore // may be nil; AuthAPI wires it before any login happens
	wallets *WalletService     // may be nil when constructed via NewAuthService; callers set via SetWalletService
	// guard 是 20260923-01 §4.4 的失败计数 + 递增锁定器。nil = 未接线
	// （security.enabled=false 或单元测试）→ 锁定/延迟/计数全部短路。
	guard *util.LoginGuard
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

// SetLoginGuard attaches the sliding-window lockout guard (20260923-01 §4.4).
// Wired from main.go with util.NewLoginGuard(cfg.Security.LoginGuard); nil
// disables locking entirely (unit tests / security.enabled=false).
func (s *AuthService) SetLoginGuard(g *util.LoginGuard) {
	s.guard = g
}

// dummyPasswordHash 是「计时均一化」专用的包级 bcrypt 常量（cost 10，
// 20260923-01 §4.4 反枚举）。它是用 crypto/rand 生成的 72 字符随机十六进制
// 串的哈希——原文已丢弃，任何真实用户密码都不可能与之匹配。账号不存在
// 路径对它执行一次 VerifyPassword，把「查无此人」与「密码错」两条路径的
// 响应耗时拉平（消除 ~50ms 级 bcrypt 计时侧信道），对外统一返回 10102。
// 这不是密钥硬编码：它不保护任何资源，仅用于耗时装平。
const dummyPasswordHash = "$2a$10$vRRkCTO2ouu5AYGUAa4SgO4fhPZZ82JExGFkGWvh.gWtzsYXSmsQS"

// humanizeDelayCapMs 封顶 credential 失败后的人工化延迟（§4.4：
// min(fail_delay_ms × 窗口内失败数, 1500ms)）。
const humanizeDelayCapMs = 1500

// lockoutError 构造 10501 响应：消息含 "retry after Ns"，前端倒计时解析
// 与 api 层 Retry-After 头共用同一秒数口径（向上取整，最小 1s）。
func lockoutError(d time.Duration) *errcode.Error {
	secs := util.CeilSeconds(d)
	if secs < 1 {
		secs = 1
	}
	return errcode.CodeMsg(errcode.ErrAuthTooManyAttempts,
		fmt.Sprintf("too many failed attempts, retry after %ds", secs))
}

// lockoutRemaining 返回账户键与 IP 键中较长的剩余锁定时长（0 = 未锁定）。
// guard==nil 或键为空时短路返回 0 —— 单元测试（IP=""）与进程内调用零感知。
func (s *AuthService) lockoutRemaining(acctKey, ipKey string) time.Duration {
	if s.guard == nil {
		return 0
	}
	d := s.guard.Remaining(acctKey)
	if r := s.guard.Remaining(ipKey); r > d {
		d = r
	}
	return d
}

// LockoutRetryAfter 供 api 层写 Retry-After 头（20260923-01 §4.4 导出）。
// 复用与 Login 完全一致的键推导；未锁定 / guard 未接线时返回 0。
func (s *AuthService) LockoutRetryAfter(in LoginInput) time.Duration {
	return s.lockoutRemaining(
		util.LoginGuardKeyAccount(in.Account, in.Phone),
		util.LoginGuardKeyIP(in.IP))
}

// noteIPFailure 把验证码类失败计入 IP 维度（§4.4 第 3 步）：
// 只 RecordFailure(ipKey)，不加延迟、不触发账户锁定 —— 真人打错码零感知，
// 爬虫批量试码会在 ip_max_failures 处被锁。空键 / guard 未接线为 no-op。
func (s *AuthService) noteIPFailure(ipKey string) {
	if s.guard != nil && ipKey != "" {
		s.guard.RecordFailure(ipKey)
	}
}

// noteRegisterFailure 把可枚举的注册失败（账号/昵称/邮箱/手机号已占用、
// 邀请码无效）计入 IP 维度（§4.4：ReferrerCode 错 / AccountTaken 等）。
// 只计数、不延迟、不锁账户 —— 与验证码失败同口径；纯校验错误与 DB 故障
// 不计数（不是攻击信号）。
func (s *AuthService) noteRegisterFailure(ipKey string, code int) {
	switch code {
	case errcode.ErrAuthAccountTaken, errcode.ErrAuthNicknameTaken,
		errcode.ErrAuthEmailTaken, errcode.ErrAuthPhoneTaken,
		errcode.ErrAuthReferrerInvalid:
		s.noteIPFailure(ipKey)
	}
}

// uniformCredentialFailure 是「账号不存在」与「密码错」共用的均一化失败
// 出口（§4.4 第 4/5 步）：双键计数 + ctx 感知人工化延迟 + 统一 10102。
func (s *AuthService) uniformCredentialFailure(ctx context.Context, acctKey, ipKey string) *errcode.Error {
	failures := 0
	if s.guard != nil {
		if n := s.guard.RecordFailure(acctKey); n > failures {
			failures = n
		}
		s.guard.RecordFailure(ipKey)
	}
	s.humanizeDelay(ctx, failures)
	return errcode.Code(errcode.ErrAuthPasswordWrong)
}

// humanizeDelay 在 credential 失败后 sleep min(fail_delay_ms×failures, 1500)ms，
// ctx 取消立即返回。cfg==nil（部分单测）/ fail_delay_ms<=0 / guard 未计数
// （failures<=0）三种情况全部关闭 —— 单元测试路径绝不产生 sleep。
func (s *AuthService) humanizeDelay(ctx context.Context, failures int) {
	if s.cfg == nil || failures <= 0 {
		return
	}
	base := s.cfg.Security.LoginGuard.FailDelayMs
	if base <= 0 {
		return
	}
	ms := base * failures
	if ms > humanizeDelayCapMs {
		ms = humanizeDelayCapMs
	}
	t := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
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
	// IP 是注册请求的来源 IP（api 层 c.ClientIP() 注入）。仅用于
	// 20260923-01 §4.4 的 IP 维度锁定键；空串（进程内调用/单测）→ 不可键，
	// 锁定与计数全部短路。
	IP string
}

// LoginInput is the payload for login. Provide Account OR Phone (phone wins
// when both are set), plus Password. All callers must also supply a valid
// CaptchaID/CaptchaAnswer（2026-08-25 起无任何旁路）.
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
	UserID             string          `json:"user_id"`
	Token              string          `json:"token"`
	ExpiresAt          int64           `json:"expires_at"`
	Language           string          `json:"language"`
	UserType           models.UserType `json:"user_type"`
	MyInviteCode       string          `json:"my_invite_code"`
	CookieValue        string          `json:"-"`
	DailyRewardClaimed bool            `json:"daily_reward_claimed"`
	DailyRewardAmount  int64           `json:"daily_reward_amount"`
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

	// 20260923-01 §4.4 — 注册入口锁定检查（acct/ip 双键）。越阈直接 10501，
	// 不触 DB。guard==nil / IP==""（进程内调用与单测）短路。
	regAcctKey := util.LoginGuardKeyAccount(in.Account, in.Phone)
	regIPKey := util.LoginGuardKeyIP(in.IP)
	if d := s.lockoutRemaining(regAcctKey, regIPKey); d > 0 {
		return nil, lockoutError(d)
	}

	// Uniqueness checks (read-only — the unique index is the source of truth,
	// but failing fast with a clear error code keeps the API clean).
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
		Where("account = ?", in.Account).Count(&count).Error; err != nil {
		return nil, errcode.Code(errcode.ErrDB)
	}
	if count > 0 {
		s.noteRegisterFailure(regIPKey, errcode.ErrAuthAccountTaken)
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
		s.noteRegisterFailure(regIPKey, errcode.ErrAuthNicknameTaken)
		return nil, errcode.Code(errcode.ErrAuthNicknameTaken)
	}

	if in.Email != "" {
		if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
			Where("email = ? AND email <> ''", in.Email).Count(&count).Error; err != nil {
			return nil, errcode.Code(errcode.ErrDB)
		}
		if count > 0 {
			s.noteRegisterFailure(regIPKey, errcode.ErrAuthEmailTaken)
			return nil, errcode.Code(errcode.ErrAuthEmailTaken)
		}
	}
	if in.Phone != "" {
		if err := s.db.WithContext(ctx).Model(&models.TLsmGameUser{}).
			Where("phone = ? AND phone <> ''", in.Phone).Count(&count).Error; err != nil {
			return nil, errcode.Code(errcode.ErrDB)
		}
		if count > 0 {
			s.noteRegisterFailure(regIPKey, errcode.ErrAuthPhoneTaken)
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
		s.noteRegisterFailure(regIPKey, ce.Code)
		return nil, ce
	}
	// 注册成功 → 清除账户键与 IP 键的失败计数（§4.4）。
	if s.guard != nil {
		s.guard.Reset(regAcctKey)
		s.guard.Reset(regIPKey)
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
//   - Every account must supply matching CaptchaID/CaptchaAnswer (otherwise
//     10301/10302/10303). 2026-08-25 起无任何旁路。
//   - bcrypt password verification is always required, even for the bypass.
//
// 20260923-01 §4.4 加固（guard==nil / IP=="" 时全部短路，行为与旧版一致）：
//  1. 入口锁定检查：账户键或 IP 键处于锁定期 → 10501（不触 DB/bcrypt）。
//  2. 验证码校验改用 VerifyWithIP（bind_ip 且两侧 IP 非空才生效）；验证码
//     失败只计入 IP 维度，不加延迟、不锁账户。
//  3. 账号不存在 → 对包级 dummy bcrypt 哈希执行一次 VerifyPassword 拉平
//     计时，对外统一返回 10102（内部日志区分 account_not_found）。
//  4. credential 失败（不存在/密码错）→ 双键计数 + ctx 感知人工化延迟
//     min(fail_delay_ms×窗口内失败数, 1500)ms。
//  5. 成功 → Reset 双键。
func (s *AuthService) Login(ctx context.Context, in LoginInput) (*AuthResponse, error) {
	in.Account = strings.TrimSpace(in.Account)
	in.Phone = strings.TrimSpace(in.Phone)
	if in.Account == "" && in.Phone == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	if in.Password == "" {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}

	// ── 锁定检查（§4.4 第 2 步）：越阈直接 10501，不做任何 DB/bcrypt 操作 ──
	acctKey := util.LoginGuardKeyAccount(in.Account, in.Phone)
	ipKey := util.LoginGuardKeyIP(in.IP)
	if d := s.lockoutRemaining(acctKey, ipKey); d > 0 {
		return nil, lockoutError(d)
	}

	// CAPTCHA gate — 全员强制，无旁路（2026-08-25 安全加固）。
	if s.captcha == nil {
		return nil, errcode.Code(errcode.ErrAuthCaptchaMissing)
	}
	// 20260923-01 §4.6：签发/校验 IP 绑定。任一侧 IP 为空（单测、进程内
	// bot、bind_ip=false）→ 与旧 Verify 完全等价。
	switch s.captcha.VerifyWithIP(in.CaptchaID, in.CaptchaAnswer, in.IP) {
	case util.CaptchaMissing:
		s.noteIPFailure(ipKey)
		return nil, errcode.Code(errcode.ErrAuthCaptchaMissing)
	case util.CaptchaExpired:
		s.noteIPFailure(ipKey)
		return nil, errcode.Code(errcode.ErrAuthCaptchaExpired)
	case util.CaptchaWrong:
		s.noteIPFailure(ipKey)
		return nil, errcode.Code(errcode.ErrAuthCaptchaWrong)
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
			// ── 反枚举 + 计时均一化（§4.4 第 4 步）──
			// 对 dummy 哈希执行一次真实 bcrypt 比较，把本路径耗时拉到与
			// 「密码错」一致；对外统一 10102，内部日志保留 account_not_found。
			_ = util.VerifyPassword(dummyPasswordHash, in.Password)
			logger.L().Info("login failed: account_not_found（对外统一 10102，20260923-01 §4.4）",
				zap.String("account", in.Account),
				zap.String("phone", in.Phone),
				zap.String("client_ip", in.IP))
			return nil, s.uniformCredentialFailure(ctx, acctKey, ipKey)
		}
		return nil, errcode.Code(errcode.ErrDB)
	}
	if err := util.VerifyPassword(user.PasswordHash, in.Password); err != nil {
		// ── 密码错 → 与账号不存在同一出口（§4.4 第 5 步）──
		return nil, s.uniformCredentialFailure(ctx, acctKey, ipKey)
	}
	// 登录成功 → 清除账户键与 IP 键（§4.4 第 6 步）。
	if s.guard != nil {
		s.guard.Reset(acctKey)
		s.guard.Reset(ipKey)
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
