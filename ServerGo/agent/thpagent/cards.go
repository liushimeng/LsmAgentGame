// Package thpagent — cards.go: 德州扑克 int 牌面编码的唯一事实来源（2026-08-20 §德州扑克Agent-B1）。
//
// 背景（B1 P0 修复）：
//   game/texasholdem/room.go 旧 cardToInt 用 `Rank*4+Suit+1`（Rank 2..14, Suit 1..4 →
//   值域 10..61），而 thpagent/decision.go 的 makeDeckExcluding 生成 1..52 牌堆、
//   scoreHand 用 `(c-1)/4` 解码 —— 两套编码不一致导致：
//     1. 底牌编码 >52 时永远不会从抽样牌堆剔除（对手可被发到同一张物理牌）
//     2. 解码出的 rank 错误（(Rank*4+Suit+1-1)/4 ≈ Rank，碰巧接近但花色全错）
//
// 唯一规范编码（canonical encoding）：
//   encode = (Rank-2)*4 + (Suit-1) + 1     值域 1..52
//   decode: rankIdx = (c-1)/4  (0..12, 0=2 ... 12=Ace)   suit = (c-1)%4 + 1  (1..4)
//
// 0 保留为「无牌」哨兵（community 未亮部分填 0）。
// game/texasholdem.room.go::cardToInt 与本文件 EncodeCard 必须逐字节一致，
// 由 game/texasholdem/card_encoding_test.go 的往返测试强制约束（防再次漂移）。
package thpagent

import "fmt"

// EncodeCard 把 (rank 2..14, suit 1..4) 编码为 1..52 的规范 int。
// 非法输入返回 0（与「无牌」哨兵一致）。
func EncodeCard(rank, suit int) int {
	if rank < 2 || rank > 14 || suit < 1 || suit > 4 {
		return 0
	}
	return (rank-2)*4 + (suit-1) + 1
}

// DecodeCard 把规范 int (1..52) 解码为 (rank 2..14, suit 1..4)。
// 0 或越界值返回 (0, 0)。
func DecodeCard(c int) (rank, suit int) {
	if c < 1 || c > 52 {
		return 0, 0
	}
	rankIdx := (c - 1) / 4
	suit = (c-1)%4 + 1
	return rankIdx + 2, suit
}

// rankLabel 返回点数的可读标签（2..10/J/Q/K/A）。
func rankLabel(rank int) string {
	switch rank {
	case 11:
		return "J"
	case 12:
		return "Q"
	case 13:
		return "K"
	case 14:
		return "A"
	default:
		return fmt.Sprintf("%d", rank)
	}
}

// suitSymbol 返回花色符号（1=♠ 2=♥ 3=♣ 4=♦，与 texasholdem/cards.go 常量一致）。
func suitSymbol(suit int) string {
	switch suit {
	case 1:
		return "♠"
	case 2:
		return "♥"
	case 3:
		return "♣"
	case 4:
		return "♦"
	default:
		return "?"
	}
}

// CardString 把规范编码渲染为可读牌面（如 "A♠" / "10♥"）。
// 0 / 非法值返回 "-"（prompt 中明确表示「未亮」）。
//
// 修复 B3：此前 prompt.go 以 %d 裸 int 打印底牌/公共牌，LLM 无法解读花色。
func CardString(c int) string {
	rank, suit := DecodeCard(c)
	if rank == 0 {
		return "-"
	}
	return rankLabel(rank) + suitSymbol(suit)
}

// CardsString 把一组规范编码渲染为空格分隔的可读牌面。
func CardsString(cards []int) string {
	if len(cards) == 0 {
		return ""
	}
	out := ""
	for i, c := range cards {
		if i > 0 {
			out += " "
		}
		out += CardString(c)
	}
	return out
}
