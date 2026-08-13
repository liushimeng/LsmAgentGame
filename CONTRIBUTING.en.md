# Contributing Guide

> Thanks for considering contributing to `LsmAgentGame`.
> This repository is a **100% AI-Agent-coded** experimental project. Following the workflow below will maximize the chance of your PR being merged.

[🇨🇳 中文](CONTRIBUTING.md) · [🇬🇧 English](CONTRIBUTING.en.md) · [🇯🇵 日本語](CONTRIBUTING.ja.md)

---

## 1. Code of Conduct

By participating, you agree to follow [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
Please keep discussions constructive, respectful and on-topic.

## 2. Opening Issues

| Category | Template | Purpose |
|----------|----------|---------|
| Bug report | `.github/ISSUE_TEMPLATE/bug_report.md` | Repro steps + expected + actual + logs |
| Feature request | `.github/ISSUE_TEMPLATE/feature_request.md` | Pain point + proposal + alternatives |
| Docs | `.github/ISSUE_TEMPLATE/documentation.md` | Link + current text + desired text |
| Security | **Do not** open a public issue | See `SECURITY.md` for private email |

## 3. Pull Requests

### 3.1 Preparation

1. **Fork** the repo → create a feature branch: `git checkout -b feat/<short-name>`
2. Init submodules: `git submodule update --init --recursive`
3. Read [`CLAUDE.md`](CLAUDE.md) §10 (workflow) and §13 (SubAgent roles) to learn the coding conventions.

### 3.2 Coding Style

- **Backend Go** — `ServerGo/`
  - Files ≤ 1800 lines (CLAUDE.md §4)
  - `go build -o LsmAgentGame main.go` must pass
  - `go test ./...` must pass
  - DB model files: `t_lsm_game_*.go`
  - Business files: `snake_case.go`
- **Frontend React + TypeScript** — `ClientWeb/`
  - Files ≤ 1800 lines
  - `tsc --noEmit` + `npm run build` must pass
  - **Cross-game imports are forbidden** (CLAUDE.md §2.1)
  - Shared components go to `components/common|chat|ui/`; game-private ones to `components/<game>/`
- **CSS** — `ClientWeb/src/styles/`
  - `globals.css` `@import` order must not be reordered
  - When splitting CSS, validate "built CSS bytes identical" to prove zero regression

### 3.3 Commit Granularity

- One commit per logical stage (CLAUDE.md §10.1).
- Land directly on `main` (unless maintainer explicitly approves a feature branch).
- Commit messages in Chinese preferred, format:

  ```
  <type>: <one-line summary>

  - change 1
  - change 2
  - reference §number or issue #
  ```

  Types: `feat` / `fix` / `refactor` / `docs` / `test` / `chore` / `perf`.

### 3.4 Cross-Stack Changes

For PRs that touch both `ClientWeb/` and `ServerGo/`, please describe:

- Trigger chain (HTTP / WSS / shared state)
- Backward-compat strategy (additive fields, keep old fields)
- End-to-end test cases

### 3.5 PR Checklist

- [ ] Files respect CLAUDE.md §4 1800-line hard limit
- [ ] `go build` + `go test ./...` passes
- [ ] `tsc --noEmit` + `npm run build` passes
- [ ] New `.proto` has been regenerated via `proto/gen.sh`
- [ ] README / docs updated (new/changed API, rules, config)
- [ ] DB schema changes include migration notes
- [ ] No secrets / internal accounts hardcoded

## 4. Automated Testing

Maintainers use `AutoTestAndSaveReport.sh` and the `go-web-debug-tool/` submodule to run regression.
Contributors are encouraged to attach local regression output in the PR description (path to `TestReport/` is enough).

## 5. Contact

- General discussion: GitHub / Gitee / GitCode Issues
- Security: see [`SECURITY.md`](SECURITY.md) (private email, do not open public issue)
- Business: see maintainer contact on the repo home page

---

> This project is a full demonstration of "Loop Agent × Graphic Agent 24/7 uninterrupted coding".
> Feel free to share your team's AI-Agent collaboration experience in the PR description — we will aggregate them into a `docs/` collaboration topic.
