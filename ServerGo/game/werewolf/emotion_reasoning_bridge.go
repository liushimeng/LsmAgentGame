// Package werewolf — emotion_reasoning_bridge.go: 狼人杀 13 人局 Agent 情绪→推理桥（§20260826-01 U4）。
//
// 设计动机（DeepSeek §四.1 / M3 §5 P2）：
//   - 现有 emotion.go 只影响**发言风格**(tone)，不影响**推理**。
//   - 真人玩家带情绪时会偏执/轻信/激进 — Agent 也应当如此。
//   - 本模块做「情绪驱动的认知偏差」：
//     wary → 假说 confidence 下限钳制（疑者偏疑）
//     guilty → 假说 confidence 上限钳制（撒谎者心虚）
//     irritated → Threat 倍率 1.5x（情绪激动放大危险感）
//     confident → 假说不轻易改（高稳定性）
//     excited → Trust / Cooperation 倍率 1.3x（情绪好时容易信任）
//
// §92a 锁约束：所有方法 *Locked 语义；调用方必须已持 r.mu。
// §128 对话即思考：本模块只修正 LLM 已经输出的 confidence 数字；不新独立调用。
// §119 协议层隔离：本模块只修改内存态；不写 chat_message / chat_history。
package werewolf

// EmotionReasoningWeights 是单一情绪对推理的修正向量（§20260826-01 §2.3）。
type EmotionReasoningWeights struct {
	HypothesisConfidenceFloor int     // 假说 confidence 下限（0=不限）
	HypothesisConfidenceCeil  int     // 假说 confidence 上限（0=不限）
	ThreatMultiplier          float32 // Threat 维度倍率（1.0=不变）
	TrustMultiplier           float32 // Trust / Cooperation 倍率（1.0=不变）
	StabilityBias             float32 // 0~1，越大越不轻易改假说
	SampleEvent               string  // 仅日志/调试用
}

// weightsForEmotion 查表：emotion → EmotionReasoningWeights。
//
// 设计动机：硬编码常量表便于 §130 接线（grep "weightsForEmotion"）。
func weightsForEmotion(emotion string) EmotionReasoningWeights {
	switch emotion {
	case "wary":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 60,
			HypothesisConfidenceCeil:  0,
			ThreatMultiplier:        1.2,
			TrustMultiplier:         0.8,
			StabilityBias:           0.7,
			SampleEvent:             "wary",
		}
	case "guilty":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 0,
			HypothesisConfidenceCeil:  50,
			ThreatMultiplier:         1.1,
			TrustMultiplier:          0.7,
			StabilityBias:            0.5,
			SampleEvent:              "guilty",
		}
	case "irritated":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 0,
			HypothesisConfidenceCeil:  0,
			ThreatMultiplier:         1.5,
			TrustMultiplier:          0.9,
			StabilityBias:            0.4,
			SampleEvent:              "irritated",
		}
	case "confused":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 30,
			HypothesisConfidenceCeil:  70,
			ThreatMultiplier:         1.0,
			TrustMultiplier:          1.0,
			StabilityBias:            0.3,
			SampleEvent:              "confused",
		}
	case "confident":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 0,
			HypothesisConfidenceCeil:  0,
			ThreatMultiplier:         0.9,
			TrustMultiplier:          1.1,
			StabilityBias:            0.8,
			SampleEvent:              "confident",
		}
	case "excited":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 0,
			HypothesisConfidenceCeil:  0,
			ThreatMultiplier:         0.8,
			TrustMultiplier:          1.3,
			StabilityBias:            0.6,
			SampleEvent:              "excited",
		}
	case "calm":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 0,
			HypothesisConfidenceCeil:  0,
			ThreatMultiplier:         1.0,
			TrustMultiplier:          1.0,
			StabilityBias:            0.7,
			SampleEvent:              "calm",
		}
	case "grievance":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 50,
			HypothesisConfidenceCeil:  0,
			ThreatMultiplier:         1.4,
			TrustMultiplier:          0.6,
			StabilityBias:            0.5,
			SampleEvent:              "grievance",
		}
	case "panic":
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 40,
			HypothesisConfidenceCeil:  80,
			ThreatMultiplier:         1.3,
			TrustMultiplier:          0.7,
			StabilityBias:            0.3,
			SampleEvent:              "panic",
		}
	default:
		return EmotionReasoningWeights{
			HypothesisConfidenceFloor: 0,
			HypothesisConfidenceCeil:  0,
			ThreatMultiplier:         1.0,
			TrustMultiplier:          1.0,
			StabilityBias:            0.5,
			SampleEvent:              emotion,
		}
	}
}

// ApplyEmotionToHypothesisConfidence 给定当前 emotion 与基础 confidence，
// 返回修正后的 confidence。
//
// 调用前置：无锁（纯函数）。
//
// 实现：钳制到 [floor, ceil] 区间。
func ApplyEmotionToHypothesisConfidence(baseConfidence int, emotion string) int {
	w := weightsForEmotion(emotion)
	c := baseConfidence
	if w.HypothesisConfidenceFloor > 0 && c < w.HypothesisConfidenceFloor {
		c = w.HypothesisConfidenceFloor
	}
	if w.HypothesisConfidenceCeil > 0 && c > w.HypothesisConfidenceCeil {
		c = w.HypothesisConfidenceCeil
	}
	if c < 0 {
		c = 0
	}
	if c > 100 {
		c = 100
	}
	return c
}

// ApplyEmotionToImpressionDims 给定当前 emotion 与基础 dim delta，
// 返回修正后的 delta（multiplier 应用于 Trust/Sincerity/Cooperation/Threat）。
//
// 调用前置：无锁。
func ApplyEmotionToImpressionDims(base ImpressionDims, emotion string) ImpressionDims {
	w := weightsForEmotion(emotion)
	out := base
	out.Trust *= w.TrustMultiplier
	// Competence 不受 emotion 倍率影响（能力是客观观察）
	out.Competence = base.Competence
	out.Sincerity *= w.TrustMultiplier
	out.Cooperation *= w.TrustMultiplier
	out.Threat *= w.ThreatMultiplier
	return out
}

// ApplyEmotionToHypothesisEntryLocked 在持锁路径上,对单条假说应用情绪修正。
//
// 典型调用：agent_runner.go 解析 LLM 输出 📊 JSON 段后、写入 HypothesisStore 之前。
func ApplyEmotionToHypothesisEntryLocked(entries []HypothesisEntry, emotion string) []HypothesisEntry {
	if len(entries) == 0 {
		return entries
	}
	out := make([]HypothesisEntry, len(entries))
	for i := range entries {
		e := entries[i]
		e.Confidence = ApplyEmotionToHypothesisConfidence(e.Confidence, emotion)
		out[i] = e
	}
	return out
}