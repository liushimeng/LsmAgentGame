package texasholdem

import (
	"math/rand"
	"testing"

	"LsmAgentGame/agent/thpagent"
)

// 2026-08-20 §B1 编码一致性回归测试。
//
// 背景：旧 cardToInt 用 `Rank*4+Suit+1`（值域 10..61），thpagent 决策引擎用
// 1..52 牌堆 + `(c-1)/4` 解码 —— 两套编码不一致导致底牌永远不会从抽样牌堆
// 剔除（对手可被发到同一张物理牌）。修复后 cardToInt 必须等于
// thpagent.EncodeCard 的唯一规范编码，本测试强制往返一致，防再次漂移。

// TestCardEncoding_001_RoundTrip: 全部 52 张牌 cardToInt → DecodeCard 往返一致。
func TestCardEncoding_001_RoundTrip(t *testing.T) {
	seen := make(map[int]bool)
	for rank := 2; rank <= 14; rank++ {
		for suit := 1; suit <= 4; suit++ {
			code := cardToInt(Card{Rank: rank, Suit: suit})
			if code < 1 || code > 52 {
				t.Fatalf("cardToInt(%d,%d) = %d out of canonical range 1..52", rank, suit, code)
			}
			if seen[code] {
				t.Fatalf("cardToInt(%d,%d) = %d collides with another card", rank, suit, code)
			}
			seen[code] = true
			if want := thpagent.EncodeCard(rank, suit); code != want {
				t.Fatalf("cardToInt(%d,%d) = %d, thpagent.EncodeCard = %d (drift!)", rank, suit, code, want)
			}
			gotRank, gotSuit := thpagent.DecodeCard(code)
			if gotRank != rank || gotSuit != suit {
				t.Fatalf("DecodeCard(%d) = (%d,%d), want (%d,%d)", code, gotRank, gotSuit, rank, suit)
			}
		}
	}
	if len(seen) != 52 {
		t.Fatalf("expected 52 unique codes, got %d", len(seen))
	}
}

// TestCardEncoding_002_ZeroCard: 零值 Card（未发牌）必须映射到 0 哨兵。
func TestCardEncoding_002_ZeroCard(t *testing.T) {
	if got := cardToInt(Card{}); got != 0 {
		t.Fatalf("cardToInt(zero Card) = %d, want 0", got)
	}
}

// TestCardEncoding_003_EvaluatorCrossCheck: thpagent 评估器与引擎自带
// EvaluateBest5 对随机 7 张牌的胜负判定必须完全一致（引擎评估器有 engine_test.go
// 独立测试套件背书，作为本地 oracle）。
//
// 类目映射：引擎 9=皇家同花顺 / 8=同花顺；thpagent 8=同花顺（皇家用 tiebreak=14
// 区分）。两手都是皇家时引擎侧 (9,[14]) vs thpagent (8,[14])，排序等价。
func TestCardEncoding_003_EvaluatorCrossCheck(t *testing.T) {
	rng := rand.New(rand.NewSource(20260820))
	for trial := 0; trial < 2000; trial++ {
		// 抽 9 张不重复牌：2 张给 A、2 张给 B、5 张公共牌
		perm := rng.Perm(52)
		codes := make([]int, 9)
		for i := range codes {
			codes[i] = perm[i] + 1
		}
		aHole := codes[0:2]
		bHole := codes[2:4]
		commCodes := codes[4:9]

		decodeCard := func(code int) Card {
			rank, suit := thpagent.DecodeCard(code)
			return Card{Rank: rank, Suit: suit}
		}
		var community [5]Card
		for i := 0; i < 5; i++ {
			community[i] = decodeCard(commCodes[i])
		}

		engineRank := func(hole []int) HandRank {
			return EvaluateBest5([2]Card{decodeCard(hole[0]), decodeCard(hole[1])}, community)
		}
		ea, eb := engineRank(aHole), engineRank(bHole)
		engineCmp := ea.Compare(eb)

		allA := append(append([]int{}, aHole...), commCodes...)
		allB := append(append([]int{}, bHole...), commCodes...)
		thpCmp := thpagent.CompareIntCards(allA, allB)

		norm := func(x int) int {
			if x > 0 {
				return 1
			}
			if x < 0 {
				return -1
			}
			return 0
		}
		if norm(engineCmp) != norm(thpCmp) {
			t.Fatalf("trial %d: engine=%d thpagent=%d; A=%v B=%v comm=%v (engine A=%+v B=%+v)",
				trial, engineCmp, thpCmp, aHole, bHole, commCodes, ea, eb)
		}
	}
}
