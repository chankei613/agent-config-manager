package sync

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chankei613/agent-config-manager/internal/config"
	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/snapshot"
	"gorm.io/gorm"
)

var ErrTemplateNotFound = errors.New("テンプレートが見つかりません")

// CreateTemplate は指定プロジェクトの設定一式に名前を付けて保存する。
// 保存するのはプロジェクトスコープのファイルだけ（ユーザー全体設定は配る対象ではない）。
func CreateTemplate(conn *gorm.DB, name, note, project string) (*db.Template, error) {
	var files []db.ConfigFile
	if err := conn.Where("project = ? AND scope = ? AND broken = ?", project, string(config.ScopeProject), false).
		Order("rel_path asc").Find(&files).Error; err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("指定プロジェクトに保存できる設定がありません")
	}

	tmpl := db.Template{Name: name, Note: note}
	if err := conn.Create(&tmpl).Error; err != nil {
		return nil, err
	}

	saved := 0
	for _, f := range files {
		content, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		entry := db.TemplateEntry{
			TemplateID: tmpl.ID,
			RelPath:    projectRelPath(f.Path, f.Project),
			Kind:       f.Kind,
			Content:    content,
		}
		if err := conn.Create(&entry).Error; err != nil {
			return nil, err
		}
		saved++
	}

	tmpl.FileCount = saved
	if err := conn.Model(&db.Template{}).Where("id = ?", tmpl.ID).
		Update("file_count", saved).Error; err != nil {
		return nil, err
	}
	return &tmpl, nil
}

func ListTemplates(conn *gorm.DB) ([]db.Template, error) {
	var rows []db.Template
	err := conn.Order("name asc").Find(&rows).Error
	return rows, err
}

func DeleteTemplate(conn *gorm.DB, id uint) error {
	if err := conn.Delete(&db.TemplateEntry{}, "template_id = ?", id).Error; err != nil {
		return err
	}
	return conn.Delete(&db.Template{}, id).Error
}

// TemplateFile はテンプレートの中身一覧（内容そのものは返さない）。
type TemplateFile struct {
	RelPath string `json:"rel_path"`
	Kind    string `json:"kind"`
	Size    int    `json:"size"`
}

func TemplateFiles(conn *gorm.DB, id uint) ([]TemplateFile, error) {
	var rows []db.TemplateEntry
	if err := conn.Where("template_id = ?", id).Order("rel_path asc").Find(&rows).Error; err != nil {
		return nil, err
	}

	files := make([]TemplateFile, 0, len(rows))
	for _, r := range rows {
		files = append(files, TemplateFile{RelPath: r.RelPath, Kind: r.Kind, Size: len(r.Content)})
	}
	return files, nil
}

// ApplyTemplatePlan は適用で何が起きるかを、何も書き換えずに返す。
// targetDir は適用先プロジェクトのルートディレクトリ。
func ApplyTemplatePlan(conn *gorm.DB, id uint, targetDir string) ([]snapshot.Change, error) {
	entries, err := templateEntries(conn, id)
	if err != nil {
		return nil, err
	}

	changes := make([]snapshot.Change, 0, len(entries))
	for _, e := range entries {
		path := filepath.Join(targetDir, e.RelPath)

		change := snapshot.Change{Path: path, Kind: e.Kind, Project: filepath.Base(targetDir)}
		switch {
		case !fileExists(path):
			change.Type = snapshot.ChangeRecreate
		case sameContent(path, e.Content):
			change.Type = snapshot.ChangeUnchanged
		default:
			change.Type = snapshot.ChangeRestore
		}
		changes = append(changes, change)
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// ApplyResult はテンプレート適用の結果。
type ApplyResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	// BackupID は実行前の自動スナップショット。ここへ戻せば適用を取り消せる。
	BackupID uint     `json:"backup_id"`
	Errors   []string `json:"errors"`
}

// ApplyTemplate はテンプレートを適用先へ書き込む。
func ApplyTemplate(conn *gorm.DB, id uint, targetDir string) (*ApplyResult, error) {
	entries, err := templateEntries(conn, id)
	if err != nil {
		return nil, err
	}

	backup, err := snapshot.Create(conn, "テンプレート適用の直前", "ApplyTemplate実行時に自動作成")
	if err != nil {
		return nil, err
	}

	result := &ApplyResult{BackupID: backup.ID, Errors: []string{}}
	for _, e := range entries {
		path := filepath.Join(targetDir, e.RelPath)

		existed := fileExists(path)
		if existed && sameContent(path, e.Content) {
			result.Skipped++
			continue
		}
		if err := writeFile(path, e.Content); err != nil {
			result.Errors = append(result.Errors, path+": "+err.Error())
			continue
		}
		if existed {
			result.Updated++
		} else {
			result.Created++
		}
	}

	return result, nil
}

func templateEntries(conn *gorm.DB, id uint) ([]db.TemplateEntry, error) {
	var tmpl db.Template
	if err := conn.First(&tmpl, id).Error; err != nil {
		return nil, ErrTemplateNotFound
	}

	var entries []db.TemplateEntry
	err := conn.Where("template_id = ?", id).Order("rel_path asc").Find(&entries).Error
	return entries, err
}

// projectRelPath は絶対パスからプロジェクトルート相対のパスを取り出す。
//
// ConfigFile.RelPath は `.claude` ディレクトリを基準に取られているため
// （`.claude/settings.json` は "settings.json" になる）、
// そのまま適用先に繋ぐと `.claude/` が抜け落ちて別の場所に書いてしまう。
// テンプレートは「どこに置くか」が本質なので、プロジェクトルート基準に直して保存する。
func projectRelPath(absPath, project string) string {
	marker := string(filepath.Separator) + project + string(filepath.Separator)
	if i := strings.LastIndex(absPath, marker); i >= 0 {
		return filepath.ToSlash(absPath[i+len(marker):])
	}
	return filepath.Base(absPath)
}

// ─── 共有ヘルパー ─────────────────────────────────────────────────────────────

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sameContent(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// writeFile は既存のパーミッションを保ったまま書き込む。
func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	return os.WriteFile(path, content, perm)
}
