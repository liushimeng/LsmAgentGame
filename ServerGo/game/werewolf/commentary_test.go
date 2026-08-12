package werewolf

import (
	"encoding/json"
	"strings"
	"testing"
)

// §20260811-09 U1 — CommentaryLine / ensureCommentaryFeedLocked / FeedSnapshot /
// NormalizeStyle 单元测试。覆盖:spectator-only 不变式 / 环形裁剪 / 序号单调 /
// style 归一化。

func TestCommentary_SetCommentaryConfigLocked_NormalizesStyle(t *testing.T) {
	r := &WerewolfRoom{}
	r.setCommentaryConfigLocked(&CommentaryConfig{Enabled: true, Style: "garbage"})
	if !r.commentaryDesired {
		t.Error("Enabled=true must set commentaryDesired")
	}
	if r.commentaryStyle != "pro" {
		t.Errorf("garbage style -> %q, want %q", r.commentaryStyle, "pro")
	}
	r.setCommentaryConfigLocked(&CommentaryConfig{Enabled: true, Style: "fun"})
	if r.commentaryStyle != "fun" {
		t.Errorf("fun style -> %q, want %q", r.commentaryStyle, "fun")
	}
	r.setCommentaryConfigLocked(nil)
	if r.commentaryDesired {
		t.Error("nil config must clear commentaryDesired")
	}
	r.setCommentaryConfigLocked(&CommentaryConfig{Enabled: false})
	if r.commentaryDesired {
		t.Error("Enabled=false must clear commentaryDesired")
	}
}

func TestCommentary_CommentaryModelKeyLocked_FallbackChain(t *testing.T) {
	r := &WerewolfRoom{commentaryModelKey: "  ", JudgeModelKey: "judge-key"}
	if got := r.commentaryModelKeyLocked(); got != "judge-key" {
		t.Errorf("空 commentaryModelKey 应回退 JudgeModelKey, got %q", got)
	}
	r.commentaryModelKey = "cmt-key"
	if got := r.commentaryModelKeyLocked(); got != "cmt-key" {
		t.Errorf("显式 commentaryModelKey 应优先, got %q", got)
	}
}

func TestCommentary_CommentaryStyleLocked_DefaultPro(t *testing.T) {
	r := &WerewolfRoom{}
	if got := r.commentaryStyleLocked(); got != "pro" {
		t.Errorf("空 style 应默认 pro, got %q", got)
	}
	r.commentaryStyle = "fun"
	if got := r.commentaryStyleLocked(); got != "fun" {
		t.Errorf("fun style 透传, got %q", got)
	}
}

func TestCommentary_EnsureCommentaryFeedLocked_RingBuffer(t *testing.T) {
	r := &WerewolfRoom{}
	for i := 0; i < commentaryFeedCap+3; i++ {
		r.ensureCommentaryFeedLocked("msg", "pro", "m", "kind")
	}
	if len(r.commentaryFeed) != commentaryFeedCap {
		t.Fatalf("feed 长度应裁剪到 %d, got %d", commentaryFeedCap, len(r.commentaryFeed))
	}
	// 序号单调递增
	if r.commentaryFeed[0].Seq != 4 { // 1+3=4 跳过的前 3 条
		t.Errorf("第 4 条应为最旧, got seq=%d", r.commentaryFeed[0].Seq)
	}
	if r.commentaryFeed[len(r.commentaryFeed)-1].Seq != commentaryFeedCap+3 {
		t.Errorf("末条 seq 应为 %d, got %d", commentaryFeedCap+3,
			r.commentaryFeed[len(r.commentaryFeed)-1].Seq)
	}
}

func TestCommentary_FeedSnapshotLocked_CopiesNotAliases(t *testing.T) {
	r := &WerewolfRoom{}
	r.ensureCommentaryFeedLocked("hello", "pro", "m1", "k1")
	snap := r.commentaryFeedSnapshotLocked()
	if len(snap) != 1 || snap[0].Text != "hello" {
		t.Fatalf("snapshot 不一致: %+v", snap)
	}
	// 修改 snapshot 不影响原 feed
	snap[0].Text = "mutated"
	if r.commentaryFeed[0].Text != "hello" {
		t.Errorf("snapshot 必须是拷贝而非别名, got %q", r.commentaryFeed[0].Text)
	}
}

// §135 spectator-only 不变式:CommentaryFeed JSON 序列化不应包含上帝视角字段。
// 我们不在 line 上暴露 Roles —— Roles 只在 prompt 构造时使用,
// ensureCommentaryFeedLocked 写入的 line 结构里没有 Roles 字段,
// 这里反向验证序列化结构。
func TestCommentary_CommentaryLineJSON_NoGodViewFields(t *testing.T) {
	line := commentaryLine{Seq: 1, Text: "t", Style: "pro", ModelKey: "k", Kind: "c", TsMs: 100}
	b, _ := json.Marshal(line)
	s := string(b)
	for _, leak := range []string{"role", "faction", "god_view", "wolf", "seer"} {
		if strings.Contains(s, leak) {
			t.Errorf("line JSON 不应泄漏上帝视角字段 %q: %s", leak, s)
		}
	}
}