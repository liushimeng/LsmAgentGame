// Package citybus — outbox.go: 消息投递留痕(批次86 §3.4.1「消息持久化与重试」)。
//
// 形态:
//   - OutboxRecord 单条投递留痕(delivery = sent | posted | dropped | failed);
//   - Outbox 线程安全环形缓冲(cap 200,Snapshot 新→旧、返回副本);
//   - 包级默认 outbox(DefaultOutbox)+ 可选 JSONL 落盘(默认关)。
//
// 接线(bus.go):
//   - Send 成功 ⇒ sent;Send 失败 ⇒ failed(含未注册);
//   - Post 入队 ⇒ posted;队列满 ⇒ dropped(不再静默);
//   - postLoop handler 失败 ⇒ failed;
//   - SendWithRetry 每次尝试各记一条(retries = 已重试次数)。
//
// 纪律:
//   - **零阻塞(默认)**:Record 只做内存写入;落盘默认关,开启后才有文件 I/O;
//   - **零 panic**:所有错误返回 / 记 zap warn,不上抛、不阻断调用方;
//   - **零值可用**:var o Outbox 直接可用(nil receiver 亦安全);
//   - **落盘轮转**:> 2000 行保尾 1000(临时文件 + os.Rename 原子替换,
//     照抄 game/virtual_city/calibration_runs.go 模式)。
package citybus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// outboxCap 是 Outbox 环形缓冲容量(超出丢最旧)。
const outboxCap = 200

// 落盘常量(照抄 calibration_runs.go 模式)。
const (
	// defaultOutboxDir 默认落盘目录(服务器工作目录相对路径)。
	defaultOutboxDir = "./virtual_city_saves"
	// outboxFileName 落盘文件名(每行 1 条 OutboxRecord)。
	outboxFileName = "outbox.jsonl"
	// maxOutboxLines 追加后总行数超过此值 ⇒ 触发轮转。
	maxOutboxLines = 2000
	// keepOutboxLines 轮转保留的尾部行数。
	keepOutboxLines = 1000
)

// OutboxDelivery 是投递结果枚举。
const (
	// OutboxSent 同步 Send 成功送达。
	OutboxSent = "sent"
	// OutboxPosted Post 已入队(尚未投递)。
	OutboxPosted = "posted"
	// OutboxDropped Post 队列满被丢弃。
	OutboxDropped = "dropped"
	// OutboxFailed 投递失败(Send 失败 / postLoop handler 失败)。
	OutboxFailed = "failed"
)

// OutboxRecord 单条消息投递留痕(批次86 §3.4.1)。
//
// 恒为值语义:环形缓冲存值,Snapshot 返回副本。
type OutboxRecord struct {
	// Seq 单调递增序号(由 Outbox 自动分配)。
	Seq int64 `json:"seq"`
	// At 记录时间(unix 毫秒;由 Outbox 自动填充)。
	At int64 `json:"at"`
	// RoomID 目标房间("" = 类级全局目标)。
	RoomID string `json:"room_id,omitempty"`
	// From / To 是 AgentClassName。
	From string `json:"from"`
	To   string `json:"to"`
	// Type 消息类型。
	Type string `json:"type"`
	// Delivery 投递结果:sent | posted | dropped | failed。
	Delivery string `json:"delivery"`
	// ErrMsg 失败原因(Delivery 为 failed 时非空)。
	ErrMsg string `json:"err_msg,omitempty"`
	// Retries 已重试次数(SendWithRetry;单次 Send 恒为 0)。
	Retries int `json:"retries"`
}

// Outbox 是线程安全的投递留痕环形缓冲(零值可用)。
//
// 内存环与落盘各持一把锁:Record 先在 mu 内更新环,再在 mu 外做文件 I/O
// (fileMu 串行),避免慢盘拖住 Send/Post 热路径。
type Outbox struct {
	mu sync.Mutex
	// recs 环形存储(长度 ≤ outboxCap)。
	recs []OutboxRecord
	// head 最旧一条的下标(count == outboxCap 时有效)。
	head int
	// count 当前条数(≤ outboxCap)。
	count int
	// seq 单调计数器(已分配的最大序号)。
	seq int64
	// enabled 是否落盘(默认 false)。
	enabled bool
	// dir 落盘目录(空 ⇒ defaultOutboxDir)。
	dir string

	// fileMu 串行化落盘写入(与 mu 分离,避免 I/O 持锁)。
	fileMu sync.Mutex
}

// NewOutbox 构造独立 Outbox(测试用;包级默认实例走 DefaultOutbox)。
func NewOutbox() *Outbox { return &Outbox{} }

// defaultOutbox 包级默认 outbox(bus.go 的 Send/Post/postLoop 直接消费)。
var defaultOutbox = NewOutbox()

// DefaultOutbox 返回包级默认 outbox(main.go 装配层 / 测试可查)。
func DefaultOutbox() *Outbox { return defaultOutbox }

// SetOutboxPersistence 开启 / 关闭包级 outbox 落盘(默认关,零回归)。
func SetOutboxPersistence(enabled bool) { defaultOutbox.SetPersistence(enabled) }

// SetOutboxDir 注入包级 outbox 落盘目录(测试用;空串回落默认目录)。
func SetOutboxDir(dir string) { defaultOutbox.SetDir(dir) }

// SetPersistence 开启 / 关闭落盘(线程安全)。
func (o *Outbox) SetPersistence(enabled bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.enabled = enabled
	o.mu.Unlock()
}

// SetDir 注入落盘目录(线程安全;空串回落默认目录)。
func (o *Outbox) SetDir(dir string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.dir = dir
	o.mu.Unlock()
}

// Record 记录一条投递留痕(线程安全;零值可用;nil receiver no-op)。
//
// Seq / At 为 0 时自动填充(单调序号 + 当前 unix 毫秒)。
// 落盘开启时追加 JSONL,失败只记 zap warn,绝不 panic、绝不阻塞调用方返回。
func (o *Outbox) Record(rec OutboxRecord) {
	if o == nil {
		return
	}
	o.mu.Lock()
	if rec.Seq == 0 {
		o.seq++
		rec.Seq = o.seq
	} else if rec.Seq > o.seq {
		o.seq = rec.Seq
	}
	if rec.At == 0 {
		rec.At = time.Now().UnixMilli()
	}
	if o.recs == nil {
		o.recs = make([]OutboxRecord, 0, outboxCap)
	}
	if o.count < outboxCap {
		o.recs = append(o.recs, rec)
		o.count++
	} else {
		o.recs[o.head] = rec
		o.head = (o.head + 1) % outboxCap
	}
	enabled := o.enabled
	dir := o.dir
	o.mu.Unlock()

	if !enabled {
		return
	}
	if err := o.appendFile(dir, rec); err != nil {
		logger.L().Warn("citybus: outbox persist failed",
			zap.Int64("seq", rec.Seq),
			zap.String("delivery", rec.Delivery),
			zap.Error(err))
	}
}

// Snapshot 返回留痕副本(新→旧;nil receiver 返回空切片)。
//
// limit > 0 时最多返回最新 limit 条;limit ≤ 0 返回全部(≤ outboxCap)。
func (o *Outbox) Snapshot(limit int) []OutboxRecord {
	if o == nil {
		return []OutboxRecord{}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]OutboxRecord, 0, o.count)
	for i := 0; i < o.count; i++ {
		idx := (o.head + i) % outboxCap
		out = append(out, o.recs[idx])
	}
	// out 当前为旧→新,反转成新→旧。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Seq 返回当前单调计数器的值(已分配的最大序号;线程安全)。
func (o *Outbox) Seq() int64 {
	if o == nil {
		return 0
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.seq
}

// appendFile 追加 1 行 OutboxRecord JSON;超过 maxOutboxLines 时保尾
// keepOutboxLines 原子重写。调用方已确认 enabled;fileMu 串行化。
func (o *Outbox) appendFile(dir string, rec OutboxRecord) error {
	o.fileMu.Lock()
	defer o.fileMu.Unlock()

	if dir == "" {
		dir = defaultOutboxDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("citybus outbox mkdir %s: %w", dir, err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("citybus outbox marshal seq %d: %w", rec.Seq, err)
	}
	line = append(line, '\n')

	path := filepath.Join(dir, outboxFileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("citybus outbox open %s: %w", path, err)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("citybus outbox write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("citybus outbox close %s: %w", path, err)
	}
	return rotateOutboxFile(path)
}

// rotateOutboxFile 行数超限时保尾重写(临时文件 + os.Rename 原子替换)。
// 已持 fileMu(仅 appendFile 调用)。
func rotateOutboxFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("citybus outbox read %s: %w", path, err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	// 末元素:文件以 \n 结尾时为空串,剔除。
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= maxOutboxLines {
		return nil
	}
	keep := lines[len(lines)-keepOutboxLines:]
	var buf bytes.Buffer
	for _, l := range keep {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("citybus outbox tmp write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("citybus outbox rename %s: %w", path, err)
	}
	return nil
}
