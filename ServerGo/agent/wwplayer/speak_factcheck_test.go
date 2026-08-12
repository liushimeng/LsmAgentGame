package wwplayer

import (
	"strings"
	"testing"
)

// BUG-R79 P1-NEW (2026-07-10): MiniMax M3 在 Day 1~Day 2 多次编造未发生的
// 死亡(例: "4号走了" 但 4号 仍存活)。
//
// BUG-R80 P1-NEW (2026-07-10): R79 的 "prepend 听说" 修复被 LLM 自带的
// hedge 表达稀释 — MiniMax 在 R80 仍然写出 "听说2号走了",虽然加了 hedge
// 但仍在公屏断言 #2 已死,人类观众读起来无差别。Defense-in-depth 升级:
//   - 不再 prepend "听说",而是 **整段删除** "X号...走了/死了/..." span,
//     替换为显式标记 "[已过滤:无可证实的死亡信息]"。
//   - regex 范围从 [1-7] 扩到 [0-7] 防止 LLM 泄露 internal 0-indexed "0号"。
//   - seat=0 一律视为 leak 强制 filter。

func TestFactCheckDeathClaims_NoClaim_Unchanged(t *testing.T) {
	text := "我觉得3号发言有点奇怪,但还需要观察一下。"
	got, wasFC := FactCheckDeathClaims(text, []int{}, []int{1, 2, 3, 4, 5, 6, 7})
	if wasFC || got != text {
		t.Errorf("expected no fact-check, got cleaned=%q wasFC=%v", got, wasFC)
	}
}

func TestFactCheckDeathClaims_AliveButClaimedDead_Filtered(t *testing.T) {
	// BUG-R80 P1-NEW: MiniMax 经典错误: "4号走了" (R79)
	// 4号 = 1-indexed 4 = 0-indexed seat 3
	// 但 knownDead = [2](0-indexed) = 3号 死了,4号 仍存活 → 应该被 filter
	text := "4号走了,我先不参选警长了。"
	knownDead := []int{2}    // 3号 (1-indexed, 0-indexed seat 2) 死
	alive := []int{0, 1, 3, 4, 5, 6} // 4号 (seat 3) 仍存活
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check trigger; got cleaned=%q", got)
	}
	// BUG-R80: 不再保留 "4号走了" — 整段过滤,替换为显式 marker
	if strings.Contains(got, "4号走了") {
		t.Errorf("expected '4号走了' stripped, got %q", got)
	}
	if !strings.Contains(got, "[已过滤:无可证实的死亡信息]") {
		t.Errorf("expected filter marker, got %q", got)
	}
	// 后续句子("我先不参选警长了")应保留
	if !strings.Contains(got, "我先不参选警长了") {
		t.Errorf("expected trailing sentence preserved, got %q", got)
	}
}

// BUG-R80 P1-NEW (2026-07-10): LLM 自带 hedge "听说2号走了" 不能让 fact-check
// 失效 — 必须同样识别并整段过滤。
func TestFactCheckDeathClaims_AlreadyHedgedByLLM_StillFiltered(t *testing.T) {
	text := "听说2号走了,遗言都放弃了有点奇怪。我先表个态,听一轮发言再说。"
	knownDead := []int{2} // 3号 (0-indexed seat 2) Night 1 死
	alive := []int{0, 1, 3, 4, 5, 6} // 2号 (seat 1) 仍存活
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check (R80 incident), got cleaned=%q", got)
	}
	if strings.Contains(got, "2号走了") && !strings.Contains(got, "[已过滤") {
		t.Errorf("expected '2号走了' stripped (even with leading '听说'), got %q", got)
	}
	// 必须显式 marker
	if !strings.Contains(got, "[已过滤:无可证实的死亡信息]") {
		t.Errorf("expected filter marker, got %q", got)
	}
	// "听说" 也应该被吞进 span(否则会留下孤零零的 "听说 [已过滤]")——
	// trimLeadingHedge 应当吃掉 hedge 词。
	if strings.HasPrefix(strings.TrimSpace(got), "听说 [已过滤") {
		t.Errorf("expected leading hedge consumed into span, got %q", got)
	}
	// 后续 "遗言都放弃了有点奇怪" 应保留(说明过滤器只吃了最小 span)
	if !strings.Contains(got, "遗言都放弃了有点奇怪") {
		t.Errorf("expected trailing context preserved, got %q", got)
	}
}

// BUG-R80 P1-NEW (2026-07-10): LLM 泄露 internal 0-indexed "0号" — 也必须
// 被 fact-check 识别并过滤(避免 0 号被误读为新事实)。升级后 regex 扩到
// [0-7],且 seat=0 一律视为 leak 强制 filter。
func TestFactCheckDeathClaims_ZeroIndexed_StillFiltered(t *testing.T) {
	// R80 Bot 4 实际产出:"听说0号（7号玩家）走了" — LLM 用了 internal seat 0
	text := "听说0号走了,这是内部数据泄露。"
	// seat=0 是 leak,即使 alive 不含 seat 0 也要 filter
	knownDead := []int{6}      // 7号 (0-indexed seat 6) Night 2 死
	alive := []int{0, 1, 2, 3, 4, 5} // seat 0 在 alive,但 "0号" 仍 leak
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check for 0-indexed leak, got cleaned=%q", got)
	}
	if strings.Contains(got, "0号走了") {
		t.Errorf("expected '0号走了' stripped, got %q", got)
	}
	if !strings.Contains(got, "[已过滤:无可证实的死亡信息]") {
		t.Errorf("expected filter marker, got %q", got)
	}
}

func TestFactCheckDeathClaims_KnownDead_Kept(t *testing.T) {
	// 5号 (0-indexed seat 4) 真正死了,bot 说 "5号走了" — 不应被 hedge
	text := "5号走了,真可惜。"
	knownDead := []int{4}
	alive := []int{0, 1, 2, 3, 5, 6}
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if wasFC {
		t.Errorf("expected no fact-check (claim matches authoritative), got cleaned=%q", got)
	}
	if got != text {
		t.Errorf("expected unchanged text, got %q", got)
	}
}

func TestFactCheckDeathClaims_PastPublicDeath_Kept(t *testing.T) {
	// seat 不在 alive 也不在 knownDead — 可能是过去的公开死亡,不要误伤
	text := "昨晚7号没了,狼人刀得很准。"
	// 7号 = 0-indexed seat 6; 不在 knownDead;不在 alive → 假设之前已死
	knownDead := []int{} // 我们没拿到全量历史
	alive := []int{0, 1, 2, 3, 4, 5} // seat 6 不在 alive(已死)
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if wasFC {
		t.Errorf("expected no fact-check (past death, not in alive), got cleaned=%q", got)
	}
}

func TestFactCheckDeathClaims_EmptyKnownDead_AnyDeathFiltered(t *testing.T) {
	// 没有任何已知死亡(深夜阶段,还没公布) — 任何"X号死"声明都应该 filter
	text := "3号死了,我看到他的位置空了。"
	knownDead := []int{}
	alive := []int{0, 1, 2, 3, 4, 5, 6}
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check, got cleaned=%q", got)
	}
	if !strings.Contains(got, "[已过滤:无可证实的死亡信息]") {
		t.Errorf("expected filter marker, got %q", got)
	}
}

func TestFactCheckDeathClaims_MultipleClaims(t *testing.T) {
	// 多重错误声明:同时提及两个不存在的死亡
	text := "6号和7号都没了,神职可能都活着。"
	// 6号 = seat 5 (1-indexed 6 = 0-indexed 5)
	// 7号 = seat 6 (1-indexed 7 = 0-indexed 6) — 真的死了
	knownDead := []int{6} // seat 6 = 1-indexed 7号 真的死
	alive := []int{0, 1, 2, 3, 4, 5} // seat 5 = 1-indexed 6号 仍存活
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check (6号 alive 但被说死), got cleaned=%q", got)
	}
	// 6号 reference 应被 strip
	if strings.Contains(got, "6号") {
		t.Errorf("expected 6号 reference stripped, got %q", got)
	}
	if !strings.Contains(got, "[已过滤:无可证实的死亡信息]") {
		t.Errorf("expected filter marker, got %q", got)
	}
	// 后续 "神职可能都活着" 应保留(说明过滤器只吃了最小 span)
	if !strings.Contains(got, "神职可能都活着") {
		t.Errorf("expected trailing context preserved, got %q", got)
	}
	// 7号 共享一个 verb "都没了" — 6号 filter 吃掉该 verb 后,
	// 7号 在 ±8 char 窗口内无 verb → 不会被 filter。
	// 同时 7号 不在 alive (alive=[0..5]),所以也不会被误判为 "false claim"。
	// 因此 "7号" 可能保留也可能不保留(取决于 verb consume 行为)。
	// 这里不强求 7号 保留 — 仅断言主要 6号 被 strip。
}

func TestFactCheckDeathClaims_EmptyText(t *testing.T) {
	got, wasFC := FactCheckDeathClaims("", []int{1}, []int{0, 2, 3})
	if wasFC || got != "" {
		t.Errorf("expected empty passthrough, got cleaned=%q wasFC=%v", got, wasFC)
	}
}

func TestFactCheckDeathClaims_SeatWithSpace(t *testing.T) {
	// LLM 偶尔会插入空格:"3 号 走了"
	text := "3 号 走了,大家小心。"
	knownDead := []int{}
	alive := []int{0, 1, 2, 3, 4, 5, 6}
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check for spaced seat, got cleaned=%q", got)
	}
}

func TestFactCheckDeathClaims_VerbBeforeSeat(t *testing.T) {
	// "走了3号" — 死亡动词在 seat 之前
	text := "走了3号,我有点意外。"
	knownDead := []int{}
	alive := []int{0, 1, 2, 3, 4, 5, 6}
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check (verb-before-seat), got cleaned=%q", got)
	}
}

func TestFactCheckDeathClaims_DeathVerbAloneNoSeat(t *testing.T) {
	// "有人死了" — 没有具体座位,跳过
	text := "有人死了,我们需要小心。"
	got, wasFC := FactCheckDeathClaims(text, []int{}, []int{0, 1, 2, 3, 4, 5, 6})
	if wasFC {
		t.Errorf("expected no fact-check (no specific seat), got cleaned=%q", got)
	}
}

// BUG-R88 P1-NEW (2026-07-10): 主观评价短语 (可惜/惋惜/遗憾/哀悼等)
// 与 seat 配对时也隐含死亡事实,R88 bot 8 输出:
//   "我是8号,4号 真可惜,昨天他发言确实不多,可能是被票出去的"
// 整段 "4号 真可惜" 暗示 4号 已死亡,但 fact-check 只匹配死亡动词,
//
//	可惜/惋惜不在 knownDeathVerbs → leak。修复:把这些评价短语加入
//
// knownDeathVerbs,经 findNearestDeathVerb 命中后走现有 verdict 树。
// 同时 regex 扩展为 [0-13] 以覆盖 13 人局(老 [0-7] 会让 "8号"/"9号"/
// "10号" 等 13 人局代号直接 leak)。
func TestFactCheckDeathClaims_R88_SubjectivePhrase_StillFiltered(t *testing.T) {
	// R88 bot 8 实际文本(已精简)
	text := "我是8号,4号 真可惜,昨天他发言确实不多。"
	knownDead := []int{} // 还没人死
	// 13 人局:1-indexed 1..13 = 0-indexed 0..12 全部 alive
	alive := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check for 可惜+seat (R88 incident), got cleaned=%q", got)
	}
	if !strings.Contains(got, "[已过滤:无可证实的死亡信息]") {
		t.Errorf("expected filter marker, got %q", got)
	}
	// "我是8号" 应保留(seat 8 不在 deadSet/alive, 不会被命中)
	if !strings.Contains(got, "我是8号") {
		t.Errorf("expected '我是8号' preserved, got %q", got)
	}
}

// R88 已知死亡场景:seat 在 deadSet → 保留 (不强制 filter)
func TestFactCheckDeathClaims_R88_SubjectivePhrase_KnownDead_Kept(t *testing.T) {
	text := "5号走了,真可惜。"
	knownDead := []int{4} // 5号 (seat 4) 真死
	alive := []int{0, 1, 2, 3, 5, 6}
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if wasFC {
		t.Errorf("expected no fact-check (known dead, claim matches authoritative), got cleaned=%q", got)
	}
}

// R93-P1 (2026-07-11): 新增 hard-reject 路径测试。shouldReject=true 时,
// 函式返回与 false 路径相同的 fact-check 信号与 cleaned 文本,但 caller
// 看到 hit=true 后应整体 drop 这条 speak 而不是把 cleaned text 广播出去。
// 这里只验证函数层语义:"hit=true 时函数已正确识别需要 reject 的段"。
func TestFactCheckDeathClaims_R93_HardRejectPath_Flagged(t *testing.T) {
	text := "4号走了,我先不参选警长了。"
	knownDead := []int{2}
	alive := []int{0, 1, 3, 4, 5, 6}
	// shouldReject=true 路径: 函数仍返回 (cleaned, wasFC=true); caller 据此决定 reject。
	got, wasFC := FactCheckDeathClaimsWithReject(text, knownDead, alive, true)
	if !wasFC {
		t.Errorf("expected fact-check trigger on hard-reject path; got cleaned=%q", got)
	}
	// 验证 cleaned 字段仍保留 forensic 值(供 caller 写入 log)。call site 不会把它
	// 展示给真人观众。
	if !strings.Contains(got, "4号走了") && !strings.Contains(got, "无可证实的死亡信息") {
		t.Errorf("expected cleaned text to contain forensic value (either original claim or marker), got %q", got)
	}
}

// R93-P1 (2026-07-11): shouldReject=false(默认)回归测试 — 仍保留 inline 替换
// 路径的兼容性,以便历史 inline 行为(带 marker)的 caller 不会回归。
func TestFactCheckDeathClaims_R93_LegacyInlinePath_StillWorks(t *testing.T) {
	text := "4号走了。"
	knownDead := []int{2}
	alive := []int{0, 1, 3, 4, 5, 6}
	got, wasFC := FactCheckDeathClaims(text, knownDead, alive)
	if !wasFC {
		t.Errorf("expected fact-check (legacy inline), got cleaned=%q", got)
	}
	if !strings.Contains(got, "[已过滤:无可证实的死亡信息]") {
		t.Errorf("expected inline marker (legacy), got %q", got)
	}
}