package service

// room_service_reveal_role_test.go — §20260830-01 「死亡亮身份」建房三态解析单测
//(设计文档 §10.2 C-1/C-2 的 service 层可测部分;C-3 非 werewolf 忽略由
// ws.GameService.SetRevealRoleOnDeath 的 kind 白名单实现,全链路断言需 DB,
// 以 resolveRevealRoleOnDeath + 调用点 kind 守卫双保险覆盖)。

import (
	"testing"

	"LsmAgentGame/config"
)

func boolPtrSvc(b bool) *bool { return &b }

// TestResolveRevealRole_C1_ExplicitValueWins 显式 true/false → 以请求为准(C-1)。
func TestResolveRevealRole_C1_ExplicitValueWins(t *testing.T) {
	cfgFalse := &config.Config{}
	cfgFalse.Werewolf.RevealRoleOnDeathDefault = boolPtrSvc(false)
	for _, want := range []bool{true, false} {
		if got := resolveRevealRoleOnDeath(boolPtrSvc(want), cfgFalse); got != want {
			t.Fatalf("显式 %v 应胜过 cfg 默认, got %v", want, got)
		}
	}
}

// TestResolveRevealRole_C2_AbsentDefaultsTrue 不传字段(nil)→ cfg 默认;
// cfg 未配置 / cfg 为 nil → true(旧客户端默认开启,C-2)。
func TestResolveRevealRole_C2_AbsentDefaultsTrue(t *testing.T) {
	if got := resolveRevealRoleOnDeath(nil, nil); !got {
		t.Fatalf("cfg nil + 请求未传 → 应默认 true")
	}
	if got := resolveRevealRoleOnDeath(nil, &config.Config{}); !got {
		t.Fatalf("cfg 字段未配置 + 请求未传 → 应默认 true")
	}
	trueCfg := &config.Config{}
	trueCfg.Werewolf.RevealRoleOnDeathDefault = boolPtrSvc(true)
	if got := resolveRevealRoleOnDeath(nil, trueCfg); !got {
		t.Fatalf("cfg 默认 true → true")
	}
}

// TestResolveRevealRole_CfgFalseDefault cfg 显式 false 且请求未传 → false
// (竞技局全局默认,运维 kill switch)。
func TestResolveRevealRole_CfgFalseDefault(t *testing.T) {
	cfg := &config.Config{}
	cfg.Werewolf.RevealRoleOnDeathDefault = boolPtrSvc(false)
	if got := resolveRevealRoleOnDeath(nil, cfg); got {
		t.Fatalf("cfg 默认 false 应生效(请求未传)")
	}
}
