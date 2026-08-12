package werewolf

import (
	"testing"
	"time"
)

func TestPhaseDeadlineScalesWithSeatCount_13p(t *testing.T) {
	gs7 := &GameState{SeatCount: 7}
	gs13 := &GameState{SeatCount: 13}

	for _, phase := range []Phase{PhaseSpeak, PhaseVote, PhaseNightWolves} {
		setPhaseAndDeadline(gs7, phase)
		setPhaseAndDeadline(gs13, phase)

		d7 := int(time.Until(gs7.PhaseDeadlineAt).Seconds())
		d13 := int(time.Until(gs13.PhaseDeadlineAt).Seconds())

		// config 默认 phase_deadline_sec 已较高,speak/vote 可能持平;这里主要验证
		// 不因为 seatCount 增加而变短,且 13p 的 floor 公式本身大于 7p。
		if d13 < d7 {
			t.Errorf("%v: 13p(%d) 不应短于 7p(%d)", phase, d13, d7)
		}
	}
}

func TestPhaseDeadline_FloorFor13p(t *testing.T) {
	// 验证 13 人局 acting phase floor 不低于 7 人局,且为正。
	gs13 := &GameState{SeatCount: 13}
	gs7 := &GameState{SeatCount: 7}
	setPhaseAndDeadline(gs13, PhaseSpeak)
	setPhaseAndDeadline(gs7, PhaseSpeak)

	d13 := int(time.Until(gs13.PhaseDeadlineAt).Seconds())
	d7 := int(time.Until(gs7.PhaseDeadlineAt).Seconds())
	if d13 < d7 {
		t.Errorf("13p(%d) speak deadline 应不低于 7p(%d)", d13, d7)
	}
	if d13 <= 0 || d7 <= 0 {
		t.Errorf("deadline 必须为正, got 13p=%d 7p=%d", d13, d7)
	}
}

func TestWatchdogDeadlineScalesWithSeatCount_13p(t *testing.T) {
	d7 := phaseWatchdogDeadlineFor(7)
	d13 := phaseWatchdogDeadlineFor(13)

	if d7 != 240*time.Second {
		t.Errorf("7p watchdog deadline 期望 240s, got %v", d7)
	}
	if d13 != 360*time.Second {
		t.Errorf("13p watchdog deadline 期望 360s, got %v", d13)
	}
}

func TestWatchdogDeadline_CapsAt20(t *testing.T) {
	d20 := phaseWatchdogDeadlineFor(20)
	d13 := phaseWatchdogDeadlineFor(13)
	if d20 != d13 {
		t.Errorf("20p 应被 cap 到 13p 值, got %v vs %v", d20, d13)
	}
}
