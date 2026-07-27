// Package sync は散逸した設定を揃える（統一する）。
//
// Phase 2 の乖離検出が「どこがズレているか」を示すのに対し、ここは「揃える」側。
// 書き込みを伴うため、snapshot パッケージと同じ安全原則に従う:
//   - Plan で何が起きるかを事前に返す（ディスクには触れない）
//   - 実行前に必ず自動スナップショットを取って取り消せるようにする
package sync

import (
	"errors"
	"os"
	"sort"

	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/snapshot"
	"gorm.io/gorm"
)

var (
	ErrSourceNotFound = errors.New("統一元のファイルが見つかりません")
	ErrNoTargets      = errors.New("統一先がありません")
)

// UnifyPlan は統一で各ファイルに起きることを、何も書き換えずに返す。
// sourcePath の内容を、同じ identity（種別＋相対パス）を持つ他の場所へ配る想定。
func UnifyPlan(conn *gorm.DB, sourcePath string) ([]snapshot.Change, error) {
	source, targets, err := resolveTargets(conn, sourcePath)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(source.Path)
	if err != nil {
		return nil, ErrSourceNotFound
	}

	changes := make([]snapshot.Change, 0, len(targets))
	for _, t := range targets {
		change := snapshot.Change{
			Path:       t.Path,
			Kind:       t.Kind,
			Project:    t.Project,
			ViaSymlink: t.ViaSymlink,
			RealPath:   t.RealPath,
		}

		switch {
		case !fileExists(t.Path):
			change.Type = snapshot.ChangeRecreate
		case sameContent(t.Path, content):
			change.Type = snapshot.ChangeUnchanged
		default:
			change.Type = snapshot.ChangeRestore
		}
		changes = append(changes, change)
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// UnifyResult は統一の結果。
type UnifyResult struct {
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	// BackupID は実行前に自動で取ったスナップショット。ここへ戻せば統一を取り消せる。
	BackupID uint     `json:"backup_id"`
	Errors   []string `json:"errors"`
}

// Unify は sourcePath の内容を、同じ identity を持つ他の場所すべてへ書き込む。
func Unify(conn *gorm.DB, sourcePath string) (*UnifyResult, error) {
	source, targets, err := resolveTargets(conn, sourcePath)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(source.Path)
	if err != nil {
		return nil, ErrSourceNotFound
	}

	backup, err := snapshot.Create(conn, "統一の直前", "Unify実行時に自動作成")
	if err != nil {
		return nil, err
	}

	result := &UnifyResult{BackupID: backup.ID, Errors: []string{}}
	for _, t := range targets {
		if fileExists(t.Path) && sameContent(t.Path, content) {
			result.Skipped++
			continue
		}
		if err := writeFile(t.Path, content); err != nil {
			result.Errors = append(result.Errors, t.Path+": "+err.Error())
			continue
		}
		result.Updated++
	}

	return result, nil
}

// resolveTargets は統一元と、その配布先（同じ種別・相対パス・スコープを持つ他のファイル）を返す。
//
// スコープを揃えるのは Phase 2 の乖離検出と同じ理由。
// ユーザー全体設定とプロジェクト設定は役割が違うので、混ぜて統一してはいけない。
func resolveTargets(conn *gorm.DB, sourcePath string) (*db.ConfigFile, []db.ConfigFile, error) {
	var source db.ConfigFile
	if err := conn.First(&source, "path = ?", sourcePath).Error; err != nil {
		return nil, nil, ErrSourceNotFound
	}

	var siblings []db.ConfigFile
	if err := conn.Where(
		"kind = ? AND rel_path = ? AND scope = ? AND path != ? AND broken = ?",
		source.Kind, source.RelPath, source.Scope, source.Path, false,
	).Order("path asc").Find(&siblings).Error; err != nil {
		return nil, nil, err
	}

	if len(siblings) == 0 {
		return nil, nil, ErrNoTargets
	}
	return &source, siblings, nil
}
