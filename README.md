# Agent Config Manager

**「AIのdotfiles manager」** — `.claude` フォルダ・workers・systemプロンプト・MCPサーバー設定を一元管理し、
プロジェクト間での設定の統一とバージョン管理を行うスタンドアロン製品。

comet-taskAI 製品ロードマップ **Product C**（2026 Q4）。
ロードマップ: `obsidian/projects/comet-taskAI/product_roadmap.md`

## 解決する問題

複数プロジェクトでAI設定が散逸し、「また `.claude` フォルダを探している」状態になる。
どこに何があるか分からず、プロジェクト間で設定が知らないうちに乖離し、
シンボリックリンクが切れて定義が読み込まれなくなっていても気付けない。

## 現在のステータス: デスクトップアプリとして動作（Phase 1・2・3・5 完了）

- [x] Phase 1: スキャナ基盤（読み取り専用）
  - 設定ファイルの発見・分類（`internal/config`）
  - SHA-256による内容識別（差分検出とバージョン管理の基盤）
  - シンボリックリンクの追跡・実体パス解決・リンク切れ検出
  - インベントリの永続化と差分集計（`internal/inventory`）
- [x] Phase 2: 散逸の可視化
  - 種別×プロジェクトのマトリクス・乖離レポート・要約（`internal/inventory/report.go`）
  - 参照API 6本（`internal/api`）
- [x] Phase 5: UI（Wails v2 + Vue 3 + Vite + Pinia）— 単体リリースを優先して前倒し
  - 概要 / 一覧マトリクス / 乖離 / リンク切れ の4ビュー
- [x] Phase 3: バージョン管理（`internal/snapshot`）
  - スナップショット作成・復元・削除
  - **復元は必ず事前確認できる**（Plan＝ドライランで何が書き換わるかを提示）
  - **復元前に自動バックアップを取るので取り消せる**
- [ ] Phase 4: 統一・同期（設定のコピー・テンプレート適用）
- [ ] Phase 6: LP・リリース

## 使い方

```bash
make test    # Goのユニットテスト
make ui-test # フロントエンドのユニットテスト
make scan    # CLIでスキャンしてレポート（読み取りのみ）
make serve   # 参照APIを :8430 で起動
make app     # デスクトップアプリ(.app)をビルド
make dev     # Wails開発モード（ホットリロード）
```

### macOSでの注意

Dropboxなどが外部ボリューム上にある場合、初回起動時に
「リムーバブルボリューム上のファイルへのアクセス」を求めるダイアログが出る。
**許可しないとスキャン結果が0件になる**ので許可すること。

任意のルートを見る場合:

```bash
go run ./cmd/acmscan -user ~/.claude -projects /path/to/projects
```

## 管理対象

| 種別 | 対象 |
|---|---|
| `claude_md` | `CLAUDE.md`（ユーザー / プロジェクト） |
| `settings` | `settings.json` |
| `settings_local` | `settings.local.json` — **機密を含みうる** |
| `mcp_config` | `claude_desktop_config.json`, `.mcp.json` |
| `agent` | `agents/*.md`（Worker定義） |
| `command` | `commands/*.md` |
| `skill` | `skills/*/SKILL.md` |

`plugins/` 配下はマーケットプレイスが配るベンダーファイルなので走査しない。
キャッシュ・セッションログ・履歴も同様に除外する。

## 書き込みの安全設計

設定ファイルを書き換えるのは復元機能だけで、以下を守っている。

1. **事前確認** — `Plan` が「何が書き戻され、何が作り直され、何が変わらないか」を返す。
   この時点でディスクには一切触れない。UIも確認してからでないと実行できない
2. **取り消し可能** — 復元は実行前に必ず現状を自動スナップショットとして退避する。
   戻り値の `backup_id` を復元すれば元に戻せる
3. **削除しない** — スナップショット後に増えたファイルは報告するだけで消さない

## シンボリックリンクの扱い（重要）

`~/.claude/agents` は Obsidian の `_guidelines/workers/` へのシンボリックリンクとして
運用されている。標準の `filepath.WalkDir` はディレクトリへのリンクを辿らないため、
素直に実装すると **Worker定義がまるごと見えなくなる**。本スキャナは自前で再帰して辿り、
以下を記録する。

- `is_symlink` — その項目自体がリンク
- `via_symlink` — 親ディレクトリがリンク。**このパスへ書くと離れた場所の実体が書き換わる**
- `real_path` — 実体の絶対パス
- `broken` — リンク先に到達できない（孤児）

書き込み系（Phase 4）はこの情報を見て、実体を壊さないよう扱う。

## ディレクトリ構成

```
internal/config/     設定ファイルの発見・分類・ハッシュ化
internal/db/         GORMモデル・SQLite初期化
internal/inventory/  スキャン結果の永続化・差分集計・孤児抽出
internal/snapshot/   バージョン管理（スナップショット・ドライラン・復元）
cmd/acmscan/         実環境スキャンの確認用CLI（読み取りのみ）
cmd/acmserve/        参照APIサーバー（:8430）
app.go / main.go     Wailsアプリ本体（バインディングは internal/api.Server を共用）
frontend/            Vue 3 + Vite + Pinia のUI
```

## 技術スタック

A（MCP Server Manager）・B（AI Scheduler）・K（Harness Manager）と揃えている。

Wails v2 + Go 1.22+ + Vue 3 + Vite + Pinia + SQLite/GORM
