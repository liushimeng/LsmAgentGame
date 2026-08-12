// Package wwjudge — judge_trust_trace_test.go: §20260812-01 U4 测试用例。
package wwjudge

import (
	"encoding/json"
	"testing"
)

func TestParseTrustTrace_Empty(t *testing.T) {
	if got := ParseTrustTrace(""); got != nil {
		t.Errorf("empty input should return nil, got: %v", got)
	}
}

func TestParseTrustTrace_NoHeader(t *testing.T) {
	raw := "【阵营胜负】好人阵营胜利\n【MVP 玩家】9 号预言家"
	if got := ParseTrustTrace(raw); got != nil {
		t.Errorf("no header should return nil, got: %v", got)
	}
}

func TestParseTrustTrace_FullFormat(t *testing.T) {
	raw := `【阵营胜负】好人阵营胜利
【关键翻盘点】D2 狼自爆
【信任度轨迹】seat1: day1=0.2, day2=0.5, day3=-0.3; seat2: day1=0.0, day2=0.3, day3=0.8
【高光时刻】[]`
	got := ParseTrustTrace(raw)
	if len(got) != 6 {
		t.Errorf("expected 6 entries (3 days × 2 seats), got %d", len(got))
	}
	// 校验 seat1 day3 score
	var found bool
	for _, e := range got {
		if e.Seat == 0 && e.Day == 3 && e.Score == -0.3 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected to find seat=1 day=3 score=-0.3, got: %v", got)
	}
}

func TestParseTrustTrace_ChineseColon(t *testing.T) {
	raw := "【信任度轨迹】seat5：day1=0.5；seat6：day2=-0.2"
	got := ParseTrustTrace(raw)
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d", len(got))
	}
}

func TestParseTrustTrace_Clamping(t *testing.T) {
	raw := "【信任度轨迹】seat1: day1=2.5, day2=-3.0"
	got := ParseTrustTrace(raw)
	if len(got) != 2 {
		t.Errorf("expected 2 entries, got %d", len(got))
	}
	for _, e := range got {
		if e.Score < -1.0 || e.Score > 1.0 {
			t.Errorf("score out of range after clamp: %f", e.Score)
		}
	}
}

func TestParseTrustTrace_InvalidDay(t *testing.T) {
	raw := "【信任度轨迹】seat1: day0=0.5, day31=0.5, day5=0.5"
	got := ParseTrustTrace(raw)
	if len(got) != 1 {
		t.Errorf("expected 1 valid entry (day=5), got %d", len(got))
	}
}

func TestMarshalTrustTrace_Empty(t *testing.T) {
	s, err := MarshalTrustTrace(nil)
	if err != nil || s != "" {
		t.Errorf("empty should marshal to empty string, got: %q (err=%v)", s, err)
	}
}

func TestMarshalTrustTrace_NonEmpty(t *testing.T) {
	entries := []TrustTraceEntry{
		{Seat: 0, Day: 1, Score: 0.5},
		{Seat: 1, Day: 2, Score: -0.3},
	}
	s, err := MarshalTrustTrace(entries)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var roundtrip []TrustTraceEntry
	if err := json.Unmarshal([]byte(s), &roundtrip); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(roundtrip) != 2 {
		t.Errorf("roundtrip should have 2 entries, got %d", len(roundtrip))
	}
}

func TestPrettyPrintTrustTrace_Empty(t *testing.T) {
	got := PrettyPrintTrustTrace(nil)
	if got != "<empty>" {
		t.Errorf("empty should print <empty>, got: %s", got)
	}
}

func TestPrettyPrintTrustTrace_NonEmpty(t *testing.T) {
	entries := []TrustTraceEntry{
		{Seat: 0, Day: 1, Score: 0.5},
	}
	got := PrettyPrintTrustTrace(entries)
	if got == "<empty>" {
		t.Errorf("non-empty should not print <empty>")
	}
}
