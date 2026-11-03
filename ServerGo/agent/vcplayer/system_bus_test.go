// Package vcplayer — system_bus_test.go: 批次81 City-Human 总线薄委托单测。
//
// 覆盖:CityRoomSource 未注入兜底 / 正常格式化(座位排序、死亡标记)/
// 空房间 / error 透传 / ctx 感知源与取消传播 / nil ctx 防御。
//
// 注:本文件操作包级全局 CityRoomSource,用例**不得** t.Parallel()(包内既有
// 用例也无并行,全局替换 + t.Cleanup 还原安全)。
package vcplayer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// withCityRoomSource 临时替换全局注入点,测试结束后还原。
func withCityRoomSource(t *testing.T, src RoomResidentsSource) {
	t.Helper()
	old := CityRoomSource
	CityRoomSource = src
	t.Cleanup(func() { CityRoomSource = old })
}

// fakePlainSource 只实现 RoomResidentsSource(无 ctx 变体)。
type fakePlainSource struct {
	briefs  []ResidentBrief
	err     error
	gotRoom string
}

func (f *fakePlainSource) SystemBusResidents(roomID string) ([]ResidentBrief, error) {
	f.gotRoom = roomID
	return f.briefs, f.err
}

// fakeCtxSource 同时实现可选的 ctxAwareResidentsSource(模拟支持取消的源:
// 收到已取消 ctx 时返回 ctx.Err(),与真实实现同语义)。
type fakeCtxSource struct {
	fakePlainSource
	gotCtx context.Context
}

func (f *fakeCtxSource) SystemBusResidentsContext(ctx context.Context, roomID string) ([]ResidentBrief, error) {
	f.gotCtx = ctx
	f.gotRoom = roomID
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.briefs, f.err
}

// TestSystemBusResidents_NilSource 未注入时返回可读说明,不返回 error。
func TestSystemBusResidents_NilSource(t *testing.T) {
	withCityRoomSource(t, nil)
	text, err := SystemBusResidents(context.Background(), "R1")
	if err != nil {
		t.Fatalf("nil source must not return error, got %v", err)
	}
	if !strings.Contains(text, "居民摘要不可用") || !strings.Contains(text, "CityRoomSource") {
		t.Fatalf("nil source message = %q, want 可读说明", text)
	}
}

// TestSystemBusResidents_NormalFormat 正常路径:座位升序、#Seat+1 显示、
// 死亡标记、存活计数与头部统计。
func TestSystemBusResidents_NormalFormat(t *testing.T) {
	src := &fakePlainSource{briefs: []ResidentBrief{
		{Seat: 2, Nickname: "王五", ModelKey: "GLM", Alive: true, Cash: 222, District: "d3"},
		{Seat: 0, Nickname: "张三", ModelKey: "DeepSeek", Alive: true, Cash: 12345, District: "d1"},
		{Seat: 1, Nickname: "李四", ModelKey: "Kimi", Alive: false, Cash: 0, District: "d2"},
	}}
	withCityRoomSource(t, src)

	text, err := SystemBusResidents(context.Background(), "R1")
	if err != nil {
		t.Fatalf("SystemBusResidents() error = %v", err)
	}
	if src.gotRoom != "R1" {
		t.Fatalf("source room = %q, want R1", src.gotRoom)
	}
	want := "房间 R1 居民 3 人(存活 2):\n" +
		"#1 张三(ModelKey=DeepSeek, 区=d1, 现金=12345)\n" +
		"#2 李四(ModelKey=Kimi, 区=d2, 现金=0, 已死亡)\n" +
		"#3 王五(ModelKey=GLM, 区=d3, 现金=222)"
	if text != want {
		t.Fatalf("format mismatch:\n got: %q\nwant: %q", text, want)
	}
}

// TestSystemBusResidents_EmptyRoom 空房间:可读说明,不 error。
func TestSystemBusResidents_EmptyRoom(t *testing.T) {
	withCityRoomSource(t, &fakePlainSource{briefs: nil})
	text, err := SystemBusResidents(context.Background(), "R9")
	if err != nil {
		t.Fatalf("empty room must not error, got %v", err)
	}
	if !strings.Contains(text, "R9") || !strings.Contains(text, "暂无居民") {
		t.Fatalf("empty room text = %q, want 含 R9 与 暂无居民", text)
	}
}

// TestSystemBusResidents_ErrorPassthrough source 的 error 原样透传,
// 且不产出文本(避免 System Agent 把错误当居民数据回灌)。
func TestSystemBusResidents_ErrorPassthrough(t *testing.T) {
	boom := errors.New("room not found: R404")
	withCityRoomSource(t, &fakePlainSource{err: boom})

	text, err := SystemBusResidents(context.Background(), "R404")
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty on error", text)
	}
}

// TestSystemBusResidents_CtxAwareSource 注入源实现 ctx 变体时,ctx 被透传。
func TestSystemBusResidents_CtxAwareSource(t *testing.T) {
	src := &fakeCtxSource{fakePlainSource: fakePlainSource{briefs: []ResidentBrief{
		{Seat: 0, Nickname: "张三", ModelKey: "DeepSeek", Alive: true, Cash: 100, District: "d1"},
	}}}
	withCityRoomSource(t, src)

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-1")
	text, err := SystemBusResidents(ctx, "R7")
	if err != nil {
		t.Fatalf("SystemBusResidents() error = %v", err)
	}
	if src.gotCtx != ctx {
		t.Fatal("ctx-aware source must receive the caller ctx")
	}
	if src.gotRoom != "R7" {
		t.Fatalf("source room = %q, want R7", src.gotRoom)
	}
	if !strings.Contains(text, "#1 张三") {
		t.Fatalf("text = %q, want 含 #1 张三", text)
	}
}

// TestSystemBusResidents_CtxCancelledBeforeCall 普通源 + 已取消 ctx:
// 调用前短路,source 不得被触达。
func TestSystemBusResidents_CtxCancelledBeforeCall(t *testing.T) {
	src := &fakePlainSource{}
	withCityRoomSource(t, src)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	text, err := SystemBusResidents(ctx, "R1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if src.gotRoom != "" {
		t.Fatal("cancelled ctx must not reach the source")
	}
}

// TestSystemBusResidents_CtxAwareCancelled ctx 感知源 + 已取消 ctx:
// 调用前 ctx.Err() 短路优先(不把死 ctx 交给 source),source 不得被触达。
func TestSystemBusResidents_CtxAwareCancelled(t *testing.T) {
	src := &fakeCtxSource{fakePlainSource: fakePlainSource{briefs: []ResidentBrief{
		{Seat: 0, Nickname: "张三", Alive: true, Cash: 1, District: "d1"},
	}}}
	withCityRoomSource(t, src)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	text, err := SystemBusResidents(ctx, "R1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if src.gotRoom != "" {
		t.Fatal("cancelled ctx must short-circuit before any source call")
	}
}

// TestSystemBusResidents_NilCtx nil ctx 不得 panic(降级 Background)。
func TestSystemBusResidents_NilCtx(t *testing.T) {
	src := &fakePlainSource{briefs: []ResidentBrief{
		{Seat: 0, Nickname: "张三", Alive: true, Cash: 1, District: "d1"},
	}}
	withCityRoomSource(t, src)

	text, err := SystemBusResidents(nil, "R1")
	if err != nil {
		t.Fatalf("nil ctx must not error, got %v", err)
	}
	if !strings.Contains(text, "张三") {
		t.Fatalf("text = %q, want 含 张三", text)
	}
}
