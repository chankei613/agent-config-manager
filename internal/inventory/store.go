// Package inventory はスキャン結果をDBのインベントリへ反映する。
// config（発見）と db（永続化）の橋渡しが役割。
package inventory

import (
	"strings"
	"time"

	"github.com/chankei613/agent-config-manager/internal/config"
	"github.com/chankei613/agent-config-manager/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SyncStats は Sync の結果。UIに「何が変わったか」を出すために使う。
type SyncStats struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
	Total   int `json:"total"`
}

// Sync はスキャン結果をインベントリに反映する。
//
// coveredRoots は「今回のスキャンが見た範囲」。この配下にありながら結果に出てこなかった
// 行は削除された設定とみなして消す。範囲を明示的に受け取るのは、
// 別のルートを対象にしたスキャンが無関係な行を巻き込んで消すのを防ぐため。
func Sync(conn *gorm.DB, res *config.Result, coveredRoots ...string) (*SyncStats, error) {
	stats := &SyncStats{Total: len(res.Files)}
	now := time.Now()

	seen := make(map[string]bool, len(res.Files))

	for _, f := range res.Files {
		seen[f.Path] = true

		var existing db.ConfigFile
		found := conn.Where("path = ?", f.Path).First(&existing).Error == nil

		row := db.ConfigFile{
			Scope:         string(f.Scope),
			Kind:          string(f.Kind),
			Project:       f.Project,
			Path:          f.Path,
			RelPath:       f.RelPath,
			Size:          f.Size,
			Hash:          f.Hash,
			ModTime:       f.ModTime,
			IsSymlink:     f.IsSymlink,
			SymlinkTarget: f.SymlinkTarget,
			Broken:        f.Broken,
			RealPath:      f.RealPath,
			ViaSymlink:    f.ViaSymlink,
			ScannedAt:     now,
		}

		// Path を競合キーにしたupsert。再スキャンのたびに最新状態で上書きする。
		if err := conn.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "path"}},
			UpdateAll: true,
		}).Create(&row).Error; err != nil {
			return nil, err
		}

		switch {
		case !found:
			stats.Added++
		case existing.Hash != f.Hash:
			stats.Updated++
		}
	}

	removed, err := prune(conn, seen, coveredRoots)
	if err != nil {
		return nil, err
	}
	stats.Removed = removed

	return stats, nil
}

// prune はスキャン範囲内にありながら今回見つからなかった行を消す。
func prune(conn *gorm.DB, seen map[string]bool, coveredRoots []string) (int, error) {
	if len(coveredRoots) == 0 {
		return 0, nil // 範囲が分からないときは何も消さない（消しすぎるより残す方が安全）
	}

	var rows []db.ConfigFile
	if err := conn.Find(&rows).Error; err != nil {
		return 0, err
	}

	var stale []uint
	for _, row := range rows {
		if seen[row.Path] {
			continue
		}
		if withinAny(row.Path, coveredRoots) {
			stale = append(stale, row.ID)
		}
	}

	if len(stale) == 0 {
		return 0, nil
	}
	if err := conn.Delete(&db.ConfigFile{}, stale).Error; err != nil {
		return 0, err
	}
	return len(stale), nil
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		if strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/") || path == root {
			return true
		}
	}
	return false
}

// Duplicates は「同じ種別なのに内容が違う」設定をプロジェクト横断で集める。
// 「設定が散逸している」という痛点をそのまま可視化するための問い合わせ。
func Duplicates(conn *gorm.DB, kind string) (map[string][]db.ConfigFile, error) {
	var rows []db.ConfigFile
	if err := conn.Where("kind = ?", kind).Order("project asc, path asc").Find(&rows).Error; err != nil {
		return nil, err
	}

	byHash := make(map[string][]db.ConfigFile)
	for _, row := range rows {
		byHash[row.Hash] = append(byHash[row.Hash], row)
	}
	return byHash, nil
}

// Orphans は実体を失ったシンボリックリンクを返す。
// workers は実体がObsidian側にあるため、リンク切れは「定義が消えた」ことを意味する。
func Orphans(conn *gorm.DB) ([]db.ConfigFile, error) {
	var rows []db.ConfigFile
	err := conn.Where("broken = ?", true).Order("path asc").Find(&rows).Error
	return rows, err
}
