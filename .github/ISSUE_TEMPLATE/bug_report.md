---
name: 缺陷报告 / Bug Report
about: 报告一个明确的缺陷（不需要修复方案）
title: "[Bug] <一句话描述>"
labels: ["bug", "needs-triage"]
assignees: []
---

## 现象 / What happened

请简明描述现象，附上截图或日志。

## 复现步骤 / Reproduction

1. 操作 1
2. 操作 2
3. 看到异常

## 期望 / Expected

描述你认为应该发生什么。

## 实际 / Actual

描述实际发生了什么，包括错误信息、堆栈、HTTP 状态码等。

## 环境 / Environment

- OS:
- Go 版本（如果是后端问题）:
- Node 版本（如果是前端问题）:
- 浏览器（如果是前端问题）:
- Commit hash: `git rev-parse HEAD`
- 分支: `git branch --show-current`

## 可见性 / Visibility

- [ ] 问题在公网部署可见
- [ ] 问题只在本地复现
- [ ] 问题与游戏类型相关（请注明：xiangqi / chess / junqi / doudizhu / texasholdem / werewolf）

## 日志 / Logs

```
粘贴 `logs/` 下的相关日志片段
```

## 额外上下文 / Additional Context

任何其他有助于定位问题的信息。
