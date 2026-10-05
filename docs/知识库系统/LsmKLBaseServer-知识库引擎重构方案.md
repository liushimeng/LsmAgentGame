# LsmKLBaseServer — 知识库引擎重构方案

> **日期**：2026-10-05 · **状态**：设计中
> **目标**：将现有「基于 G-RAG + LangChain 的通用知识库系统」重构为
> 「LLM 大模型抽取特征字符串数组 + MySQL 数据库 MCP 服务 + Graphic + LangChain
> 的通用知识库系统」。

---

## 一、现状分析

### 1.1 现有架构

当前知识库系统即**虚拟城市职业卡知识库系统**，为虚拟城市游戏提供 10 万级人物卡：

```
lag_docs/虚拟城市/玩家职业设计/          ← 知识源(~100,267 张 Markdown 人物卡)
    │
    ▼
profession.Loader (loader.go)           ← 懒加载:walk 目录 → 索引路径 → 抽卡
    │                                    │
    │ frontmatter.go                     │ loader_batch.go
    │ (YAML 容错解析 → Card)             │ (DrawPaths + HydrateBatch 并行水合)
    ▼                                    ▼
Card 结构体 ──────────────────────▶ city.Backdrop.AnchorProfiles
                                         │
                                         ▼
                                   ResidentProfile (居民档案)
                                         │
                    ┌────────────────────┼────────────────────┐
                    ▼                    ▼                    ▼
              REST API            城市之声 LLM           前端档案抽屉
              /city/residents      (voice.go)            (VirtualCityLobbyPage)
```

### 1.2 现有组件清单

| 层 | 文件 | 职责 |
|---|---|---|
| 加载器 | `ServerGo/game/virtual_city/profession/loader.go` | 懒加载索引、抽卡、LRU 缓存、合成兜底 |
| 解析器 | `ServerGo/game/virtual_city/profession/frontmatter.go` | YAML 容错解析、城区映射、Card 映射 |
| 批量 | `ServerGo/game/virtual_city/profession/loader_batch.go` | DrawPaths + HydrateBatch 并行水合 |
| 卡结构 | `ServerGo/game/virtual_city/profession/card.go` | Card 结构体 + Validate + 词库 |
| 域 | `ServerGo/game/virtual_city/profession/domain.go` | DomainCard(L1 行业域随卡返回) |
| 合成 | `ServerGo/game/virtual_city/profession/synthetic.go` | 合成兜底卡 |
| 档案 | `ServerGo/game/virtual_city/city/profile.go` | 居民档案锚定、分页查询 |
| API | `ServerGo/api/virtual_city_api.go` | REST 居民档案端点 |
| 前端 | `ClientWeb/src/api/virtualCity.ts` | fetchCityResidents / fetchCityResident |
| 前端 | `ClientWeb/src/pages/VirtualCityLobbyPage.tsx` | 居民档案抽屉 UI |
| i18n | `ClientWeb/src/i18n/locales/zh-CN.ts` | virtualCity.residentCountHint |

### 1.3 现有架构的问题

1. **运行时解析开销大**：每次抽卡都要读文件 + 解析 YAML，10 万级文档池的 frontmatter 解析虽已优化（LRU + 批量水合），但仍是 IO 密集型
2. **特征提取靠规则**：frontmatter 字段映射靠硬编码规则（`mapCard`），新字段/新形状需要改代码
3. **检索能力弱**：只能按姓名/职业/卡号做包含匹配，无法按特征组合查询
4. **无知识图谱**：卡片之间没有关系表示，无法做「同行业的卡」「同城区的卡」等关联查询
5. **无 MCP 接口**：知识库无法被外部 LLM Agent 通过标准协议消费

---

## 二、新架构设计

### 2.1 总体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                     LsmKLBaseServer 知识库引擎                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐   │
│  │  ① 特征抽取层    │   │  ② MySQL MCP    │   │  ③ Graph +      │   │
│  │  LLM Feature    │   │  服务层          │   │  LangChain      │   │
│  │  Extraction     │   │  MCP Server     │   │  检索层          │   │
│  │                 │   │                 │   │                 │   │
│  │  Markdown → LLM│   │  MySQL 特征表   │   │  知识图谱 +     │   │
│  │  → 特征字符串数组│   │  → MCP Tools    │   │  LangChain 链   │   │
│  └────────┬────────┘   └────────┬────────┘   └────────┬────────┘   │
│           │                     │                     │             │
│           ▼                     ▼                     ▼             │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    存储层                                     │   │
│  │  t_lsm_game_knowledge_card (MySQL)  +  进程内图缓存          │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.2 三层职责

#### ① LLM 特征抽取层 (Feature Extraction)

**职责**：将 Markdown 人物卡通过 LLM 抽取为结构化特征字符串数组，存入 MySQL。

**核心流程**：
```
Markdown 文件 → 读取内容 → 构造 LLM 请求(抽取 prompt)
    → LLM 返回 JSON(特征字符串数组) → 校验 → 写入 MySQL
```

**特征字符串数组格式**：
```json
{
  "card_id": "N9012345",
  "features": [
    "职业:外卖骑手",
    "行业:交通运输",
    "收入:5000",
    "支出:3200",
    "储蓄:8000",
    "年龄:28",
    "性别:男",
    "人格:务实主义、果决有力",
    "行为:精打细算",
    "健康:A",
    "风险偏好:aggressive",
    "就业形态:全职",
    "城区:commerce",
    "婚姻:单身",
    "子女数:0",
    "赡养老人:0"
  ]
}
```

**关键设计**：
- 抽取 prompt 要求 LLM 返回**固定格式的 JSON**，特征字符串采用 `键:值` 格式
- 支持批量抽取（一次 LLM 调用处理多张卡，减少 API 调用次数）
- 抽取失败时回退到现有 frontmatter 解析（零回归）
- 抽取结果缓存到 MySQL，避免重复抽取

#### ② MySQL MCP 服务层 (MCP Server)

**职责**：将 MySQL 中的特征数据通过 MCP (Model Context Protocol) 标准协议暴露，供外部 LLM Agent 消费。

**MCP Tools 定义**：

| Tool | 说明 | 参数 |
|------|------|------|
| `search_cards` | 按特征字符串搜索卡片 | `features: string[]`, `limit: int` |
| `get_card` | 按卡号获取单卡 | `card_id: string` |
| `list_by_domain` | 按行业域列出卡片 | `domain: string`, `offset: int`, `limit: int` |
| `list_by_district` | 按城区列出卡片 | `district: string`, `offset: int`, `limit: int` |
| `get_stats` | 获取知识库统计 | 无 |
| `extract_features` | 触发特征抽取(管理) | `card_ids: string[]` |

**HTTP MCP 端点**：
- `POST /api/knowledge/mcp` — MCP 协议端点（JSON-RPC 2.0）
- `GET /api/knowledge/cards` — REST 搜索
- `GET /api/knowledge/cards/:id` — REST 单卡
- `GET /api/knowledge/stats` — REST 统计

#### ③ Graph + LangChain 检索层

**职责**：基于知识图谱 + LangChain 风格的检索链，提供智能卡片检索。

**知识图谱结构**：
- **节点**：每张人物卡是一个节点，特征字符串作为节点属性
- **边**：卡片之间的关系
  - `same_industry` — 同行业
  - `same_district` — 同城区
  - `same_personality` — 共同人格标签
  - `same_age_group` — 同年龄段
  - `same_risk` — 同风险偏好

**LangChain 检索链**：
```
用户查询 → QueryParser(解析查询意图)
         → GraphRetriever(图遍历:从匹配节点出发,BFS 扩展关联卡片)
         → FeatureRanker(按特征匹配度排序)
         → ResultFormatter(格式化输出)
```

**关键设计**：
- 图缓存在进程内（10 万级节点 + 边，内存约 200MB），启动时从 MySQL 加载
- 图遍历深度默认 2（直接关联 + 二度关联）
- 支持特征组合查询：`行业:交通运输 + 城区:commerce + 风险:aggressive`

---

## 三、数据库设计

### 3.1 新表：`t_lsm_game_knowledge_card`

```sql
CREATE TABLE t_lsm_game_knowledge_card (
  id              VARCHAR(64)     NOT NULL PRIMARY KEY COMMENT '卡号(N9012345)',
  source_path     VARCHAR(512)    NOT NULL COMMENT '来源 Markdown 相对路径',
  domain          VARCHAR(128)    DEFAULT NULL COMMENT 'L1 行业域(A-农林牧渔)',
  occupation      VARCHAR(256)    DEFAULT NULL COMMENT '职业名',
  salary          INT             DEFAULT 0 COMMENT '月薪(元)',
  expense         INT             DEFAULT 0 COMMENT '月支出(元)',
  savings         INT             DEFAULT 0 COMMENT '初始储蓄(元)',
  age             INT             DEFAULT 25 COMMENT '年龄',
  gender          VARCHAR(4)      DEFAULT 'u' COMMENT '性别(m/f/u)',
  health_grade    VARCHAR(4)      DEFAULT 'B' COMMENT '健康档(A/B/C)',
  risk_preference VARCHAR(32)     DEFAULT 'balanced' COMMENT '风险偏好',
  employment      VARCHAR(128)    DEFAULT NULL COMMENT '就业形态',
  home_district   VARCHAR(64)     DEFAULT NULL COMMENT '城区 id',
  personality     VARCHAR(512)    DEFAULT NULL COMMENT '人格标签(、连接)',
  behavior_traits VARCHAR(512)    DEFAULT NULL COMMENT '行为特征(、连接)',
  marital         VARCHAR(32)     DEFAULT 'single' COMMENT '婚姻状态',
  children_count  INT             DEFAULT 0 COMMENT '子女数',
  elders_dependent INT           DEFAULT 0 COMMENT '赡养老人数',
  opening_hook    TEXT            COMMENT '开场白',
  goals           TEXT            COMMENT '目标(JSON 数组)',
  features        JSON            NOT NULL COMMENT 'LLM 抽取的特征字符串数组',
  feature_text    TEXT            COMMENT '特征全文(全文索引用)',
  extract_status  TINYINT         DEFAULT 0 COMMENT '抽取状态(0=未抽取,1=已抽取,2=抽取失败)',
  created_at      DATETIME        DEFAULT CURRENT_TIMESTAMP,
  updated_at      DATETIME        DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_domain (domain),
  INDEX idx_district (home_district),
  INDEX idx_occupation (occupation),
  INDEX idx_extract_status (extract_status),
  FULLTEXT INDEX idx_feature_text (feature_text)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.2 现有表复用

- `t_lsm_game_agent_player_profile` — Agent 玩家行为画像（不变）
- `t_lsm_game_agent_memory` — Agent 持久化记忆（不变）

---

## 四、文件变更清单

### 4.1 新增文件

| 文件 | 职责 |
|------|------|
| `ServerGo/knowledge/types.go` | 知识库特征类型定义 |
| `ServerGo/knowledge/feature_extract.go` | LLM 特征抽取管线 |
| `ServerGo/knowledge/mcp_server.go` | MySQL MCP 服务 |
| `ServerGo/knowledge/graph.go` | 知识图谱 + 图遍历 |
| `ServerGo/knowledge/langchain.go` | LangChain 风格检索链 |
| `ServerGo/knowledge/loader.go` | 知识库加载器(从 MySQL 加载) |
| `ServerGo/models/t_lsm_game_knowledge_card.go` | GORM 模型 |
| `ServerGo/api/knowledge_api.go` | 知识库 HTTP API |
| `ClientWeb/src/api/knowledge.ts` | 前端知识库 API |

### 4.2 修改文件

| 文件 | 变更 |
|------|------|
| `ServerGo/db/db.go` | AutoMigrate 追加 `TLsmGameKnowledgeCard` |
| `ServerGo/router/router.go` | 注册知识库路由 |
| `ServerGo/main.go` | 装配知识库引擎 |
| `ServerGo/agent/class_names.go` | 注册 `LsmAgentGame-Knowledge-Extractor` |
| `ClientWeb/src/api/virtualCity.ts` | 更新知识库相关文案 |
| `ClientWeb/src/pages/VirtualCityLobbyPage.tsx` | 更新 UI 文案 |
| `ClientWeb/src/i18n/locales/zh-CN.ts` | 更新 i18n 文案 |
| `ClientWeb/src/i18n/locales/en-US.ts` | 更新 i18n 文案 |
| `ClientWeb/src/i18n/locales/ja-JP.ts` | 更新 i18n 文案 |

### 4.3 文档更新

| 文件 | 变更 |
|------|------|
| `lag_docs/虚拟城市/已实现/03-Agent设计/虚拟城市-职业卡与加载器设计-v1.md` | 追加重构说明 |
| `lag_docs/知识库系统/LsmKLBaseServer-知识库引擎重构方案.md` | 本设计文档 |

---

## 五、核心流程

### 5.1 特征抽取流程

```
启动 / 管理触发
    │
    ▼
扫描文档池索引(复用 profession.Loader.buildIndex)
    │
    ▼
分批(每批 10 张)读取 Markdown 内容
    │
    ▼
构造 LLM 请求(抽取 prompt + 卡内容)
    │
    ▼
LLM 返回 JSON(特征字符串数组)
    │
    ▼
校验 + 写入 MySQL (t_lsm_game_knowledge_card)
    │
    ▼
更新 extract_status = 1
```

### 5.2 MCP 查询流程

```
外部 Agent / 前端
    │
    ▼
POST /api/knowledge/mcp  (JSON-RPC 2.0)
    │
    ▼
MCP Server 解析 tool 调用
    │
    ▼
执行 MySQL 查询 / 图遍历
    │
    ▼
返回结果
```

### 5.3 LangChain 检索流程

```
用户查询(自然语言 / 特征字符串)
    │
    ▼
QueryParser → 解析为特征过滤条件
    │
    ▼
GraphRetriever → 从 MySQL 加载图 → BFS 遍历
    │
    ▼
FeatureRanker → 按特征匹配度排序
    │
    ▼
ResultFormatter → 格式化输出
```

---

## 六、接口契约

### 6.1 REST API

#### GET /api/knowledge/cards
```
query: features(可选,逗号分隔的特征字符串)、domain、district、offset、limit
data: { cards: KnowledgeCard[], matched: int, total: int }
```

#### GET /api/knowledge/cards/:id
```
data: { card: KnowledgeCard }
```

#### GET /api/knowledge/stats
```
data: { total, extracted, failed, domains, districts }
```

#### POST /api/knowledge/extract
```
body: { card_ids: string[] }  (空 = 全量抽取)
data: { extracted: int, failed: int }
```

### 6.2 MCP Protocol

#### POST /api/knowledge/mcp
```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "search_cards",
    "arguments": {
      "features": ["行业:交通运输", "城区:commerce"],
      "limit": 10
    }
  }
}
```

---

## 七、实施计划

### 阶段 1：数据库 + 类型定义
1. 创建 `t_lsm_game_knowledge_card` GORM 模型
2. 在 `db.go` AutoMigrate 注册
3. 创建 `knowledge/types.go` 类型定义

### 阶段 2：特征抽取层
1. 实现 `feature_extract.go` — LLM 抽取管线
2. 注册 `LsmAgentGame-Knowledge-Extractor` AgentClassName
3. 实现批量抽取 + 失败回退

### 阶段 3：MySQL MCP 服务层
1. 实现 `mcp_server.go` — MCP Tools
2. 实现 `api/knowledge_api.go` — REST + MCP 端点
3. 注册路由

### 阶段 4：Graph + LangChain 检索层
1. 实现 `graph.go` — 知识图谱
2. 实现 `langchain.go` — 检索链
3. 集成到 MCP 服务

### 阶段 5：前端 + 文档
1. 更新前端 API + UI + i18n
2. 更新 lag_docs 文档
3. 全量测试 + 编译验证

---

## 八、风险与缓解

| 风险 | 缓解 |
|------|------|
| LLM 抽取 10 万张卡耗时长 | 分批并行抽取 + 断点续抽 + 失败回退 frontmatter |
| 图缓存内存占用大(10 万节点) | 懒加载 + LRU 淘汰 + 可选持久化 |
| MCP 协议兼容性 | 严格遵循 JSON-RPC 2.0 + MCP 规范 |
| 现有功能回归 | 特征抽取失败时回退到现有 frontmatter 解析 |
| MySQL 全文索引性能 | feature_text 字段 + 特征字符串精确匹配优先 |
