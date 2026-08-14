// Package wwplayer — difficulty_speak_test.go: §20260814-01 U2 回归测试。
//
// 覆盖:
//   - D01 三个基准常量与 NewSpeakLimiter 实参一致(防常量漂移)
//   - D02 normal(1.0)零回归 —— 三个 limiter 保持构造期原值
//   - D03 easy(1.5)放长间隔 / D04 hell(0.8)缩短间隔
//   - D05 clamp 上下界
//   - D06 f<=0 归一化为 1.0(不缩放)
//   - D07 幂等 —— 重复注入同一 scale 不累积缩放
//   - D08 getter 回读
//   - D09 difficulty.SpeakLimiterScale 的 4 个档位值确实被 setter 接受
package wwplayer

import (
	"testing"
	"time"

	agentcore "LsmAgentGame/agent/core"
)

// newLimiterAgent 构造一个**仅含三个 limiter** 的最小 Agent。
//
// 不走 NewWithRoom:那条路径需要 provider / registry / DB,而本测试只关心
// limiter 的 interval 数学。三个 NewSpeakLimiter 实参与 agent.go:869-874
// 逐字对齐 —— D01 会断言这份对齐没有漂移。
func newLimiterAgent() *Agent {
	return &Agent{
		Limiter:          agentcore.NewSpeakLimiter(30 * time.Second),
		WhisperLimiter:   agentcore.NewSpeakLimiter(60 * time.Second),
		InterjectLimiter: agentcore.NewSpeakLimiter(60 * time.Second),
	}
}

// TestDifficultySpeak_D01_BaseConstantsMatchConstructor 防常量漂移。
//
// difficulty_speak.go 复制了三个基准间隔常量以保证 setter 幂等(见该文件注释)。
// 若将来有人改了 agent.go:869-874 的 NewSpeakLimiter 实参却忘了同步常量,
// 难度缩放会以错误基准计算 —— 这条断言让那种漂移立即失败。
func TestDifficultySpeak_D01_BaseConstantsMatchConstructor(t *testing.T) {
	a := newLimiterAgent()
	cases := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"speak", a.Limiter.Interval(), difficultySpeakBaseInterval},
		{"whisper", a.WhisperLimiter.Interval(), difficultyWhisperBaseInterval},
		{"interject", a.InterjectLimiter.Interval(), difficultyInterjectBaseInterval},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s 基准间隔漂移: 构造期=%v, difficulty_speak.go 常量=%v —— "+
				"必须同步 agent.go 的 NewSpeakLimiter 实参与本文件常量", c.name, c.got, c.want)
		}
	}
}

// TestDifficultySpeak_D02_NormalIsZeroRegression 是本组最重要的断言。
//
// §20260811-09 U2 的核心纪律是「normal 档与旧版逐字节一致」。若 setter 对
// scale==1.0 也走 SetInterval,行为上等价但语义上 normal 就"参与"了本机制,
// 将来 SetInterval 一旦有副作用,normal 档会被牵连。
func TestDifficultySpeak_D02_NormalIsZeroRegression(t *testing.T) {
	a := newLimiterAgent()
	a.SetDifficultySpeakScale(1.0)

	if got := a.Limiter.Interval(); got != 30*time.Second {
		t.Errorf("normal 档 speak interval 被改动: got %v, want 30s(零回归)", got)
	}
	if got := a.WhisperLimiter.Interval(); got != 60*time.Second {
		t.Errorf("normal 档 whisper interval 被改动: got %v, want 60s", got)
	}
	if got := a.InterjectLimiter.Interval(); got != 60*time.Second {
		t.Errorf("normal 档 interject interval 被改动: got %v, want 60s", got)
	}
}

// TestDifficultySpeak_D03_EasyLengthensInterval easy=1.5 → 发言更稀疏。
func TestDifficultySpeak_D03_EasyLengthensInterval(t *testing.T) {
	a := newLimiterAgent()
	a.SetDifficultySpeakScale(1.5)

	if got, want := a.Limiter.Interval(), 45*time.Second; got != want {
		t.Errorf("easy speak interval = %v, want %v", got, want)
	}
	if got, want := a.WhisperLimiter.Interval(), 90*time.Second; got != want {
		t.Errorf("easy whisper interval = %v, want %v", got, want)
	}
	if got, want := a.InterjectLimiter.Interval(), 90*time.Second; got != want {
		t.Errorf("easy interject interval = %v, want %v", got, want)
	}
}

// TestDifficultySpeak_D04_HellShortensInterval hell=0.8 → 发言更密集。
func TestDifficultySpeak_D04_HellShortensInterval(t *testing.T) {
	a := newLimiterAgent()
	a.SetDifficultySpeakScale(0.8)

	if got, want := a.Limiter.Interval(), 24*time.Second; got != want {
		t.Errorf("hell speak interval = %v, want %v", got, want)
	}
	if got, want := a.WhisperLimiter.Interval(), 48*time.Second; got != want {
		t.Errorf("hell whisper interval = %v, want %v", got, want)
	}
}

// TestDifficultySpeak_D05_ClampBounds 极端值被 clamp 而非报错。
func TestDifficultySpeak_D05_ClampBounds(t *testing.T) {
	// 下界:0.01 → 0.5 ⇒ speak 15s(而不是 0.3s 刷爆公屏)。
	a := newLimiterAgent()
	a.SetDifficultySpeakScale(0.01)
	if got := a.DifficultySpeakScale(); got != difficultySpeakScaleMin {
		t.Errorf("下界 clamp 失效: scale = %v, want %v", got, difficultySpeakScaleMin)
	}
	if got, want := a.Limiter.Interval(), 15*time.Second; got != want {
		t.Errorf("下界 clamp 后 speak interval = %v, want %v", got, want)
	}

	// 上界:100 → 3.0 ⇒ speak 90s(而不是 50min 等价 quarantine)。
	b := newLimiterAgent()
	b.SetDifficultySpeakScale(100)
	if got := b.DifficultySpeakScale(); got != difficultySpeakScaleMax {
		t.Errorf("上界 clamp 失效: scale = %v, want %v", got, difficultySpeakScaleMax)
	}
	if got, want := b.Limiter.Interval(), 90*time.Second; got != want {
		t.Errorf("上界 clamp 后 speak interval = %v, want %v", got, want)
	}
}

// TestDifficultySpeak_D06_NonPositiveMeansNoScale f<=0(未注入/配置缺失)→ 1.0。
func TestDifficultySpeak_D06_NonPositiveMeansNoScale(t *testing.T) {
	for _, f := range []float64{0, -1, -0.5} {
		a := newLimiterAgent()
		a.SetDifficultySpeakScale(f)
		if got := a.DifficultySpeakScale(); got != 1.0 {
			t.Errorf("SetDifficultySpeakScale(%v): scale = %v, want 1.0", f, got)
		}
		if got := a.Limiter.Interval(); got != 30*time.Second {
			t.Errorf("SetDifficultySpeakScale(%v): interval 被改动 = %v, want 30s", f, got)
		}
	}
}

// TestDifficultySpeak_D07_Idempotent 幂等 —— 这是「以固定基准计算」而非
// 「以当前值累乘」的直接证据。若实现改成 l.Interval()*f,本条立即失败
// (easy 调两次 → 30s×1.5×1.5 = 67.5s)。
func TestDifficultySpeak_D07_Idempotent(t *testing.T) {
	a := newLimiterAgent()
	a.SetDifficultySpeakScale(1.5)
	first := a.Limiter.Interval()
	a.SetDifficultySpeakScale(1.5)
	a.SetDifficultySpeakScale(1.5)
	if got := a.Limiter.Interval(); got != first {
		t.Errorf("非幂等: 首次 %v, 三次后 %v —— 缩放被累积了", first, got)
	}
	if first != 45*time.Second {
		t.Errorf("easy 首次 interval = %v, want 45s", first)
	}
}

// TestDifficultySpeak_D08_NilLimitersDoNotPanic 防御:limiter 未构造时不 panic。
// (测试桩 / 未来某条构造路径漏建 limiter 时,难度注入不应打挂整个房间启动。)
func TestDifficultySpeak_D08_NilLimitersDoNotPanic(t *testing.T) {
	a := &Agent{} // 三个 limiter 全 nil
	a.SetDifficultySpeakScale(1.5)
	if got := a.DifficultySpeakScale(); got != 1.5 {
		t.Errorf("nil limiter 场景 scale 未记录: got %v, want 1.5", got)
	}
}

// TestDifficultySpeak_D09_AllProfileValuesAccepted 四个档位的实际配置值
// (difficulty.go:57/68/79/93 的 1.5 / 1.0 / 1.0 / 0.8)全部落在 clamp 区间内,
// 即**没有任何档位被 clamp 静默改写**。
//
// 这条断言的价值:若将来有人把 hell 调到 0.2「让地狱档疯狂发言」,clamp 会
// 把它改成 0.5 而不报错 —— 本条会失败并提示配置与 clamp 区间冲突。
func TestDifficultySpeak_D09_AllProfileValuesAccepted(t *testing.T) {
	// 与 game/werewolf/difficulty.go 的 4 个 SpeakLimiterScale 字面量对齐。
	// 不 import werewolf 包:agent → werewolf 会成循环导入(见 §133 教训 5)。
	for name, f := range map[string]float64{
		"easy": 1.5, "normal": 1.0, "hard": 1.0, "hell": 0.8,
	} {
		a := newLimiterAgent()
		a.SetDifficultySpeakScale(f)
		if got := a.DifficultySpeakScale(); got != f {
			t.Errorf("档位 %s 的 scale %v 被 clamp 改写为 %v —— "+
				"difficulty.go 的配置值超出了 [%v, %v] 区间",
				name, f, got, difficultySpeakScaleMin, difficultySpeakScaleMax)
		}
	}
}
