// Package knowledge — loader.go: 知识库加载器(从 MySQL 加载特征数据)
// (2026-10-05 §LsmKLBaseServer 知识库引擎重构)。
//
// 职责:
//   1. 从 MySQL t_lsm_game_knowledge_card 表加载知识卡
//   2. 提供搜索/查询接口(按特征、域、城区)
//   3. 管理特征抽取状态(触发抽取、查询进度)
//   4. 与 profession.Loader 协作(抽取失败时回退 frontmatter)
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"LsmAgentGame/models"

	"gorm.io/gorm"
)

// Loader 是知识库加载器(从 MySQL 加载)。
type Loader struct {
	db       *gorm.DB
	extractor *FeatureExtractor

	mu       sync.RWMutex
	cards    map[string]KnowledgeCard // 卡号 → 知识卡(进程内缓存)
	loaded   bool                    // 是否已从 MySQL 加载
}

// NewLoader 构造知识库加载器。
// db 为 nil 或 extractor 为 nil 时,加载器仍可用(查询返回空,抽取不可用)。
func NewLoader(db *gorm.DB, extractor *FeatureExtractor) *Loader {
	return &Loader{
		db:        db,
		extractor: extractor,
		cards:     make(map[string]KnowledgeCard),
	}
}

// LoadAll 从 MySQL 加载全部知识卡到进程内缓存。
// 返回加载的卡数。失败返回错误。
func (l *Loader) LoadAll(ctx context.Context) (int, error) {
	if l.db == nil {
		return 0, fmt.Errorf("knowledge loader: db not available")
	}

	var rows []models.TLsmGameKnowledgeCard
	if err := l.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("knowledge loader: query failed: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.cards = make(map[string]KnowledgeCard, len(rows))
	for _, row := range rows {
		card := toKnowledgeCard(row)
		l.cards[card.ID] = card
	}
	l.loaded = true
	return len(l.cards), nil
}

// GetCard 按卡号获取知识卡。
func (l *Loader) GetCard(cardID string) (KnowledgeCard, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	card, ok := l.cards[cardID]
	return card, ok
}

// Search 按条件搜索知识卡。
// features 为特征字符串过滤(AND 语义),domain/district 为精确匹配。
func (l *Loader) Search(query SearchQuery) SearchResult {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var matched []KnowledgeCard
	for _, card := range l.cards {
		if !matchCard(card, query) {
			continue
		}
		matched = append(matched, card)
	}

	// 分页
	total := len(matched)
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	end := offset + limit
	if end > total {
		end = total
	}

	return SearchResult{
		Cards:   matched[offset:end],
		Matched: total,
		Total:   len(l.cards),
	}
}

// GetStats 返回知识库统计。
func (l *Loader) GetStats() KBStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	stats := KBStats{
		Total:     len(l.cards),
		Domains:   make(map[string]int),
		Districts: make(map[string]int),
	}
	for _, card := range l.cards {
		if card.ExtractStatus == ExtractStatusDone {
			stats.Extracted++
		} else if card.ExtractStatus == ExtractStatusFailed {
			stats.Failed++
		}
		if card.Domain != "" {
			stats.Domains[card.Domain]++
		}
		if card.HomeDistrict != "" {
			stats.Districts[card.HomeDistrict]++
		}
	}
	return stats
}

// ExtractPending 触发对未抽取卡的特征抽取。
// root 为文档池根目录;pendingPaths 为卡号 → 相对路径映射。
// 调用 LLM 抽取,写回 MySQL。返回(成功数, 失败数)。
func (l *Loader) ExtractPending(ctx context.Context, root string, pendingPaths map[string]string) (int, int) {
	if l.extractor == nil || l.db == nil {
		return 0, 0
	}

	// 收集未抽取的卡
	var pending []ExtractRequest
	for cardID, sourcePath := range pendingPaths {
		content := readDocContent(root, sourcePath)
		if content == "" {
			continue
		}
		pending = append(pending, ExtractRequest{
			CardID:     cardID,
			SourcePath: sourcePath,
			Content:    content,
		})
	}

	if len(pending) == 0 {
		return 0, 0
	}

	// 批量抽取
	results := l.extractor.ExtractBatch(ctx, pending)

	// 写回 MySQL
	success := 0
	failed := 0
	for _, result := range results {
		if len(result.Features) > 0 {
			// 抽取成功
			featureJSON := featuresToJSON(result.Features)
			featureText := strings.Join(result.Features, " ")
			err := l.db.Model(&models.TLsmGameKnowledgeCard{}).
				Where("id = ?", result.CardID).
				Updates(map[string]interface{}{
					"features":       featureJSON,
					"feature_text":   featureText,
					"extract_status": ExtractStatusDone,
				}).Error
			if err != nil {
				failed++
				logExtractFailure(result.CardID, err)
			} else {
				success++
				// 更新缓存
				l.mu.Lock()
				if card, ok := l.cards[result.CardID]; ok {
					card.Features = result.Features
					card.ExtractStatus = ExtractStatusDone
					l.cards[result.CardID] = card
				}
				l.mu.Unlock()
			}
		} else {
			// 抽取失败
			failed++
			l.db.Model(&models.TLsmGameKnowledgeCard{}).
				Where("id = ?", result.CardID).
				Update("extract_status", ExtractStatusFailed)
			logExtractFailure(result.CardID, fmt.Errorf("%s", result.Error))
		}
	}

	return success, failed
}

// matchCard 检查知识卡是否匹配搜索条件。
func matchCard(card KnowledgeCard, query SearchQuery) bool {
	// 域过滤
	if query.Domain != "" && card.Domain != query.Domain {
		return false
	}
	// 城区过滤
	if query.District != "" && card.HomeDistrict != query.District {
		return false
	}
	// 特征过滤(AND 语义)
	for _, f := range query.Features {
		if !cardHasFeature(card, f) {
			return false
		}
	}
	return true
}

// cardHasFeature 检查知识卡是否包含指定特征字符串。
// 支持部分匹配(特征字符串是子串即可)。
func cardHasFeature(card KnowledgeCard, feature string) bool {
	feature = strings.TrimSpace(feature)
	if feature == "" {
		return true
	}
	for _, f := range card.Features {
		if strings.Contains(f, feature) {
			return true
		}
	}
	// 也检查 feature_text(如果 Features 为空但 FeatureText 有值)
	if len(card.Features) == 0 {
		// 从其他字段拼接检查
		allText := card.Title + " " + card.Domain + " " + card.HomeDistrict + " " +
			card.Employment + " " + card.RiskPreference
		if strings.Contains(allText, feature) {
			return true
		}
	}
	return false
}

// toKnowledgeCard 将 GORM 模型转为 KnowledgeCard。
func toKnowledgeCard(row models.TLsmGameKnowledgeCard) KnowledgeCard {
	card := KnowledgeCard{
		ID:              row.ID,
		SourcePath:      row.SourcePath,
		Domain:          row.Domain,
		Title:           row.Occupation,
		Salary:          int64(row.Salary),
		Expense:         int64(row.Expense),
		Savings:         int64(row.Savings),
		StartAge:        row.Age,
		HomeDistrict:    row.HomeDistrict,
		RiskPreference:  row.RiskPreference,
		HealthGrade:     row.HealthGrade,
		Gender:          row.Gender,
		Employment:      row.Employment,
		Marital:         row.Marital,
		ChildrenCount:   row.ChildrenCount,
		EldersDependent: row.EldersDependent,
		OpeningHook:     row.OpeningHook,
		ExtractStatus:   row.ExtractStatus,
	}

	// 解析特征字符串数组
	if row.Features != "" {
		var features []string
		if err := json.Unmarshal([]byte(row.Features), &features); err == nil {
			card.Features = features
		}
	}

	// 解析目标
	if row.Goals != "" {
		var goals []string
		if err := json.Unmarshal([]byte(row.Goals), &goals); err == nil {
			card.Goals = goals
		}
	}

	// 解析人格/行为(顿号连接 → 数组)
	if row.Personality != "" {
		card.Personality = strings.Split(row.Personality, "、")
	}
	if row.BehaviorTraits != "" {
		card.BehaviorTraits = strings.Split(row.BehaviorTraits, "、")
	}

	return card
}

// featuresToJSON 将特征字符串数组序列化为 JSON。
func featuresToJSON(features []string) string {
	data, err := json.Marshal(features)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// readDocContent 读取文档池中指定相对路径的 Markdown 内容。
// root 为文档池根目录。文件不存在或读取失败返回空串。
func readDocContent(root, relPath string) string {
	if root == "" || relPath == "" {
		return ""
	}
	full := filepath.Join(root, relPath)
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	return string(data)
}
