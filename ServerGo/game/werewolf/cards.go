// Package werewolf implements the "狼人杀 13 人标准竞技局" engine.
//
// 13 人标准竞技局配置(默认 v2026-07-10,详见 docs/狼人杀13人标准局规则.md):
//   - 4 名普通狼人 (werewolf 阵营)
//   - 1 预言家 (好人阵营神职)
//   - 1 女巫 (好人阵营神职,持有解药+毒药各一次)
//   - 1 猎人 (好人阵营神职,死时可开枪)
//   - 1 白痴 (好人阵营神职,被白天投票放逐可翻牌免死)
//   - 5 名普通平民 (好人阵营)
//
// 12 人历史兼容配置(非默认,werewolf_12 专用):
//   - 4 名普通狼人 + 1 预言家 + 1 女巫 + 1 猎人 + 1 白痴 + 4 平民
//
// 7 人历史兼容配置(非默认,werewolf_7 专用):
//   - 2 名普通狼人 + 1 预言家 + 1 女巫 + 1 猎人 + 2 平民
//
// 胜负规则("屠边"规则):
//   - 狼人:杀光所有神职(预+女巫+猎+白痴=4) 或 杀光所有平民(13 人局 5 / 12 人局 4 / 7 人局 2) 即胜
//   - 好人:放逐全部 狼人(13/12 人局 4 名 / 7 人局 2 名) 即胜
//
// 关键阶段机:
//   - 准备 (filling  → 满本局人数后自动开局:13 人默认 / werewolf_12 入座 12 / werewolf_7 入座 7)
//   - 第一夜:狼人协商刀人(可空刀) → 预言家查验 → 女巫用药 → 公布死亡
//   - 第一天:警长竞选(可选)→ 轮流发言 → 投票放逐 → 白痴翻牌(若最高票)→ 遗言
//   - 第N夜/N天循环,直到任一阵营达成胜利条件
//
// 核心约束:
//   - 所有人夜间操作严格按固定顺序唤醒,只有醒着的角色才能发动作
//   - 解药只能救当晚被狼杀的玩家;毒药不能与解药同晚使用
//   - 猎人被毒杀时不能开枪(只能被狼杀/投票放逐时开枪)
//   - 狼人可在白天发言阶段自爆,直接进黑天,出局且无遗言
//   - 警长票权 1.5 票,平票默认不投死(无人出局直接进黑天)
//   - 警徽流:预言家警长夜间死亡后按真实验人结果结算金水/查杀/撕警徽
//
// 三模式兼容(MaxPlayers 常量 = 13,seatCount 字段区分本局实际人数):
//   - 默认 werewolf / werewolf_13: seatCount = 13,使用 StandardDeck13 / AssignRoles13
//   - werewolf_12 (历史兼容): seatCount = 12,使用 StandardDeck12 / AssignRoles12
//   - werewolf_7 (历史兼容): seatCount = 7,使用 StandardDeck / AssignRoles
//
// 本文件 cards.go 仅定义角色身份牌与洗牌(随机分配),具体状态机与
// 阶段机见 engine.go,房间见 room.go,视图过滤见 view.go。
package werewolf

import (
	"math/rand"
)

// Role 身份牌类型。
type Role int

const (
	RoleUnknown  Role = iota // 占位(死亡后或观察者)
	RoleWerewolf             // 狼人
	RoleSeer                 // 预言家
	RoleWitch                // 女巫
	RoleHunter               // 猎人
	RoleIdiot                // 白痴(神职,被白天投票放逐可翻牌免死)
	RoleVillager             // 普通平民

	// 2026-07-11: 扩展神职角色(13人随机牌组池)。
	RoleGuard       // 守卫:每晚守护一名玩家免被狼人袭击(§134)
	RoleKnight      // §198 骑士:白天可翻牌与一名玩家决斗;命中狼则对方出局,否则自己出局,每局限一次
	RoleDemonHunter // §猎魔人 猎魔人:第2晚起每晚狩猎一名玩家;命中狼则对方死亡(verdict=death,cause=wolf),命中好人则自己出局(verdict=execution,cause=demon_hunter_misjudge);每晚可发动,发动即公开身份
	RoleMagician    // 魔术师:每晚交换两名玩家的号码牌 ⚠️ 2026-07-29 已退役:无引擎/工具实现,仅保留 wire 兼容
	RoleMerchant    // 奇迹商人:每晚选择幸运儿赋予技能 ⚠️ 2026-07-29 已退役:无引擎/工具实现,仅保留 wire 兼容
	RoleDreamer     // 射梦人:每晚选择梦游者免疫夜间伤害 ⚠️ 2026-07-29 已退役:无引擎/工具实现,仅保留 wire 兼容
	RoleCrow        // 乌鸦:每晚诅咒一名玩家使其多一票 ⚠️ 2026-07-29 已退役:无引擎/工具实现,仅保留 wire 兼容
	RoleScarecrow   // 稻草人:每晚得知被放逐玩家身份 ⚠️ 2026-07-29 已退役:无引擎/工具实现,仅保留 wire 兼容
	RolePrince      // 定序王子:被放逐时可翻牌逆转投票 ⚠️ 2026-07-29 已退役:无引擎/工具实现,仅保留 wire 兼容
	RolePureWhite   // 纯白之女:每晚查验狼人则其出局 ⚠️ 2026-07-29 已退役:无引擎/工具实现,仅保留 wire 兼容
)

func (r Role) String() string {
	switch r {
	case RoleWerewolf:
		return "werewolf"
	case RoleSeer:
		return "seer"
	case RoleWitch:
		return "witch"
	case RoleHunter:
		return "hunter"
	case RoleIdiot:
		return "idiot"
	case RoleVillager:
		return "villager"
	case RoleGuard:
		return "guard"
	case RoleKnight:
		return "knight" // §198 已重新实现
	case RoleMagician:
		return "magician" // ⚠️ 2026-07-29 已退役:仅保留 wire 兼容
	case RoleMerchant:
		return "merchant" // ⚠️ 2026-07-29 已退役:仅保留 wire 兼容
	case RoleDreamer:
		return "dreamer" // ⚠️ 2026-07-29 已退役:仅保留 wire 兼容
	case RoleCrow:
		return "crow" // ⚠️ 2026-07-29 已退役:仅保留 wire 兼容
	case RoleScarecrow:
		return "scarecrow" // ⚠️ 2026-07-29 已退役:仅保留 wire 兼容
	case RolePrince:
		return "prince" // ⚠️ 2026-07-29 已退役:仅保留 wire 兼容
	case RoleDemonHunter:
		return "demon_hunter" // §猎魔人 复活:引擎/工具全链路已补全
	case RolePureWhite:
		return "pure_white" // ⚠️ 2026-07-29 已退役:仅保留 wire 兼容
	default:
		return "unknown"
	}
}

// Faction 阵营。
type Faction int

const (
	FactionUnknown Faction = iota
	FactionWolf            // 狼人阵营
	FactionGood            // 好人阵营(神职+平民)
)

func (f Faction) String() string {
	switch f {
	case FactionWolf:
		return "wolf"
	case FactionGood:
		return "good"
	default:
		return "unknown"
	}
}

// FactionOf 返回角色所属阵营(供胜负判定使用)。
//
// ⚠️ 2026-07-29 已退役角色(magician/merchant/dreamer/crow/scarecrow/prince/pure_white):
// 无引擎/工具实现,从 case 列表中移除,让它们走 default 分支返回 FactionUnknown。
// 枚举值与 String() 仍保留,以兼容历史 DB 行 / 已发出 wire 帧 / 历史回放。
// §198 knight 重新实现,加回 FactionGood。
// §猎魔人 demon_hunter 重新实现,加回 FactionGood。
func FactionOf(r Role) Faction {
	switch r {
	case RoleWerewolf:
		return FactionWolf
	case RoleSeer, RoleWitch, RoleHunter, RoleIdiot, RoleVillager,
		RoleGuard, RoleKnight, // §198 加回骑士
		RoleDemonHunter: // §猎魔人 加回
		return FactionGood
	// ── ⚠️ 2026-07-29 已退役(无引擎/工具实现,仅保留 wire 兼容) ──
	//case RoleMagician:
	//	return FactionGood
	//case RoleMerchant:
	//	return FactionGood
	//case RoleDreamer:
	//	return FactionGood
	//case RoleCrow:
	//	return FactionGood
	//case RoleScarecrow:
	//	return FactionGood
	//case RolePrince:
	//	return FactionGood
	//case RolePureWhite:
	//	return FactionGood
	default:
		return FactionUnknown
	}
}

// MaxPlayers 引擎支持的最大固定人数(13 人标准竞技局)。
// 所有 [MaxPlayers] 数组与 for i<MaxPlayers 循环均以此为界。
// 历史 12 人/7 人局通过 GameState.SeatCount 兼容,不另设常量。
const MaxPlayers = 13

// Seat 玩家的座位号,固定 0..12。
type Seat int

const NoSeat Seat = -1

// StandardDeck13 13 人标准竞技局的固定角色牌组合:
// 4狼 + 预言家 + 女巫 + 猎人 + 白痴 + 5平民 = 13
// 12 人局相比,多加 1 个好人平民(好人阵营规模优势更大,
// 倒逼 4 狼提高夜间协同与白天欺骗能力 — 详见 docs/design/狼人杀13人局重构设计.md §2.1)。
func StandardDeck13() []Role {
	return []Role{
		RoleWerewolf,
		RoleWerewolf,
		RoleWerewolf,
		RoleWerewolf,
		RoleSeer,
		RoleWitch,
		RoleHunter,
		RoleIdiot,
		RoleVillager,
		RoleVillager,
		RoleVillager,
		RoleVillager,
		RoleVillager,
	}
}

// StandardDeck12 12 人标准竞技局的固定角色牌组合(保留兼容 werewolf_12):
// 4狼 + 预言家 + 女巫 + 猎人 + 白痴 + 4平民 = 12
func StandardDeck12() []Role {
	return []Role{
		RoleWerewolf,
		RoleWerewolf,
		RoleWerewolf,
		RoleWerewolf,
		RoleSeer,
		RoleWitch,
		RoleHunter,
		RoleIdiot,
		RoleVillager,
		RoleVillager,
		RoleVillager,
		RoleVillager,
	}
}

// StandardDeck 历史 7 人标准局的固定角色牌组合(保留兼容 werewolf_7):
// 2狼 + 预言家 + 女巫 + 猎人 + 2平民 = 7
func StandardDeck() []Role {
	return []Role{
		RoleWerewolf,
		RoleWerewolf,
		RoleSeer,
		RoleWitch,
		RoleHunter,
		RoleVillager,
		RoleVillager,
	}
}

// Shuffle 用注入的 *rand.Rand 原地洗牌。
func Shuffle(deck []Role, rng *rand.Rand) {
	rng.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
}

// AssignRoles13 给定 13 人座位 userIDs,按 seed 洗一副 13 人标准牌并分配;
// 返回 roles 数组(roles[seat] = 该座位身份)。13 座全满才发牌。
func AssignRoles13(seed int64, userIDs [MaxPlayers]string) [MaxPlayers]Role {
	return assignRolesImpl(seed, userIDs, StandardDeck13())
}

// AssignRoles12 给定 12 人座位 userIDs,按 seed 洗一副 12 人标准牌并分配(保留兼容 werewolf_12);
// 返回 roles 数组(roles[seat] = 该座位身份)。12 座全满才发牌。
func AssignRoles12(seed int64, userIDs [MaxPlayers]string) [MaxPlayers]Role {
	return assignRolesImpl(seed, userIDs, StandardDeck12())
}

// AssignRoles 历史 7 人局发牌(保留兼容 werewolf_7):
// 按 seed 洗一副 7 人标准牌并分配,7 座全满才发牌。
func AssignRoles(seed int64, userIDs [MaxPlayers]string) [MaxPlayers]Role {
	return assignRolesImpl(seed, userIDs, StandardDeck())
}

// assignRolesImpl 通用发牌实现:仅对有人的座位洗牌分配,恰好 deck 长度个
// 非空座位时才发牌,否则留空(RoleUnknown)。
func assignRolesImpl(seed int64, userIDs [MaxPlayers]string, deck []Role) [MaxPlayers]Role {
	var roles [MaxPlayers]Role
	nonEmpty := 0
	for _, u := range userIDs {
		if u != "" {
			nonEmpty++
		}
	}
	if nonEmpty != len(deck) {
		// 非空座位数与牌数不匹配:还不该发牌,角色全空
		return roles
	}
	// 从头开始的非空座位按序分牌(fillSeats 总是从 0 连续入座)。
	shuffled := make([]Role, len(deck))
	copy(shuffled, deck)
	rng := rand.New(rand.NewSource(seed))
	Shuffle(shuffled, rng)
	k := 0
	for i := 0; i < MaxPlayers; i++ {
		if userIDs[i] == "" {
			roles[i] = RoleUnknown
			continue
		}
		roles[i] = shuffled[k]
		k++
	}
	return roles
}

// IsGodRole 报告角色是否为好人阵营神职(非平民、非狼人)。
//
// ⚠️ 2026-07-29 已退役角色(magician/merchant/dreamer/crow/scarecrow/prince/pure_white):
// 无引擎/工具实现,从 case 列表中移除,让它们走 default 分支返回 false。
// 枚举值与 String() 仍保留,以兼容历史 DB 行 / 已发出 wire 帧 / 历史回放。
// §198 knight 重新实现,加回 IsGodRole → true。
// §猎魔人 demon_hunter 重新实现,加回 IsGodRole → true。
func IsGodRole(r Role) bool {
	switch r {
	case RoleSeer, RoleWitch, RoleHunter, RoleIdiot,
		RoleGuard, RoleKnight, // §198 加回骑士
		RoleDemonHunter: // §猎魔人 加回
		return true
	// ── ⚠️ 2026-07-29 已退役(无引擎/工具实现,仅保留 wire 兼容) ──
	//case RoleMagician:
	//	return true
	//case RoleMerchant:
	//	return true
	//case RoleDreamer:
	//	return true
	//case RoleCrow:
	//	return true
	//case RoleScarecrow:
	//	return true
	//case RolePrince:
	//	return true
	//case RolePureWhite:
	//	return true
	default:
		return false
	}
}

// ─────────────────── 随机牌组(13人标准竞技局 v2026-07-11) ───────────────────

// godRolePool 可被随机选入牌组的神职角色池(不含预言家,预言家每局必出)。
//
// 注意(2026-07-24 §卡池瘦身):RoleScarecrow 与 RolePrince 已从活动卡池中移除,
// 新局 RandomDeck13 / AssignRoles13Random 永远不会再发这两张牌。它们的枚举值、
// String() / FactionOf() / IsGodRole() / RoleDisplayName() 仍保留,以兼容历史
// 回放 / 数据库行 / 已发出的 wire 帧;但 godRolePool 不再把它们列入候选池,
// 因此新局不会再出现这两个角色。
//
// 注意(2026-07-29 §卡池再瘦身):5 个"半实现"角色(magician/merchant/dreamer/
// crow/pure_white)完全没有引擎实现、没有 Agent 工具、没有技能逻辑,
// 继续留在 godRolePool 会让玩家拿到"无效身份"(违反 §134/§198 守则)。已从活动卡池中移除,
// 枚举值/String()/FactionOf()/IsGodRole()/RoleDisplayName() 仍保留 wire 兼容。
// 注意(2026-07-30 §198 骑士复活):骑士已通过全链路实现守卫同等公民(详见
// `docs/狼人杀骑士角色设计.md`),重新纳入 godRolePool。
// 注意(2026-07-30 §猎魔人 复活):猎魔人已通过全链路补全(详见
// `docs/狼人杀猎魔人角色设计.md`),重新纳入 godRolePool。
// 当前 godRolePool 含 6 个完整实现的神职:女巫/猎人/白痴/守卫/骑士/猎魔人。
var godRolePool = []Role{
	RoleWitch,
	RoleHunter,
	RoleIdiot,
	RoleGuard,
	RoleKnight,      // §198 重新加入
	RoleDemonHunter, // §猎魔人 重新加入
}

// RandomDeck13 生成 13 人随机牌组:
//   - 固定 4 狼人 + 1 预言家(核心角色,每局必须有)
//   - 从 godRolePool 中随机选取 2~3 个神职(种子确定性)
//   - 剩余全部平民(≥ 5 平民,保证平民占多数)
//   - 牌组最后一位强制为 RoleVillager(第13人必须是好人平民)
//
// 总计: 4狼 + (1 seer + 2~3 random gods) + (5~6 villager) = 13
func RandomDeck13(seed int64) []Role {
	rng := rand.New(rand.NewSource(seed))

	// 1. 固定: 4狼 + 1预言家
	deck := []Role{
		RoleWerewolf, RoleWerewolf, RoleWerewolf, RoleWerewolf,
		RoleSeer,
	}

	// 2. 随机选取 2~3 个神职
	godCount := 2 + rng.Intn(2) // 2 or 3
	pool := make([]Role, len(godRolePool))
	copy(pool, godRolePool)
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	for i := 0; i < godCount && i < len(pool); i++ {
		deck = append(deck, pool[i])
	}

	// 3. 平民补齐(第13位强制平民)
	villagerCount := 13 - len(deck) - 1 // 预留最后一位
	for i := 0; i < villagerCount; i++ {
		deck = append(deck, RoleVillager)
	}
	deck = append(deck, RoleVillager) // 第13人强制平民

	return deck
}

// AssignRoles13Random 给定 13 人座位 userIDs,按 seed 生成随机牌组并分配;
// 返回 roles 数组(roles[seat] = 该座位身份)。13 座全满才发牌。
// 额外约束:最后入座的非空座位(第13人)强制为 RoleVillager。
func AssignRoles13Random(seed int64, userIDs [MaxPlayers]string) [MaxPlayers]Role {
	deck := RandomDeck13(seed)
	roles := assignRolesImpl(seed, userIDs, deck)

	// 找到最后一个非空座位(第13人),强制平民
	lastOccupied := -1
	for i := MaxPlayers - 1; i >= 0; i-- {
		if userIDs[i] != "" {
			lastOccupied = i
			break
		}
	}
	if lastOccupied >= 0 && roles[lastOccupied] != RoleVillager {
		// 把原角色换到其他平民位,让第13人变为平民
		origRole := roles[lastOccupied]
		for i := 0; i < MaxPlayers; i++ {
			if i != lastOccupied && roles[i] == RoleVillager {
				roles[i] = origRole
				roles[lastOccupied] = RoleVillager
				break
			}
		}
	}

	return roles
}

// RoleDisplayName 返回角色的中文显示名(用于前后端共享)。
//
// ⚠️ 2026-07-29 已退役角色(magician/merchant/dreamer/crow/scarecrow/prince/pure_white):
// 无引擎/工具实现,从 case 列表中移除,让它们走 default 分支返回"未知"。
// 枚举值与 String() 仍保留,以兼容历史 DB 行 / 已发出 wire 帧 / 历史回放。
// §198 knight 重新实现,加回中文名"骑士"。
// §猎魔人 demon_hunter 重新实现,加回中文名"猎魔人"。
func RoleDisplayName(r Role) string {
	switch r {
	case RoleWerewolf:
		return "狼人"
	case RoleSeer:
		return "预言家"
	case RoleWitch:
		return "女巫"
	case RoleHunter:
		return "猎人"
	case RoleIdiot:
		return "白痴"
	case RoleVillager:
		return "平民"
	case RoleGuard:
		return "守卫"
	case RoleKnight:
		return "骑士" // §198 加回
	case RoleDemonHunter:
		return "猎魔人" // §猎魔人 加回
	// ── ⚠️ 2026-07-29 已退役(无引擎/工具实现,仅保留 wire 兼容) ──
	//case RoleMagician:
	//	return "魔术师"
	//case RoleMerchant:
	//	return "奇迹商人"
	//case RoleDreamer:
	//	return "射梦人"
	//case RoleCrow:
	//	return "乌鸦"
	//case RoleScarecrow:
	//	return "稻草人"
	//case RolePrince:
	//	return "定序王子"
	//case RolePureWhite:
	//	return "纯白之女"
	default:
		return "未知"
	}
}

// ─────────────────── 自选角色注入(2026-08-06 §20260806-03) ───────────────────

// SelectableRoleNames 创建房间时用户可为座位指定的角色名白名单。
// 仅包含全链路已实现的角色(§134/§198 卡池契约):狼人 + 6 神职 + 平民。
// 已退役角色(magician/merchant/dreamer/crow/scarecrow/prince/pure_white)不可选。
var SelectableRoleNames = []string{
	"werewolf", "seer", "witch", "hunter", "idiot",
	"guard", "knight", "demon_hunter", "villager",
}

// ParseRoleName 把 API 层角色名字符串解析为 Role。
// ""/"random" 返回 (RoleUnknown, true) — 表示"随机",调用方应跳过注入。
// 非法/已退役角色名返回 (RoleUnknown, false)。
func ParseRoleName(name string) (Role, bool) {
	switch name {
	case "", "random":
		return RoleUnknown, true
	case "werewolf":
		return RoleWerewolf, true
	case "seer":
		return RoleSeer, true
	case "witch":
		return RoleWitch, true
	case "hunter":
		return RoleHunter, true
	case "idiot":
		return RoleIdiot, true
	case "guard":
		return RoleGuard, true
	case "knight":
		return RoleKnight, true
	case "demon_hunter":
		return RoleDemonHunter, true
	case "villager":
		return RoleVillager, true
	default:
		return RoleUnknown, false
	}
}

// ApplyPreferredRoles 在洗牌完成后按座位偏好做"牌组内座位置换"(swap-only)。
//
// 不变式:返回的 roles 与输入是同一多重集(每张牌总数不变,仅座位归属变化),
// 因此 13 人标准竞技局的牌组组成契约(4狼+1预言家+2~3神职+平民补齐)不被破坏。
//
// 语义(详见 docs/狼人杀13人局-优化和解决-20260806-03.md §3.4):
//   - 按座位号升序处理,先到先得(确定性,可单测);
//   - 偏好座位已命中 → no-op;
//   - 牌组中无该角色(如本局随机牌组未抽到骑士)→ 该座位保持随机(降级),
//     函数返回未满足的座位列表供调用方记日志;
//   - 多座位抢同一限量角色 → 升序首个得到满足,其余降级;
//   - "尾位强制平民"(AssignRoles13Random 末尾)是软约定:用户显式指定优先,
//     调用方须在 AssignRoles13Random 返回后再调本函数。
//
// prefs 的 key 是座位号(0..MaxPlayers-1),value 是已解析的 Role(RoleUnknown
// 会被跳过)。空位(无人座位)上的偏好自动无效(不参与 swap 目标,也不会成为
// 交换来源——swap 只发生在有人座位之间)。
func ApplyPreferredRoles(roles *[MaxPlayers]Role, occupied [MaxPlayers]bool, prefs map[int]Role) []int {
	if len(prefs) == 0 {
		return nil
	}
	// 确定性顺序:座位号升序。
	order := make([]int, 0, len(prefs))
	for seat := range prefs {
		order = append(order, seat)
	}
	for i := 1; i < len(order); i++ { // 插入排序(最多 13 项,免 import sort)
		for j := i; j > 0 && order[j] < order[j-1]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}

	satisfied := make(map[int]bool, len(order)) // 已满足偏好的座位(其牌不可被换走)
	var unmet []int
	for _, seat := range order {
		want, ok := prefs[seat]
		if !ok || want <= RoleUnknown {
			continue
		}
		if seat < 0 || seat >= MaxPlayers || !occupied[seat] {
			continue // 空座偏好无效
		}
		if roles[seat] == want {
			satisfied[seat] = true
			continue
		}
		swapped := false
		for i := 0; i < MaxPlayers; i++ {
			if i == seat || satisfied[i] || !occupied[i] {
				continue
			}
			if roles[i] == want {
				roles[i], roles[seat] = roles[seat], roles[i]
				satisfied[seat] = true
				swapped = true
				break
			}
		}
		if !swapped {
			unmet = append(unmet, seat)
		}
	}
	return unmet
}
