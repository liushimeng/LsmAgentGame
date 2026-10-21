// Package virtual_city — calibration_runs_test.go: 批次72 运行记录写盘测试
// (方案 §9 T-R1~T-R3)。
package virtual_city

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// mcRunLineCount 数 runs.jsonl 行数(按 \n 计;末行无换行不计)。
func mcRunLineCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// mcRunRec 造一条最小 RunRecord。
func mcRunRec(id string) RunRecord {
	return RunRecord{RunID: id, Seed: 42, Strategy: MCStrategyBalanced,
		OutcomeCounts: map[string]int{}, InvariantsOK: []bool{}, InvariantFails: []string{}}
}

// T-R1 追加与轮转:5 局 = 5 行;灌 2005 局 ⇒ 恰 1000 行且保尾。
func TestRunStore_AppendAndRotate(t *testing.T) {
	dir := t.TempDir()
	store := &RunStore{dir: dir}

	// 追加 5 局 = 5 行,逐行可解析、RunID 对齐。
	for i := 1; i <= 5; i++ {
		if err := store.AppendRun(mcRunRec("CR" + itoa(i))); err != nil {
			t.Fatalf("append #%d: %v", i, err)
		}
	}
	path := filepath.Join(dir, calibrationRunsFile)
	if got := mcRunLineCount(t, path); got != 5 {
		t.Fatalf("追加 5 局后行数 = %d,期望 5", got)
	}
	data, _ := os.ReadFile(path)
	for i, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		var rec RunRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("第 %d 行不可解析: %v", i+1, err)
		}
		if want := "CR" + itoa(i+1); rec.RunID != want {
			t.Errorf("第 %d 行 run_id = %s,期望 %s", i+1, rec.RunID, want)
		}
	}

	// 灌过轮转阈值:第 2001 次追加后(2001 > 2000)恰保尾 1000 行
	//(CR1002..CR2001);之后每次追加线性增长,直到再次越过 2000。
	rotatedAt := 0
	for i := 6; i <= 2005; i++ {
		if err := store.AppendRun(mcRunRec("CR" + itoa(i))); err != nil {
			t.Fatalf("append #%d: %v", i, err)
		}
		if i == maxRunLines+1 {
			if got := mcRunLineCount(t, path); got != keepRunLines {
				t.Fatalf("第 %d 次追加后应恰轮转为 %d 行,got %d", i, keepRunLines, got)
			}
			rotatedAt = i
		}
	}
	if rotatedAt == 0 {
		t.Fatal("未观测到轮转点")
	}
	// 2001 轮转 → 1000;2002..2005 再追加 4 条 ⇒ 1004(§5.2:仅在 >2000 时轮转)。
	if got := mcRunLineCount(t, path); got != keepRunLines+4 {
		t.Fatalf("2005 次追加后行数 = %d,期望 %d(1000 + 4)", got, keepRunLines+4)
	}
	data, _ = os.ReadFile(path)
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'})
	if string(lines[0]) == "" || !bytes.Contains(lines[0], []byte(`"CR1002"`)) {
		t.Errorf("轮转后首行应保留 CR1002(保尾 1000 的头): %s", lines[0])
	}
	if !bytes.Contains(lines[len(lines)-1], []byte(`"CR2005"`)) {
		t.Errorf("轮转后末行应保留 CR2005: %s", lines[len(lines)-1])
	}
	// 残留 tmp 文件不应存在(重命名成功)。
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("轮转后残留 .tmp 文件")
	}
}

// itoa 局部小工具。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

// T-R2 并发:100 goroutine 各追加 1 局 ⇒ 恰 100 行、全部可解析。
func TestRunStore_ConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	store := &RunStore{dir: dir}
	const n = 100
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := store.AppendRun(mcRunRec("CR" + itoa(i+1))); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发追加失败: %v", err)
	}
	path := filepath.Join(dir, calibrationRunsFile)
	if got := mcRunLineCount(t, path); got != n {
		t.Fatalf("并发追加后行数 = %d,期望 %d", got, n)
	}
	data, _ := os.ReadFile(path)
	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		var rec RunRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("存在不可解析行: %v (%s)", err, line)
		}
		seen[rec.RunID] = true
	}
	if len(seen) != n {
		t.Errorf("去重后 run_id 数 = %d,期望 %d(有覆盖/丢失)", len(seen), n)
	}
}

// T-R3 失败安全:目录不可用(路径被普通文件占用)⇒ 返回 error、不 panic、
// 收口路径(appendRunLogging)不受影响。
func TestRunStore_FailureSafety(t *testing.T) {
	// 用普通文件路径当目录:MkdirAll 恒 ENOTDIR(对 root 用户也成立)。
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	store := &RunStore{dir: blocker}
	err := store.AppendRun(mcRunRec("CRX"))
	if err == nil {
		t.Fatal("目录被占用时 AppendRun 应返回 error")
	}

	// 收口接线在写盘失败时绝不 panic(仅 warn 日志)。
	prev := func() string {
		// 暂存 TestMain 注入的临时目录,测后恢复。
		return os.Getenv("LSM_MC_RUNS_DIR")
	}()
	SetCalibrationRunsDir(blocker)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("appendRunLogging 在写盘失败时 panic: %v", r)
			}
		}()
		appendRunLogging(mcRunRec("CRY"))
	}()
	if prev == "" {
		// 恢复为新的临时目录(与 TestMain 同语义,防后续测试写仓库目录)。
		SetCalibrationRunsDir(t.TempDir())
	} else {
		SetCalibrationRunsDir(prev)
	}
}

// TestRunStore_SetCalibrationRunsDir 测试注入生效(包级单例)。
func TestRunStore_SetCalibrationRunsDir(t *testing.T) {
	dir := t.TempDir()
	SetCalibrationRunsDir(dir)
	defer SetCalibrationRunsDir(t.TempDir()) // 测后继续指向临时目录
	if err := AppendRun(mcRunRec("CR-SETDIR")); err != nil {
		t.Fatalf("AppendRun: %v", err)
	}
	if got := mcRunLineCount(t, filepath.Join(dir, calibrationRunsFile)); got != 1 {
		t.Errorf("注入目录行数 = %d,期望 1", got)
	}
}
