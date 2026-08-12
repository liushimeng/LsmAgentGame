package werewolf

// 2026-08-10 §20260810-08 — 信息账本二期：说漏嘴检测。
//
// 本文件只做 InformationLedger 的纯派生计算，不持有房间引用、不获取 r.mu。
// 检测结果仅供 spectator 复盘，绝不进入 GameContext、prompt、聊天或对局裁决。

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// InfoLeak 是一条疑似说漏嘴记录，仅 spectator 下发。
type InfoLeak struct {
	Seq        int64      `json:"seq"`
	Seat       int        `json:"seat"`
	Round      int        `json:"round"`
	Phase      string     `json:"phase"`
	HintSeat   int        `json:"hint_seat"`
	FromSource InfoSource `json:"from_source"`
	Excerpt    string     `json:"excerpt"`
}

// seatRefOneIndexed 是人类可读形式（`N号` / `第N位`），内部 1-indexed，
// 归一化到 0-indexed 时需 -1；seatRefZeroIndexed 是结构化形式
// （`seat=N` / `target=N` / `from=N`），本身已是 0-indexed，原值保留。
//
// ⚠️ R213 关键事实：账本 fact 在同一账本内**两套编号并存** ——
//   - 0-indexed（结构化 Action_* 写入点）：`seer_check seat=2 target=4`、
//     `wolf_vote seat=2 target=4 reason=…`、`wolf_pack from=2 text=…`、
//     `role_deal seat=2`。这是机器生成，`%d` 取自 int(seat)。
//   - 1-indexed（人类可读）：`InfoSourcePublicSpeech`、`InfoSourceDeathEvent`、
//     `InfoSourceWhisper` 的 fact 来自人类/bot 发言原文；`wolf_pack` 的
//     `text` 内嵌字段也是人手写（"今晚刀5号"）；prompt 渲染统一按 1-indexed
//     输出（见 agent/wwplayer/prompt.go:236 `itoa(seat+1) + "号→"` 等）。
//
// 早期版本的 extractSeatRefs 对两种形式都按 0-indexed 解析，导致：
//   - 「私密 seat=4 / 公开 5号」永远对不上 → 真实泄漏系统性漏报；
//   - 「公开 5号」会被解析成 ref=5 与另一条 `seat=5` 的私密条误交叉 → 系统性误报。
//
// 修复：按模式分支归一化，最终结果统一到 0-indexed。
var (
	seatRefOneIndexed = []*regexp.Regexp{
		regexp.MustCompile(`([0-9]{1,2})号`),
		regexp.MustCompile(`第\s*([0-9]{1,2})\s*位`),
	}
	seatRefZeroIndexed = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:seat|target|from|to)\s*=\s*([0-9]{1,2})\b`),
	}
)

// extractSeatRefs 提取文本中的座位号指纹，全部归一化到引擎内部 0-indexed。
// 两套编号并存的事实见包注释（R213 复盘），切勿回退成单模式解析。
func extractSeatRefs(fact string) map[int]bool {
	refs := make(map[int]bool)
	collect := func(patterns []*regexp.Regexp, oneIndexed bool) {
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllStringSubmatch(fact, -1) {
				if len(match) < 2 {
					continue
				}
				seat, err := strconv.Atoi(match[1])
				if err != nil {
					continue
				}
				if oneIndexed {
					// 1-indexed → 0-indexed；UI「3号」= 内部座位 2。
					seat--
				}
				if seat < 0 || seat >= MaxPlayers {
					continue
				}
				refs[seat] = true
			}
		}
	}
	collect(seatRefZeroIndexed, false)
	collect(seatRefOneIndexed, true)
	return refs
}

// isPrivateSource 明确归类全部 InfoSource；未知来源保守视为私密，避免漏审计。
// role_deal 在这里保留为私密（语义正确）：每个座位都仅从 role_deal 知道自己
// 的座位号，但「知道自己的座位号」不构成任何信息优势，DetectLeaks 内部
// 会用 ref==speaker 跳过自指，避免对每个玩家产生稳定噪声。
func isPrivateSource(src InfoSource) bool {
	switch src {
	case InfoSourceWhisper, InfoSourceWolfPack, InfoSourceNightSeer,
		InfoSourceNightWitch, InfoSourceNightGuard, InfoSourceNightWolfVote,
		InfoSourcePropInject, InfoSourceRoleDeal:
		return true
	case InfoSourcePublicSpeech, InfoSourceDayVoteMap, InfoSourceSheriffStream,
		InfoSourceSheriffElect, InfoSourceDeathEvent, InfoSourceHunterShot,
		InfoSourceKnightDuel, InfoSourceIdiotReveal, InfoSourceDemonHunter:
		return false
	default:
		return true
	}
}

// DetectLeaks 扫描账本，找出公开发言中首次引用、且发言者此前仅从私密渠道
// 获知的座位号。结果只是一条复盘线索，不能用于惩罚或干预对局。
//
// 判定规则（含 R213 修复）：
//   - 私密条目按 InfoSource 标记为「私密知识」，并把指纹座位归到发言者
//     的 privateKnown 集合；
//   - 公开发言（含 public_speech / 票型 / 警徽流 / 死亡事件 / 公开技能等）
//     解析指纹座位：
//       * 非 public_speech 直接计入 publicSeen（任何人都能看见）；
//       * public_speech 按座位遍历发言者，对每个指纹座位 ref：
//         - 已被 publicSeen 覆盖 → 跳过（公开信息不构成泄漏）；
//         - ref == speaker → 跳过（R213 自指不构成信息优势，role_deal
//           私秘路径给每个座位都种下「私知自己座位号」，不跳过会产生
//           每局 ~13 条稳定噪声）；
//         - 否则若 speaker 在 privateKnown[ref] 里有该座位 → 记一条 leak。
func DetectLeaks(ledger *InformationLedger) []InfoLeak {
	if ledger == nil {
		return nil
	}
	entries := ledger.entriesSnapshot()
	if len(entries) == 0 {
		return nil
	}
	publicSeen := make(map[int]bool)
	privateKnown := make(map[int]map[int]InfoSource)
	var leaks []InfoLeak
	for _, entry := range entries {
		refs := extractSeatRefs(entry.Fact)
		if isPrivateSource(entry.Source) {
			for knower, known := range entry.KnowerSeats {
				if !known {
					continue
				}
				if privateKnown[knower] == nil {
					privateKnown[knower] = make(map[int]InfoSource)
				}
				for ref := range refs {
					if _, exists := privateKnown[knower][ref]; !exists {
						privateKnown[knower][ref] = entry.Source
					}
				}
			}
			continue
		}
		if entry.Source != InfoSourcePublicSpeech {
			for ref := range refs {
				publicSeen[ref] = true
			}
			continue
		}
		for speaker, known := range entry.KnowerSeats {
			if !known {
				continue
			}
			for ref := range refs {
				if ref == speaker {
					// R213 缺陷 2：「知道自己的座位号」不构成信息优势，
					// 跳过自指避免每局 ~13 条稳定噪声淹没真实信号。
					continue
				}
				if publicSeen[ref] {
					continue
				}
				if source, ok := privateKnown[speaker][ref]; ok {
					leaks = append(leaks, InfoLeak{
						Seq: entry.Seq, Seat: speaker, Round: entry.Round,
						Phase: entry.Phase, HintSeat: ref, FromSource: source,
						Excerpt: truncateRunes(strings.TrimSpace(entry.Fact), 60),
					})
				}
			}
		}
		for ref := range refs {
			publicSeen[ref] = true
		}
	}
	sort.Slice(leaks, func(i, j int) bool {
		if leaks[i].Seq != leaks[j].Seq {
			return leaks[i].Seq < leaks[j].Seq
		}
		if leaks[i].Seat != leaks[j].Seat {
			return leaks[i].Seat < leaks[j].Seat
		}
		return leaks[i].HintSeat < leaks[j].HintSeat
	})
	return leaks
}

// detectLeaksLocked 是观战视图的缓存入口。caller 必须已持 r.mu（§92a）。
func (r *WerewolfRoom) detectLeaksLocked() []InfoLeak {
	if r == nil || r.infoLedger == nil {
		return nil
	}
	seq := r.infoLedger.seq
	if r.leakCacheSeq == seq {
		return r.leakCache
	}
	r.leakCache = DetectLeaks(r.infoLedger)
	r.leakCacheSeq = seq
	return r.leakCache
}