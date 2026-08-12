// Package agent — speak_dedup_test.go: 验证 DedupSpeakText 在 LLM 复读 /
// 整段重复 / 超长 文本上的清理行为。Round 39+ 报告 N1 的回归测试。
package agentcore

import (
	"strings"
	"testing"
)

func TestDedupSpeakText_Normal(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		want       string
		wantDeDup  bool
		wantTrunc  bool
	}{
		{
			name: "正常短句,不动",
			in:   "我是2号。首夜还没刀人。",
			want: "我是2号。首夜还没刀人。",
		},
		{
			name: "空字符串",
			in:   "",
			want: "",
		},
		{
			name:      "相邻重复:经典 N1 现象",
			in:        "我是2号。首夜还没刀人,大家先聊聊身份吧。1号和3号谁先说?我是2号。首夜还没刀人,大家先聊聊身份吧。1号和3号谁先说?",
			wantDeDup: true,
		},
		{
			name:      "整段复读(重复比例 > 50%)",
			in:        "A。B。A。B。A。B。A。B。",
			want:      "A。B。",
			wantDeDup: true,
		},
		{
			name:      "超长截断",
			in:        strings.Repeat("一", 120),
			wantTrunc: true,
		},
		{
			name:      "只有重复 chunk",
			in:        "哈。哈。哈。哈。",
			want:      "哈。",
			wantDeDup: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, dedup, trunc := DedupSpeakText(tc.in)
			if tc.want != "" && got != tc.want {
				t.Errorf("cleaned text mismatch\nwant: %q\ngot:  %q", tc.want, got)
			}
			if dedup != tc.wantDeDup {
				t.Errorf("wasDeDuped: want %v got %v", tc.wantDeDup, dedup)
			}
			if trunc != tc.wantTrunc {
				t.Errorf("wasTruncated: want %v got %v", tc.wantTrunc, trunc)
			}
		})
	}
}

func TestDedupSpeakText_LengthCap(t *testing.T) {
	in := strings.Repeat("啊", 200)
	got, _, trunc := DedupSpeakText(in)
	if !trunc {
		t.Errorf("expected wasTruncated=true, got false")
	}
	// 80 字封顶
	runes := []rune(got)
	if len(runes) > 80 {
		t.Errorf("expected <=80 runes, got %d", len(runes))
	}
}

func TestSplitSpeakChunks(t *testing.T) {
	cases := map[string]int{
		"a。b。c。":   3,
		"no separator": 1,
		"a!b?c\nd;":   4,
		"":            0,
	}
	for in, want := range cases {
		got := splitSpeakChunks(in)
		if len(got) != want {
			t.Errorf("splitSpeakChunks(%q): want %d chunks, got %d (%v)", in, want, len(got), got)
		}
	}
}

// TestNormalizeDeathTerms_ExecutionAndDeath 验证 §123 死亡语义规范化。
func TestNormalizeDeathTerms_ExecutionAndDeath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "vote→execution",
			in:   "3号被投票杀,请大家同意",
			want: "3号被处决,请大家同意",
		},
		{
			name: "banish→execution",
			in:   "1号被放逐出局",
			want: "1号被处决",
		},
		{
			name: "wolf→death",
			in:   "5号被狼杀了",
			want: "5号被狼刀死亡了",
		},
		{
			name: "witch_poison→death",
			in:   "7号被毒杀",
			want: "7号被女巫毒杀死亡",
		},
		{
			name: "hunter→death",
			in:   "2号被猎人杀",
			want: "2号被猎人反杀死亡",
		},
		{
			name: "died→死亡",
			in:   "4号死了",
			want: "4号死亡",
		},
		{
			name: "suicide keeps as is",
			in:   "9号自爆了",
			want: "9号自爆了",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
		{
			name: "no death term, no change",
			in:   "我同意1号的发言",
			want: "我同意1号的发言",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeDeathTerms(c.in)
			if got != c.want {
				t.Errorf("normalizeDeathTerms(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
