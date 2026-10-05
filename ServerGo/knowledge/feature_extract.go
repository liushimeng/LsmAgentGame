// Package knowledge — feature_extract.go: LLM 特征抽取管线
// (2026-10-05 §LsmKLBaseServer 知识库引擎重构)。
//
// 职责: 将 Markdown 人物卡通过 LLM 抽取为结构化特征字符串数组。
// 特征字符串采用「键:值」格式，如「职业:外卖骑手」「行业:交通运输」。
//
// 抽取流程:
//   1. 读取 Markdown 内容
//   2. 构造 LLM 请求(抽取 prompt + 卡内容)
//   3. LLM 返回 JSON(特征字符串数组)
//   4. 校验 + 返回
//
// 失败语义:
//   - LLM 调用失败 → 返回错误(调用方回退到 frontmatter 解析)
//   - LLM 返回格式异常 → 尝试修复(提取 JSON 数组)
//   - 修复失败 → 返回错误
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"LsmAgentGame/llm"
	"LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// extractBatchSize 是批量抽取时每批的卡数(减少 LLM 调用次数)。
const extractBatchSize = 5

// extractMaxContentLength 是发送给 LLM 的单卡内容最大长度(字符)。
// 超长截断 — 人物卡通常 < 2000 字符,截断只是兜底。
const extractMaxContentLength = 4000

// FeatureExtractor 是 LLM 特征抽取器。
type FeatureExtractor struct {
	registry *llm.Registry
	modelKey string
	mu       sync.Mutex
}

// NewFeatureExtractor 构造特征抽取器。
// registry 为 nil 时抽取不可用(调用方回退 frontmatter)。
func NewFeatureExtractor(registry *llm.Registry, modelKey string) *FeatureExtractor {
	return &FeatureExtractor{
		registry: registry,
		modelKey: modelKey,
	}
}

// extractSystemPrompt 是特征抽取的 system prompt。
// 要求 LLM 返回固定格式的 JSON: {"features": ["键:值", ...]}
const extractSystemPrompt = `你是一个知识库特征抽取引擎。你的任务是从给定的人物卡 Markdown 文档中提取结构化特征，输出为字符串数组。

## 输出格式
严格返回 JSON，不要包含其他文字：
{"features": ["键:值", "键:值", ...]}

## 特征键值对提取规则
从文档中提取以下特征（存在才提取，不存在跳过）：
- 职业: 职业名称
- 行业: 所属行业
- 收入: 月收入（数字，单位元）
- 支出: 月支出（数字，单位元）
- 储蓄: 初始储蓄（数字，单位元）
- 年龄: 年龄（数字）
- 性别: 男/女
- 人格: 人格标签（多个用顿号连接）
- 行为: 行为特征（多个用顿号连接）
- 健康: 健康等级（A/B/C）
- 风险偏好: conservative/balanced/aggressive
- 就业形态: 全职/兼职/灵活就业等
- 城区: 所在城区
- 婚姻: 已婚/单身
- 子女数: 子女数量（数字）
- 赡养老人: 需赡养老人数量（数字）

## 注意事项
1. 只提取文档中明确存在的信息，不要推测
2. 数值只保留数字，不要带单位
3. 特征字符串格式严格为「键:值」
4. 返回的 JSON 必须是合法 JSON，不能包含注释或多余文字`

// ExtractFromContent 从 Markdown 内容抽取特征字符串数组。
// 返回特征数组 + 错误。失败时调用方应回退到 frontmatter 解析。
func (e *FeatureExtractor) ExtractFromContent(ctx context.Context, cardID, content string) ([]string, error) {
	if e.registry == nil {
		return nil, fmt.Errorf("knowledge extractor: registry not available")
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("knowledge extractor: empty content for card %s", cardID)
	}

	// 截断超长内容
	if len(content) > extractMaxContentLength {
		content = content[:extractMaxContentLength]
	}

	provider, key, err := e.registry.Get(e.modelKey)
	if err != nil {
		return nil, fmt.Errorf("knowledge extractor: registry.Get(%s): %w", e.modelKey, err)
	}

	req := llm.LLMRequest{
		Model: e.modelKey,
		System: []types.SystemBlock{
			{Type: "text", Text: extractSystemPrompt},
		},
		Messages: []types.Message{
			{
				Role: "user",
				Content: []types.ContentBlock{
					{Type: "text", Text: content},
				},
			},
		},
		MaxTokens: 1024,
		AgentClassName: "LsmAgentGame-Knowledge-Extractor",
	}

	resp, err := provider.Chat(ctx, key, req)
	if err != nil {
		return nil, fmt.Errorf("knowledge extractor: LLM chat failed for card %s: %w", cardID, err)
	}

	features := parseExtractResponse(resp.Text())
	if len(features) == 0 {
		return nil, fmt.Errorf("knowledge extractor: no features extracted for card %s", cardID)
	}
	return features, nil
}

// ExtractBatch 批量抽取特征(一次 LLM 调用处理多张卡)。
// 返回每张卡的抽取结果(成功/失败)。
func (e *FeatureExtractor) ExtractBatch(ctx context.Context, requests []ExtractRequest) []ExtractResult {
	results := make([]ExtractResult, len(requests))
	if e.registry == nil || len(requests) == 0 {
		return results
	}

	// 分批处理
	for start := 0; start < len(requests); start += extractBatchSize {
		end := start + extractBatchSize
		if end > len(requests) {
			end = len(requests)
		}
		batch := requests[start:end]

		// 构造批量抽取的 user message
		var sb strings.Builder
		sb.WriteString("请从以下多个人物卡文档中分别提取特征。每张卡用 === 分隔。\n")
		sb.WriteString("返回 JSON 格式: {\"results\": [{\"card_id\": \"...\", \"features\": [\"键:值\", ...]}, ...]}\n\n")
		for i, req := range batch {
			content := req.Content
			if len(content) > extractMaxContentLength {
				content = content[:extractMaxContentLength]
			}
			sb.WriteString(fmt.Sprintf("=== 卡 %d (id: %s) ===\n%s\n\n", i+1, req.CardID, content))
		}

		provider, key, err := e.registry.Get(e.modelKey)
		if err != nil {
			for i := range batch {
				results[start+i] = ExtractResult{CardID: batch[i].CardID, Error: err.Error()}
			}
			continue
		}

		req := llm.LLMRequest{
			Model: e.modelKey,
			System: []types.SystemBlock{
				{Type: "text", Text: extractSystemPrompt},
			},
			Messages: []types.Message{
				{
					Role: "user",
					Content: []types.ContentBlock{
						{Type: "text", Text: sb.String()},
					},
				},
			},
			MaxTokens: 4096,
			AgentClassName: "LsmAgentGame-Knowledge-Extractor",
		}

		resp, err := provider.Chat(ctx, key, req)
		if err != nil {
			for i := range batch {
				results[start+i] = ExtractResult{CardID: batch[i].CardID, Error: err.Error()}
			}
			continue
		}

		batchResults := parseBatchExtractResponse(resp.Text())
		for i := range batch {
			if r, ok := batchResults[batch[i].CardID]; ok {
				results[start+i] = r
			} else {
				results[start+i] = ExtractResult{CardID: batch[i].CardID, Error: "not found in batch response"}
			}
		}
	}

	return results
}

// parseExtractResponse 解析 LLM 返回的特征抽取响应。
// 期望格式: {"features": ["键:值", ...]}
// 容错: 尝试从文本中提取 JSON 数组。
func parseExtractResponse(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// 尝试直接解析 JSON
	var raw struct {
		Features []string `json:"features"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err == nil && len(raw.Features) > 0 {
		return raw.Features
	}

	// 容错: 提取 JSON 对象
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		jsonText := text[start : end+1]
		if err := json.Unmarshal([]byte(jsonText), &raw); err == nil && len(raw.Features) > 0 {
			return raw.Features
		}
	}

	// 容错: 直接提取数组
	start = strings.Index(text, "[")
	end = strings.LastIndex(text, "]")
	if start >= 0 && end > start {
		var features []string
		if err := json.Unmarshal([]byte(text[start:end+1]), &features); err == nil && len(features) > 0 {
			return features
		}
	}

	return nil
}

// parseBatchExtractResponse 解析批量抽取响应。
// 期望格式: {"results": [{"card_id": "...", "features": [...]}, ...]}
func parseBatchExtractResponse(text string) map[string]ExtractResult {
	text = strings.TrimSpace(text)
	result := make(map[string]ExtractResult)
	if text == "" {
		return result
	}

	var raw struct {
		Results []struct {
			CardID   string   `json:"card_id"`
			Features []string `json:"features"`
		} `json:"results"`
	}

	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		// 容错: 提取 JSON 对象
		start := strings.Index(text, "{")
		end := strings.LastIndex(text, "}")
		if start >= 0 && end > start {
			jsonText := text[start : end+1]
			if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
				return result
			}
		} else {
			return result
		}
	}

	for _, r := range raw.Results {
		if r.CardID != "" && len(r.Features) > 0 {
			result[r.CardID] = ExtractResult{CardID: r.CardID, Features: r.Features}
		}
	}
	return result
}

// logExtractFailure 记录抽取失败日志(可观测性)。
func logExtractFailure(cardID string, err error) {
	logger.L().Warn("knowledge feature extraction failed, will fallback to frontmatter",
		zap.String("card_id", cardID),
		zap.Error(err))
}
