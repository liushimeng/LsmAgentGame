// Package virtual_city — calibration_runs.go: 运行记录 JSONL 写盘
// (批次72 §5;N2「RunRecord 仅内存」的解法)。
//
// 形态:
//   - 目录 ./calibration_runs/(服务器工作目录,已入 .gitignore);
//   - 文件 runs.jsonl,每完成一局追加 1 行 RunRecord JSON;
//   - 总行数 > 2000 ⇒ 保尾 1000 重写(临时文件 + os.Rename 原子替换);
//
// 纪律:
//   - **零 rand、零游戏语义改动** —— 写盘是纯旁路观测(批次71 同一纪律);
//   - **零 panic、绝不阻断对局收口** —— 所有失败返回 error,
//     由 finalizeCalibrationRun 记 zap warn(同包 room.go 同款 logger);
//   - 并发安全 —— 包级 RunStore 全程持 mu。
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

// 写盘常量(方案 §5.2)。
const (
	// defaultCalibrationRunsDir 默认目录(服务器工作目录相对路径)。
	defaultCalibrationRunsDir = "./calibration_runs"
	// calibrationRunsFile 文件名(每行 1 条 RunRecord)。
	calibrationRunsFile = "runs.jsonl"
	// maxRunLines 追加后总行数超过此值 ⇒ 触发轮转。
	maxRunLines = 2000
	// keepRunLines 轮转保留的尾部行数。
	keepRunLines = 1000
)

// RunStore 运行记录写盘器(目录 + 互斥锁;零值可用,dir 空串回落默认目录)。
type RunStore struct {
	mu  sync.Mutex
	dir string
}

// defaultRunStore 包级单例(finalizeCalibrationRun 接线消费)。
var defaultRunStore = &RunStore{}

// SetCalibrationRunsDir 注入写盘目录(测试用;影响包级单例:runs +
// 批次73 residents/events 同目录)。零 rand。
func SetCalibrationRunsDir(dir string) {
	defaultRunStore.mu.Lock()
	defer defaultRunStore.mu.Unlock()
	defaultRunStore.dir = dir
	defaultResidentStore.mu.Lock()
	defer defaultResidentStore.mu.Unlock()
	defaultResidentStore.dir = dir
	defaultEventStore.mu.Lock()
	defer defaultEventStore.mu.Unlock()
	defaultEventStore.dir = dir
}

// AppendRun 经包级单例追加一条运行记录(失败返回 error,绝不 panic)。
func AppendRun(rec RunRecord) error {
	return defaultRunStore.AppendRun(rec)
}

// AppendRun 追加 1 行 RunRecord JSON;超过 maxRunLines 时保尾 keepRunLines
// 原子重写。全程持锁(并发追加安全);目录不存在自动创建(0755)。
func (s *RunStore) AppendRun(rec RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.dir
	if dir == "" {
		dir = defaultCalibrationRunsDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("virtual_city calibration runs mkdir %s: %w", dir, err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("virtual_city calibration runs marshal %s: %w", rec.RunID, err)
	}
	line = append(line, '\n')

	path := filepath.Join(dir, calibrationRunsFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("virtual_city calibration runs open %s: %w", path, err)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("virtual_city calibration runs write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("virtual_city calibration runs close %s: %w", path, err)
	}
	return s.rotateIfFull(path)
}

// rotateIfFull 行数超限时保尾重写(临时文件 + os.Rename 原子替换)。
// 已持 s.mu(仅 AppendRun 调用)。
func (s *RunStore) rotateIfFull(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("virtual_city calibration runs read %s: %w", path, err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	// 末元素:文件以 \n 结尾时为空串,剔除。
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= maxRunLines {
		return nil
	}
	keep := lines[len(lines)-keepRunLines:]
	var buf bytes.Buffer
	for _, l := range keep {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("virtual_city calibration runs tmp write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("virtual_city calibration runs rename %s: %w", path, err)
	}
	return nil
}

// appendRunLogging 写盘收口接线(失败 zap warn,绝不 panic、不阻断收口)。
// 由 finalizeCalibrationRun 调用(§5.1 触发点)。
func appendRunLogging(rec RunRecord) {
	if err := AppendRun(rec); err != nil {
		logger.L().Warn("virtual_city calibration run append failed",
			zap.String("run_id", rec.RunID), zap.String("strategy", rec.Strategy), zap.Error(err))
	}
}
