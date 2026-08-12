package wwplayer

import (
	"strings"
	"testing"
	"time"
)

// TestRecentSpeakDedup_RejectsHighJaccard (BUG-R70-P2)
//
// 验证:相同主题的两条发言(90s 窗口内)被拒绝;不同主题被允许。
func TestRecentSpeakDedup_RejectsHighJaccard(t *testing.T) {
	d := NewRecentSpeakDedup()
	now := time.Now()

	// 第一条:自我介绍
	if allowed, hint := d.CheckAndRecord("我是 2 号,好人阵营,首夜平安。", now); !allowed {
		t.Fatalf("first speak should be allowed, got %q", hint)
	}
	// 间隔 10s,内容高度雷同(只是改了几个字)→ 应被拒
	if allowed, hint := d.CheckAndRecord("我是 2 号,好人阵营,首夜平安!", now.Add(10*time.Second)); allowed {
		t.Fatal("highly similar speak should be rejected")
	} else if !strings.Contains(hint, "rejected") {
		t.Fatalf("hint should mention rejected, got %q", hint)
	}
}

// TestRecentSpeakDedup_AllowsDifferentContent (BUG-R70-P2)
//
// 验证:主题差异大的发言被允许,即使时间很近。
func TestRecentSpeakDedup_AllowsDifferentContent(t *testing.T) {
	d := NewRecentSpeakDedup()
	now := time.Now()

	if allowed, _ := d.CheckAndRecord("我是 2 号,好人阵营", now); !allowed {
		t.Fatal("first speak rejected")
	}
	if allowed, _ := d.CheckAndRecord("我怀疑 5 号,昨晚的发言太可疑", now.Add(5*time.Second)); !allowed {
		t.Fatal("different content should be allowed")
	}
}

// TestRecentSpeakDedup_WindowExpiry (BUG-R70-P2)
//
// 验证:300s 窗口外的旧发言不再参与比较,允许新一轮重复。
// R93 P2: 窗口从 90s → 300s,测试同步更新。
func TestRecentSpeakDedup_WindowExpiry(t *testing.T) {
	d := NewRecentSpeakDedup()
	now := time.Now()

	d.CheckAndRecord("我是 2 号,好人阵营", now)
	// 301s 之后同样的内容应允许(窗口外)
	if allowed, _ := d.CheckAndRecord("我是 2 号,好人阵营", now.Add(301*time.Second)); !allowed {
		t.Fatal("speak after window should be allowed")
	}
}

// TestNormalizeSpeakForCompare (BUG-R70-P2)
//
// 验证:标准化函数正确处理全角/空格/大小写。
func TestNormalizeSpeakForCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"我是2号", "我 是 2 号", 1.0},           // 空白应剥离
		{"Hello", "hello", 1.0},              // 大小写应一致
		{"Hello", "World", 2.0 / 7.0},       // H,e,l,o 与 W,o,r,l 共享 l,o => 交集2, 并集7
		{"我是2号", "你是3号", 2.0 / 6.0},     // 共享"是"+"号": A{我,是,2,号} B{你,是,3,号} => 交集2, 并集6
	}
	for _, c := range cases {
		got := jaccardSimilarity(normalizeSpeakForCompare(c.a), normalizeSpeakForCompare(c.b))
		if got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("jaccard(%q, %q) = %f, want %f", c.a, c.b, got, c.want)
		}
	}
}

// TestRecentSpeakDedup_R93_ShortIntroRepeatAcrossScenes (R93 P2)
//
// 验证:同一个 bot 在跨场景(2 分钟一次 intro)+ 不同字面(emoji / 标点微差)
// 时仍被识别为重复。R93 报告 Bot 2 重复"我是2号,13人新手局大家多包涵..."
// 旧 0.6 阈值在短句上漏报,新 0.5 阈值配合 300s 窗口应能 catch。
func TestRecentSpeakDedup_R93_ShortIntroRepeatAcrossScenes(t *testing.T) {
	d := NewRecentSpeakDedup()
	now := time.Now()
	first := "我是2号,13人新手局大家多包涵"
	// 第二次加 emoji + 标点,内容基本相同
	second := "我是2号,13人新手局,大家多包涵~"

	if allowed, _ := d.CheckAndRecord(first, now); !allowed {
		t.Fatal("first intro should be allowed")
	}
	// 间隔 2 分钟(场景切换),内容有微差,但 R93 P2 阈值 0.5 应识别为重复。
	if allowed, hint := d.CheckAndRecord(second, now.Add(2*time.Minute)); allowed {
		t.Fatalf("R93 short-intro repeat should be rejected at 0.5 threshold; got allowed")
	} else if hint == "" {
		t.Fatal("rejected speak should provide hint for LLM")
	}
}