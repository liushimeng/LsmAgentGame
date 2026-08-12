<!-- markdownlint-disable -->
<div align="center">

[🇨🇳 中文](README.md) · [🇬🇧 English](README.en.md) · [🇯🇵 日本語](README.ja.md)

</div>
<!-- markdownlint-restore -->

# LsmAgentGame

> **🤖 100% AI エージェント自動プログラミング** —— 人間が一文字も手書きしていないコード。レガシーな手動コーディングは一切なし。
> プロジェクト全体（Go バックエンド、React フロントエンド、6 つのゲームエンジン、人狼 AI エージェント、Proto プロトコル、CI スクリプト）は、AI エージェントが自律的に記述・テスト・リファクタ・デプロイしました。
> 本リポジトリは「エージェントプログラミング」能力の完全なデモンストレーションです。

マルチ AI エージェント駆動の Web ゲームプラットフォーム：人狼 13 人局（AI 駆動）＋ 象棋 ＋ チェス ＋ 軍棋 ＋ 闘地主 ＋ テキサスホールデム。
バックエンドは Go（HTTPS + WSS）、フロントエンドは React + TypeScript + Three.js、WebSocket 上で Protobuf プロトコル、MySQL（GORM 経由）永続化。

![人狼エージェント対局](ProjectPic/werewolf-game.png)

---

## 🎮 ゲーム一覧

| ゲーム | 人数 | 特徴 |
|--------|------|------|
| **人狼 13 人局** | 3-13 | AI エージェント駆動、7 種類の LLM モデルが同場競技、プロップ心理戦、審判エージェント |
| 象棋（中国将棋） | 2 | 3D ボード、ターン制 |
| チェス | 2 | 3D ボード、FEN 記譜 |
| 軍棋 | 2 | ダークモード、工兵の機雷除去 |
| 闘地主（ドゥオドゥーズー） | 3 | ビッディング、コンボ認識、農民同盟 |
| テキサスホールデム | 2-6 | ノーリミット、ベッティングラウンド、役評価 |

---

## 🤖 エージェント自動プログラミング

本リポジトリの全コードは AI エージェント（Claude Code、Kilo Code、OpenCode など）が自律的に記述しました。

- **手動コーディングゼロ**：人間が Go / TypeScript / CSS / SQL の一行も手書きしていません。
- **エージェント協業**：8 つの SubAgent 責任ライン（フロントエンド、バックエンド、ゲームデザイン、アート、統合、ビジュアル、LLM Provider、人狼エージェント）に分割し、それぞれが独立して作業・自動コミット。
- **自己テスト＆自己修正**：エージェントが `go test ./...`、`tsc --noEmit`、`npm run build` を自動実行し、バグを自動特定・修正・回帰検証。
- **自己デプロイ**：`rebuild_restart_app.sh` はエージェントが記述——ワンクリックでフロントエンド＋バックエンドをコンパイルしサービスを再起動。
- **継続的改善**：CLAUDE.md には 130 以上の教訓（§1–§213）を記録。それぞれがエージェントが失敗から自動的に書き込んだ「経験メモリ」であり、後続のエージェントが自動ロードして再発を防ぎます。

> **リポジトリ統計**：5 コミット、約 10 万行のコード、6 つのゲーム、完全プレイ可能な 13 人局人狼 AI。
> これらすべて、人間は一文字も書いていません。

---

## 🏗 技術スタック

- **バックエンド**：Go（モジュール名 `LsmAgentGame`）、Gin、GORM + MySQL/MariaDB、gorilla/websocket、JWT (HS256)、bcrypt、zap ロギング
- **フロントエンド**：React 18 + TypeScript、Vite、`@react-three/fiber` + `@react-three/drei`、zustand、react-router-dom v6
- **通信**：HTTP API（JSON over HTTPS）、リアルタイムゲームトラフィック（Protobuf over WSS）
- **データベース**：MariaDB `127.0.0.1:3306`、スキーマ `lsmDB`

---

## 🚀 クイックスタート

### 前提要件

- Go 1.22+
- Node.js 18+
- MariaDB / MySQL（スキーマ `lsmDB` とユーザー `superuser` は既存、初期化不要）
- Linux（サービスは自己署名 TLS 証明書を使用、ローカル開発用）

### インストールとデプロイ

```bash
# 1. リポジトリをクローン（サブモジュール込み）
git clone <your-repo-url> LsmAgentGame
cd LsmAgentGame
git submodule update --init --recursive

# 2. ランタイム設定をコピーして編集
cp LsmAgentGame.conf.example LsmAgentGame.conf
# LsmAgentGame.conf を編集 —— db.password、jwt.secret、llm.providers[].api_key などを設定
# 実際のシークレットは LsmAgentGame.conf のみ（.gitignore で除外済み）

# 3. フロントエンド依存をインストール
cd ClientWeb && npm install && cd ..

# 4. ワンクリックでフロントエンド＋バックエンドをコンパイルして起動
./rebuild_restart_app.sh

# 5. ブラウザで開く
# https://127.0.0.1:39001
```

サービスエンドポイント：

| サービス | ポート | URL |
|----------|--------|-----|
| HTTPS（REST + 静的ファイル） | 39001 | `https://127.0.0.1:39001` |
| WSS（リアルタイムゲームトラフィック） | 39002 | `wss://127.0.0.1:39002/ws` |

### 動作確認

```bash
curl -sk https://127.0.0.1:39001/api/version
# {"code":0,"data":{"version":"v1.0.0-<sha>","build_time":"..."},"message":"ok"}
```

---

## 📁 ディレクトリ構成

```
ServerGo/                       Go バックエンド（HTTPS 39001, WSS 39002）
ClientWeb/                      React + Vite フロントエンド
proto/                          Protobuf ソースファイル（単一の真実源）
docs/                           設計、認証フロー、API リファレンス、エージェント教訓
python-generate-image-tool/     サブモジュール —— AI 画像生成（Volcengine Ark API）
go-web-debug-tool/              サブモジュール —— Chrome CDP 自動デバッグ/スクリーンショット
ProjectPic/                     プロジェクトアセット（ローカル表示のみ、コミット不可）
```

全体の設計：[docs/架构与协议/整体架构.md](docs/架构与协议/整体架构.md)、
認証ライフサイクル：[docs/架构与协议/鉴权流程.md](docs/架构与协议/鉴权流程.md)、
HTTP/WS API：[docs/架构与协议/API参考.md](docs/架构与协议/API参考.md)。

---

## 🐺 人狼 13 人局エージェント

人狼は本プロジェクトの目玉です。13 人局は 3-13 人対応、任意のシートを AI エージェント（LLM 駆動）に設定可能：

- **7 種類の LLM モデルが同場競技**：DeepSeek / Kimi / Qwen / GLM / 豆包 / MiniMax / 小米、ゲームごとに自動シャッフルして同モデル対決を回避。
- **Token 消費をリアルタイム表示**：ステータスバーに各エージェントの入力/出力 Token 累計を表示。30 分の対局で数万 Token を消費（1 時間あたり約 5-10 万 Token、モデルとフェーズ密度による）。
- **プロップ心理戦**：Markdown 注入、プロンプトネスト、文字欺瞞など 6 種類の LLM 注入攻撃を購入可能なプロップとしてパッケージ化。
- **審判エージェント**：非プレイヤーホスト、LLM 駆動のフェーズナレーションと対局全体の要約。
- **本音と建前の分離**：`speak_with_thought` ツールで公開発言と内心の独白を分離、プロトコル層で物理的に隔離。
- **永続化メモリ**：対局終了後、エージェントが `MEMORY.md` を自己改善し、対局を跨いで学習。

> 詳しくは [狼人杀13人局-游戏相关信息和Agent现状文档.md](狼人杀13人局-游戏相关信息和Agent现状文档.md) を参照。

---

## 🧪 自動テスト

プロジェクトは `go-web-debug-tool`（Chrome CDP）を使用して Web 側の自動テストとスクリーンショットを取得：

```bash
# デバッグツールを起動
cd go-web-debug-tool && ./GoWebDebugTool -d

# エージェントが REST で Chrome を操作：ページ開く、ログイン、部屋作成、スクリーンショット
curl -X POST http://localhost:28999/NewChromePage ...
curl -X POST http://localhost:28999/ControlChromePage \
  -d '{"page_id":"p_xxx","action":"screenshot","params":{"format":"png"}}'
```

スクリーンショットは `ProjectPic/` に保存され、ゲームのハイライトシーンを紹介します。

---

## 🤝 フォローとサポート

このプロジェクトが面白いと思ったら、以下のプラットフォームでアカウントをフォローしてゲームデモ動画をご覧ください：

| プラットフォーム | 検索アカウント |
|----------------|---------------|
| 快手 | **封刀灌海** |
| 抖音 | **封刀灌海** |
| Bilibili | **封刀灌海** |
| 小紅書 | **封刀灌海** |
| WeChat チャンネル | **封刀灌海** |

---

## ☕ 作者にコーヒーを奢ろう

プロジェクトのサーバー、LLM API 呼び出し、画像生成などには継続的なコストがかかります。このプロジェクトが役に立った、または面白いと思ったら、チップでサポートしてください：

| WeChat チップ | Alipay チップ |
|:-------------:|:-------------:|
| ![WeChat QR](ProjectPic/wechat_qr.jpg) | ![Alipay QR](ProjectPic/alipay_qr.jpg) |

> 画像はローカル閲覧時に表示されます。`ProjectPic/` はコミットされていないため GitHub では表示されない場合があります——クローンしてローカルでご覧ください。

**連絡先**：

- 📱 携帯：`13520647302`
- 💬 WeChat：`liushimeng109117198`

---

## 📜 ライセンス

プライベート / 内部プロジェクト。全コードは AI エージェントが自動記述。

---

## 🌟 GitHub でサポート

このプロジェクトが「エージェントプログラミング」に対する考えを変えたなら：

- ⭐ **Star** —— より多くの人がエージェントプログラミングの力を目にできます
- 👁️ **Watch** —— 今後のイテレーションを追跡
- 🍴 **Fork** —— 自分の環境で人狼エージェント対局を動かそう

**これは一人の人間が書いたコードではありません。AI エージェントチームの作品です。**

---

**バージョン**：v1.0.0  |  **最終更新**：2026-08-12  |  **ビルド**：エージェント自動ビルド
