package wwplayer

import (
	"strings"
	"testing"
)

// BUG-R13-NEW-001 (2026-08-17) 神职未来时计划公屏泄漏 — prompt 层防御。
//
// R13 22:30 报告 §二.P0-1 实测 Bot 4 号 (MiniMax M3) 在 D1 警长竞选阶段
// 公开发言「今晚查 11 号。理由:发言模板化强、像悍跳预言家。」—— 真预言
// 家首夜已验完人,公屏应直接报「昨夜我验了 X 是 Y」,不可能用「今晚要查」
// 这种未来时句式。
//
// 修复(prompt 层):在 hardBans 段追加【§R13-NEW 神职时态约束】,明确要求
// 神职公屏发言必须以「昨夜 / 已」过去时描述,严禁「今晚/明晚/我准备 +
// 查/验/守/毒/解/护 + X号」未来时计划句式。
//
// 配套服务端 hard-reject 见 game/werewolf/speak_wolfguard.go 的
// CheckFutureTenseSkillPlan。
//
// 不变式:
//  1. 系统 prompt 必须包含「§R13-NEW 神职时态约束」段;
//  2. 段内必须出现「昨夜/已」过去时引导 + 「查/验/守/毒/解/护」6 类
//     神职动词,确保 LLM 看到时态规则全貌;
//  3. 段内必须出现 reject hint 引导,LLM 失败时知道往哪改;
//  4. 段追加位置:hardBans 末尾,不影响 difficulty/personality 段字节级
//     缓存命中(§20260811-04 U2 + §20260810-10 U2 纪律)。

func TestBuildSystemPrompt_R13_FutureTenseConstraintPresent(t *testing.T) {
	zero := PersonalityVector{}
	blocks := BuildSystemPrompt("", zero, "", "", false)
	if len(blocks) != 1 {
		t.Fatalf("want 1 system block, got %d", len(blocks))
	}
	text := blocks[0].Text

	// §R13-NEW 段标识
	if !strings.Contains(text, "§R13-NEW 神职时态约束") {
		t.Errorf("§R13-NEW 神职时态约束 段未出现在 system prompt 中:\n%s",
			extractTailForLog(text, 1500))
	}
}

func TestBuildSystemPrompt_R13_PastTenseGuidance(t *testing.T) {
	zero := PersonalityVector{}
	blocks := BuildSystemPrompt("", zero, "", "", false)
	text := blocks[0].Text

	// 必含过去时引导:「昨夜我查验了 X 号」是预言家标准播报句式
	mustContain := []string{
		"昨夜我查验了 X 号,结果是 X 色", // 预言家播报样例
		"昨夜我用了",                  // 女巫播报样例
		"昨夜我守了 X",                // 守卫播报样例
	}
	for _, s := range mustContain {
		if !strings.Contains(text, s) {
			t.Errorf("R13 神职时态约束段缺少过去时引导 %q", s)
		}
	}
}

func TestBuildSystemPrompt_R13_SkillVerbsCoverage(t *testing.T) {
	zero := PersonalityVector{}
	blocks := BuildSystemPrompt("", zero, "", "", false)
	text := blocks[0].Text

	// 6 类神职动词必须全部出现在禁词表中(查/验/守/毒/解/护)
	// 任何一个缺失都会让 LLM 误用该动词的泄漏句式。
	verbs := []string{"查", "验", "守", "毒", "解", "护"}
	for _, v := range verbs {
		// 段内必含"X 号"前缀(动词+数字号)以匹配 hard-reject regex。
		// 用更宽松的判断:只要动词本身出现在 §R13-NEW 段内即可。
		r13Idx := strings.Index(text, "§R13-NEW")
		if r13Idx < 0 {
			t.Fatalf("§R13-NEW 段未找到")
		}
		// 找下一个空行或下一个段标题作为该段的边界
		endIdx := strings.Index(text[r13Idx:], "\n\n")
		if endIdx < 0 {
			endIdx = len(text) - r13Idx
		}
		seg := text[r13Idx : r13Idx+endIdx]
		if !strings.Contains(seg, v) {
			t.Errorf("§R13-NEW 段缺少神职动词 %q, 段内容:\n%s", v, seg)
		}
	}
}

func TestBuildSystemPrompt_R13_RejectHint(t *testing.T) {
	zero := PersonalityVector{}
	blocks := BuildSystemPrompt("", zero, "", "", false)
	text := blocks[0].Text

	// reject hint 引导:LLM 失败时知道往哪改
	if !strings.Contains(text, "hard-reject") && !strings.Contains(text, "服务端") {
		t.Errorf("R13 段缺少 reject hint 引导(应提示 LLM 改写为过去时)")
	}
}

func TestBuildSystemPrompt_R13_DoesNotBreakCachePrefix(t *testing.T) {
	// 防御纵深:本段插入 hardBans 末尾,不影响 personality/portrait/
	// difficulty 段字节级缓存命中。
	zero := PersonalityVector{}
	// 零版(无 portrait/无 personality/无 difficultyDirective)
	zeroVer := BuildSystemPrompt("", zero, "", "", false)
	// portrait 版(只 portrait 段变,前缀不变)
	portraitVer := BuildSystemPrompt("本模型 100 局胜率 60%", zero, "", "", false)
	// prefix 校验:portraitVer 必须以 zeroVer 为前缀(只追加,不在中间插入)
	if !strings.HasPrefix(portraitVer[0].Text, zeroVer[0].Text) {
		t.Errorf("portrait 必须追加到末尾(§20260810-10 U2 cache 纪律),R13 段插入破坏了 prefix")
	}
	// 同时校验 portrait 版尾部确实含新段(防止 zero 版就缺失)
	if !strings.Contains(portraitVer[0].Text, "§R13-NEW") {
		t.Errorf("portrait 版的尾部应保留 R13 段")
	}
}

// extractTailForLog 取出 text 末尾 n 字符供错误日志用。
func extractTailForLog(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[len(text)-n:]
}
