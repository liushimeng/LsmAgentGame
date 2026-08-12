# Protocol Buffers 官方技术栈指南

> 资料来源：[Protocol Buffers 官方文档](https://protobuf.dev/)
> 整理日期：2026-08-11
> 适用版本：**proto3**（本项目后端使用 Go + protobuf，见 `proto/` 目录）

---

## 目录

1. [概述与核心概念](#1-概述与核心概念)
2. [Proto3 语言指南](#2-proto3-语言指南)
3. [编码规范（Wire Format）](#3-编码规范wire-format)
4. [风格指南](#4-风格指南)
5. [Go 代码生成指南](#5-go-代码生成指南)
6. [最佳实践与常见陷阱](#6-最佳实践与常见陷阱)
7. [附录：本项目 proto 使用情况](#7-附录本项目-proto-使用情况)

---

## 1. 概述与核心概念

### 1.1 什么是 Protocol Buffers

**Protocol Buffers**（简称 **protobuf**）是 Google 开发的一种**语言中立、平台中立、可扩展**的结构化数据序列化机制。可以将它类比为 XML，但**更小、更快、更简单**。

核心工作流：

1. 定义一次数据结构（`.proto` 文件）
2. 使用 `protoc` 编译器生成多种语言的源代码
3. 在代码中读写结构化数据到各种数据流

### 1.2 为什么使用 Protocol Buffers

| 特性 | 说明 |
|------|------|
| **体积小** | 二进制编码，比 JSON/XML 小 3~10 倍 |
| **速度快** | 序列化/反序列化比 JSON 快 20~100 倍 |
| **跨语言** | 支持 C++、C#、Dart、**Go**、Java、Kotlin、Objective-C、Python、Rust、Ruby、PHP 等 |
| **向后兼容** | 字段可增可删，不破坏旧版本解析（只要遵守编号规则） |
| **强类型** | 编译期类型检查，减少运行时错误 |
| **代码生成** | 自动生成读写类，无需手动编解码 |

### 1.3 适用场景

- 微服务间 RPC 通信（配合 gRPC）
- 实时游戏数据传输（如本项目狼人杀 WS 游戏流量）
- 数据持久化（日志、配置、缓存）
- 跨语言系统集成

### 1.4 与 JSON / XML 对比

| 维度 | Protobuf | JSON | XML |
|------|----------|------|-----|
| 格式 | 二进制 | 文本 | 文本 |
| 可读性 | 差（需解码） | 好 | 好 |
| 体积 | 最小 | 中等 | 最大 |
| 序列化速度 | 最快 | 中等 | 最慢 |
| Schema 约束 | 有（.proto） | 无（可加 JSON Schema） | 有（XSD） |
| 跨语言 | 原生支持 | 原生支持 | 原生支持 |
| 字段演进 | 优秀（编号机制） | 一般（靠约定） | 一般 |

---

## 2. Proto3 语言指南

### 2.1 基本语法

```proto
syntax = "proto3";

// 包声明
package search;

// 消息定义
message SearchRequest {
  string query = 1;
  int32 page_number = 2;
  int32 results_per_page = 3;
}
```

**关键规则**：

- 第一行必须声明 `syntax = "proto3";`（省略则默认 proto2）
- 字段编号范围：`1` ~ `536,870,911`
- **19000 ~ 19999** 为 Protobuf 内部保留编号，不可使用
- 字段编号一旦使用**不可更改**（等同于删除旧字段 + 新增同类型新字段）

### 2.2 字段编号最佳实践

- **1 ~ 15** 号字段用于高频字段（编码仅占 1 字节）
- **16 ~ 2047** 号字段占 2 字节
- 字段编号**绝对不可复用**（删除字段后必须 `reserved`）

### 2.3 字段基数（Cardinality）

proto3 中字段有以下几种基数：

| 基数 | 关键字 | 说明 |
|------|--------|------|
| **Singular (implicit)** | （无标签） | 标量字段：设置为非默认值时才序列化；无法区分"未设置"和"设为零值" |
| **Singular (optional)** | `optional` | 显式存在性：有 set/unset 两种状态，可检测是否设置 |
| **Repeated** | `repeated` | 可重复零次或多次，顺序保留；数值类型默认 packed 编码 |
| **Map** | `map<K,V>` | 键值对映射 |

> **推荐使用 `optional`**：最大化与 proto2 / editions 的兼容性，且可检测字段存在性。

### 2.4 标量类型一览

#### 2.4.1 类型说明

| Proto 类型 | 说明 |
|------------|------|
| `double` | IEEE 754 双精度浮点数 |
| `float` | IEEE 754 单精度浮点数 |
| `int32` | 变长编码，负数效率低（负数用 `sint32`） |
| `int64` | 变长编码，负数效率低（负数用 `sint64`） |
| `uint32` | 变长无符号 32 位整数 |
| `uint64` | 变长无符号 64 位整数 |
| `sint32` | 变长有符号整数，负数编码更高效（ZigZag） |
| `sint64` | 变长有符号整数，负数编码更高效（ZigZag） |
| `fixed32` | 固定 4 字节；值 > 2^28 时比 `uint32` 高效 |
| `fixed64` | 固定 8 字节；值 > 2^56 时比 `uint64` 高效 |
| `sfixed32` | 固定 4 字节有符号整数 |
| `sfixed64` | 固定 8 字节有符号整数 |
| `bool` | 布尔值 |
| `string` | UTF-8 或 7-bit ASCII，最大 2^32 字节 |
| `bytes` | 任意字节序列，最大 2^32 字节 |

#### 2.4.2 跨语言类型映射表

| Proto 类型 | Go 类型 | JS/TS 类型 | C++ 类型 | Java 类型 | Python 类型 |
|------------|---------|------------|----------|-----------|-------------|
| `double` | `float64` | `number` | `double` | `double` | `float` |
| `float` | `float32` | `number` | `float` | `float` | `float` |
| `int32` | `int32` | `number` | `int32_t` | `int` | `int` |
| `int64` | `int64` | `string`(number 可能丢精度) | `int64_t` | `long` | `int/long` |
| `uint32` | `uint32` | `number` | `uint32_t` | `int` | `int/long` |
| `uint64` | `uint64` | `string` | `uint64_t` | `long` | `int/long` |
| `sint32` | `int32` | `number` | `int32_t` | `int` | `int` |
| `sint64` | `int64` | `string` | `int64_t` | `long` | `int/long` |
| `fixed32` | `uint32` | `number` | `uint32_t` | `int` | `int/long` |
| `fixed64` | `uint64` | `string` | `uint64_t` | `long` | `int/long` |
| `sfixed32` | `int32` | `number` | `int32_t` | `int` | `int` |
| `sfixed64` | `int64` | `string` | `int64_t` | `long` | `int/long` |
| `bool` | `bool` | `boolean` | `bool` | `boolean` | `bool` |
| `string` | `string` | `string` | `std::string` | `String` | `str` |
| `bytes` | `[]byte` | `Uint8Array`/`string` | `std::string` | `ByteString` | `bytes` |

> **前端注意**：`int64`/`uint64`/`sfixed64`/`fixed64` 在 JS 中若用 `number` 可能丢失精度（JS number 为 64 位浮点数，精确整数仅到 2^53）。推荐用字符串处理。

### 2.5 默认值

解析消息时，若编码中不含某字段，则返回该类型的默认值：

| 类型 | 默认值 |
|------|--------|
| `string` | 空字符串 `""` |
| `bytes` | 空字节 `[]` |
| `bool` | `false` |
| 数值类型 | `0` |
| `enum` | 第一个枚举值（必须为 0） |
| 消息类型 | `nil` / `null`（语言相关） |
| `repeated` | 空列表 / 空切片 |
| `map` | 空 map |

> **重要**：implicit-presence 标量字段**无法区分**"未设置"和"设为默认值"。例如 `bool` 设为 `false` 与未设置在 wire 上完全相同。

### 2.6 枚举（Enum）

```proto
enum Corpus {
  CORPUS_UNSPECIFIED = 0;  // 零值，第一个值必须为 0
  CORPUS_UNIVERSAL = 1;
  CORPUS_WEB = 2;
  CORPUS_IMAGES = 3;
}

message SearchRequest {
  Corpus corpus = 4;
}
```

**枚举规则**：

- 第一个值**必须**为 0，命名应为 `ENUM_NAME_UNSPECIFIED` 或 `ENUM_NAME_UNKNOWN`
- 枚举值使用 `UPPER_SNAKE_CASE`，且必须加枚举名前缀（防止全局命名冲突）
- 前缀剥离后仍须是合法枚举名（如 `DEVICE_TIER_TIER1` 而非 `DEVICE_TIER_1`）
- 可定义别名（需设置 `option allow_alias = true;`），但反序列化时只取第一个值
- 枚举值范围为 32 位整数，负数不推荐（varint 效率低）
- 未知枚举值：Go/C++ 保留为底层整数；Java 用特殊 case 表示

**保留枚举值**：

```proto
enum Foo {
  reserved 2, 15, 9 to 11, 40 to max;
  reserved "FOO", "BAR";
}
```

### 2.7 嵌套类型

```proto
message SearchResponse {
  message Result {
    string url = 1;
    string title = 2;
    repeated string snippets = 3;
  }
  repeated Result results = 1;
}

// 外部引用使用 Parent.Type
message OtherMessage {
  SearchResponse.Result result = 1;
}
```

### 2.8 Oneof

当多个字段中**至多只有一个**会被设置时，使用 `oneof` 可节省内存并强制互斥语义。

```proto
message SampleMessage {
  oneof test_oneof {
    string name = 4;
    SubMessage sub_message = 9;
  }
}
```

**Oneof 特性**：

- 设置 oneof 中任一字段会自动清除其他字段
- 最后设置的值生效（Last One Wins）
- 字段编号在消息内必须唯一
- **不能**包含 `repeated` 和 `map` 字段（可用嵌套消息包装）
- `oneof` 不可加 `repeated` 修饰

### 2.9 Maps

```proto
message Project {
  map<string, string> attributes = 1;
  map<int32, User> users = 2;
}
```

**Map 规则**：

- 键类型：除 `float`/`double`/`bytes` 外的所有标量类型、枚举
- 值类型：任意类型（包括消息）
- Map 字段不可为 `repeated`
- 键值对**顺序不保证**（不要依赖遍历顺序）
- 二进制兼容于 `repeated` 消息字段（每条 entry 含 `key` 和 `value`）

### 2.10 包（Package）与导入

```proto
package foo.bar;

import "myproject/other_protos.proto";      // 直接导入
import public "myproject/new_protos.proto";  // 公共导入（可传递）
```

- `import` 仅直接导入的文件中定义可用
- `import public` 可传递依赖（用于文件迁移时的占位转发）
- 导入路径通过 `protoc --proto_path=...` 指定

### 2.11 服务（Service）定义

```proto
service SearchService {
  rpc Search(SearchRequest) returns (SearchResponse);
}
```

- protobuf 本身仅定义接口，RPC 实现由 gRPC 等框架提供
- 最常用的是 **gRPC**：可通过 `protoc-gen-go-grpc` 插件生成 Go gRPC 代码

### 2.12 常用选项（Options）

| 选项 | 级别 | 说明 |
|------|------|------|
| `java_package` | 文件 | Java 包名 |
| `java_outer_classname` | 文件 | 外部包装类名 |
| `java_multiple_files` | 文件 | 是否拆分为多个 Java 文件 |
| `optimize_for` | 文件 | `SPEED`/`CODE_SIZE`/`LITE_RUNTIME` |
| `go_package` | 文件 | **Go 导入路径**（本项目必设） |
| `deprecated` | 字段 | 标记为已弃用 |
| `allow_alias` | 枚举 | 允许枚举值别名 |

### 2.13 保留字段（Reserved）

删除字段后**必须**保留编号和名称，防止未来复用：

```proto
message Foo {
  reserved 2, 15, 9 to 11;      // 保留编号
  reserved "foo", "bar";         // 保留字段名
}
```

- 字段编号和名称不能混在同一条 `reserved` 语句中
- 保留名称主要影响 JSON/TextFormat 编码

### 2.14 Any 类型

`Any` 允许嵌入任意消息类型而无需其 `.proto` 定义：

```proto
import "google/protobuf/any.proto";

message ErrorStatus {
  string message = 1;
  repeated google.protobuf.Any details = 2;
}
```

- 默认 type URL 格式：`type.googleapis.com/packagename.messagename`
- 各语言提供 `Pack()`/`Unpack()` 辅助方法
- **注意**：官方建议优先使用 extensions（extensions），`Any` 有设计缺陷

### 2.15 消息演进规则

#### Wire-Safe（安全变更）

- 新增字段 ✅
- 删除字段 ✅（编号必须 reserved）
- 新增枚举值 ✅
- 单字段移入新 oneof ✅
- oneof 只有一个字段时改为普通字段 ✅

#### Wire-unsafe（不安全变更）

- 更改字段编号 ❌
- 更改字段类型 ❌（部分数值类型兼容，但有风险）
- 将字段移入已有 oneof ❌

#### Wire-compatible（条件兼容）

- `int32` ↔ `uint32` ↔ `int64` ↔ `uint64` ↔ `bool`（值超范围会截断）
- `sint32` ↔ `sint64`
- `string` ↔ `bytes`（bytes 需为有效 UTF-8）
- `enum` ↔ 整型（数值类型）
- `singular` ↔ `repeated`（消息类型取最后一个/合并；数值 packed 格式不兼容）

---

## 3. 编码规范（Wire Format）

### 3.1 Varint 编码

**Varint**（变长整数）是 protobuf 线格式的核心。小数字用更少字节编码。

- 每个字节的**最高位（MSB）**是 continuation bit：`1` 表示后续还有字节，`0` 表示结束
- 低 7 位是有效 payload，以**小端序**排列
- 可编码 64 位无符号整数，占 1~10 字节

**示例：150 的编码**

```
150 = 0b10010110
Varint 编码: 10010110 00000001  (即 0x96 0x01)
           ^       ^
         继续     结束
```

解码过程：去掉每个字节的 MSB，反转顺序拼接 → `0000001 0010110` = `128 + 16 + 4 + 2 = 150`

### 3.2 Wire Types（线类型）

protobuf 消息是一系列 **tag-length-value (TLV)** 记录。每个字段的 tag 由**字段编号**和**线类型**组成。

| 线类型 ID | 名称 | 适用类型 |
|-----------|------|----------|
| `0` | `VARINT` | `int32`, `int64`, `uint32`, `uint64`, `sint32`, `sint64`, `bool`, `enum` |
| `1` | `I64` | `fixed64`, `sfixed64`, `double` |
| `2` | `LEN` | `string`, `bytes`, 嵌套消息, packed repeated |
| `3` | `SGROUP` | group 起始（已废弃） |
| `4` | `EGROUP` | group 结束（已废弃） |
| `5` | `I32` | `fixed32`, `sfixed32`, `float` |

**Tag 编码公式**：

```
tag = (field_number << 3) | wire_type
```

例如字段号 1、wire type 0（VARINT）的 tag 是 `(1 << 3) | 0 = 8` → 编码为 `0x08`。

### 3.3 消息结构示例

```proto
message Test1 {
  int32 a = 1;  // = 150
}
```

编码结果：`08 96 01`

- `08` = tag (field=1, wire=VARINT)
- `96 01` = varint 编码的 150

### 3.4 有符号整数：ZigZag 编码

普通 `int32`/`int64` 编码负数时，因高位全 1，总是占满 10 字节。

`sint32`/`sint64` 使用 **ZigZag 编码**，正负交替映射到无符号数：

| 原值 | 编码后 |
|------|--------|
| 0 | 0 |
| -1 | 1 |
| 1 | 2 |
| -2 | 3 |
| 2 | 4 |
| ... | ... |
| 0x7fffffff | 0xfffffffe |
| -0x80000000 | 0xffffffff |

**编码公式**：
- `sint32`: `(n << 1) ^ (n >> 31)`
- `sint64`: `(n << 1) ^ (n >> 63)`

### 3.5 定长数值类型

- `double` / `fixed64` / `sfixed64` → wire type `I64`（8 字节）
- `float` / `fixed32` / `sfixed32` → wire type `I32`（4 字节）
- 小端序存储

### 3.6 长度分隔类型（LEN）

`string`、`bytes`、嵌套消息、packed repeated 都使用 wire type `LEN`：

```
[tag:varint] [length:varint] [payload bytes]
```

**示例**：`string b = "testing"`（字段号 2）

```
12 07 74 65 73 74 69 6e 67
|  |   └── "testing" (7 bytes)
|  └── length = 7
└── tag = (2 << 3) | 2 = 18 (0x12)
```

### 3.7 Packed Repeated Fields

proto3 中，标量数值类型的 `repeated` 字段默认使用 **packed** 编码：

```proto
message Test4 {
  repeated int32 e = 5;  // values: [1, 2, 3]
}
```

编码：`2a 03 01 02 03`

- `2a` = tag (field=5, wire=LEN)
- `03` = 长度 3 字节
- `01 02 03` = 三个 varint 拼接

> 非 packable 类型（string/bytes/message）的 repeated 字段仍为每条独立记录。

### 3.8 字段顺序

- 编码时字段顺序不保证（不依赖字段定义顺序）
- 解码时必须能处理任意顺序的字段
- **未知字段**：proto3 保留未知字段并在序列化时原样输出
- 序列化到 JSON 或逐字段拷贝消息会丢失未知字段

### 3.9 Last One Wins

singular 字段在 wire 中出现多次时，**最后一个**值生效（消息类型则合并）。

---

## 4. 风格指南

### 4.1 文件格式

- **行宽**：80 字符
- **缩进**：2 空格
- **字符串**：优先使用双引号

### 4.2 文件命名

- 使用 `lower_snake_case.proto`
- 一个文件尽量只包含少量相关消息定义（避免依赖膨胀）

### 4.3 文件结构顺序

1. License 头（如有）
2. 文件概述
3. `syntax` 或 `edition` 声明
4. `package` 声明
5. `import`（按字母排序）
6. 文件级选项
7. 其他所有内容

### 4.4 命名规范一览

| 元素 | 命名风格 | 示例 |
|------|----------|------|
| 文件 | `lower_snake_case.proto` | `song_request.proto` |
| 包 | 点分隔 `lower_snake_case` | `my_project.audio.v1` |
| 消息 | `TitleCase` (PascalCase) | `SongRequest` |
| 字段 | `lower_snake_case` | `song_name` |
| repeated 字段 | 复数 `lower_snake_case` | `songs` |
| oneof 名 | `lower_snake_case` | `song_id` |
| 枚举类型 | `TitleCase` | `SongGenre` |
| 枚举值 | `UPPER_SNAKE_CASE`，加类型前缀 | `SONG_GENRE_UNSPECIFIED` |
| 服务名 | `TitleCase` | `SongService` |
| RPC 方法名 | `TitleCase` | `GetSong` |

### 4.5 包命名规则

- 不使用 Java 风格的 `com.company.x.y` 作为 proto package
- 用 `x.y` 作 proto 包名，Java 包名通过 `option java_package = "..."` 单独设置
- 包名应简短且基于项目名，不要与目录深度耦合

### 4.6 枚举前缀规则

枚举值必须加枚举名前缀（因为 proto 中枚举值在同级命名空间内全局可见）：

```proto
// 正确
enum CollectionType {
  COLLECTION_TYPE_UNSPECIFIED = 0;
  COLLECTION_TYPE_SET = 1;
  COLLECTION_TYPE_MAP = 2;
}

// 错误：SET 在两个枚举中冲突
enum CollectionType { SET = 1; }
enum TennisVictoryType { SET = 2; }
```

### 4.7 缩写处理

缩写当作单个单词处理：

- `GetDnsRequest` ✅（不是 `GetDNSRequest`）
- `dns_request` ✅（不是 `d_n_s_request`）

### 4.8 应避免的名称模式

不要使用以下前缀/后缀，避免与生成代码的 getters/setters 冲突：

- 前缀：`has_`、`get_`、`set_`、`clear_`
- 后缀：`_value`
- 名称：`descriptor`
- 各语言关键字

### 4.9 避免项

- ❌ 避免使用 `required`（proto3 已移除，proto2 也强烈不推荐）
- ❌ 避免 Groups（已废弃，用嵌套消息替代）
- ❌ 避免相对包引用（用完整包路径 `a.b.C` 而非 `b.C`）
- ❌ 避免下划线开头/结尾的标识符
- ❌ 避免数字紧跟下划线（如 `DNS_2`，用 `DNS_V2` 或 `XYZ2`）

---

## 5. Go 代码生成指南

### 5.1 安装编译器插件

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
```

### 5.2 编译器调用

```bash
# 基本用法
protoc --proto_path=src \
       --go_out=out \
       --go_opt=paths=source_relative \
       foo.proto bar/baz.proto
```

**输出模式（paths 选项）**：

| 模式 | 说明 |
|------|------|
| `paths=import`（默认） | 按 `go_package` 的导入路径输出文件 |
| `paths=source_relative` | 输出到与 .proto 相同的相对目录 |
| `module=$PREFIX` | 按 go_package 输出但去掉指定前缀（适合 Go module 内生成） |

### 5.3 Go 包声明

**推荐在 .proto 文件中声明**：

```proto
option go_package = "example.com/project/protos/fizz";
```

也可通过命令行参数指定：

```bash
protoc --go_opt=Mprotos/buzz.proto=example.com/project/protos/fizz buzz.proto
```

> `go_package` 的值与 proto `package` 声明**无关联**：前者决定 Go 命名空间，后者决定 protobuf 命名空间。

### 5.4 命名转换规则

proto `lower_snake_case` 字段名转换为 Go `PascalCase` 导出字段：

- 首字母大写（导出用）
- 内部下划线后跟小写字母 → 去下划线 + 大写字母
- 首字符为下划线 → 去下划线 + 加 `X` 前缀

| proto 字段名 | Go 字段名 |
|-------------|-----------|
| `song_name` | `SongName` |
| `birth_year` | `BirthYear` |
| `_id` | `XId` |

### 5.5 消息（Message）

每个 `message` 生成一个 Go `struct`，实现 `proto.Message` 接口：

```go
type Artist struct {
    // ... 字段
}

// proto.Message 接口的核心方法
func (m *Artist) ProtoReflect() protoreflect.Message
```

**并发规则**：

- 并发读取不同字段是安全的
- 并发修改不同字段是安全的
- 并发修改同一字段不安全
- `proto.Marshal` / `proto.Size` 等函数与修改并发不安全

### 5.6 字段类型映射

#### 5.6.1 Singular 标量字段

**Implicit presence**（proto3 默认，无 `optional`）：

```proto
int32 birth_year = 1;
```

```go
type Artist struct {
    BirthYear int32
}

// Getter 返回值或零值
func (m *Artist) GetBirthYear() int32
```

**Explicit presence**（`optional` 修饰）：

```proto
optional int32 first_active_year = 1;
```

```go
type Artist struct {
    FirstActiveYear *int32  // 指针，nil 表示未设置
}

func (m *Artist) GetFirstActiveYear() int32  // nil 时返回零值
```

#### 5.6.2 Singular 消息字段

```proto
message Concert {
    Band headliner = 1;
}
```

```go
type Concert struct {
    Headliner *Band  // 总是指针，nil 表示未设置
}

func (m *Concert) GetHeadliner() *Band
```

> proto3 中消息类型字段天然有存在性，加 `optional` 不改变生成代码。
> Getter 方法支持链式调用，`nil` receiver 安全返回零值。

#### 5.6.3 Repeated 字段

```proto
repeated Band support_acts = 1;
repeated bytes promo_images = 2;
repeated MusicGenre genres = 3;
```

```go
type Concert struct {
    SupportActs []*Band   // 消息类型: []*T
    PromoImages [][]byte  // bytes: [][]byte
    Genres      []MusicGenre  // 枚举: []T
}
```

#### 5.6.4 Map 字段

```proto
map<string, MerchItem> items = 1;
```

```go
type MerchBooth struct {
    Items map[string]*MerchItem
}
```

#### 5.6.5 Oneof 字段

```proto
message Profile {
  oneof avatar {
    string image_url = 1;
    bytes image_data = 2;
  }
}
```

生成代码：

```go
type Profile struct {
    Avatar isProfile_Avatar  // 接口类型
}

type Profile_ImageUrl struct {
    ImageUrl string
}
type Profile_ImageData struct {
    ImageData []byte
}

// 两者都实现 isProfile_Avatar 接口
```

**使用方式**：

```go
// 设置
p := &Profile{
    Avatar: &Profile_ImageUrl{ImageUrl: "http://..."},
}

// 读取（type switch）
switch x := m.Avatar.(type) {
case *Profile_ImageUrl:
    // 使用 x.ImageUrl
case *Profile_ImageData:
    // 使用 x.ImageData
case nil:
    // 未设置
}
```

每个 oneof 成员也生成 Getter 方法（返回零值若未设置）。

### 5.7 枚举（Enum）

```proto
enum Kind {
  KIND_UNSPECIFIED = 0;
  KIND_CONCERT_HALL = 1;
}
```

生成 Go 枚举类型：

```go
type Kind int32

const (
    Kind_KIND_UNSPECIFIED Kind = 0
    Kind_KIND_CONCERT_HALL Kind = 1
)

// 字符串表示
func (k Kind) String() string

// 枚举值映射
var Kind_name = map[int32]string{...}
var Kind_value = map[string]int32{...}
```

### 5.8 嵌套类型

```proto
message Artist {
  message Name { }
}
```

生成：

```go
type Artist struct { ... }
type Artist_Name struct { ... }  // Parent_Child 命名
```

### 5.9 服务（Service）

`protoc-gen-go` **本身不生成 gRPC 服务代码**，需使用 gRPC 插件：

```bash
# 安装 gRPC 插件
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 生成 gRPC 代码
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       foo.proto
```

生成内容：
- 服务端接口（Server API）
- 客户端存根（Client API）
- 注册函数

> 本项目未使用 gRPC，而是通过 WebSocket 传输 protobuf 二进制消息。

### 5.10 API Level

| .proto 语法 | 默认 API Level |
|-------------|---------------|
| proto2 | Open Struct API |
| proto3 | Open Struct API |
| edition 2023 | Open Struct API |
| edition 2024+ | Opaque API |

**Open Struct API**：生成的 struct 字段直接可见，可直接读写。
**Opaque API**：新 API，字段不直接暴露，通过方法访问。

本项目使用 proto3，因此是 Open Struct API。

---

## 6. 最佳实践与常见陷阱

### 6.1 Do: 删除字段后保留编号和名称

```proto
message Foo {
  reserved 2, 5, 9 to 11;  // 保留编号
  reserved "old_field";     // 保留名称
}
```

**原因**：防止未来复用字段编号导致数据解析错乱。即使字段已删除，历史数据/日志中可能仍有旧格式。

### 6.2 Don't: 复用字段编号

- ❌ 绝对不要复用字段编号
- ❌ 即使你认为没人在用该字段
- ❌ 即使只是"重新排列"字段编号

**后果**：反序列化歧义、数据损坏、隐私泄露（PII/SPII 泄露）、调试噩梦。

### 6.3 Don't: 更改字段类型

几乎永远不要更改字段类型。虽然某些数值类型在线格式上兼容，但部署不同步会导致数据截断或错误。

### 6.4 Do: 枚举第一个值为 UNSPECIFIED

```proto
enum PhotoType {
  PHOTO_TYPE_UNSPECIFIED = 0;  // 零值，无语义
  PHOTO_TYPE_GIF = 1;
  PHOTO_TYPE_WEBP = 2;
}
```

- 零值应无实际语义，仅表示"未指定"
- 帮助协议演进：新增枚举值时，旧客户端看到的是未设置状态

### 6.5 Don't: 使用 required 字段

- proto3 已移除 `required`
- 字段永远用 `optional` 或 `repeated`
- 如需表达"必填"，在注释中写明 `// required` 即可

**原因**：长期演进中，今天 required 的字段未来可能不再需要，但已无法安全移除。

### 6.6 Don't: 单消息字段过多

- 一个消息不要有上百个字段
- 字段过多会导致生成代码体积膨胀，甚至编译失败
- 按职责拆分为嵌套消息或多个消息

### 6.7 Do: 使用 Well-Known Types

优先使用官方标准类型而非自己造：

| 类型 | 用途 |
|------|------|
| `google.protobuf.Timestamp` | 时间点 |
| `google.protobuf.Duration` | 时间跨度 |
| `google.protobuf.FieldMask` | 字段掩码（部分更新） |
| `google.protobuf.Struct` | 动态 JSON 结构 |
| `google.protobuf.Empty` | 空消息 |

### 6.8 Don't: 用 Boolean 表示可能扩展的二态

如果未来可能出现第三种状态，直接用 `enum`：

```proto
// ❌ 不好：未来可能需要更多类型
optional bool is_gif = 1;

// ✅ 好：可扩展
enum PhotoFormat {
  PHOTO_FORMAT_UNSPECIFIED = 0;
  PHOTO_FORMAT_GIF = 1;
  PHOTO_FORMAT_WEBP = 2;
  PHOTO_FORMAT_PNG = 3;
}
```

### 6.9 Don't: 用 Text Format 做数据交换

- TextFormat 和 JSON 基于字段名/枚举名，重命名字段会破坏兼容性
- **二进制格式**是数据交换的首选
- TextFormat 仅用于人类编辑和调试

### 6.10 Never: 依赖序列化字节的稳定性

- 同一消息在不同构建/版本中，序列化结果可能不同
- 不要用 protobuf 序列化结果做 cache key 或哈希
- 如需确定性序列化，使用专门的确定性序列化 API

### 6.11 Do: RPC 和存储用不同消息

- 不要复用同一消息同时做 API 和持久化
- 需求会分化，分开定义给你更多演进自由度
- 在中间加一层转换代码（虽然初期多写点代码，但长期收益大）

### 6.12 Don't: 从 repeated 改为 scalar

- repeated → scalar：会丢失数据（数值类型 packed 格式完全无法解析；消息类型取最后一个）
- scalar → repeated：二进制兼容（非 packed 格式下变成单元素列表）

### 6.13 Don't: 更改字段默认值

- proto3 已不支持自定义默认值（零值即默认）
- 更改默认值会导致客户端/服务器版本不同步时行为不一致

### 6.14 Do: 每个文件少放消息类型

- 一个文件放太多消息会导致依赖膨胀
- 每个文件只放一个消息/枚举/服务，或一组循环依赖的消息
- 迁移和重构更方便

### 6.15 常见陷阱总结

| 陷阱 | 后果 | 规避 |
|------|------|------|
| 字段编号复用 | 数据损坏、解析失败 | 删除字段必 reserved |
| 负数用 int32 | 体积膨胀（10 字节） | 改用 sint32/sint64 |
| 大值用 uint32 | 体积膨胀 | 大值改用 fixed32 |
| int64 传 JS | 精度丢失 | 用字符串或 uint64 字符串接收 |
| 依赖 map 顺序 | 非确定性 | 不要依赖遍历顺序 |
| optional 忘加 | 无法区分零值和未设置 | 推荐加 optional |
| 枚举无前缀 | 命名冲突 | 枚举值必加类型前缀 |
| required 字段 | 无法演进 | 用 optional + 注释 |

---

## 7. 附录：本项目 proto 使用情况

### 7.1 项目配置

- **proto 源文件**：`proto/` 目录
- **编译脚本**：`proto/gen.sh`
- **后端 Go 使用**：`ServerGo/` 中通过生成的 `.pb.go` 文件处理 WS 游戏流量
- **前端使用**：`ClientWeb/` 使用 protobuf.js / 生成的 TS 类型

### 7.2 通信协议

- **HTTP API**：JSON over HTTPS（端口 39001）
- **实时游戏流量**：**Protobuf (proto3) over WSS**（端口 39002，路径 `/ws`）

### 7.3 相关文档

- 项目架构与通信协议：见 `docs/架构与协议/` 目录
- 各游戏 WS 帧格式：见对应游戏的规则与协议文档

### 7.4 本项目 proto 编写建议

1. 遵守官方风格指南（lower_snake_case、TitleCase 消息名等）
2. 字段编号 1~15 留给高频字段
3. 合理使用 `optional` 明确存在性
4. 删除字段必须 `reserved`
5. 枚举零值使用 `*_UNSPECIFIED` 命名
6. 生成的 Go 代码不手动修改
