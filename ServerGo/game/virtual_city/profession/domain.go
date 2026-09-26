// Package profession — domain.go: 抽卡随卡返回 L1 行业域(2026-09-21
// §虚拟城市-城市Agent规模化)。
//
// 契约: 城市背景模拟设计 v1 §3.2 —— L1 域名从「卡来源路径首段」提取
// (如 `A-农林牧渔/N9012/xxx.md` → "A-农林牧渔")。Card 本身不携带磁盘路径
// (wire/持久化形状不扩),故由 loader 在 Draw 侧随卡返回 —— 契约允许的二选一
// 实现之一。城市校准表(city.CalibTable)是唯一消费方。
package profession

import (
	"math/rand"
	"strings"
)

// DomainCard 是带 L1 行业域来源的单张抽卡结果。Domain 为空串表示该卡无法
// 归入 26 个 L1 域(合成兜底卡 / `维度2-…` 等非行业目录来源)。
type DomainCard struct {
	Card
	// Domain 是 L1 行业域名(路径首段,形如 "A-农林牧渔");非 26 域来源为 ""。
	Domain string
	// SourcePath 卡来源相对路径(如 "A-农林牧渔/N9012/xxx.md";合成兜底卡为 "")。
	// 2026-09-26 §批次25 persona 统一:座位卡池记住来源路径,N≤12 时档案锚定
	// 直接复用同一批路径,使背景居民 i 的人物卡 = 座位 i 的人物卡。
	SourcePath string
}

// domainOfPath 从卡池相对路径提取 L1 域名(首段)。合法 L1 域目录名形如
// `<大写字母>-<名称>`;其余(`维度2-…` 等)返回 ""。`_框架` 前缀目录已被
// buildIndex 整体跳过,不会走到这里。
func domainOfPath(rel string) string {
	seg := rel
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		seg = rel[:i]
	}
	if len(seg) < 2 || seg[0] < 'A' || seg[0] > 'Z' || seg[1] != '-' {
		return ""
	}
	return seg
}

// DrawWithDomain 抽 n 张卡并随卡返回 L1 域名。与 Draw 共用同一候选/补抽/
// 回退链(drawPairs),同 rng 消耗序列 → 同 seed 同结果,仅多携带 Domain。
func (l *Loader) DrawWithDomain(n int, rng *rand.Rand) []DomainCard {
	return l.drawPairs(n, rng)
}
