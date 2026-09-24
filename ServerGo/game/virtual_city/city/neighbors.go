// Package city — neighbors.go: 城区邻居抽样与感官快照类型
// (2026-09-22 §CityHuman重构,设计契约: lag_docs/虚拟城市/已实现/12-CityHuman重构/
// 虚拟城市-CityHuman-Agent合并与感知系统设计-v1.md §3/§5)。
//
// 供 wealth 层 see/hear 感知工具使用:返回同城区背景居民的已锚定真实档案摘要
// (姓名/职业/状态),档案未就绪时降级为代号语义并省略 CardID。
package city

// DistrictNeighbor 是单名背景居民的感知摘要(wealth 层 NeighborBrief 数据源)。
type DistrictNeighbor struct {
	CardID       string // 已锚定:人物卡编号;未锚定:空
	Name         string // 已锚定:真实姓名;未锚定:代号(如 "A1024·金融CBD")
	Occupation   string // 职业(未锚定:行业域名)
	DistrictName string // 城区中文名
	Employed     bool
	Stressed     bool
}

// AmbianceTags 是单城区当月气味/声响标签(city.Snapshot.ambiance 下发;
// wealth 层基底表 + 事件叠加的计算结果, city 包只做载体)。
type AmbianceTags struct {
	Smells []string `json:"smells"`
	Sounds []string `json:"sounds"`
}

// DistrictIndexOf 返回 wealth 城区 id 对应的城区索引(-1 = 未知)。
func DistrictIndexOf(id string) int {
	if idx, ok := districtIDIndex[id]; ok {
		return idx
	}
	return -1
}

// SampleDistrictNeighbors 抽样返回指定城区的 ≤count 名背景居民摘要。
// 锚定居民优先(真实姓名/职业);不足时用未锚定居民代号补齐。
// 内部使用 voiceRng 洗牌(与经济演化 rng 流分离,不影响确定性)。
func (b *Backdrop) SampleDistrictNeighbors(districtID string, count int) []DistrictNeighbor {
	if count <= 0 {
		return nil
	}
	idx := DistrictIndexOf(districtID)
	if idx < 0 {
		return nil
	}
	return b.sampleNeighborsExcluding(idx, -1, count)
}

// sampleNeighborsExcluding 抽样指定城区下标的 ≤count 名背景居民摘要
// (exclude<0 = 不排除;driver.go DriverBrief 用它排除居民本人)。
// 锚定居民优先;内部使用 voiceRng 洗牌(与经济演化 rng 流分离)。
func (b *Backdrop) sampleNeighborsExcluding(districtIdx, exclude, count int) []DistrictNeighbor {
	if count <= 0 || districtIdx < 0 || districtIdx >= districtCount {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var anchored, rest []int
	for i := range b.residents {
		r := &b.residents[i]
		if int(r.district) != districtIdx || i == exclude {
			continue
		}
		if i < b.profAnchored && i < len(b.profiles) {
			anchored = append(anchored, i)
		} else {
			rest = append(rest, i)
		}
	}
	b.voiceRng.Shuffle(len(anchored), func(i, j int) { anchored[i], anchored[j] = anchored[j], anchored[i] })
	b.voiceRng.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	out := make([]DistrictNeighbor, 0, count)
	pick := func(pool []int) {
		for _, i := range pool {
			if len(out) >= count {
				return
			}
			br, ok := b.briefLocked(i)
			if !ok {
				continue
			}
			n := DistrictNeighbor{
				CardID:       br.CardID,
				Name:         br.Name,
				Occupation:   br.Occupation,
				DistrictName: br.DistrictName,
				Employed:     br.Employed,
				Stressed:     br.Stressed,
			}
			if n.Name == "" {
				n.Name = b.codenameLocked(i) // 未锚定降级为代号
			}
			if n.Occupation == "" {
				n.Occupation = br.DomainName
			}
			out = append(out, n)
		}
	}
	pick(anchored)
	pick(rest)
	return out
}
