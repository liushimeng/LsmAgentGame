// Package virtual_city — mc_event.go: 事件级采集(批次73 O2,
// 框架 §4.3 七字段)+ events.jsonl 落盘(gate 专属)。
//
// 载体:World.MCEvents 环形缓冲(cap mcEventCap,满则丢最旧 +
// World.EventDropped 丢计数)。recordEvent helper nil 安全。
//
// 纪律(方案 §3):
//   - **零 rand**:事件采集纯旁路;
//   - **零账目语义改动**:六类触发点只加 recordEvent 调用;
//   - **零 panic**:落盘失败 zap warn;聚合失败静默降级。
//
// ⚠ 口径标注(方案 §5):
//   - step 三值代理(引擎无 S1–S7/T1–T4 阶段枚举):act=动作期 /
//     settle=月结期 / inject=注入期;
//   - 六类计数 = trade/auction/loan/bias/bankruptcy/market;其余类型
//     (含 scenario 注入)归入 unrecognized 桶;
//   - events.jsonl 明细**仅 balance_gate 构建的 gate 运行**落盘
//     (MCRunOneWith 出口按 mcGateDetail 常量判定),默认 CI 只写
//     RunRecord.event_counts 聚合,保证 T10 时延不涨超 5%。
package virtual_city

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 事件级采集常量(方案 §5)。
const (
	// mcEventCap 环形缓冲容量(满则丢最旧)。
	mcEventCap = 5000
	// MCStepAct / MCStepSettle / MCStepInject 步骤三值代理。
	MCStepAct    = "act"
	MCStepSettle = "settle"
	MCStepInject = "inject"
	// 事件类型(六类 + scenario + 未识别桶)。
	MCEventTrade      = "trade"      // P2P 资产/副业成交
	MCEventAuction    = "auction"    // 拍卖成交
	MCEventLoan       = "loan"       // 放贷发放(银行 4 类 + P2P 合约)
	MCEventBias       = "bias"       // 偏差触发(与 BiasHits 同增量口径)
	MCEventBankruptcy = "bankruptcy" // 破产清算
	MCEventMarket     = "market"     // 市场周期自然切换
	MCEventScenario   = "scenario"   // 场景注入(MC stress / 真实注入)
	// MCEventUnrecognized 六类之外的聚合桶(event_counts 口径)。
	MCEventUnrecognized = "unrecognized"
)

// mcEventKnownTypes 六类已知事件类型(event_counts 聚合口径;其余入桶)。
var mcEventKnownTypes = map[string]bool{
	MCEventTrade: true, MCEventAuction: true, MCEventLoan: true,
	MCEventBias: true, MCEventBankruptcy: true, MCEventMarket: true,
}

// MCEvent 单条事件级采集记录(框架 §4.3 七字段)。
type MCEvent struct {
	EventTime int      `json:"event_time"`           // 世界月 w.Month(模拟时刻)
	Step      string   `json:"step"`                 // act|settle|inject(代理口径)
	EventType string   `json:"event_type"`           // 六类 + scenario
	Actors    []string `json:"actors"`               // 座位号/实体名(恒非 null)
	Price     int64    `json:"price"`                // 成交类必填,其余 0
	Qty       int64    `json:"qty"`                  // 数量(偏差增量/医疗人数等)
	InfoAsymFlag bool  `json:"info_asym_flag"`       // 信息不对称标记(信息交易=true)
	ScenarioID   string `json:"scenario_id,omitempty"` // stress_*/场景 id;非注入空串
}

// recordEvent 追加一条事件级采集记录(环形缓冲;nil World / 超限皆安全;
// 满则丢最旧 + EventDropped 计数)。零 rand、纯旁路。
func (w *World) recordEvent(step, eventType string, actors []string, price, qty int64, infoAsym bool, scenarioID string) {
	if w == nil {
		return
	}
	if len(w.MCEvents) >= mcEventCap {
		drop := len(w.MCEvents) - mcEventCap + 1
		w.MCEvents = append([]MCEvent(nil), w.MCEvents[drop:]...)
		w.EventDropped += drop
	}
	if actors == nil {
		actors = []string{}
	}
	w.MCEvents = append(w.MCEvents, MCEvent{
		EventTime: w.Month, Step: step, EventType: eventType,
		Actors: actors, Price: price, Qty: qty,
		InfoAsymFlag: infoAsym, ScenarioID: scenarioID,
	})
}

// mcEventCounts 事件计数聚合(RunRecord.event_counts 出口):
// 六类各自计数 + 其余类型(含 scenario)归入 unrecognized 桶。
// nil World ⇒ 空 map(静默降级)。零 rand。
func (w *World) mcEventCounts() map[string]int {
	out := map[string]int{}
	if w == nil {
		return out
	}
	for _, e := range w.MCEvents {
		if mcEventKnownTypes[e.EventType] {
			out[e.EventType]++
		} else {
			out[MCEventUnrecognized]++
		}
	}
	return out
}

// ── events.jsonl 落盘(仅 balance_gate 构建的 gate 运行;方案 §5)───────

const (
	// calibrationEventsFile 事件明细文件名(gate 运行每事件 1 行)。
	calibrationEventsFile = "events.jsonl"
	// maxEventLines / keepEventLines 轮转阈值(gate 1000 局 × 数百事件
	// 会频繁触发轮转;阈值取大以降低重写频率)。
	maxEventLines = 50000
	keepEventLines = 25000
)

// mcGateDetail 事件明细落盘开关(build tag 常量;默认 false ⇒ T10 无写盘
// 开销)在 mc_gate_detail_off.go / mc_gate_detail_on.go 两文件中按
// balance_gate tag 分别定义;MCRunOneWith 出口据此判定是否落盘 events.jsonl。

// EventStore 事件明细写盘器(形态与 RunStore 同款;零值可用)。
type EventStore struct {
	mu  sync.Mutex
	dir string
}

// defaultEventStore 包级单例(gate 运行出口接线消费)。
var defaultEventStore = &EventStore{}

// AppendEvents 经包级单例追加一批事件明细(每事件 1 行;失败返回 error)。
func AppendEvents(events []MCEvent) error {
	return defaultEventStore.AppendEvents(events)
}

// AppendEvents 追加事件明细;超过 maxEventLines 时保尾 keepEventLines
// 原子重写。全程持锁(并发安全)。
func (s *EventStore) AppendEvents(events []MCEvent) error {
	if len(events) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.dir
	if dir == "" {
		dir = defaultCalibrationRunsDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("virtual_city calibration events mkdir %s: %w", dir, err)
	}
	var buf bytes.Buffer
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("virtual_city calibration events marshal %s/%d: %w", ev.EventType, ev.EventTime, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	path := filepath.Join(dir, calibrationEventsFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("virtual_city calibration events open %s: %w", path, err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return fmt.Errorf("virtual_city calibration events write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("virtual_city calibration events close %s: %w", path, err)
	}
	return s.rotateIfFull(path)
}

// rotateIfFull 行数超限时保尾重写(临时文件 + os.Rename 原子替换)。
// 已持 s.mu(仅 AppendEvents 调用)。
func (s *EventStore) rotateIfFull(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("virtual_city calibration events read %s: %w", path, err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= maxEventLines {
		return nil
	}
	keep := lines[len(lines)-keepEventLines:]
	var buf bytes.Buffer
	for _, l := range keep {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("virtual_city calibration events tmp write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("virtual_city calibration events rename %s: %w", path, err)
	}
	return nil
}

// appendEventsLogging 事件明细写盘收口(失败 zap warn,绝不 panic、
// 不阻断 MC 收口)。仅 gate 运行调用(mcGateDetail 判定)。
func appendEventsLogging(events []MCEvent) {
	if err := AppendEvents(events); err != nil {
		logger.L().Warn("virtual_city calibration events append failed",
			zap.Int("count", len(events)), zap.Error(err))
	}
}
