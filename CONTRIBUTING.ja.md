# コントリビューションガイド

> `LsmAgentGame` への貢献をご検討いただきありがとうございます。
> 本リポジトリは **100% AI Agent 自動プログラミング** の実験的プロジェクトです。以下の手順に従って PR を提出すると、マージされる可能性が大幅に高まります。

[🇨🇳 中文](CONTRIBUTING.md) · [🇬🇧 English](CONTRIBUTING.en.md) · [🇯🇵 日本語](CONTRIBUTING.ja.md)

---

## 1. 行動規範

参加することで [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) の遵守に同意したものとみなされます。
建設的・敬意ある・本題に沿った議論を心がけてください。

## 2. Issue の提出

| 種別 | テンプレート | 用途 |
|------|-------------|------|
| バグ報告 | `.github/ISSUE_TEMPLATE/bug_report.md` | 再現手順 + 期待 + 実際 + ログ |
| 機能要望 | `.github/ISSUE_TEMPLATE/feature_request.md` | 課題 + 提案 + 代替案 |
| ドキュメント | `.github/ISSUE_TEMPLATE/documentation.md` | リンク + 現状 + 期待 |
| セキュリティ | **公開 issue 禁止** | `SECURITY.md` の非公開メールに従う |

## 3. Pull Request

### 3.1 準備

1. **Fork** してフィーチャーブランチ作成：`git checkout -b feat/<short-name>`
2. サブモジュール初期化：`git submodule update --init --recursive`
3. [`CLAUDE.md`](CLAUDE.md) §10（ワークフロー）と §13（SubAgent 役割分担）を読む。

### 3.2 コーディング規約

- **バックエンド Go** — `ServerGo/`
  - ファイル ≤ 1800 行（CLAUDE.md §4）
  - `go build -o LsmAgentGame main.go` 通過必須
  - `go test ./...` 通過必須
  - DB モデル：`t_lsm_game_*.go`
  - 業務ファイル：`snake_case.go`
- **フロントエンド React + TypeScript** — `ClientWeb/`
  - ファイル ≤ 1800 行
  - `tsc --noEmit` + `npm run build` 通過必須
  - **ゲーム間 import 禁止**（CLAUDE.md §2.1）
  - 共有コンポーネントは `components/common|chat|ui/`、ゲーム私有は `components/<game>/`
- **CSS** — `ClientWeb/src/styles/`
  - `globals.css` の `@import` 順序変更禁止
  - CSS 分割時は「ビルド後 CSS バイト一致」でゼロ回帰検証

### 3.3 コミット粒度

- 1 commit = 1 論理ステージ（CLAUDE.md §10.1）
- 原則 `main` 直コミット（メンテナ明示承認時のみフィーチャーブランチ可）
- コミットメッセージは中国語優先、フォーマット：

  ```
  <type>: <1行要約>

  - 変更 1
  - 変更 2
  - 関連 §番号 / issue 番号
  ```

  種別：`feat` / `fix` / `refactor` / `docs` / `test` / `chore` / `perf`。

### 3.4 クロススタック変更

`ClientWeb/` と `ServerGo/` を同時に変更する PR では：

- トリガーチェーン（HTTP / WSS / 共有 state）
- 後方互換戦略（追加フィールド、既存フィールド保持）
- E2E テストケース

を記述してください。

### 3.5 PR チェックリスト

- [ ] CLAUDE.md §4 1800 行制限遵守
- [ ] `go build` + `go test ./...` 通過
- [ ] `tsc --noEmit` + `npm run build` 通過
- [ ] 新規 `.proto` は `proto/gen.sh` 再生成済み
- [ ] README / docs 同期更新
- [ ] DB スキーマ変更：マイグレーション手順記述
- [ ] 鍵 / 内部アカウントのハードコード無し

## 4. 自動テスト

メンテナは `AutoTestAndSaveReport.sh` と `go-web-debug-tool/` サブモジュールで回帰テストを実施します。
PR 説明欄にローカル回帰出力（`TestReport/` パスで十分）を添付してください。

## 5. 連絡先

- 一般議論：GitHub / Gitee / GitCode Issues
- セキュリティ：[`SECURITY.md`](SECURITY.md)（非公開メール、公開 issue 禁止）
- ビジネス：リポジトリホームのメンテナ連絡先参照

---

> 本プロジェクトは「Loop Agent × Graphic Agent 24/7 ノンストップ・コーディング」の完全な実証です。
> 皆さんのチームの AI Agent 協調経験を PR 説明欄で共有してください — `docs/` 配下の協調特集に集約します。
