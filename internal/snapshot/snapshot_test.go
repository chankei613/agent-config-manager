package snapshot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chankei613/agent-config-manager/internal/config"
	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/inventory"
	"gorm.io/gorm"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// fixture はプロジェクト群を作ってインベントリに取り込む。
func fixture(t *testing.T, files map[string]string) (*gorm.DB, string) {
	t.Helper()

	conn, err := db.Init(filepath.Join(t.TempDir(), "snap.db"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for rel, content := range files {
		writeTestFile(t, filepath.Join(root, rel), content)
	}
	rescan(t, conn, root)
	return conn, root
}

func rescan(t *testing.T, conn *gorm.DB, root string) {
	t.Helper()
	res, err := config.ScanProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inventory.Sync(conn, res, root); err != nil {
		t.Fatal(err)
	}
}

func TestCreateCapturesContent(t *testing.T) {
	conn, _ := fixture(t, map[string]string{
		"proj-a/CLAUDE.md":             "rules v1",
		"proj-a/.claude/settings.json": `{"model":"opus"}`,
	})

	snap, err := Create(conn, "v1", "初期状態")
	if err != nil {
		t.Fatal(err)
	}
	if snap.FileCount != 2 {
		t.Errorf("got %d files, want 2", snap.FileCount)
	}

	entries, err := Entries(conn, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	// 一覧に内容そのものは載せない（機密が混ざりうるため）
	for _, e := range entries {
		if e.Size == 0 {
			t.Errorf("%s: サイズが記録されていない", e.Path)
		}
	}
}

func TestPlanDetectsEachChangeType(t *testing.T) {
	conn, root := fixture(t, map[string]string{
		"proj-a/CLAUDE.md":             "original",
		"proj-a/.claude/settings.json": "{}",
	})

	snap, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}

	// 1件を書き換え、1件を削除し、1件を新規追加する
	writeTestFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "edited")
	if err := os.Remove(filepath.Join(root, "proj-a", ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "proj-b", "CLAUDE.md"), "brand new")
	rescan(t, conn, root)

	changes, err := Plan(conn, snap.ID)
	if err != nil {
		t.Fatal(err)
	}

	got := map[ChangeType]int{}
	for _, c := range changes {
		got[c.Type]++
	}
	if got[ChangeRestore] != 1 {
		t.Errorf("書き換えられた1件が restore になるはず: %+v", changes)
	}
	if got[ChangeRecreate] != 1 {
		t.Errorf("削除された1件が recreate になるはず: %+v", changes)
	}
	if got[ChangeExtra] != 1 {
		t.Errorf("新規追加の1件が extra になるはず: %+v", changes)
	}
}

func TestPlanDoesNotTouchDisk(t *testing.T) {
	conn, root := fixture(t, map[string]string{"proj-a/CLAUDE.md": "original"})

	snap, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "edited")

	if _, err := Plan(conn, snap.ID); err != nil {
		t.Fatal(err)
	}

	// ドライランなのでファイルは変わっていないこと
	if got := readFile(t, filepath.Join(root, "proj-a", "CLAUDE.md")); got != "edited" {
		t.Errorf("Planがファイルを書き換えている: %q", got)
	}
}

func TestRestoreRevertsContent(t *testing.T) {
	conn, root := fixture(t, map[string]string{"proj-a/CLAUDE.md": "original"})
	path := filepath.Join(root, "proj-a", "CLAUDE.md")

	snap, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, "accidentally changed")

	result, err := Restore(conn, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Restored != 1 {
		t.Errorf("got %d restored, want 1: %+v", result.Restored, result)
	}
	if got := readFile(t, path); got != "original" {
		t.Errorf("got %q, want original", got)
	}
}

func TestRestoreRecreatesDeletedFile(t *testing.T) {
	conn, root := fixture(t, map[string]string{"proj-a/CLAUDE.md": "original"})
	path := filepath.Join(root, "proj-a", "CLAUDE.md")

	snap, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	result, err := Restore(conn, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Recreated != 1 {
		t.Errorf("got %d recreated, want 1", result.Recreated)
	}
	if got := readFile(t, path); got != "original" {
		t.Errorf("got %q, want original", got)
	}
}

// 復元は取り消せなければならない。実行前の自動バックアップがその手段。
func TestRestoreIsUndoable(t *testing.T) {
	conn, root := fixture(t, map[string]string{"proj-a/CLAUDE.md": "version 1"})
	path := filepath.Join(root, "proj-a", "CLAUDE.md")

	v1, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}

	// v2 の状態にしてから v1 へ戻す
	writeTestFile(t, path, "version 2")
	rescan(t, conn, root)

	result, err := Restore(conn, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, path) != "version 1" {
		t.Fatal("v1へ戻っていない")
	}
	if result.BackupID == 0 {
		t.Fatal("自動バックアップが作られていない")
	}

	// 自動バックアップへ戻せば復元前（version 2）に帰れる
	if _, err := Restore(conn, result.BackupID); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "version 2" {
		t.Errorf("復元を取り消せていない: got %q, want version 2", got)
	}
}

func TestRestoreSkipsUnchangedFiles(t *testing.T) {
	conn, _ := fixture(t, map[string]string{"proj-a/CLAUDE.md": "same"})

	snap, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}

	result, err := Restore(conn, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Restored != 0 || result.Skipped != 1 {
		t.Errorf("変化のないファイルは触らないはず: %+v", result)
	}
}

// スナップショット後に増えたファイルを勝手に消さないこと
func TestRestoreDoesNotDeleteExtraFiles(t *testing.T) {
	conn, root := fixture(t, map[string]string{"proj-a/CLAUDE.md": "original"})

	snap, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}

	extra := filepath.Join(root, "proj-b", "CLAUDE.md")
	writeTestFile(t, extra, "added later")
	rescan(t, conn, root)

	if _, err := Restore(conn, snap.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extra); err != nil {
		t.Error("スナップショットに無いファイルを削除してはいけない")
	}
}

// リンク経由のファイルへ書くと実体が更新されること（意図した挙動の確認）
func TestRestoreWritesThroughSymlinkToRealFile(t *testing.T) {
	conn, err := db.Init(filepath.Join(t.TempDir(), "snap.db"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	realDir := filepath.Join(dir, "obsidian", "workers")
	realFile := filepath.Join(realDir, "worker-a.md")

	writeTestFile(t, realFile, "original definition")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(claude, "agents")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := config.ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inventory.Sync(conn, res, claude); err != nil {
		t.Fatal(err)
	}

	snap, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}

	// 実体側を書き換えてから復元する
	writeTestFile(t, realFile, "changed")

	// Plan がリンク経由であることを申告すること
	changes, err := Plan(conn, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].ViaSymlink {
		t.Errorf("リンク経由であることを申告するべき: %+v", changes)
	}

	if _, err := Restore(conn, snap.ID); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, realFile); got != "original definition" {
		t.Errorf("実体が復元されていない: %q", got)
	}
}

func TestDeleteRemovesEntries(t *testing.T) {
	conn, _ := fixture(t, map[string]string{"proj-a/CLAUDE.md": "x"})

	snap, err := Create(conn, "v1", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := Delete(conn, snap.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := Get(conn, snap.ID); err != ErrNotFound {
		t.Error("削除後は見つからないはず")
	}
	var count int64
	conn.Model(&db.SnapshotEntry{}).Where("snapshot_id = ?", snap.ID).Count(&count)
	if count != 0 {
		t.Errorf("エントリが残っている: %d", count)
	}
}

func TestPlanRejectsUnknownSnapshot(t *testing.T) {
	conn, _ := fixture(t, map[string]string{"proj-a/CLAUDE.md": "x"})

	if _, err := Plan(conn, 999); err != ErrNotFound {
		t.Errorf("存在しないIDは ErrNotFound を返すべき: %v", err)
	}
}
