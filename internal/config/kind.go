// Package config は散逸したAI設定ファイルの発見・分類を担う。
// Agent Config Manager（製品C）の中核で、「どこに何があるか」を機械可読にすることが役割。
package config

import (
	"path/filepath"
	"strings"
)

// Kind は設定ファイルの種別。パスの形から判定する。
type Kind string

const (
	KindClaudeMD      Kind = "claude_md"      // CLAUDE.md（プロジェクト指示）
	KindSettings      Kind = "settings"       // settings.json
	KindSettingsLocal Kind = "settings_local" // settings.local.json（機密を含みうる）
	KindMCPConfig     Kind = "mcp_config"     // claude_desktop_config.json / .mcp.json
	KindAgent         Kind = "agent"          // agents/*.md（Worker定義）
	KindCommand       Kind = "command"        // commands/*.md（スラッシュコマンド）
	KindSkill         Kind = "skill"          // skills/*/SKILL.md
	KindOther         Kind = "other"          // .claude配下だが分類できないもの
)

// Scope は設定の適用範囲。
type Scope string

const (
	ScopeUser    Scope = "user"    // ~/.claude/ — 全プロジェクト共通
	ScopeProject Scope = "project" // <project>/.claude/ or <project>/CLAUDE.md
)

// SensitiveKinds はUIに素で出す前にマスク検討が要る種別。
// settings.local.json にはAPIキーや環境変数が入りうる。
var SensitiveKinds = map[Kind]bool{
	KindSettingsLocal: true,
	KindMCPConfig:     true,
}

func (k Kind) IsSensitive() bool { return SensitiveKinds[k] }

// Classify はファイルパスから種別を判定する。
// rel は .claude ディレクトリ（またはプロジェクトルート）からの相対パス。
func Classify(rel string) Kind {
	rel = filepath.ToSlash(rel)
	base := filepath.Base(rel)

	switch base {
	case "CLAUDE.md":
		return KindClaudeMD
	case "settings.json":
		return KindSettings
	case "settings.local.json":
		return KindSettingsLocal
	case "claude_desktop_config.json", ".mcp.json":
		return KindMCPConfig
	}

	// ディレクトリ配置で決まるもの。ネストしていても先頭セグメントで判定する。
	segments := strings.Split(rel, "/")
	if len(segments) >= 2 {
		switch segments[0] {
		case "agents":
			if strings.HasSuffix(base, ".md") {
				return KindAgent
			}
		case "commands":
			if strings.HasSuffix(base, ".md") {
				return KindCommand
			}
		case "skills":
			if base == "SKILL.md" {
				return KindSkill
			}
		}
	}

	return KindOther
}

// configDirKinds は「そのディレクトリ自体が設定群を表す」名前。
// リンクが切れているとき、配下を見られないので名前から種別を推測する。
var configDirKinds = map[string]Kind{
	"agents":   KindAgent,
	"commands": KindCommand,
	"skills":   KindSkill,
}

// isConfigDirName は「設定群を表す既知のディレクトリ名」かを返す。
// シンボリックリンクを辿ってよいかの判断に使う。
func isConfigDirName(name string) bool {
	_, ok := configDirKinds[name]
	return ok
}

// kindForDanglingLink は到達できないリンクの種別を決める。
// ディレクトリ名で判る場合はそれを使い、判らなければ通常の分類にフォールバックする。
func kindForDanglingLink(rel string) Kind {
	if kind, ok := configDirKinds[filepath.Base(rel)]; ok {
		return kind
	}
	if kind := Classify(rel); kind != KindOther {
		return kind
	}
	// 種別不明でも孤児は報告する価値があるので other のまま残す
	return KindOther
}

// IsTracked は「インベントリに載せる価値がある種別か」を返す。
// KindOther（キャッシュ・履歴・セッションログ等）は数が多く管理対象にならないため除外する。
func IsTracked(k Kind) bool { return k != KindOther }
