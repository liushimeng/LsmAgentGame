# LsmAgentGame

Web 游戏平台 —— 后端 Go（HTTPS + WSS），前端 React + TypeScript + Three.js，WebSocket 之上使用 Protobuf 协议，MySQL（通过 GORM）作为持久化。

## 快速开始

```bash
# 1. 复制并编辑运行时配置（数据库 lsmDB 与 superuser 账号已存在，无需初始化）
cp LsmAgentGame.conf.example LsmAgentGame.conf
# 编辑 LsmAgentGame.conf —— 设置 db.password、jwt.secret 等

# 2. 编译前端 + 后端，然后运行
./rebuild_restart_app.sh
# 服务监听地址：https://127.0.0.1:39001
```

## 目录结构

```
ServerGo/                       Go 后端（HTTPS 39001, WSS 39002）
ClientWeb/                      React + Vite 前端
proto/                          Protobuf 源文件（单一事实源）
docs/                           架构设计、鉴权流程、API 参考
python-generate-image-tool/     子模块 —— AI 图像生成
go-web-debug-tool/              子模块 —— Chrome CDP 自动化调试服务
```

完整设计见 [docs/架构与协议/整体架构.md](docs/架构与协议/整体架构.md)，登录生命周期见 [docs/架构与协议/鉴权流程.md](docs/架构与协议/鉴权流程.md)，HTTP/WS 接口见 [docs/架构与协议/API参考.md](docs/架构与协议/API参考.md)。

## 约定

- 所有 Go 文件使用 `snake_case.go` 命名。**唯一例外**：`ServerGo/models/` 目录下的 GORM 模型文件使用 `t_lsm_game_*.go` 前缀（按规范要求）。
- 任何涉及基础设施的提交都应保持 `.gitignore` 的完整性（不得提交 `LsmAgentGame.conf`、`node_modules/`、生成的 proto 代码）。
- 任何贡献者在克隆后必须执行 `git submodule update --init --recursive`。

## 协议

私有 / 内部项目。
