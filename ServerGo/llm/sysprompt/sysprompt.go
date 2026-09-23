// Package sysprompt — 三段式 Anthropic system 提示词头(Claude Code 协议对齐)。
//
// # 为什么是一个独立 leaf 包
//
// 三段式头是**协议层**能力(Claude Code SDK 就是把这三段插在 system 数组
// 最前面),不是某个 Agent 的私有 prompt。因此它既不能散落在 17 个 Agent
// 的 prompt builder 里(§130「声明了却从不接线」+ 新增 Agent 必然漏配),
// 也不能塞进 llm/types(wire 类型包不承载文本资产)。本包只依赖 llm/types,
// 是标准 leaf 包,可被 provider 与测试自由 import。
//
// # 注入点(单一收口)
//
// llm/anthropic 与 llm/openai 两个 Provider 在**序列化前**统一调用
// EnsureHead,所以:
//
//   - 全部 Agent(狼人杀玩家/法官/解说、辩论玩家/裁判/解说、德扑玩家、
//     财商居民、记忆迭代器、上下文压缩器…以及未来新增 Agent)自动带上三段;
//   - Agent 侧代码**不得**再手工拼接这三段(会撞幂等判定,且重复计入字节预算);
//   - 覆盖率由 provider 层的 wire 断言测试保证(见
//     llm/anthropic/sysprompt_head_test.go)。
//
// 详见 lag_docs/LLM与Agent/AgentAnthropic系统提示词三段式规范.md。
package sysprompt

import (
	"strings"

	types "LsmAgentGame/llm/types"
)

// ephemeralCacheControl 返回 Anthropic prompt cache 断点位
// ({"type":"ephemeral"})。每次调用新建 map:块会被上层 json.Marshal 且
// 可能被调用方改写,共享同一个 map 会造成跨请求串扰。
func ephemeralCacheControl() map[string]string {
	return map[string]string{"type": "ephemeral"}
}

// BillingBlock 返回第 ① 段:计费元数据头。**不带** cache_control —— 它
// 逐 Agent 类变化(cc_agent_name),作为缓存前缀会击穿命中率。
func BillingBlock(agentClass string) types.SystemBlock {
	return types.SystemBlock{
		Type: "text",
		Text: BillingHeaderText(EntrypointServer, agentClass != "", agentClass),
	}
}

// IdentityBlock 返回第 ② 段:基础身份声明(ephemeral 缓存断点)。
func IdentityBlock() types.SystemBlock {
	return types.SystemBlock{
		Type:         "text",
		Text:         IdentityText,
		CacheControl: ephemeralCacheControl(),
	}
}

// CoreRulesBlock 返回第 ③ 段:核心行为规则(ephemeral 缓存断点)。
func CoreRulesBlock() types.SystemBlock {
	return types.SystemBlock{
		Type:         "text",
		Text:         CoreRulesText,
		CacheControl: ephemeralCacheControl(),
	}
}

// Head 返回标准三段式头 [①计费头 ②身份 ③核心规则]。
//
// agentClass 取自 types.LLMRequest.AgentClassName(本仓库的 AgentClassName,
// 例 "LsmAgentGame-Werewolf-Player")。非空 ⇒ cc_is_subagent=true:该调用由
// 服务端引擎以 task 形态驱动,行为等价于 Claude Code 的 subagent;为空 ⇒
// 非 Agent 调用(健康探针 / 后台自检),cc_is_subagent=false 且不输出
// cc_agent_name。
func Head(agentClass string) []types.SystemBlock {
	return []types.SystemBlock{
		BillingBlock(agentClass),
		IdentityBlock(),
		CoreRulesBlock(),
	}
}

// EnsureHead 把三段式头插到 body 之前,并保证**幂等**(body 已带头时原样返回)。
//
// 幂等判定的存在意义:Agent 侧若显式构造了头(测试、或未来某个 Agent 需要
// 自定义 cc_entrypoint),Provider 不能再插一遍 —— 两遍头既浪费 token,也会
// 让 strict 代理(豆包)对重复的 x-anthropic-billing-header 文本产生歧义。
func EnsureHead(body []types.SystemBlock, agentClass string) []types.SystemBlock {
	if HasHead(body) {
		return body
	}
	head := Head(agentClass)
	if len(body) == 0 {
		return head
	}
	out := make([]types.SystemBlock, 0, len(head)+len(body))
	out = append(out, head...)
	out = append(out, body...)
	return out
}

// HasHead 判断 system 数组是否已带三段式头的第 ① 段。
func HasHead(body []types.SystemBlock) bool {
	return len(body) > 0 && strings.HasPrefix(strings.TrimSpace(body[0].Text), BillingHeaderPrefix)
}

// HeadBytes 估算三段式头在 wire 上的字节数(不含 JSON 包装)。
// 供 Agent 侧上下文字节预算把这段"隐形开销"计入(§20260810-14 的同类问题:
// 只算 messages 会让小窗口模型实际超限而预算未触发)。
func HeadBytes(agentClass string) int {
	n := 0
	for _, b := range Head(agentClass) {
		n += len(b.Type) + len(b.Text)
		if len(b.CacheControl) > 0 {
			// cache_control JSON 近似:{"type":"ephemeral"} ≈ 40 bytes
			n += 40
		}
	}
	return n
}
