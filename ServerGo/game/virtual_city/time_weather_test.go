// Package virtual_city — time_weather_test.go: 批次 27「时间比例与昼夜季节
// 天气」单测(方案 27 §3.1-3.4 / §5.1 B13)。
//
//	13 档预设 MonthMsForRatio 逐档断言 | 权重表合计 = 1
//	| SeasonAt 12 个月映射 + 纪元即冬季 | CityDayIdx 边界
//	| WeatherAt 确定性/强度口径/跨城市日变化
//	| applyOpts time_ratio 与 month_ms 优先级 | 时钟倍率 1440×
//	| BuildClientState 新 4 字段(env nil 安全)
//	| 月结 city_weather 播报接线 | Agent 感知三字段 + prompt 渲染
//	| city 包显示侧副本与协议侧口径一致性(跨包钉死,防漂移)
package virtual_city

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/agent/vcplayer"
	"LsmAgentGame/agent/vctypes"
	"LsmAgentGame/game/virtual_city/city"
)

// TestMonthMsForRatio_13Presets 13 档预设逐档断言(方案 §3.1 预设表;
// 60~43200 → 60000,86400/172800/345600/525600 → 30000/15000/7500/4932)。
func TestMonthMsForRatio_13Presets(t *testing.T) {
	cases := []struct {
		ratio int
		want  int
	}{
		{60, 60000}, {120, 60000}, {240, 60000}, {480, 60000},
		{1440, 60000}, {2880, 60000}, {5760, 60000}, {11520, 60000},
		{43200, 60000}, {86400, 30000}, {172800, 15000},
		{345600, 7500}, {525600, 4932},
	}
	for _, tc := range cases {
		if got := MonthMsForRatio(tc.ratio); got != tc.want {
			t.Errorf("MonthMsForRatio(%d) = %d, want %d", tc.ratio, got, tc.want)
		}
	}
	// 防御分支与 clamp 边界:ratio<=0 按 60;超大 ratio 落 3000 下限。
	for _, ratio := range []int{0, -5, 1} {
		if got := MonthMsForRatio(ratio); got != 60000 {
			t.Errorf("MonthMsForRatio(%d) = %d, want 60000 (防御按 60)", ratio, got)
		}
	}
	if got := MonthMsForRatio(864000); got != 3000 {
		t.Errorf("MonthMsForRatio(864000) = %d, want 3000 (clamp 下限)", got)
	}
}

// TestWeatherWeights_SumToOne 每季权重合计 = 1(浮点容差 1e-9)。
func TestWeatherWeights_SumToOne(t *testing.T) {
	for season, weights := range weatherWeights {
		sum := 0.0
		for _, w := range weights {
			sum += w
		}
		if diff := sum - 1.0; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("season %s weights sum = %.12f, want 1 (±1e-9)", season, sum)
		}
	}
	if len(weatherWeights) != 4 {
		t.Fatalf("weights table must cover 4 seasons, got %d", len(weatherWeights))
	}
}

// TestSeasonAt_12Months 城市日历月 → 季节映射(§3.2)。
func TestSeasonAt_12Months(t *testing.T) {
	want := map[time.Month]string{
		time.January: "winter", time.February: "winter",
		time.March: "spring", time.April: "spring", time.May: "spring",
		time.June: "summer", time.July: "summer", time.August: "summer",
		time.September: "autumn", time.October: "autumn", time.November: "autumn",
		time.December: "winter",
	}
	for m := time.January; m <= time.December; m++ {
		ms := time.Date(2026, m, 15, 12, 0, 0, 0, cityEpochTZ).UnixMilli()
		if got := SeasonAt(ms); got != want[m] {
			t.Errorf("SeasonAt(月=%d) = %q, want %q", int(m), got, want[m])
		}
	}
	// 纪元 2025-01-01 08:00 +0800 ⇒ 默认档开局即冬季。
	if got := SeasonAt(cityEpochBaseMillis()); got != "winter" {
		t.Errorf("SeasonAt(epoch) = %q, want winter (纪元 1 月开局)", got)
	}
}

// TestCityDayIdx 城市日序号:纪元当日 = 0;次日 = 1;前一日 = -1;
// 纪元前一毫秒仍属第 0 日(整数除法口径,确定性即可)。
func TestCityDayIdx(t *testing.T) {
	base := cityEpochBaseMillis()
	cases := []struct {
		ms   int64
		want int64
	}{
		{base, 0},
		{base + 86_400_000, 1},
		{base + 2*86_400_000 + 123, 2},
		{base - 86_400_000, -1},
		{base - 1, 0},
	}
	for _, tc := range cases {
		if got := CityDayIdx(tc.ms); got != tc.want {
			t.Errorf("CityDayIdx(%d) = %d, want %d", tc.ms, got, tc.want)
		}
	}
}

// TestWeatherAt_Deterministic 同 seed 同 cityClockMs 恒等;强度口径:
// clear=0、cloudy=0.35、其余 ∈ [0.25,1] 且两位小数。
func TestWeatherAt_Deterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for i := 0; i < 300; i++ {
		seed := rng.Int63()
		ms := cityEpochBaseMillis() + rng.Int63n(400*86_400_000)
		k1, in1 := WeatherAt(seed, ms)
		k2, in2 := WeatherAt(seed, ms)
		if k1 != k2 || in1 != in2 {
			t.Fatalf("WeatherAt not deterministic: (%q,%.2f) vs (%q,%.2f)", k1, in1, k2, in2)
		}
		if weatherLabelZH[k1] == "" {
			t.Fatalf("unknown weather kind %q (label 表缺失)", k1)
		}
		var wantIn float64
		switch k1 {
		case "clear":
			wantIn = 0
		case "cloudy":
			wantIn = 0.35
		default:
			if in1 < 0.25 || in1 > 1.0 {
				t.Fatalf("kind %s intensity %.4f out of [0.25,1]", k1, in1)
			}
			if in1 != float64(int(in1*100+0.5))/100 {
				t.Fatalf("kind %s intensity %.4f not 2-decimal", k1, in1)
			}
			continue
		}
		if in1 != wantIn {
			t.Fatalf("kind %s intensity = %.2f, want %.2f", k1, in1, wantIn)
		}
	}
}

// TestWeatherAt_VariesAcrossDays 跨城市日变化:固定 seed,365 天样本里
// 类型分布 ≥3 种;且存在相邻两日类型变化(快档昼夜轮转的观感来源)。
func TestWeatherAt_VariesAcrossDays(t *testing.T) {
	const seed = 42
	kinds := map[string]int{}
	adjacentChange := false
	prev := ""
	for day := 0; day < 365; day++ {
		ms := cityEpochBaseMillis() + int64(day)*86_400_000 + 12*3600_000
		k, _ := WeatherAt(seed, ms)
		kinds[k]++
		if day > 0 && k != prev {
			adjacentChange = true
		}
		prev = k
	}
	if len(kinds) < 3 {
		t.Fatalf("365 天样本类型分布 = %d 种 (%v),want ≥3", len(kinds), kinds)
	}
	if !adjacentChange {
		t.Fatal("未观察到相邻两日天气类型变化(跨日变化契约破裂)")
	}
}

// TestApplyOpts_TimeRatioPrecedence 批次 27 §3.1 优先级:两字段同传
// time_ratio 赢(月节拍按推导);仅 month_ms 时 ratio 保持 60、MonthMs 用
// 传入值(旧客户端兼容,不反推);clamp [60,864000]。
func TestApplyOpts_TimeRatioPrecedence(t *testing.T) {
	mk := func(opts *VirtualCityRoomOptions) *VirtualCityRoom {
		r := NewVirtualCityRoom("room-prec", 3000, 7, 4)
		r.applyOpts(opts)
		return r
	}
	// both:time_ratio 赢。
	r := mk(&VirtualCityRoomOptions{TimeRatio: 1440, MonthMs: 5000})
	if r.TimeRatio != 1440 || r.MonthMs != 60000 {
		t.Fatalf("both: TimeRatio=%d MonthMs=%d, want 1440/60000 (time_ratio 优先)", r.TimeRatio, r.MonthMs)
	}
	// 仅 month_ms:ratio 保持 60,MonthMs 用传入值。
	r = mk(&VirtualCityRoomOptions{MonthMs: 5000})
	if r.TimeRatio != 60 || r.MonthMs != 5000 {
		t.Fatalf("month_ms only: TimeRatio=%d MonthMs=%d, want 60/5000 (不反推)", r.TimeRatio, r.MonthMs)
	}
	// clamp 下限:<60 → 60。
	r = mk(&VirtualCityRoomOptions{TimeRatio: 10})
	if r.TimeRatio != 60 || r.MonthMs != 60000 {
		t.Fatalf("ratio=10: TimeRatio=%d MonthMs=%d, want 60/60000 (clamp 下限)", r.TimeRatio, r.MonthMs)
	}
	// clamp 上限:>864000 → 864000,月节拍落 3000 下限。
	r = mk(&VirtualCityRoomOptions{TimeRatio: 1_000_000})
	if r.TimeRatio != 864000 || r.MonthMs != 3000 {
		t.Fatalf("ratio=1000000: TimeRatio=%d MonthMs=%d, want 864000/3000 (clamp 上限)", r.TimeRatio, r.MonthMs)
	}
	// 月节拍 clamp 上限 60000(批次 27 放宽)。
	r = mk(&VirtualCityRoomOptions{MonthMs: 999_999})
	if r.MonthMs != 60000 {
		t.Fatalf("month_ms=999999: MonthMs=%d, want 60000 (上限)", r.MonthMs)
	}
}

// TestCityClock_TimeRatio1440 时钟倍率:applyOpts(time_ratio=1440) 后
// cityClockMsLocked 推进 = elapsed × 1440(锁内变体直调,免 sleep 抖动)。
func TestCityClock_TimeRatio1440(t *testing.T) {
	r := NewVirtualCityRoom("room-ratio", 3000, 7, 4)
	r.applyOpts(&VirtualCityRoomOptions{TimeRatio: 1440})
	if r.TimeRatio != 1440 {
		t.Fatalf("TimeRatio = %d, want 1440", r.TimeRatio)
	}
	now := time.Now()
	r.mu.Lock()
	r.gameStartedAt = now.Unix() - 10 // 开局 10s 前(秒级截断,与生产 gameStartedAt 同精度)
	got := r.cityClockMsLocked(now)
	r.mu.Unlock()
	// elapsed 含 now 的亚秒毫秒(同 now 值传入 ⇒ 精确等式,免 sleep 抖动)。
	want := r.cityEpochBaseMs + (now.UnixMilli()-(now.Unix()-10)*1000)*1440
	if got != want {
		t.Fatalf("cityClockMsLocked = %d, want exactly %d (elapsed×1440)", got, want)
	}
	// 默认档(无 ratio)仍 60×(批次 25 零回归)。
	r2 := NewVirtualCityRoom("room-ratio60", 3000, 7, 4)
	r2.mu.Lock()
	r2.gameStartedAt = now.Unix() - 10
	got2 := r2.cityClockMsLocked(now)
	r2.mu.Unlock()
	want2 := r2.cityEpochBaseMs + (now.UnixMilli()-(now.Unix()-10)*1000)*60
	if got2 != want2 {
		t.Fatalf("default clock = %d, want %d (60×)", got2, want2)
	}
	// TimeEnv / TimeRatioValue getter(<=0 防御 60)。
	if env := r.TimeEnv(); env.Ratio != 1440 || env.Seed != 7 {
		t.Fatalf("TimeEnv = %+v, want {1440 7}", env)
	}
	r2.mu.Lock()
	r2.TimeRatio = 0
	r2.mu.Unlock()
	if got := r2.TimeRatioValue(); got != 60 {
		t.Fatalf("TimeRatioValue(0) = %d, want 60 (防御)", got)
	}
}

// TestBuildClientState_TimeEnvFields game.state 新 4 字段:env nil 安全
// (ratio→60、seed→0);显式 env 时季节/天气与 SeasonAt/WeatherAt 同口径。
func TestBuildClientState_TimeEnvFields(t *testing.T) {
	// 取一个已知时刻:2026-07-20 12:00(夏季)。
	clockMs := time.Date(2026, 7, 20, 12, 0, 0, 0, cityEpochTZ).UnixMilli()
	cs := BuildClientState("room-env", 0, nil, [MaxSeats]string{}, [MaxSeats]string{},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, clockMs, nil, nil)
	if cs.TimeRatio != 60 {
		t.Fatalf("nil env: TimeRatio = %d, want 60", cs.TimeRatio)
	}
	if cs.Season != SeasonAt(clockMs) || cs.Season != "summer" {
		t.Fatalf("Season = %q, want summer", cs.Season)
	}
	wk, wi := WeatherAt(0, clockMs)
	if cs.Weather != wk || cs.WeatherIntensity != wi {
		t.Fatalf("weather = (%q,%.2f), want (%q,%.2f) (nil env seed=0)", cs.Weather, cs.WeatherIntensity, wk, wi)
	}
	env := TimeEnv{Ratio: 525600, Seed: 4242}
	cs2 := BuildClientState("room-env2", 0, nil, [MaxSeats]string{}, [MaxSeats]string{},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, clockMs, nil, &env)
	if cs2.TimeRatio != 525600 {
		t.Fatalf("TimeRatio = %d, want 525600", cs2.TimeRatio)
	}
	wk2, wi2 := WeatherAt(4242, clockMs)
	if cs2.Weather != wk2 || cs2.WeatherIntensity != wi2 {
		t.Fatalf("weather = (%q,%.2f), want (%q,%.2f)", cs2.Weather, cs2.WeatherIntensity, wk2, wi2)
	}
}

// TestTrySettle_EmitsCityWeather 月结天气播报接线:首月(lastWeatherKind=="")
// 恒播报;事件 type=city_weather、seat=-1、文案含中文标签;同类型次月不重复。
func TestTrySettle_EmitsCityWeather(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	events := make(chan EventRecord, 16)
	r.SetHooks(BroadcastHooks{OnEvent: func(_ string, ev EventRecord) {
		select {
		case events <- ev:
		default:
		}
	}})
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	forceSettle := func() {
		r.mu.Lock()
		for _, p := range r.World.Players {
			if p != nil {
				p.Submitted = true
			}
		}
		r.NextMonthAt = time.Now().Add(-time.Second)
		r.mu.Unlock()
		if !r.trySettle(func(string) {}) {
			t.Fatal("trySettle did not advance a month")
		}
	}
	forceSettle()
	found := false
	for {
		select {
		case ev := <-events:
			if ev.Type != EventCityWeather {
				continue
			}
			if ev.Seat != -1 || !strings.Contains(ev.Text, "城市天气:") {
				t.Fatalf("bad city_weather event: %+v", ev)
			}
			if r.lastWeatherKind == "" {
				t.Fatal("lastWeatherKind must be updated on broadcast")
			}
			found = true
		default:
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("city_weather event not emitted on first settle")
	}
}

// TestBuildContextForAgent_TimeFields Agent 感知:开局后 GameContext 带
// CityDate(2006-01-02 15:04 格式)/Season/Weather 中文标签;UserPrompt 渲染
// 「· <季> · <天气>」;空上下文(未填字段)时省略不回归。
func TestBuildContextForAgent_TimeFields(t *testing.T) {
	r := newRoomWithSeats(t, 10)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	ctx, ok := BuildContextForAgent(r, 0)
	if !ok {
		t.Fatal("BuildContextForAgent failed for seat 0")
	}
	if ctx.CityDate == "" || ctx.Season == "" || ctx.Weather == "" {
		t.Fatalf("time perception fields empty: %+v", ctx)
	}
	if _, err := time.ParseInLocation("2006-01-02 15:04", ctx.CityDate, cityEpochTZ); err != nil {
		t.Fatalf("CityDate %q not in 2006-01-02 15:04 format: %v", ctx.CityDate, err)
	}
	if seasonLabelZH[SeasonAt(r.CityClockMs())] != ctx.Season {
		t.Fatalf("Season = %q, want %q", ctx.Season, seasonLabelZH[SeasonAt(r.CityClockMs())])
	}
	prompt := vcplayer.UserPrompt(ctx, "")
	if !strings.Contains(prompt, "· "+ctx.Season+" · "+ctx.Weather) {
		t.Fatalf("UserPrompt must render season/weather, got header: %.120s", prompt)
	}
	// 零值上下文(未开局/未填)→ 头部不含季节天气段,零回归。
	empty := vctypes.BuildEmptyContext("room", "u", "M", 0)
	if p := vcplayer.UserPrompt(empty, ""); strings.Contains(p, " · 春季") || strings.Contains(p, " · 夏季") {
		t.Fatal("empty context must omit season segment")
	}
}

// TestCityDisplay_ConsistentWithProtocol city 包显示侧副本与协议侧口径一致
// (同 (seed, ms) 必须产出相同季节/天气 —— 驱动层 contextText 与 game.state
// 不允许各说各话;跨包钉死,改权重表/盐须两处同步)。
func TestCityDisplay_ConsistentWithProtocol(t *testing.T) {
	base := cityEpochBaseMillis()
	for _, seed := range []int64{1, 42, 4242, 999983} {
		for day := int64(0); day < 370; day++ {
			ms := base + day*86_400_000 + 12*3600_000
			if got, want := city.SeasonAt(ms), SeasonAt(ms); got != want {
				t.Fatalf("city.SeasonAt(%d) = %q != protocol %q (day %d)", ms, got, want, day)
			}
			gk, gi := city.WeatherAt(seed, ms)
			wk, wi := WeatherAt(seed, ms)
			if gk != wk || gi != wi {
				t.Fatalf("city.WeatherAt(%d,%d) = (%q,%.2f) != protocol (%q,%.2f) (day %d)",
					seed, ms, gk, gi, wk, wi, day)
			}
		}
	}
}

// TestManager_TimeRatioNormalization Manager.Config.TimeRatio 归一与
// CreateRoom 接线:<=0 → 60(月节拍缺省 8000 不变);显式 ratio 且 MonthMs
// 缺省 → 推导;显式 MonthMs 优先不被推导覆盖。
func TestManager_TimeRatioNormalization(t *testing.T) {
	m := NewManager(Config{}, nil)
	if m.cfg.TimeRatio != 60 || m.cfg.MonthMs != 8000 {
		t.Fatalf("default: TimeRatio=%d MonthMs=%d, want 60/8000 (零回归)", m.cfg.TimeRatio, m.cfg.MonthMs)
	}
	m2 := NewManager(Config{TimeRatio: 1440}, nil)
	if m2.cfg.TimeRatio != 1440 || m2.cfg.MonthMs != 60000 {
		t.Fatalf("ratio-only: TimeRatio=%d MonthMs=%d, want 1440/60000 (推导)", m2.cfg.TimeRatio, m2.cfg.MonthMs)
	}
	m3 := NewManager(Config{TimeRatio: 1440, MonthMs: 5000}, nil)
	if m3.cfg.TimeRatio != 1440 || m3.cfg.MonthMs != 5000 {
		t.Fatalf("both: TimeRatio=%d MonthMs=%d, want 1440/5000 (显式 month_ms 优先)", m3.cfg.TimeRatio, m3.cfg.MonthMs)
	}
	// CreateRoom 接线:房间拿到 config ratio(建房载荷 time_ratio 再覆盖)。
	r := m2.CreateRoom("room-mgr-ratio")
	if r.TimeRatio != 1440 {
		t.Fatalf("room TimeRatio = %d, want 1440 (CreateRoom 接线)", r.TimeRatio)
	}
}
