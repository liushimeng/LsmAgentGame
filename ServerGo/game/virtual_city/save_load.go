// Package virtual_city — save_load.go: 批次86 世界快照存盘 / 读档
// (2026-11-06 §可靠性与可观测性)。
//
// 契约:lag_docs/虚拟城市/已实现/86-可靠性与可观测性/01-方案设计.md §3.2。
//
// 形态(照抄 calibration_runs.go 的写盘纪律):
//
//	./virtual_city_saves/<roomID>/
//	    ├── recovery.json          # 灾难恢复点(最近一次成功月结后自动写)
//	    └── <snapshot_id>.json     # 手动 / 自动回滚 / 回放基线快照
//
// 纪律:
//   - **原子写**:先写 `<name>.tmp` 再 os.Rename;
//   - **零 panic、绝不阻断对局**:所有失败返回 error,由调用方 zap warn;
//   - **目录 0755 / 文件 0644**;
//   - **并发安全**:包级 SnapshotStore 全程持 mu;
//   - **零 rand**:写盘是纯旁路,不消耗 w.Rand、不改 StateHash(契约 §3.1.2)。
package virtual_city

import (
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SnapshotReason 快照用途(留痕)。
const (
	SnapshotReasonManual       = "manual"        // 用户/Agent 手动存档
	SnapshotReasonAutoRollback = "auto_rollback" // 注入前自动留档(回滚用)
	SnapshotReasonRecovery     = "recovery"      // 灾难恢复点
	SnapshotReasonReplay       = "replay"        // 回放基线
)

// 存盘常量(契约 §3.2.1)。
const (
	// SnapshotSchemaVersion 快照 schema 版本(当前 1)。
	SnapshotSchemaVersion = 1
	// defaultSnapshotDir 默认写盘目录(服务器工作目录相对路径)。
	defaultSnapshotDir = "./virtual_city_saves"
	// recoverySnapshotID 灾难恢复点的固定 snapshot_id(恒 1 份,覆盖写)。
	recoverySnapshotID = "recovery"
	// recoveryFileName 恢复点文件名。
	recoveryFileName = "recovery.json"
	// snapshotFileExt 快照文件扩展名。
	snapshotFileExt = ".json"
	// autoRollbackKeep auto_rollback 快照保留份数(超出删最旧,防磁盘膨胀)。
	autoRollbackKeep = 3
)

// WorldSnapshot 世界快照(存盘格式;JSON)。
type WorldSnapshot struct {
	SchemaVersion int             `json:"schema_version"` // 1
	RoomID        string          `json:"room_id"`
	SnapshotID    string          `json:"snapshot_id"` // "snap-<unixnano>-<rand6>" | "recovery"
	CreatedAt     int64           `json:"created_at"`  // unix 毫秒
	Month         int             `json:"month"`
	StateHash     string          `json:"state_hash"`
	Reason        string          `json:"reason"`
	WorldJSON     json.RawMessage `json:"world_json"` // World 全量 JSON
}

// SnapshotMeta 快照元信息(列表用;不含 WorldJSON)。
type SnapshotMeta struct {
	SnapshotID string `json:"snapshot_id"`
	CreatedAt  int64  `json:"created_at"`
	Month      int    `json:"month"`
	StateHash  string `json:"state_hash"`
	Reason     string `json:"reason"`
	SizeBytes  int    `json:"size_bytes"`
}

// SnapshotStore 快照写盘器(目录 + 互斥锁;零值可用,dir 空串回落默认目录)。
type SnapshotStore struct {
	mu  sync.Mutex
	dir string
}

// defaultSnapshotStore 包级单例(room 层与引擎层 snapshot 共用)。
var defaultSnapshotStore = &SnapshotStore{}

// SetSnapshotDir 注入写盘目录(测试用;影响包级单例)。零 rand。
func SetSnapshotDir(dir string) {
	defaultSnapshotStore.mu.Lock()
	defer defaultSnapshotStore.mu.Unlock()
	defaultSnapshotStore.dir = dir
}

// newSnapshotID 生成 snapshot_id:"snap-<unixnano>-<rand6>"(密码学随机 suffix)。
func newSnapshotID() string {
	var b [3]byte
	suffix := uint32(0)
	if _, err := crand.Read(b[:]); err == nil {
		suffix = uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
	} else {
		// 兜底:退化为时间低 24 位(仍与 unixnano 拼接,碰撞概率可忽略)。
		suffix = uint32(time.Now().UnixNano()) & 0xffffff
	}
	return fmt.Sprintf("snap-%d-%06x", time.Now().UnixNano(), suffix)
}

// validateSnapshotRoomID 校验 roomID 可安全用作路径分量。
func validateSnapshotRoomID(roomID string) error {
	if roomID == "" {
		return errors.New("virtual_city snapshot: empty room id")
	}
	if strings.ContainsAny(roomID, `/\`) || roomID == "." || roomID == ".." {
		return fmt.Errorf("virtual_city snapshot: illegal room id %q", roomID)
	}
	return nil
}

// validateSnapshotID 校验 snapshotID 可安全用作文件名。
func validateSnapshotID(snapshotID string) error {
	if snapshotID == "" {
		return errors.New("virtual_city snapshot: empty snapshot id")
	}
	if strings.ContainsAny(snapshotID, `/\`) || snapshotID == "." || snapshotID == ".." {
		return fmt.Errorf("virtual_city snapshot: illegal snapshot id %q", snapshotID)
	}
	return nil
}

// roomDir 返回房间快照子目录(相对 s.dir;空 ⇒ 默认目录)。
func (s *SnapshotStore) roomDir(roomID string) string {
	dir := s.dir
	if dir == "" {
		dir = defaultSnapshotDir
	}
	return filepath.Join(dir, roomID)
}

// snapshotPath 返回快照文件路径(recovery ⇒ recovery.json)。
func (s *SnapshotStore) snapshotPath(roomID, snapshotID string) string {
	name := snapshotID + snapshotFileExt
	if snapshotID == recoverySnapshotID {
		name = recoveryFileName
	}
	return filepath.Join(s.roomDir(roomID), name)
}

// writeFileAtomic 原子写(临时文件 + os.Rename);目录自动创建(0755)。
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("virtual_city snapshot mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("virtual_city snapshot tmp write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("virtual_city snapshot rename %s: %w", path, err)
	}
	return nil
}

// SaveWorldSnapshot 保存 World 快照到磁盘(持锁由调用方保证)。
//
// 返回元信息(含 SnapshotID,供留痕关联);失败返回 error,绝不 panic。
func SaveWorldSnapshot(roomID string, w *World, reason string) (*SnapshotMeta, error) {
	if w == nil {
		return nil, errors.New("virtual_city snapshot: nil world")
	}
	data, err := marshalWorld(w)
	if err != nil {
		return nil, err
	}
	return saveSnapshotPayload(roomID, w.StateHash(), w.Month, reason, data)
}

// saveSnapshotPayload 把已序列化的 World JSON 落盘(包级单例;持锁)。
//
// stateHash / month 由调用方在锁内取好(避免本函数回头读 World)。
func saveSnapshotPayload(roomID, stateHash string, month int, reason string, worldJSON []byte) (*SnapshotMeta, error) {
	return defaultSnapshotStore.save(roomID, stateHash, month, reason, worldJSON)
}

// save 落盘一份快照(持 s.mu;零 rand)。
func (s *SnapshotStore) save(roomID, stateHash string, month int, reason string, worldJSON []byte) (*SnapshotMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateSnapshotRoomID(roomID); err != nil {
		return nil, err
	}
	snapshotID := newSnapshotID()
	if reason == SnapshotReasonRecovery {
		snapshotID = recoverySnapshotID // 恢复点恒 1 份,覆盖写
	}
	createdAt := time.Now().UnixMilli()
	snap := WorldSnapshot{
		SchemaVersion: SnapshotSchemaVersion,
		RoomID:        roomID,
		SnapshotID:    snapshotID,
		CreatedAt:     createdAt,
		Month:         month,
		StateHash:     stateHash,
		Reason:        reason,
		WorldJSON:     worldJSON,
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, fmt.Errorf("virtual_city snapshot marshal %s: %w", snapshotID, err)
	}
	path := s.snapshotPath(roomID, snapshotID)
	if err := writeFileAtomic(path, raw); err != nil {
		return nil, err
	}
	meta := &SnapshotMeta{
		SnapshotID: snapshotID,
		CreatedAt:  createdAt,
		Month:      month,
		StateHash:  stateHash,
		Reason:     reason,
		SizeBytes:  len(raw),
	}
	if reason == SnapshotReasonAutoRollback {
		s.pruneAutoRollbackLocked(roomID)
	}
	return meta, nil
}

// LoadWorldSnapshot 读全量快照(含 WorldJSON)。
func LoadWorldSnapshot(roomID, snapshotID string) (*WorldSnapshot, error) {
	return defaultSnapshotStore.load(roomID, snapshotID)
}

// load 读全量快照(持 s.mu)。
func (s *SnapshotStore) load(roomID, snapshotID string) (*WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.pathOfLocked(roomID, snapshotID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("virtual_city snapshot read %s: %w", path, err)
	}
	var snap WorldSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("virtual_city snapshot decode %s: %w", path, err)
	}
	if len(snap.WorldJSON) == 0 {
		return nil, fmt.Errorf("virtual_city snapshot %s: empty world_json", path)
	}
	return &snap, nil
}

// LoadWorldSnapshotMeta 读元信息(不加载 WorldJSON,列表用)。
func LoadWorldSnapshotMeta(roomID, snapshotID string) (*SnapshotMeta, error) {
	return defaultSnapshotStore.loadMeta(roomID, snapshotID)
}

// loadMeta 读元信息(持 s.mu)。
func (s *SnapshotStore) loadMeta(roomID, snapshotID string) (*SnapshotMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.pathOfLocked(roomID, snapshotID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("virtual_city snapshot read %s: %w", path, err)
	}
	var snap WorldSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("virtual_city snapshot decode %s: %w", path, err)
	}
	return &SnapshotMeta{
		SnapshotID: snap.SnapshotID,
		CreatedAt:  snap.CreatedAt,
		Month:      snap.Month,
		StateHash:  snap.StateHash,
		Reason:     snap.Reason,
		SizeBytes:  len(raw),
	}, nil
}

// pathOfLocked 校验并返回快照文件路径(持 s.mu)。
func (s *SnapshotStore) pathOfLocked(roomID, snapshotID string) (string, error) {
	if err := validateSnapshotRoomID(roomID); err != nil {
		return "", err
	}
	if err := validateSnapshotID(snapshotID); err != nil {
		return "", err
	}
	return s.snapshotPath(roomID, snapshotID), nil
}

// ListWorldSnapshots 列出房间的全部快照(按 CreatedAt 降序;副本)。
func ListWorldSnapshots(roomID string) []SnapshotMeta {
	return defaultSnapshotStore.list(roomID)
}

// list 列出快照(持 s.mu);目录不存在 ⇒ 空切片(非 nil)。
func (s *SnapshotStore) list(roomID string) []SnapshotMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []SnapshotMeta{}
	if err := validateSnapshotRoomID(roomID); err != nil {
		return out
	}
	dir := s.roomDir(roomID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), snapshotFileExt) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue // 单个文件不可读 ⇒ 跳过(列表是观测功能,不阻断)
		}
		var snap WorldSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			continue
		}
		out = append(out, SnapshotMeta{
			SnapshotID: snap.SnapshotID,
			CreatedAt:  snap.CreatedAt,
			Month:      snap.Month,
			StateHash:  snap.StateHash,
			Reason:     snap.Reason,
			SizeBytes:  len(raw),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// DeleteWorldSnapshot 删除快照(不存在 = no-op)。
func DeleteWorldSnapshot(roomID, snapshotID string) error {
	return defaultSnapshotStore.delete(roomID, snapshotID)
}

// delete 删除快照(持 s.mu)。
func (s *SnapshotStore) delete(roomID, snapshotID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.pathOfLocked(roomID, snapshotID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // 不存在 = no-op
		}
		return fmt.Errorf("virtual_city snapshot remove %s: %w", path, err)
	}
	return nil
}

// pruneAutoRollbackLocked 裁剪 auto_rollback 快照到最近 autoRollbackKeep 份
// (超出删最旧;持 s.mu)。零 rand。
func (s *SnapshotStore) pruneAutoRollbackLocked(roomID string) {
	dir := s.roomDir(roomID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type item struct {
		id  string
		at  int64
	}
	var items []item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), snapshotFileExt) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), snapshotFileExt)
		if id == recoverySnapshotID {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var snap WorldSnapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			continue
		}
		if snap.Reason != SnapshotReasonAutoRollback {
			continue
		}
		items = append(items, item{id: id, at: snap.CreatedAt})
	}
	if len(items) <= autoRollbackKeep {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at > items[j].at })
	for _, it := range items[autoRollbackKeep:] {
		_ = os.Remove(filepath.Join(dir, it.id+snapshotFileExt))
	}
}

// ─────────────────────────── World JSON round-trip ───────────────────────────

// marshalWorld 序列化 World 全量状态(零 rand)。
func marshalWorld(w *World) ([]byte, error) {
	if w == nil {
		return nil, errors.New("virtual_city snapshot: nil world")
	}
	data, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("virtual_city snapshot marshal world: %w", err)
	}
	return data, nil
}

// unmarshalWorld 恢复 World:JSON → World,然后修复不可序列化字段:
//   - Rand:用 World.Seed 重新播种(rand.New(rand.NewSource(seed))
//     —— 内部状态不可序列化,语义等价(同 seed 同序列);
//   - startAge:恢复为 MasterStartAge(未导出字段,JSON 丢失);
//   - nil 子系统:按需惰性初始化(照抄 NewWorld 的构造口径)。
//
// ⚠ **恢复口径**:Rand 的内部状态无法序列化 ⇒ 恢复后随机序列不与恢复前
// 逐位相同;但 World.Seed 被保存 ⇒ 重新播种后同 seed 的派生逻辑仍一致。
// 这是「语义等价」而非「逐位等价」(契约 §3.2.2)。
func unmarshalWorld(data []byte) (*World, error) {
	var w World
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("virtual_city snapshot unmarshal world: %w", err)
	}
	// Rand:JSON 里是 &rand.Rand{}(零值,不可用)⇒ 用 Seed 重新播种。
	w.Rand = rand.New(rand.NewSource(w.Seed))
	// startAge:未导出字段,JSON round-trip 丢失 ⇒ 恢复为开局基准。
	w.startAge = MasterStartAge
	// nil 子系统惰性初始化(真实快照恒非 nil,此处仅防御性兜底;照抄 NewWorld)。
	if w.Market == nil {
		w.Market = NewMarket(w.Rand)
	}
	if w.Ledger == nil {
		w.Ledger = &Ledger{}
	}
	if w.CB == nil {
		w.CB = NewCentralBank()
	}
	if w.Treasury == nil {
		w.Treasury = NewTreasury()
	}
	if w.Goods == nil {
		w.Goods = NewGoodsMarket()
	}
	if w.Labor == nil {
		w.Labor = NewFirmSector()
	}
	if w.SocietyHist == nil {
		w.SocietyHist = &SocietyHistory{}
	}
	if w.SupplyChain == nil {
		w.SupplyChain = NewSupplyChain()
	}
	if w.Clusters == nil {
		w.Clusters = NewIndustrialClusters()
	}
	if w.CDMarket == nil {
		w.CDMarket = NewCDMarket()
	}
	if w.ShortBook == nil {
		w.ShortBook = NewShortBook()
	}
	if w.QuantEngine == nil {
		w.QuantEngine = NewQuantFundEngine()
	}
	if w.PublicSvc == nil {
		w.PublicSvc = NewPublicServices()
	}
	if w.Regulators == nil {
		w.Regulators = NewRegulatorBundle()
	}
	if w.Election == nil {
		w.Election = NewCivicElection()
	}
	if w.ListingBook == nil {
		w.ListingBook = NewListingBook()
	}
	if w.AuctionHouse == nil {
		w.AuctionHouse = NewAuctionHouse()
	}
	if w.OffLedger == nil {
		w.OffLedger = &OffLedgerBook{}
	}
	return &w, nil
}

// ─────────────────────── 引擎层 snapshot / restore 内部件 ───────────────────────

// saveSnapshotForTrace 引擎层注入前留档(契约 §3.1.3 ①)。
//
// roomID 为空 ⇒ 跳过快照(返回空 id + error);调用方据此跳过回滚兜底。
func saveSnapshotForTrace(w *World, roomID, reason string) (string, error) {
	if w == nil {
		return "", errors.New("nil world")
	}
	if roomID == "" {
		return "", errors.New("empty room id")
	}
	meta, err := SaveWorldSnapshot(roomID, w, reason)
	if err != nil {
		return "", err
	}
	return meta.SnapshotID, nil
}

// restoreSnapshotIntoWorld 把指定快照恢复进既有 World(契约 §3.1.3 ⑤)。
//
// 用整体赋值 *w = *nw —— 引擎层拿不到调用方的指针,只能原地覆盖;调用方
// 持房间锁,期间无人读到中间态。
func restoreSnapshotIntoWorld(w *World, roomID, snapshotID string) error {
	if w == nil {
		return errors.New("nil world")
	}
	snap, err := LoadWorldSnapshot(roomID, snapshotID)
	if err != nil {
		return err
	}
	nw, err := unmarshalWorld(snap.WorldJSON)
	if err != nil {
		return err
	}
	*w = *nw
	return nil
}
