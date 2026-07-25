# Agent Config Manager

**「AIのdotfiles manager」** — `.claude` フォルダ・workers・systemプロンプト・MCPサーバー設定を一元管理し、
プロジェクト間での設定の統一とバージョン管理を行うスタンドアロン製品。

comet-taskAI 製品ロードマップ **Product C**（2026 Q4）。
ロードマップ: `obsidian/projects/comet-taskAI/product_roadmap.md`

## 解決する問題

複数プロジェクトでAI設定が散逸し、「また `.claude` フォルダを探している」状態になる。
どこに何があるか分からず、プロジェクト間で設定が知らないうちに乖離し、
シンボリックリンクが切れて定義が読み込まれなくなっていても気付けない。

## 現在のステータス: Phase 1（スキャナ基盤）完了

- [x] Phase 1: スキャナ基盤（読み取り専用）
  - 設定ファイルの発見・分類（`internal/config`）
  - SHA-256による内容識別（差分検出とバージョン管理の基盤）
  - シンボリックリンクの追跡・実体パス解決・リンク切れ検出
  - インベントリの永続化と差分集計（`internal/inventory`）
- [ ] Phase 2: 散逸の可視化（横断比較・差分・孤児検出のAPI化）
- [ ] Phase 3: バージョン管理（スナップショット・ロールバック）
- [ ] Phase 4: 統一・同期（書き込み系）
- [ ] Phase 5: UI（Vue 3 + Vite）

## 使い方

```bash
make test   # ユニットテスト
make scan   # ~/.claude と cometinc/ をスキャンしてレポート（読み取りのみ）
```

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
cmd/acmscan/         実環境スキャンの確認用CLI（読み取りのみ）
```

## 技術スタック

A（MCP Server Manager）・B（AI Scheduler）・K（Harness Manager）と揃えている。

Go 1.22+ / SQLite + GORM / （Phase 5で Wails v2 + Vue 3 + Vite + Pinia）
