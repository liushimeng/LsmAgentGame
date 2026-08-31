// Package db initializes the GORM connection and runs migrations.
package db

import (
	"fmt"
	"strings"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/util"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Init opens the database, configures the connection pool, and migrates the schema.
func Init(cfg *config.Config) (*gorm.DB, error) {
	gormLog := gormlogger.New(
		zapGormWriter{logger: logger.L()},
		gormlogger.Config{
			SlowThreshold: 200 * time.Millisecond,
			LogLevel:      gormlogger.Warn,
			Colorful:      false,
		},
	)

	gormDB, err := gorm.Open(mysql.Open(cfg.DB.DSN()), &gorm.Config{
		Logger: gormLog,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DB.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.DB.ConnMaxLifetimeSeconds) * time.Second)

	// Backfill personal invite codes BEFORE AutoMigrate adds the unique index
	// on my_invite_code. Existing rows would otherwise share an empty-string
	// value and collide when the unique index is created.
	if err := backfillMyInviteCodes(gormDB); err != nil {
		return nil, err
	}
	if err := backfillNicknames(gormDB); err != nil {
		return nil, err
	}
	// Backfill the new role column on t_lsm_game_player BEFORE the NOT NULL
	// constraint is applied by AutoMigrate.
	if err := backfillPlayerRole(gormDB); err != nil {
		return nil, err
	}
	// Backfill the new bot-identity columns on t_lsm_game_user BEFORE
	// AutoMigrate narrows them with NOT NULL / unique constraints.
	if err := backfillIsBot(gormDB); err != nil {
		return nil, err
	}

	if err := gormDB.AutoMigrate(
		&models.TLsmGameUser{},
		&models.TLsmGameSession{},
		&models.TLsmGameRoom{},
		&models.TLsmGamePlayer{},
		&models.TLsmGameChatMessage{},
		&models.TLsmGameWallet{},
		&models.TLsmGameWalletTx{},
		&models.TLsmGameDailyReward{},
		// Model management tables (kind-skipping-moth §2.6). Added in 2026-07.
		&models.TLsmGameLlmProvider{},
		&models.TLsmGameModelGameLog{},
		&models.TLsmGameModelChatMessage{},
		&models.TLsmGameModelAction{},
		&models.TLsmGameKV{},
		// 2026-07-14 §135 — super-admin daily grant dedup table.
		&models.TLsmGameAdminGrant{},
		// 2026-07-20 §131 — Agent 持久化记忆表(狼人杀,一模型一行)。
		&models.TLsmGameAgentMemory{},
		// 2026-08-11 §20260811-05 U1 — Agent 玩家行为画像表(一模型×一人类一行)。
		&models.TLsmGameAgentPlayerProfile{},
		// 2026-07-21 道具系统 — 道具目录 + 使用日志表。
		&models.TLsmGameProp{},
		&models.TLsmGamePropUsageLog{},
		// §20260812-02 U3 — 观众押注竞猜表。
		&models.TLsmGameSpectatorBet{},
		// 2026-08-31 §20260831-08 — 辩论比赛持久化:房间记录 / 发言 /
		// 评审 / 模型胜率统计 / 自定义辩题(写入方 game/debate/persistence.go)。
		&models.TLsmGameDebateRoom{},
		&models.TLsmGameDebateSpeech{},
		&models.TLsmGameDebateScore{},
		&models.TLsmGameDebateModelStats{},
		&models.TLsmGameDebateTopic{},
	); err != nil {
		return nil, err
	}

	// Replace the loose per-column indexes on t_lsm_game_player with a single
	// composite uniqueness index on (room_id, user_id). GORM's AutoMigrate would
	// only emit non-unique indexes (since the model has no declared uniqueness),
	// so the strict constraint is added explicitly here. Spectators and players
	// share this index — RoomService guarantees they never overlap for one user.
	if err := ensurePlayerUniqueIndex(gormDB); err != nil {
		return nil, err
	}
	// 2026-08-20 §P0-2 — ref_id 列宽手工迁移:模型已改 varchar(128),但
	// AutoMigrate 不会变更已存在列的类型/宽度(线上仍是 char(36)),德扑结算
	// 写 refID = roomID+":"+handNum(38+ 字符)必然 ERROR 1406。
	if err := ensureWalletTxRefIDWidth(gormDB); err != nil {
		return nil, err
	}
	logger.L().Info("db migrated",
		zap.String("host", cfg.DB.Host),
		zap.Int("port", cfg.DB.Port),
		zap.String("name", cfg.DB.Name))
	return gormDB, nil
}

// backfillMyInviteCodes ensures every existing user row has a unique personal
// invite code before the unique index on my_invite_code is created. It is a
// no-op on a fresh database (table not yet present) and idempotent on a
// migrated one (only rows with NULL/empty codes are touched).
func backfillMyInviteCodes(gormDB *gorm.DB) error {
	mig := gormDB.Migrator()
	// Nothing to backfill if the table doesn't exist yet — AutoMigrate will
	// create it fresh with no pre-existing rows to collide.
	if !mig.HasTable(&models.TLsmGameUser{}) {
		return nil
	}
	if !mig.HasColumn(&models.TLsmGameUser{}, "MyInviteCode") {
		// Add the column WITHOUT the unique index first so we can fill it in
		// before AutoMigrate attempts to add the unique constraint.
		if err := gormDB.Exec(
			"ALTER TABLE t_lsm_game_user ADD COLUMN my_invite_code varchar(32) NULL",
		).Error; err != nil {
			return err
		}
	}

	// Find rows that still need a code.
	var ids []string
	if err := gormDB.Model(&models.TLsmGameUser{}).
		Where("my_invite_code IS NULL OR my_invite_code = ''").
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		code, err := util.NewInviteCode()
		if err != nil {
			return err
		}
		if err := gormDB.Model(&models.TLsmGameUser{}).
			Where("id = ?", id).
			Update("my_invite_code", code).Error; err != nil {
			return err
		}
	}
	return nil
}

// backfillNicknames ensures every existing user row has a nickname (defaulting
// to their account name) before the unique index on nickname is created.
// It is a no-op on a fresh database and idempotent on a migrated one.
func backfillNicknames(gormDB *gorm.DB) error {
	mig := gormDB.Migrator()
	if !mig.HasTable(&models.TLsmGameUser{}) {
		return nil
	}
	if !mig.HasColumn(&models.TLsmGameUser{}, "Nickname") {
		if err := gormDB.Exec(
			"ALTER TABLE t_lsm_game_user ADD COLUMN nickname varchar(64) NULL",
		).Error; err != nil {
			return err
		}
	}
	// Backfill NULL/empty nicknames with the account name.
	if err := gormDB.Model(&models.TLsmGameUser{}).
		Where("nickname IS NULL OR nickname = ''").
		Update("nickname", gorm.Expr("account")).Error; err != nil {
		return err
	}
	return nil
}

// zapGormWriter adapts zap to gorm's logger.Writer interface.
type zapGormWriter struct {
	logger *zap.Logger
}

func (z zapGormWriter) Printf(format string, args ...any) {
	z.logger.Sugar().Infof(format, args...)
}

// backfillPlayerRole assigns Role='player' to every existing row in
// t_lsm_game_player that doesn't yet have a role set, so the NOT NULL
// constraint that AutoMigrate adds can be applied without errors. Idempotent.
func backfillPlayerRole(gormDB *gorm.DB) error {
	mig := gormDB.Migrator()
	if !mig.HasTable(&models.TLsmGamePlayer{}) {
		return nil
	}
	if mig.HasColumn(&models.TLsmGamePlayer{}, "Role") {
		// Column already exists — just normalize empties to "player" so any
		// legacy "" or NULL value gets a sensible default.
		return gormDB.Model(&models.TLsmGamePlayer{}).
			Where("role IS NULL OR role = ''").
			Update("role", models.PlayerRolePlayer).Error
	}
	// Column absent: add it WITHOUT the NOT NULL clause first so we can
	// populate every row, then AutoMigrate will tighten the constraint.
	if err := gormDB.Exec(
		"ALTER TABLE t_lsm_game_player ADD COLUMN role varchar(16) NULL",
	).Error; err != nil {
		return err
	}
	return gormDB.Model(&models.TLsmGamePlayer{}).
		Where("role IS NULL OR role = ''").
		Update("role", models.PlayerRolePlayer).Error
}

// backfillIsBot ensures every existing t_lsm_game_user row has the bot-identity
// columns populated with their safe defaults before AutoMigrate tightens them
// to NOT NULL / indexed. Idempotent. The model defines:
//
//	is_bot                  bool   default false
//	bot_provider_id         char(36) default '' (indexed)
//	linked_provider_account varchar(64) default ''
//
// Existing rows must end up at the safe defaults; the empty-string default
// for bot_provider_id matches the model so the new index sees no NULLs.
func backfillIsBot(gormDB *gorm.DB) error {
	mig := gormDB.Migrator()
	if !mig.HasTable(&models.TLsmGameUser{}) {
		return nil
	}

	// is_bot: add the column without NOT NULL first if missing, normalize any
	// NULL/legacy values to false, then AutoMigrate will tighten it.
	if !mig.HasColumn(&models.TLsmGameUser{}, "IsBot") {
		if err := gormDB.Exec(
			"ALTER TABLE t_lsm_game_user ADD COLUMN is_bot tinyint(1) NULL DEFAULT 0",
		).Error; err != nil {
			return err
		}
	}
	if err := gormDB.Exec(
		"UPDATE t_lsm_game_user SET is_bot = 0 WHERE is_bot IS NULL",
	).Error; err != nil {
		return err
	}

	// bot_provider_id: add the column without NOT NULL first if missing,
	// normalize any NULL values to '' (empty string sentinel). The model
	// declares default:'' so the index on this column will not collide.
	if !mig.HasColumn(&models.TLsmGameUser{}, "BotProviderID") {
		if err := gormDB.Exec(
			"ALTER TABLE t_lsm_game_user ADD COLUMN bot_provider_id char(36) NULL DEFAULT ''",
		).Error; err != nil {
			return err
		}
	}
	if err := gormDB.Exec(
		"UPDATE t_lsm_game_user SET bot_provider_id = '' WHERE bot_provider_id IS NULL",
	).Error; err != nil {
		return err
	}

	// linked_provider_account: same pattern as bot_provider_id.
	if !mig.HasColumn(&models.TLsmGameUser{}, "LinkedProviderAccount") {
		if err := gormDB.Exec(
			"ALTER TABLE t_lsm_game_user ADD COLUMN linked_provider_account varchar(64) NULL DEFAULT ''",
		).Error; err != nil {
			return err
		}
	}
	if err := gormDB.Exec(
		"UPDATE t_lsm_game_user SET linked_provider_account = '' WHERE linked_provider_account IS NULL",
	).Error; err != nil {
		return err
	}

	return nil
}

// ensurePlayerUniqueIndex makes (room_id, user_id) unique. We add the index
// manually (rather than relying on AutoMigrate) so it shows up even on legacy
// databases whose model declaration didn't include the role column. Idempotent.
func ensurePlayerUniqueIndex(gormDB *gorm.DB) error {
	mig := gormDB.Migrator()
	if !mig.HasTable(&models.TLsmGamePlayer{}) {
		return nil
	}
	// Probe via information_schema so we don't need a tagged struct field.
	var exists int64
	if err := gormDB.Raw(
		`SELECT COUNT(*) FROM information_schema.statistics
		 WHERE table_schema = DATABASE()
		   AND table_name   = 't_lsm_game_player'
		   AND index_name   = 'uk_room_user'`,
	).Scan(&exists).Error; err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	return gormDB.Exec(
		`ALTER TABLE t_lsm_game_player
		 ADD UNIQUE KEY uk_room_user (room_id, user_id)`,
	).Error
}

// ensureWalletTxRefIDWidth widens t_lsm_game_wallet_tx.ref_id to varchar(128)
// (2026-08-20 §P0-2). The model already declares varchar(128)
// (models/t_lsm_game_wallet_tx.go), but GORM AutoMigrate never alters the
// type/width of an existing column — a legacy database keeps char(36) and the
// texasholdem settlement refID ("<36-char roomID>:<handNum>", 38+ chars) fails
// with ERROR 1406 under STRICT_TRANS_TABLES.
//
// Idempotent: skips when the table is absent (AutoMigrate creates it fresh
// with the correct width) and when the column is already varchar(>=128).
// The nullability semantics of the existing column are preserved (legacy
// databases have NULL DEFAULT ”). Indexes on the column are kept by MySQL
// across MODIFY COLUMN.
func ensureWalletTxRefIDWidth(gormDB *gorm.DB) error {
	mig := gormDB.Migrator()
	if !mig.HasTable(&models.TLsmGameWalletTx{}) {
		return nil
	}
	var cols []struct {
		DataType  string `gorm:"column:DATA_TYPE"`
		MaxLength *int64 `gorm:"column:CHARACTER_MAXIMUM_LENGTH"`
		Nullable  string `gorm:"column:IS_NULLABLE"`
	}
	if err := gormDB.Raw(
		`SELECT DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, IS_NULLABLE
		 FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE()
		   AND TABLE_NAME   = 't_lsm_game_wallet_tx'
		   AND COLUMN_NAME  = 'ref_id'`,
	).Scan(&cols).Error; err != nil {
		return err
	}
	if len(cols) == 0 {
		// 列不存在 —— 交给 AutoMigrate 按模型定义补齐。
		return nil
	}
	c := cols[0]
	if strings.EqualFold(c.DataType, "varchar") && c.MaxLength != nil && *c.MaxLength >= 128 {
		return nil
	}
	// 保持现有 nullability 语义(2026-08-20 实测线上为 NULL DEFAULT '')。
	nullClause := "NULL"
	if strings.EqualFold(c.Nullable, "NO") {
		nullClause = "NOT NULL"
	}
	fromDesc := c.DataType
	if c.MaxLength != nil {
		fromDesc = fmt.Sprintf("%s(%d)", c.DataType, *c.MaxLength)
	}
	logger.L().Info("widening t_lsm_game_wallet_tx.ref_id",
		zap.String("from", fromDesc),
		zap.String("to", "varchar(128)"),
		zap.String("null", nullClause))
	if err := gormDB.Exec(fmt.Sprintf(
		"ALTER TABLE t_lsm_game_wallet_tx MODIFY COLUMN ref_id varchar(128) %s DEFAULT ''",
		nullClause,
	)).Error; err != nil {
		return err
	}
	logger.L().Info("t_lsm_game_wallet_tx.ref_id widened to varchar(128)")
	return nil
}
