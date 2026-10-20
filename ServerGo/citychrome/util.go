// Package citychrome — util.go: 小工具(数值/时间)。
package citychrome

import "math"

// isNaNOrInf 报告 f 是否 NaN 或 ±Inf(JSON 无法表达,必须拒)。
func isNaNOrInf(f float64) bool {
	return math.IsNaN(f) || math.IsInf(f, 0)
}
