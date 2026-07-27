// Package snapshot は設定の状態を1バージョンとして保存し、復元する。
//
// 復元はこの製品で初めてユーザーのファイルを書き換える操作なので、
// 以下を原則とする:
//   - 復元前に必ず現状を自動スナップショットとして退避する（取り消せるようにする）
//   - 何が起きるかを Plan() で事前に見せられるようにする（ドライラン）
//   - 親ディレクトリがシンボリックリンクのファイルは、書くと離れた場所の実体が変わることを明示する
package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/chankei613/agent-config-manager/internal/db"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("スナップショットが見つかりません")

// maxEntrySize を超えるファイルはスナップショットに含めない。
// 設定ファイルは通常数KBで、巨大なものは設定ではない可能性が高い。
const maxEntrySize = 1 << 20 // 1MB

// Create は現在のインベントリに載っているファイルの内容を1バージョンとして保存する。
func Create(conn *gorm.DB, label, note string) (*db.Snapshot, error) {
	var files []db.ConfigFile
	if err := conn.Where("broken = ?", false).Order("path asc").Find(&files).Error; err != nil {
		return nil, err
	}

	snap := db.Snapshot{Label: label, Note: note}
	if err := conn.Create(&snap).Error; err != nil {
		return nil, err
	}

	saved := 0
	for _, f := range files {
		content, err := os.ReadFile(f.Path)
		if err != nil {
			continue // 読めないファイルは飛ばす。スナップショット全体を失敗させない
		}
		if len(content) > maxEntrySize {
			continue
		}

		entry := db.SnapshotEntry{
			SnapshotID: snap.ID,
			Path:       f.Path,
			Kind:       f.Kind,
			Project:    f.Project,
			Hash:       f.Hash,
			Content:    content,
		}
		if err := conn.Create(&entry).Error; err != nil {
			return nil, err
		}
		saved++
	}

	snap.FileCount = saved
	if err := conn.Model(&db.Snapshot{}).Where("id = ?", snap.ID).
		Update("file_count", saved).Error; err != nil {
		return nil, err
	}

	return &snap, nil
}

func List(conn *gorm.DB) ([]db.Snapshot, error) {
	var snaps []db.Snapshot
	err := conn.Order("id desc").Find(&snaps).Error
	return snaps, err
}

func Get(conn *gorm.DB, id uint) (*db.Snapshot, error) {
	var snap db.Snapshot
	if err := conn.First(&snap, id).Error; err != nil {
		return nil, ErrNotFound
	}
	return &snap, nil
}

func Delete(conn *gorm.DB, id uint) error {
	if err := conn.Delete(&db.SnapshotEntry{}, "snapshot_id = ?", id).Error; err != nil {
		return err
	}
	return conn.Delete(&db.Snapshot{}, id).Error
}

// ChangeType は復元によって各ファイルに起きること。
type ChangeType string

const (
	ChangeRestore   ChangeType = "restore"   // 内容が違うので書き戻す
	ChangeRecreate  ChangeType = "recreate"  // 現在存在しないので作り直す
	ChangeUnchanged ChangeType = "unchanged" // 同じなので触らない
	// ChangeExtra は「今あるがスナップショットに無い」ファイル。
	// 削除は行わない（消して困るより残って困る方が軽い）。報告だけする。
	ChangeExtra ChangeType = "extra"
)

// Change は復元時の1ファイル分の予定。
type Change struct {
	Type    ChangeType `json:"type"`
	Path    string     `json:"path"`
	Kind    string     `json:"kind"`
	Project string     `json:"project"`
	// ViaSymlink が true なら、このパスへの書き込みは離れた場所の実体に届く。
	ViaSymlink bool   `json:"via_symlink"`
	RealPath   string `json:"real_path,omitempty"`
}

// Plan は復元で何が起きるかを、実際には何も書き換えずに返す（ドライラン）。
func Plan(conn *gorm.DB, snapshotID uint) ([]Change, error) {
	if _, err := Get(conn, snapshotID); err != nil {
		return nil, err
	}

	var entries []db.SnapshotEntry
	if err := conn.Where("snapshot_id = ?", snapshotID).Order("path asc").Find(&entries).Error; err != nil {
		return nil, err
	}

	// 現在のインベントリを引くための索引
	var current []db.ConfigFile
	if err := conn.Find(&current).Error; err != nil {
		return nil, err
	}
	byPath := make(map[string]db.ConfigFile, len(current))
	for _, f := range current {
		byPath[f.Path] = f
	}

	changes := make([]Change, 0, len(entries))
	inSnapshot := make(map[string]bool, len(entries))

	for _, e := range entries {
		inSnapshot[e.Path] = true

		change := Change{Path: e.Path, Kind: e.Kind, Project: e.Project}
		if f, ok := byPath[e.Path]; ok {
			change.ViaSymlink = f.ViaSymlink
			change.RealPath = f.RealPath
		}

		switch {
		case !fileExists(e.Path):
			change.Type = ChangeRecreate
		case sameContent(e.Path, e.Content):
			change.Type = ChangeUnchanged
		default:
			change.Type = ChangeRestore
		}
		changes = append(changes, change)
	}

	// スナップショット後に増えたファイルは消さずに報告するだけ
	for _, f := range current {
		if !inSnapshot[f.Path] && !f.Broken {
			changes = append(changes, Change{
				Type:       ChangeExtra,
				Path:       f.Path,
				Kind:       f.Kind,
				Project:    f.Project,
				ViaSymlink: f.ViaSymlink,
				RealPath:   f.RealPath,
			})
		}
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// RestoreResult は復元の結果。
type RestoreResult struct {
	Restored  int `json:"restored"`
	Recreated int `json:"recreated"`
	Skipped   int `json:"skipped"`
	// BackupID は復元直前に自動で取ったスナップショット。ここに戻せば復元を取り消せる。
	BackupID uint     `json:"backup_id"`
	Errors   []string `json:"errors"`
}

// Restore はスナップショットの内容をディスクに書き戻す。
// 実行前に必ず現状を自動スナップショットとして保存するので、失敗しても戻せる。
func Restore(conn *gorm.DB, snapshotID uint) (*RestoreResult, error) {
	snap, err := Get(conn, snapshotID)
	if err != nil {
		return nil, err
	}

	// 取り消せるようにしてから書く
	label := fmt.Sprintf("復元前の自動バックアップ（#%d %s へ戻す直前）", snap.ID, snap.Label)
	backup, err := Create(conn, label, "Restore実行時に自動作成")
	if err != nil {
		return nil, fmt.Errorf("自動バックアップに失敗したため復元を中止しました: %w", err)
	}

	var entries []db.SnapshotEntry
	if err := conn.Where("snapshot_id = ?", snapshotID).Order("path asc").Find(&entries).Error; err != nil {
		return nil, err
	}

	result := &RestoreResult{BackupID: backup.ID, Errors: []string{}}

	for _, e := range entries {
		existed := fileExists(e.Path)
		if existed && sameContent(e.Path, e.Content) {
			result.Skipped++
			continue
		}

		if err := writeFile(e.Path, e.Content); err != nil {
			result.Errors = append(result.Errors, e.Path+": "+err.Error())
			continue
		}
		if existed {
			result.Restored++
		} else {
			result.Recreated++
		}
	}

	return result, nil
}

// writeFile は元のパーミッションを保ったまま書き戻す。
// パスがシンボリックリンク経由でも、そのまま書けば実体に届く（意図した挙動）。
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

// Entry は差分表示用（内容は含めない。機密が混ざりうるため）。
type Entry struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	Project   string    `json:"project"`
	Hash      string    `json:"hash"`
	Size      int       `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// Entries はスナップショットに含まれるファイル一覧を返す。
func Entries(conn *gorm.DB, snapshotID uint) ([]Entry, error) {
	var rows []db.SnapshotEntry
	if err := conn.Where("snapshot_id = ?", snapshotID).Order("path asc").Find(&rows).Error; err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, Entry{
			Path:    r.Path,
			Kind:    r.Kind,
			Project: r.Project,
			Hash:    r.Hash,
			Size:    len(r.Content),
		})
	}
	return entries, nil
}
