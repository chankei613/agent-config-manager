package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chankei613/agent-config-manager/internal/config"
	"github.com/chankei613/agent-config-manager/internal/db"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	conn, err := db.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	return conn
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scanProjects(t *testing.T, root string) *config.Result {
	t.Helper()
	res, err := config.ScanProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSyncAddsUpdatesAndRemoves(t *testing.T) {
	conn := newTestDB(t)
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "version 1")
	writeFile(t, filepath.Join(root, "proj-b", "CLAUDE.md"), "b")

	stats, err := Sync(conn, scanProjects(t, root), root)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 2 || stats.Updated != 0 || stats.Removed != 0 {
		t.Fatalf("first sync: %+v, want 2 added", stats)
	}

	// 内容を変える → Updated として数えられる
	writeFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "version 2")
	stats, err = Sync(conn, scanProjects(t, root), root)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 0 || stats.Updated != 1 {
		t.Errorf("after edit: %+v, want 1 updated", stats)
	}

	// 消す → Removed として数えられ、インベントリからも消える
	if err := os.Remove(filepath.Join(root, "proj-b", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	stats, err = Sync(conn, scanProjects(t, root), root)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Errorf("after delete: %+v, want 1 removed", stats)
	}

	var count int64
	conn.Model(&db.ConfigFile{}).Count(&count)
	if count != 1 {
		t.Errorf("got %d rows, want 1", count)
	}
}

// スキャン範囲外の行を巻き込んで消さないこと。
func TestSyncDoesNotPruneOutsideCoveredRoots(t *testing.T) {
	conn := newTestDB(t)
	rootA := t.TempDir()
	rootB := t.TempDir()

	writeFile(t, filepath.Join(rootA, "proj-a", "CLAUDE.md"), "a")
	writeFile(t, filepath.Join(rootB, "proj-b", "CLAUDE.md"), "b")

	if _, err := Sync(conn, scanProjects(t, rootA), rootA); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(conn, scanProjects(t, rootB), rootB); err != nil {
		t.Fatal(err)
	}

	var count int64
	conn.Model(&db.ConfigFile{}).Count(&count)
	if count != 2 {
		t.Fatalf("got %d rows, want 2 — the second scan must not prune the first root", count)
	}
}

// 範囲が渡されないときは何も消さない（消しすぎるより残す方が安全）
func TestSyncWithoutCoveredRootsPrunesNothing(t *testing.T) {
	conn := newTestDB(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "a")

	if _, err := Sync(conn, scanProjects(t, root), root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "proj-a", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	stats, err := Sync(conn, scanProjects(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 0 {
		t.Errorf("got %d removed, want 0 when no roots are given", stats.Removed)
	}
}

// 「設定が散逸している」ことの可視化 — 同じ種別で内容が割れているのを検出する
func TestDuplicatesGroupsByContent(t *testing.T) {
	conn := newTestDB(t)
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "shared rules")
	writeFile(t, filepath.Join(root, "proj-b", "CLAUDE.md"), "shared rules")
	writeFile(t, filepath.Join(root, "proj-c", "CLAUDE.md"), "drifted rules")

	if _, err := Sync(conn, scanProjects(t, root), root); err != nil {
		t.Fatal(err)
	}

	groups, err := Duplicates(conn, string(config.KindClaudeMD))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d content groups, want 2 (2つが同一・1つが乖離)", len(groups))
	}

	sizes := map[int]int{}
	for _, files := range groups {
		sizes[len(files)]++
	}
	if sizes[2] != 1 || sizes[1] != 1 {
		t.Errorf("expected one group of 2 and one of 1, got %+v", sizes)
	}
}

func TestOrphansReportsBrokenSymlinks(t *testing.T) {
	conn := newTestDB(t)
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")

	if err := os.MkdirAll(filepath.Join(claude, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(claude, "agents", "alive.md"), "ok")
	if err := os.Symlink(filepath.Join(dir, "missing.md"), filepath.Join(claude, "agents", "dead.md")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := config.ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(conn, res, claude); err != nil {
		t.Fatal(err)
	}

	orphans, err := Orphans(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 {
		t.Fatalf("got %d orphans, want 1: %+v", len(orphans), orphans)
	}
	if filepath.Base(orphans[0].Path) != "dead.md" {
		t.Errorf("got orphan %q, want dead.md", orphans[0].Path)
	}
}
