// Package ws — game_service_virtual_city_system_flag_test.go: 批次87 P0-1
// 「系统对话假停用」回归网(2026-11-06 §批次87)。
//
// 根因:虚拟城市客户端快照共有 4 个构造点,其中 2 个(game.state 玩家分支 /
// 观战分支)漏盖 system_agent_enabled ⇒ 字段恒为 Go 零值 false ⇒ 前端
// (VirtualCityGamePage.tsx:107)判「服务端 enabled=false」⇒ 内联红条 +
// 发送按钮禁用 ⇒ **整个系统对话窗口不可用**(前端每 8s 轮询 game.state,
// 于是必然复现)。
//
// 本文件锁死三件事:
//  1. enabled=true/false 时,game.state 玩家分支与观战分支的应答载荷
//     system_agent_enabled 取值必须正确;
//  2. 观战分支「房间未开局 ⇒ 回 30001」的原语义未被改动;
//  3. **构造入口收口性**:ServerGo/ws/ 下对 virtual_city.BuildClientState
//     的调用只允许出现在 vcClientState 内(新增调用点再漏盖 ⇒ 结构上不可能)。
package ws

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"
)

// newVCFlagTestGame 构造已开局的虚拟城市房间 + GameService(开关可配)。
func newVCFlagTestGame(t *testing.T, enabled bool) (*GameService, string) {
	t.Helper()
	manager := virtual_city.NewManager(virtual_city.Config{
		MonthMs: 3000, AgentEnabled: false, AgentConcurrency: 4,
	}, nil)
	room := manager.CreateRoom("room-vc-flag")
	botUsers := map[int]string{}
	botModels := map[int]string{}
	for seat := 0; seat < virtual_city.MinSeats; seat++ {
		botUsers[seat] = "vc-bot-" + strconv.Itoa(seat)
		botModels[seat] = "TestModel"
	}
	room.RegisterBotSeats(botUsers, botModels)
	if e := room.Start(nil); e != nil {
		room.Close()
		t.Fatalf("start room: %v", e)
	}
	t.Cleanup(room.Close)
	gs := &GameService{hub: NewHub(), vcMgr: manager}
	gs.SetCitySystemRuntime(enabled, config.ResolvedCitySystemConfig{
		ModelKey: "FakeModel", MaxSteps: 2, MaxMessages: 10,
		RequestTimeout: 2 * time.Second, ToolResultLimit: 500,
	}, nil)
	return gs, room.RoomID
}

// vcFlagStateFlag 从 game.state 应答载荷里取 system_agent_enabled。
func vcFlagStateFlag(t *testing.T, env Envelope) bool {
	t.Helper()
	if env.Type != "game.state" {
		t.Fatalf("frame type = %s, want game.state (payload=%s)", env.Type, string(env.Payload))
	}
	var p map[string]any
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal game.state payload: %v", err)
	}
	raw, ok := p["system_agent_enabled"]
	if !ok {
		t.Fatalf("game.state payload missing system_agent_enabled: %s", string(env.Payload))
	}
	b, ok := raw.(bool)
	if !ok {
		t.Fatalf("system_agent_enabled type = %T, want bool", raw)
	}
	return b
}

// TestVCClientState_GameStatePlayerBranch 玩家分支:enabled 两种取值都必须
// 原样出现在 game.state 应答里(P0-1 主诉路径)。
func TestVCClientState_GameStatePlayerBranch(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		gs, roomID := newVCFlagTestGame(t, enabled)
		c := sysClient("u-player")
		gs.handleGetState(c, Envelope{Type: "game.state", Seq: 1,
			Payload: json.RawMessage(`{"room_id":"` + roomID + `","game_kind":"virtual_city"}`)})
		env := nextSysEnvelope(t, c)
		if got := vcFlagStateFlag(t, env); got != enabled {
			t.Fatalf("enabled=%v: game.state system_agent_enabled = %v, want %v",
				enabled, got, enabled)
		}
	}
}

// TestVCClientState_GameStateSpectatorBranch 观战分支:enabled 两种取值都必须
// 原样出现在 game.state 应答里(P0-1 漏盖点之一)。
func TestVCClientState_GameStateSpectatorBranch(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		gs, roomID := newVCFlagTestGame(t, enabled)
		c := sysClient("u-spectator")
		gs.hub.SpectateRoom(roomID, c)
		gs.handleGetState(c, Envelope{Type: "game.state", Seq: 1,
			Payload: json.RawMessage(`{"room_id":"` + roomID + `","game_kind":"virtual_city"}`)})
		env := nextSysEnvelope(t, c)
		if got := vcFlagStateFlag(t, env); got != enabled {
			t.Fatalf("enabled=%v: spectator game.state system_agent_enabled = %v, want %v",
				enabled, got, enabled)
		}
	}
}

// TestVCClientState_SpectatorBranchUnstartedRoom 观战分支「房间未开局 ⇒
// 回 30001」的原语义保持不变(收口重构不得改动该判定)。
func TestVCClientState_SpectatorBranchUnstartedRoom(t *testing.T) {
	manager := virtual_city.NewManager(virtual_city.Config{
		MonthMs: 3000, AgentEnabled: false, AgentConcurrency: 4,
	}, nil)
	room := manager.CreateRoom("room-vc-unstarted")
	t.Cleanup(room.Close)
	gs := &GameService{hub: NewHub(), vcMgr: manager}
	gs.SetCitySystemRuntime(true, config.ResolvedCitySystemConfig{}, nil)

	c := sysClient("u-spectator")
	gs.hub.SpectateRoom(room.RoomID, c)
	gs.handleGetState(c, Envelope{Type: "game.state", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"` + room.RoomID + `","game_kind":"virtual_city"}`)})
	env := nextSysEnvelope(t, c)
	if env.Type != "game.error" {
		t.Fatalf("frame type = %s, want game.error", env.Type)
	}
	if code := sysEnvelopeCode(t, env); code != errcode.ErrRoomNotFound {
		t.Fatalf("code = %d, want %d", code, errcode.ErrRoomNotFound)
	}
}

// TestVCClientState_SoleConstructionEntry 防回归(**收口性**):
// ServerGo/ws/ 下对 virtual_city.BuildClientState 的调用**只允许**出现在
// vcClientState 内 —— 新增调用点若忘记盖 system_agent_enabled,本单测红。
//
// 实现用 go/ast 解析 ws 包源码(AST 而非文本 grep ⇒ 对格式/换行不敏感,
// 不会因重构误报),并按文件解析 import 别名,避免硬编码包名。
func TestVCClientState_SoleConstructionEntry(t *testing.T) {
	dir := wsPackageDir(t)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse ws package: %v", err)
	}
	total := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			alias := virtualCityImportAlias(file)
			if alias == "" {
				continue
			}
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				calls := 0
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					x, ok := sel.X.(*ast.Ident)
					if !ok {
						return true
					}
					if x.Name == alias && sel.Sel.Name == "BuildClientState" {
						calls++
					}
					return true
				})
				if calls == 0 {
					continue
				}
				total += calls
				if fd.Name.Name != "vcClientState" {
					t.Errorf("%s: %s() 调用 virtual_city.BuildClientState %d 次 —— "+
						"必须统一走 vcClientState(否则会漏盖 system_agent_enabled,批次87 P0-1)",
						filepath.Base(path), fd.Name.Name, calls)
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("ws 包内未找到 virtual_city.BuildClientState 调用 —— vcClientState 可能被误删")
	}
}

// virtualCityImportAlias 返回文件里 virtual_city 包的本地别名(未 import ⇒ "")。
func virtualCityImportAlias(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if path != "LsmAgentGame/game/virtual_city" {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "virtual_city"
	}
	return ""
}

// wsPackageDir 返回本包源码目录(测试运行时定位 ws 包源文件用)。
func wsPackageDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(thisFile)
}
