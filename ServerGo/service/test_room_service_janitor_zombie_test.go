// BUG-R219 回归测试 — JanitorSweepZombiePlaying 的僵尸房间判定 (2026-08-01)。
//
// 缺陷背景(报告 20260801_061438 / 20260801_083235):
//
//   - 旧主路径谓词 `status='playing' AND updated_at < now-4h`。
//     `updated_at` 是 `gorm:"autoUpdateTime"` 的**行写入时间戳**,不是**对局
//     进度信号** —— watchdog 每跳过一轮就 touch 一次,卡死房间因此永远刷不出
//     4h 阈值(实测房间 ce288893 在大厅存活 15h+)。
//   - cea6126 追加的二次扫描用了与主路径**完全相同**的谓词 + 相同 cutoff,
//     而主路径的 UPDATE 已经在前一条语句把所有匹配行翻成 'over',所以二次
//     扫描的 Find 恒返回 0 行、`AND status='playing'` 复核也恒失败 —— 死代码。
//
// 修复:改用 `created_at`(autoCreateTime,建行后不再改写)作为"这局跑了
// 多久"的进度信号,并把二次扫描重写成一条**谓词不同、真实可达**的规则
// (无人观战的纯 bot 狼人杀局提前到 zombieMaxAge/4 回收)。
//
// 测试策略:本仓库 service 包没有 sqlite / 内存 DB 基建(go.mod 只有
// gorm.io/driver/mysql),因此按任务约定**直接驱动生产判定函数**
// zombieRuleFor —— 它是 JanitorSweepZombiePlaying 逐行判定的唯一事实来源,
// 不是测试里复刻的副本(避免 orphanRouter 那种"测试副本会漂移"的隐患)。
package service

import (
	"context"
	"testing"
	"time"

	"LsmAgentGame/models"
)

// zombieRuleForOldPredicate 复刻**修复前**的判定(updated_at + 相同 cutoff
// 的二次扫描),仅供双向验证使用:下面的
// TestZombieSweep_R219_OldPredicateFailsOnTouchedRow 断言它在核心回归场景
// 下**放过**了僵尸房间,证明这些测试确实能抓到旧缺陷。
func zombieRuleForOldPredicate(room models.TLsmGameRoom, now time.Time,
	zombieMaxAge time.Duration, hubEmpty bool) string {
	cutoff := now.Add(-zombieMaxAge)
	if room.Status != "playing" {
		return zombieRuleNone
	}
	// 旧主路径:updated_at < cutoff。
	if room.UpdatedAt.Before(cutoff) {
		return zombieRuleMaxDuration
	}
	// 旧二次扫描:同一谓词 + hub 空 + werewolf。谓词与主路径完全相同,
	// 主路径不命中 ⟹ 这里也不可能命中(这正是"死代码"的含义)。
	if room.UpdatedAt.Before(cutoff) && room.GameKind == "werewolf" && hubEmpty {
		return zombieRuleAbandoned
	}
	return zombieRuleNone
}

// TestZombieSweep_R219_SweepsRoomPastDurationCeilingDespiteFreshUpdatedAt 是
// **核心回归**:一个 15 小时前创建、仍 status='playing' 的房间,即使
// watchdog 在 3 秒前刚 touch 过 updated_at,也必须被回收。
func TestZombieSweep_R219_SweepsRoomPastDurationCeilingDespiteFreshUpdatedAt(t *testing.T) {
	now := time.Now()
	room := models.TLsmGameRoom{
		ID:        "ce288893",
		GameKind:  "werewolf",
		Status:    "playing",
		CreatedAt: now.Add(-15 * time.Hour),  // 15h 前开局
		UpdatedAt: now.Add(-3 * time.Second), // watchdog 3s 前刚 touch
	}
	// hub 上还有一个观众挂着(hubEmpty=false)+ 有真人行 —— 刻意把规则 2
	// 的两个条件都做成"不满足",证明规则 1 完全不依赖它们。
	got := zombieRuleFor(room, now, 4*time.Hour, false, 3)
	if got != zombieRuleMaxDuration {
		t.Fatalf("15h 前开局的 playing 房间必须按 %q 回收,got %q(updated_at 被 watchdog 刷新不应豁免)",
			zombieRuleMaxDuration, got)
	}
}

// TestZombieSweep_R219_OldPredicateFailsOnTouchedRow 是**双向验证的反向半边**:
// 同一行数据喂给修复前的谓词,必须放过 —— 证明上面的回归测试针对的是真实
// 缺陷,而不是恒真断言。
func TestZombieSweep_R219_OldPredicateFailsOnTouchedRow(t *testing.T) {
	now := time.Now()
	room := models.TLsmGameRoom{
		ID:        "ce288893",
		GameKind:  "werewolf",
		Status:    "playing",
		CreatedAt: now.Add(-15 * time.Hour),
		UpdatedAt: now.Add(-3 * time.Second),
	}
	if got := zombieRuleForOldPredicate(room, now, 4*time.Hour, true); got != zombieRuleNone {
		t.Fatalf("修复前谓词本应放过被 touch 的僵尸房间(这是缺陷本身),got %q —— "+
			"若这里命中说明复刻的旧谓词写错了,回归测试失去意义", got)
	}
	// 修复后的谓词对同一行必须命中 —— 双向验证闭环。
	if got := zombieRuleFor(room, now, 4*time.Hour, true, 0); got == zombieRuleNone {
		t.Fatal("修复后谓词必须命中同一行")
	}
}

// TestZombieSweep_HealthyRecentPlayingRoomIsNotSwept:刚开局的健康房间
// 绝不能被扫。10 分钟前创建 + updated_at 也很新。
func TestZombieSweep_HealthyRecentPlayingRoomIsNotSwept(t *testing.T) {
	now := time.Now()
	room := models.TLsmGameRoom{
		ID:        "r-healthy",
		GameKind:  "werewolf",
		Status:    "playing",
		CreatedAt: now.Add(-10 * time.Minute),
		UpdatedAt: now.Add(-5 * time.Second),
	}
	if got := zombieRuleFor(room, now, 4*time.Hour, true, 0); got != zombieRuleNone {
		t.Fatalf("10 分钟前开局的健康房间不应被回收,got %q", got)
	}
}

// TestZombieSweep_AbandonedBotRoomSweptInSecondaryWindow:规则 2 的正向用例。
// 90 分钟前创建的纯 bot 狼人杀房(> 4h/4 = 1h),hub 零连接 + 零真人行 → 回收。
// 这同时证明二次扫描现在处于一段主路径覆盖不到的**真实区间**(1h..4h)。
func TestZombieSweep_AbandonedBotRoomSweptInSecondaryWindow(t *testing.T) {
	now := time.Now()
	room := models.TLsmGameRoom{
		ID:        "r-botonly",
		GameKind:  "werewolf",
		Status:    "playing",
		CreatedAt: now.Add(-90 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Second),
	}
	// 规则 1 不该命中(90min < 4h),必须是规则 2。
	if zombieExceedsMaxDuration(room.CreatedAt, now, 4*time.Hour) {
		t.Fatal("90min < 4h,规则 1 不应命中 —— 本用例失去对规则 2 的覆盖")
	}
	if got := zombieRuleFor(room, now, 4*time.Hour, true, 0); got != zombieRuleAbandoned {
		t.Fatalf("无人观战的纯 bot 房应按 %q 回收,got %q", zombieRuleAbandoned, got)
	}
}

// TestZombieSweep_AbandonedRuleSkipsWhenSpectatorConnected:规则 2 的 hub 守卫。
// 同一间房,只要 hub 上还有连接(观众/掉线重连中的真人)就放过 —— 误判方向
// 只会导致跳过,最终由规则 1 在 4h 兜底。
func TestZombieSweep_AbandonedRuleSkipsWhenSpectatorConnected(t *testing.T) {
	now := time.Now()
	room := models.TLsmGameRoom{
		ID:        "r-botonly",
		GameKind:  "werewolf",
		Status:    "playing",
		CreatedAt: now.Add(-90 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Second),
	}
	if got := zombieRuleFor(room, now, 4*time.Hour, false, 0); got != zombieRuleNone {
		t.Fatalf("hub 上有连接时规则 2 应放过,got %q", got)
	}
}

// TestZombieSweep_AbandonedRuleSkipsWhenHumanPlayerRowsExist:规则 2 的 DB 守卫。
// bot 座位不持有 hub 连接,IsRoomEmpty 对纯 bot 房恒为 true(假"空"),
// 所以不能只靠 hub —— 还要求 t_lsm_game_player 里零非 agent 行。
func TestZombieSweep_AbandonedRuleSkipsWhenHumanPlayerRowsExist(t *testing.T) {
	now := time.Now()
	room := models.TLsmGameRoom{
		ID:        "r-mixed",
		GameKind:  "werewolf",
		Status:    "playing",
		CreatedAt: now.Add(-90 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Second),
	}
	if got := zombieRuleFor(room, now, 4*time.Hour, true, 1); got != zombieRuleNone {
		t.Fatalf("房内存在真人玩家行时规则 2 应放过,got %q", got)
	}
	// humanRows = -1 表示 COUNT 探测失败,同样必须保守放过。
	if got := zombieRuleFor(room, now, 4*time.Hour, true, -1); got != zombieRuleNone {
		t.Fatalf("COUNT 探测失败(-1)时应保守放过,got %q", got)
	}
}

// TestZombieSweep_AbandonedRuleAppliesToWerewolfAndTexasholdem:2026-08-20 §20260819-02
// 扩展:`zombieAbandonedCandidate` 把德州扑克也纳入「纯 bot 房间提前回收」窗口
// (纯 bot 房间可在 hub vacancy 之外无限打,需 janitor 兜底)。其它 3 款
//(xiangqi/chess/junqi/doudizhu)以分钟计自然收敛,不走提前回收窗口(仍受规则 1
// 的 4h 上限约束)。
func TestZombieSweep_AbandonedRuleAppliesToWerewolfAndTexasholdem(t *testing.T) {
	now := time.Now()
	// 提前回收:werewolf + texasholdem 在 zombieAbandonedAge(默认 1h) 之前的
	// playing 房间 + hub 空 + 0 非 agent 行 -> abandoned_bot_room。
	for _, kind := range []string{"werewolf", "texasholdem"} {
		room := models.TLsmGameRoom{
			ID:        "r-" + kind,
			GameKind:  kind,
			Status:    "playing",
			CreatedAt: now.Add(-90 * time.Minute),
			UpdatedAt: now.Add(-1 * time.Second),
		}
		if got := zombieRuleFor(room, now, 4*time.Hour, true, 0); got != zombieRuleAbandoned {
			t.Fatalf("%s 应走纯 bot 提前回收窗口,got %q", kind, got)
		}
	}
	// 不走提前回收:xiangqi/chess/junqi/doudizhu 在同一时间窗内,游戏以分钟
	// 计自然收敛,继续按规则 1 的 4h 上限走(本测试用 90min < 4h 验证不命中)。
	for _, kind := range []string{"xiangqi", "chess", "junqi", "doudizhu"} {
		room := models.TLsmGameRoom{
			ID:        "r-" + kind,
			GameKind:  kind,
			Status:    "playing",
			CreatedAt: now.Add(-90 * time.Minute),
			UpdatedAt: now.Add(-1 * time.Second),
		}
		if got := zombieRuleFor(room, now, 4*time.Hour, true, 0); got != zombieRuleNone {
			t.Fatalf("%s 不应走提前回收窗口,got %q", kind, got)
		}
	}
	// 但超过 4h 上限时仍要被规则 1 回收(全局,所有游戏一致)。
	room := models.TLsmGameRoom{
		ID:        "r-overflow",
		GameKind:  "texasholdem",
		Status:    "playing",
		CreatedAt: now.Add(-5 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Second),
	}
	if got := zombieRuleFor(room, now, 4*time.Hour, true, 0); got != zombieRuleMaxDuration {
		t.Fatalf("texasholdem 超过 4h 上限仍应按 %q 回收,got %q", zombieRuleMaxDuration, got)
	}
}

// TestZombieSweep_NonPlayingStatusNeverSwept:防御性 —— open / over 状态
// 由 JanitorSweep / JanitorSweepStale 负责,本 sweep 不越界。
func TestZombieSweep_NonPlayingStatusNeverSwept(t *testing.T) {
	now := time.Now()
	for _, st := range []string{"open", "over", "filling", ""} {
		room := models.TLsmGameRoom{
			ID:        "r-" + st,
			GameKind:  "werewolf",
			Status:    st,
			CreatedAt: now.Add(-99 * time.Hour),
			UpdatedAt: now.Add(-99 * time.Hour),
		}
		if got := zombieRuleFor(room, now, 4*time.Hour, true, 0); got != zombieRuleNone {
			t.Fatalf("status=%q 不应被 zombie sweep 处理,got %q", st, got)
		}
	}
}

// TestZombieSweep_ZeroCreatedAtIsSkipped:历史行 created_at 可能为零值
// (老 schema / 手工插入)。零值无法判定年龄 → 保守放过,交给
// JanitorSweepStale 的 30 分钟兜底(与 filling reaper 同策略)。
func TestZombieSweep_ZeroCreatedAtIsSkipped(t *testing.T) {
	now := time.Now()
	room := models.TLsmGameRoom{
		ID:       "r-legacy",
		GameKind: "werewolf",
		Status:   "playing",
		// CreatedAt 零值
		UpdatedAt: now.Add(-99 * time.Hour),
	}
	if got := zombieRuleFor(room, now, 4*time.Hour, true, 0); got != zombieRuleNone {
		t.Fatalf("created_at 零值应保守放过,got %q", got)
	}
}

// TestZombieSweep_NewPredicateIsSupersetOfOld 是**回收面积不缩小**的证明。
// GORM 建行时同时写 created_at 与 updated_at,故恒有 updated_at >= created_at,
// 于是旧谓词 `updated_at < cutoff` ⟹ 新谓词 `created_at < cutoff`。
// 换言之:旧逻辑能扫到的行,新逻辑一定能扫到。这条不变式是"删除旧规则
// 而非并存"的依据(§130 不留死代码)。
func TestZombieSweep_NewPredicateIsSupersetOfOld(t *testing.T) {
	now := time.Now()
	maxAge := 4 * time.Hour
	// 遍历一组 (createdAgo, extraIdle) 组合,保证 updated_at >= created_at。
	for _, createdAgo := range []time.Duration{
		1 * time.Minute, 30 * time.Minute, 3 * time.Hour,
		4*time.Hour + time.Second, 15 * time.Hour, 99 * time.Hour,
	} {
		for _, sinceUpdate := range []time.Duration{
			0, time.Second, 10 * time.Minute, 5 * time.Hour,
		} {
			if sinceUpdate > createdAgo {
				continue // updated_at 不可能早于 created_at
			}
			room := models.TLsmGameRoom{
				ID:        "r",
				GameKind:  "werewolf",
				Status:    "playing",
				CreatedAt: now.Add(-createdAgo),
				UpdatedAt: now.Add(-sinceUpdate),
			}
			oldHit := zombieRuleForOldPredicate(room, now, maxAge, true) != zombieRuleNone
			newHit := zombieRuleFor(room, now, maxAge, true, 0) != zombieRuleNone
			if oldHit && !newHit {
				t.Fatalf("回收面积缩小: created=%v ago / updated=%v ago 旧谓词命中但新谓词放过",
					createdAgo, sinceUpdate)
			}
		}
	}
}

// TestZombieSweep_AbandonedAgeWindow 验证提前回收窗口的缩放与下限:
// zombieMaxAge/4,不低于 15min,不超过 zombieMaxAge 本身。
// 目的:调用方(main.go 传 4h)改小参数时不会把窗口压到几分钟误杀新房。
func TestZombieSweep_AbandonedAgeWindow(t *testing.T) {
	cases := []struct {
		maxAge time.Duration
		want   time.Duration
	}{
		{4 * time.Hour, time.Hour},           // 默认:4h/4 = 1h
		{8 * time.Hour, 2 * time.Hour},       // 缩放生效
		{20 * time.Minute, 15 * time.Minute}, // 20/4=5min → 抬到下限 15min
		{10 * time.Minute, 10 * time.Minute}, // 下限 15min > maxAge → 收敛到 maxAge
		{0, 0},                               // 关闭
		{-time.Hour, 0},                      // 非法输入
	}
	for _, c := range cases {
		if got := zombieAbandonedAge(c.maxAge); got != c.want {
			t.Fatalf("zombieAbandonedAge(%v) = %v, 期望 %v", c.maxAge, got, c.want)
		}
	}
}

// TestZombieSweep_ZeroMaxAgeDisablesRule:zombieMaxAge<=0 = 整个 sweep 关闭。
// RunJanitor 里已有 `if zombieMaxAge > 0` 守卫,这里是函数自身的纵深防御。
func TestZombieSweep_ZeroMaxAgeDisablesRule(t *testing.T) {
	now := time.Now()
	room := models.TLsmGameRoom{
		ID:        "r",
		GameKind:  "werewolf",
		Status:    "playing",
		CreatedAt: now.Add(-99 * time.Hour),
		UpdatedAt: now.Add(-99 * time.Hour),
	}
	if got := zombieRuleFor(room, now, 0, true, 0); got != zombieRuleNone {
		t.Fatalf("zombieMaxAge=0 应关闭回收,got %q", got)
	}
}

// TestJanitorSweepZombiePlaying_NilDB:nil DB 时必须零值返回而非 panic
// (与 BootCleanupOrphanedAgentRooms / JanitorSweepStaleFilling 同策略)。
func TestJanitorSweepZombiePlaying_NilDB(t *testing.T) {
	s := &RoomService{}
	stats := s.JanitorSweepZombiePlaying(context.Background(), 4*time.Hour)
	if stats.Scanned != 0 || stats.Deleted != 0 || stats.Skipped != 0 {
		t.Fatalf("nil db 应返回零值 stats,got %+v", stats)
	}
}
