// Package db は Agent Config Manager のGORMモデルとSQLite初期化を提供する。
// A/B/K の既存製品と同じ GORM + SQLite の構成に揃えてある（統合時の摩擦を減らすため）。
package db

import "time"

// ConfigRoot はスキャン対象のルート。ユーザーが「ここを見て」と登録する。
type ConfigRoot struct {
	ID    uint   `gorm:"primaryKey" json:"id"`
	Path  string `gorm:"uniqueIndex;not null" json:"path"`
	Label string `json:"label"`
	// Scope が "user" の場合はその .claude ディレクトリ自体、
	// "project" の場合は配下の各ディレクトリをプロジェクトとして走査する。
	Scope   string `json:"scope"`
	Enabled bool   `json:"enabled"`

	LastScannedAt *time.Time `json:"last_scanned_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ConfigFile はインベントリ1件。config.File をそのまま永続化した形。
// Path で一意にし、再スキャンのたびに上書きする（履歴は Snapshot 側が持つ）。
type ConfigFile struct {
	ID uint `gorm:"primaryKey" json:"id"`

	Scope   string `gorm:"index" json:"scope"`
	Kind    string `gorm:"index" json:"kind"`
	Project string `gorm:"index" json:"project"`

	Path    string `gorm:"uniqueIndex;not null" json:"path"`
	RelPath string `json:"rel_path"`

	Size    int64     `json:"size"`
	Hash    string    `gorm:"index" json:"hash"` // 同一内容の検出に使うのでindexを張る
	ModTime time.Time `json:"mod_time"`

	IsSymlink     bool   `json:"is_symlink"`
	SymlinkTarget string `json:"symlink_target"`
	Broken        bool   `gorm:"index" json:"broken"`

	// RealPath / ViaSymlink は書き込み系（Phase 4）で実体を壊さないために持つ。
	RealPath   string `json:"real_path"`
	ViaSymlink bool   `gorm:"index" json:"via_symlink"`

	ScannedAt time.Time `json:"scanned_at"`
}

// Snapshot は「ある時点の全設定」を1バージョンとして束ねたもの（Phase 3のロールバック用）。
type Snapshot struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Label     string    `json:"label"`
	Note      string    `json:"note"`
	FileCount int       `json:"file_count"`
	CreatedAt time.Time `json:"created_at"`
}

// Template はよく使う設定セットに名前を付けて保存したもの。
// 新規プロジェクトに同じ構成を配るために使う。
type Template struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	Note      string    `json:"note"`
	FileCount int       `json:"file_count"`
	CreatedAt time.Time `json:"created_at"`
}

// TemplateEntry はテンプレート1件分の中身。
// RelPath はプロジェクトルートからの相対パス（例: "CLAUDE.md", ".claude/settings.json"）で、
// 適用先が変わっても同じ位置に置けるようにしている。
type TemplateEntry struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	TemplateID uint   `gorm:"index;not null" json:"template_id"`
	RelPath    string `json:"rel_path"`
	Kind       string `json:"kind"`
	Content    []byte `json:"-"` // APIレスポンスには載せない（機密が混ざりうるため）
}

// SnapshotEntry はスナップショット時点のファイル内容そのもの。
// 設定ファイルは小さいので、パスとハッシュだけでなく中身を持つことでロールバックを自己完結させる。
type SnapshotEntry struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	SnapshotID uint   `gorm:"index;not null" json:"snapshot_id"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Project    string `json:"project"`
	Hash       string `json:"hash"`
	Content    []byte `json:"-"` // APIレスポンスには載せない（機密が混ざりうるため）
}
