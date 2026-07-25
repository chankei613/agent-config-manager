package config

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxHashSize を超えるファイルはハッシュを取らない。
// 設定ファイルは通常数KBで、巨大なものは管理対象の設定ではない可能性が高い。
const maxHashSize = 5 << 20 // 5MB

// maxDepth はシンボリックリンクを辿る際の再帰上限。
// 訪問済み管理でループは防げるが、深いツリーを延々と掘らないための保険。
const maxDepth = 12

// skipDirs は .claude 配下でも走査しないディレクトリ。
// キャッシュ・セッションログ・バックアップは設定ではなく、数が多くスキャンを重くする。
var skipDirs = map[string]bool{
	"cache":           true,
	"paste-cache":     true,
	"sessions":        true,
	"session-env":     true,
	"shell-snapshots": true,
	"file-history":    true,
	"telemetry":       true,
	"downloads":       true,
	"backups":         true,
	"projects":        true,
	"ide":             true,
	"node_modules":    true,
	".git":            true,

	// plugins 配下はマーケットプレイスが配る読み取り専用のベンダーファイル。
	// 中に .mcp.json が十数個あるが、ユーザーが手で管理する設定ではないので
	// インベントリに混ぜると「設定が散逸している」判定が誤検知だらけになる。
	"plugins": true,
}

// File はインベントリ1件分。DBモデル（internal/db）へそのまま写せる形にしてある。
type File struct {
	Scope   Scope  `json:"scope"`
	Kind    Kind   `json:"kind"`
	Project string `json:"project"` // プロジェクト名（user scope では空）

	Path    string `json:"path"`     // 絶対パス
	RelPath string `json:"rel_path"` // .claude（またはプロジェクトルート）からの相対パス

	Size    int64     `json:"size"`
	Hash    string    `json:"hash"` // SHA-256。差分検出とバージョン管理の基盤
	ModTime time.Time `json:"mod_time"`

	// workers は ~/.claude/agents → obsidian/_guidelines/workers のシンボリックリンク。
	// リンクを実体と誤認して上書きすると本体を壊すため、必ず記録する。
	IsSymlink     bool   `json:"is_symlink"`
	SymlinkTarget string `json:"symlink_target,omitempty"`
	Broken        bool   `json:"broken"` // リンク先が存在しない（孤児）

	// RealPath は実体の絶対パス。経路にリンクが無ければ Path と同じ。
	RealPath string `json:"real_path,omitempty"`
	// ViaSymlink はファイル自体はリンクでないが、親ディレクトリがリンクである状態。
	// この場合 Path へ書くと離れた場所の実体（例: Obsidian側のworker定義）が書き換わる。
	ViaSymlink bool `json:"via_symlink"`
}

// Result はスキャン1回分の結果。
type Result struct {
	Files    []File    `json:"files"`
	Errors   []string  `json:"errors"` // 読めなかった場所（権限等）。スキャン自体は止めない
	ScanedAt time.Time `json:"scanned_at"`
}

// ScanUserScope は ~/.claude を走査する。
func ScanUserScope(claudeDir string) (*Result, error) {
	res := newResult()
	scanClaudeDir(claudeDir, ScopeUser, "", res)
	sortFiles(res.Files)
	return res, nil
}

// ScanProjects は roots 配下の各プロジェクトを走査する。
// 「プロジェクト」= roots直下のディレクトリ、と定義する（cometinc/ の構成に合わせる）。
func ScanProjects(roots ...string) (*Result, error) {
	res := newResult()

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			res.Errors = append(res.Errors, root+": "+err.Error())
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			projectDir := filepath.Join(root, entry.Name())
			scanProject(projectDir, entry.Name(), res)
		}
	}

	sortFiles(res.Files)
	return res, nil
}

func newResult() *Result {
	return &Result{Files: []File{}, Errors: []string{}, ScanedAt: time.Now()}
}

// scanProject はプロジェクト1つ分を見る。
// .claude/ 配下に加えて、ルート直下の CLAUDE.md と .mcp.json も設定として拾う。
func scanProject(projectDir, projectName string, res *Result) {
	scanClaudeDir(filepath.Join(projectDir, ".claude"), ScopeProject, projectName, res)

	for _, name := range []string{"CLAUDE.md", ".mcp.json"} {
		path := filepath.Join(projectDir, name)
		if file, ok := inspect(path, name, ScopeProject, projectName, res); ok {
			res.Files = append(res.Files, file)
		}
	}
}

func scanClaudeDir(claudeDir string, scope Scope, projectName string, res *Result) {
	info, err := os.Stat(claudeDir)
	if err != nil || !info.IsDir() {
		return // .claude を持たないプロジェクトは珍しくないのでエラー扱いしない
	}
	walkDir(claudeDir, claudeDir, scope, projectName, res, map[string]bool{}, 0)
}

// walkDir は filepath.WalkDir を使わず自前で再帰する。
// `~/.claude/agents` は Obsidian の workers ディレクトリへのシンボリックリンクであり、
// WalkDir はディレクトリへのリンクを辿らないため、標準の走査では
// Worker定義153件がまるごと見えなくなる。この製品の管理対象の中核なので自前で辿る。
func walkDir(dir, base string, scope Scope, projectName string, res *Result, visited map[string]bool, depth int) {
	if depth > maxDepth {
		return
	}

	// リンクのループで無限再帰しないよう、実体パスで訪問済みを管理する
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		if visited[real] {
			return
		}
		visited[real] = true
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		res.Errors = append(res.Errors, dir+": "+err.Error())
		return
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		// 到達できないシンボリックリンクは孤児。種別が判別できなくても必ず記録する。
		// ディレクトリへのリンクが切れているケース（~/.claude/agents がまさにそれ）を
		// 「ただの不明ファイル」として捨てると、配下の定義が丸ごと消えたことに気付けない。
		if entry.Type()&os.ModeSymlink != 0 {
			if _, err := os.Stat(path); err != nil {
				if rel, relErr := filepath.Rel(base, path); relErr == nil {
					target, _ := os.Readlink(path)
					res.Files = append(res.Files, File{
						Scope:         scope,
						Kind:          kindForDanglingLink(rel),
						Project:       projectName,
						Path:          path,
						RelPath:       filepath.ToSlash(rel),
						IsSymlink:     true,
						SymlinkTarget: target,
						Broken:        true,
					})
				}
				continue
			}
		}

		if isDirFollowingLinks(entry, path) {
			if skipDirs[entry.Name()] {
				continue
			}
			walkDir(path, base, scope, projectName, res, visited, depth+1)
			continue
		}

		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			continue
		}
		if file, ok := inspect(path, rel, scope, projectName, res); ok {
			res.Files = append(res.Files, file)
		}
	}
}

// isDirFollowingLinks はディレクトリ、またはディレクトリを指すシンボリックリンクなら true。
func isDirFollowingLinks(entry fs.DirEntry, path string) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Stat(path)
	return err == nil && target.IsDir()
}

// inspect は1ファイルを調べる。管理対象外なら ok=false。
func inspect(path, rel string, scope Scope, projectName string, res *Result) (File, bool) {
	kind := Classify(rel)
	if !IsTracked(kind) {
		return File{}, false
	}

	// Lstat で見ることでシンボリックリンク自体の情報を取る（辿らない）
	info, err := os.Lstat(path)
	if err != nil {
		return File{}, false
	}

	file := File{
		Scope:   scope,
		Kind:    kind,
		Project: projectName,
		Path:    path,
		RelPath: filepath.ToSlash(rel),
	}

	if info.Mode()&os.ModeSymlink != 0 {
		file.IsSymlink = true
		if target, err := os.Readlink(path); err == nil {
			file.SymlinkTarget = target
		}
		// リンク先の実体を見る。辿れなければ孤児（Broken）
		target, err := os.Stat(path)
		if err != nil {
			file.Broken = true
			return file, true
		}
		info = target
	}

	if info.IsDir() {
		return File{}, false
	}

	// 実体の位置を控える。経路のどこかにリンクがあると Path とズレる。
	if real, err := filepath.EvalSymlinks(path); err == nil && real != path {
		file.RealPath = real
		// ファイル自身がリンクでないのに実体が違う = 親ディレクトリ経由のリンク
		file.ViaSymlink = !file.IsSymlink
	}

	file.Size = info.Size()
	file.ModTime = info.ModTime()

	if info.Size() <= maxHashSize {
		if hash, err := hashFile(path); err == nil {
			file.Hash = hash
		} else {
			res.Errors = append(res.Errors, path+": "+err.Error())
		}
	}

	return file, true
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sortFiles は出力を安定させる（テストと差分表示のため）。
func sortFiles(files []File) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Project != files[j].Project {
			return files[i].Project < files[j].Project
		}
		return files[i].Path < files[j].Path
	})
}
