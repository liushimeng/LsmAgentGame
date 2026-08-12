// Package wwjudge — judge_trust_trace.go: 狼人杀 13 人局「发言信任度轨迹」(§20260812-01 U4)。
//
// 设计动机 (§7.1 / M3 §6 V1):
//   - 法官 5 段总结(已由 §20260810-11 H1 落地)外新增「第 7 段【信任度轨迹】」。
//   - **不修改**现有 ParseSummary(6 段逻辑) — 在独立函数 ParseTrustTrace 中
//     扫描原始输出,捕获 LLM 主动输出的「【信任度轨迹】」段,容错过失历史 LLM
//     5~6 段输出(返回空 TrustTrace)。
//   - 信任度分数是 LLM 主观判断(-1.0 ~ +1.0),**不暴露身份**(§135),仅
//     反映「发言一致性 / 投票跟随度 / 情绪稳定性」。
//   - 仅在 status=="over" 后供前端 trust_trace 用,前端 HistoryDrawer 渲染
//     折线图(5 维 §26 对比度色板)。
//
// 全局约束(CLAUDE.md §13 / Agent-Surpport-01 §12):
//   - §130 接线:本文件由 ParseSummary 调用一次生产注入点(可作为可选 hook)。
//   - §135 公平性:TrustTraceEntry 不含 Role/RoleName,仅 seat + score。
//   - §197 流式续命:本模块纯解析,**不**新开 LLM 调用 — 仅复用主 LLM 响应。
//   - §121 数据形状:TrustTrace 用 []TrustTraceEntry JSON 数组;空时 omitempty 不下发。
package wwjudge

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// TrustTraceEntry 是单座位单日信任度。
type TrustTraceEntry struct {
	Seat  int     `json:"seat"`  // 0..numSeats-1
	Day   int     `json:"day"`   // 1..N
	Score float64 `json:"score"` // -1.0 ~ +1.0
}

// trustTraceLineRe 匹配 LLM 输出格式:
//   "【信任度轨迹】seat1: day1=0.2, day2=0.5, day3=-0.3; seat2: day1=0.0, ..."
// 终止字符:中英文分号 + 换行 + 段结束符(】)。
var trustTraceLineRe = regexp.MustCompile(`seat(\d+)\s*[:：]\s*([^;；\n】]+)`)
// trustTraceHeaderRe 只匹配标题本身,不吃入 body。
var trustTraceHeaderRe = regexp.MustCompile(`【信任度轨迹】`)

// ParseTrustTrace 从 LLM 原始输出中解析信任度轨迹段。
//
// 容错策略:
//   - 段不存在 → 返回空 []TrustTraceEntry(nil)
//   - 段存在但格式不全 → 返回已解析的部分
//   - 解析失败 → 不返回 panic,只返回空(LLM 旧格式不破坏 6 段解析)
//
// 调用方(ParseSummary 的 caller)可选地把返回的 TrustTrace 注入
// JudgeSummaryJSON.TrustTrace 字段(omitempty)。
func ParseTrustTrace(raw string) []TrustTraceEntry {
	if raw == "" {
		return nil
	}
	// 找到【信任度轨迹】段
	header := trustTraceHeaderRe.FindStringIndex(raw)
	if header == nil {
		return nil
	}
	start := header[1]
	// 段终止到下一个 "\n【" 段(且不是【信任度轨迹】本人);若没有则到 raw 末尾。
	end := len(raw)
	// 由简到繁:用子串搜索
	rest := raw[start:]
	for pos := 0; pos < len(rest); {
		idx := strings.Index(rest[pos:], "\n【")
		if idx < 0 {
			break
		}
		abs := pos + idx
		// 跳过【信任度轨迹】本身(LLM 可能重复输出)
		if abs+7 < len(rest) && rest[abs+1:abs+8] == "【信任度" {
			pos = abs + 1
			continue
		}
		end = start + abs
		break
	}
	body := strings.TrimSpace(raw[start:end])
	if body == "" {
		return nil
	}
	return parseTrustTraceBody(body)
}

// parseTrustTraceBody 解析段主体:逐条匹配 "seatN: day1=0.2, day2=0.5"。
func parseTrustTraceBody(body string) []TrustTraceEntry {
	matches := trustTraceLineRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]TrustTraceEntry, 0, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		seat1, err := strconv.Atoi(m[1])
		if err != nil || seat1 <= 0 {
			continue
		}
		// 提取 dayN=score 段
		dayScores := parseDayScores(m[2])
		for _, ds := range dayScores {
			if ds.Day < 1 || ds.Day > 30 {
				continue
			}
			if ds.Score < -1.0 {
				ds.Score = -1.0
			}
			if ds.Score > 1.0 {
				ds.Score = 1.0
			}
			out = append(out, TrustTraceEntry{
				Seat:  seat1 - 1, // 1-indexed → 0-indexed
				Day:   ds.Day,
				Score: ds.Score,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type dayScore struct {
	Day   int
	Score float64
}

var dayScoreRe = regexp.MustCompile(`day(\d+)\s*=\s*(-?[0-9]*\.?[0-9]+)`)

func parseDayScores(s string) []dayScore {
	matches := dayScoreRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]dayScore, 0, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		day, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		score, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		out = append(out, dayScore{Day: day, Score: score})
	}
	return out
}

// MarshalTrustTrace 把 TrustTraceEntry 数组序列化成 JSON 字符串(供 view.go 写 JSON Tag)。
func MarshalTrustTrace(entries []TrustTraceEntry) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// PrettyPrintTrustTrace 调试用格式化(测试可见)。
func PrettyPrintTrustTrace(entries []TrustTraceEntry) string {
	if len(entries) == 0 {
		return "<empty>"
	}
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("seat=%d day=%d score=%.2f\n", e.Seat+1, e.Day, e.Score))
	}
	return sb.String()
}
