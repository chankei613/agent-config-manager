// Package content は設定ファイルの中身を読んで表示可能な形にする。
//
// settings.local.json や MCP設定にはAPIキーが入りうるため、
// 素の内容をそのままUIに流さず、秘密らしき値をマスクしてから返すのが原則。
package content

import (
	"os"
	"regexp"
	"strings"
)

// maxDisplaySize を超えるファイルは途中で打ち切る。
// 設定ファイルは通常数KBで、それを大きく超えるものは画面に出す意味がない。
const maxDisplaySize = 256 << 10 // 256KB

// File は表示用に整えたファイルの中身。
type File struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
	Text string `json:"text"`
	// Masked は秘密らしき値を伏せたかどうか。UIで「一部マスクしています」と出すために使う。
	Masked bool `json:"masked"`
	// Truncated は大きすぎて途中で切ったかどうか。
	Truncated bool `json:"truncated"`
	Lines     int  `json:"lines"`
}

// secretKeyPattern はJSON/env形式で「値を隠すべきキー」を拾う。
// 値部分（グループ2）だけを差し替えるため、キー名は残る＝何が設定されているかは分かる。
var secretKeyPattern = regexp.MustCompile(
	`(?i)("(?:[^"]*(?:key|token|secret|password|passwd|credential|auth)[^"]*)"\s*:\s*)"[^"]*"`,
)

// envSecretPattern は "KEY=value" 形式の秘密。
var envSecretPattern = regexp.MustCompile(
	`(?im)^(\s*(?:[A-Z0-9_]*(?:KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|AUTH)[A-Z0-9_]*)\s*=\s*).+$`,
)

// bearerPattern は本文中に直接書かれたトークンらしき文字列。
var bearerPattern = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._\-]{16,}`)

// longSecretPattern は sk-... のようなAPIキー形式。
var longSecretPattern = regexp.MustCompile(`\b(sk|pk|ghp|gho|github_pat|xoxb|xoxp)[-_][A-Za-z0-9_\-]{16,}\b`)

const redacted = "***"

// Mask は秘密らしき値を伏せる。キー名は残すので「何が設定されているか」は分かる。
func Mask(text string) (string, bool) {
	masked := secretKeyPattern.ReplaceAllString(text, `${1}"`+redacted+`"`)
	masked = envSecretPattern.ReplaceAllString(masked, "${1}"+redacted)
	masked = bearerPattern.ReplaceAllString(masked, "${1}"+redacted)
	masked = longSecretPattern.ReplaceAllString(masked, redacted)

	return masked, masked != text
}

// Read はファイルを読み、必要ならマスクして返す。
// alwaysMask が true のときは種別に関わらずマスクする（UIの既定動作）。
func Read(path, kind string, alwaysMask bool) (*File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	file := &File{Path: path, Kind: kind, Size: info.Size()}

	if len(raw) > maxDisplaySize {
		raw = raw[:maxDisplaySize]
		file.Truncated = true
	}

	text := string(raw)
	if alwaysMask || IsSensitiveKind(kind) {
		text, file.Masked = Mask(text)
	}

	file.Text = text
	file.Lines = strings.Count(text, "\n") + 1
	return file, nil
}

// IsSensitiveKind は internal/config の分類と対応する。
// ここで独自に持つのは、content パッケージが config に依存しないようにするため。
func IsSensitiveKind(kind string) bool {
	return kind == "settings_local" || kind == "mcp_config"
}
