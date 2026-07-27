package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chankei613/agent-config-manager/internal/config"
	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/inventory"
	"github.com/chankei613/agent-config-manager/internal/snapshot"
	"gorm.io/gorm"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func fixture(t *testing.T, files map[string]string) (*gorm.DB, string) {
	t.Helper()

	conn, err := db.Init(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for rel, content := range files {
		write(t, filepath.Join(root, rel), content)
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

// ─── 統一 ─────────────────────────────────────────────────────────────────────

func TestUnifyPlanShowsWhatWouldChange(t *testing.T) {
	conn, root := fixture(t, map[string]string{
		"proj-a/CLAUDE.md": "正しいルール",
		"proj-b/CLAUDE.md": "ズレたルール",
		"proj-c/CLAUDE.md": "正しいルール",
	})
	source := filepath.Join(root, "proj-a", "CLAUDE.md")

	changes, err := UnifyPlan(conn, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("統一元以外の2件が対象: %+v", changes)
	}

	byProject := map[string]snapshot.ChangeType{}
	for _, c := range changes {
		byProject[c.Project] = c.Type
	}
	if byProject["proj-b"] != snapshot.ChangeRestore {
		t.Errorf("proj-b は書き換え対象のはず: %v", byProject)
	}
	if byProject["proj-c"] != snapshot.ChangeUnchanged {
		t.Errorf("proj-c は同内容なので変更なしのはず: %v", byProject)
	}

	// ドライランなのでディスクは無変更
	if read(t, filepath.Join(root, "proj-b", "CLAUDE.md")) != "ズレたルール" {
		t.Error("Planがファイルを書き換えている")
	}
}

func TestUnifyWritesSourceToOthers(t *testing.T) {
	conn, root := fixture(t, map[string]string{
		"proj-a/CLAUDE.md": "正しいルール",
		"proj-b/CLAUDE.md": "ズレたルール",
		"proj-c/CLAUDE.md": "正しいルール",
	})

	result, err := Unify(conn, filepath.Join(root, "proj-a", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 || result.Skipped != 1 {
		t.Errorf("1件更新・1件スキップのはず: %+v", result)
	}
	if got := read(t, filepath.Join(root, "proj-b", "CLAUDE.md")); got != "正しいルール" {
		t.Errorf("統一されていない: %q", got)
	}
	// 統一元は当然そのまま
	if got := read(t, filepath.Join(root, "proj-a", "CLAUDE.md")); got != "正しいルール" {
		t.Errorf("統一元が変わっている: %q", got)
	}
}

// 統一も取り消せなければならない
func TestUnifyIsUndoable(t *testing.T) {
	conn, root := fixture(t, map[string]string{
		"proj-a/CLAUDE.md": "正しいルール",
		"proj-b/CLAUDE.md": "ズレたルール",
	})
	target := filepath.Join(root, "proj-b", "CLAUDE.md")

	result, err := Unify(conn, filepath.Join(root, "proj-a", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if read(t, target) != "正しいルール" {
		t.Fatal("統一されていない")
	}
	if result.BackupID == 0 {
		t.Fatal("自動バックアップが作られていない")
	}

	if _, err := snapshot.Restore(conn, result.BackupID); err != nil {
		t.Fatal(err)
	}
	if got := read(t, target); got != "ズレたルール" {
		t.Errorf("統一を取り消せていない: %q", got)
	}
}

// スコープが違うものを混ぜて統一しないこと（ユーザー全体設定とプロジェクト設定は役割が違う）
func TestUnifyDoesNotCrossScopes(t *testing.T) {
	conn, err := db.Init(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}

	userDir := t.TempDir()
	claude := filepath.Join(userDir, ".claude")
	write(t, filepath.Join(claude, "settings.json"), `{"scope":"user"}`)

	projectRoot := t.TempDir()
	write(t, filepath.Join(projectRoot, "proj-a", ".claude", "settings.json"), `{"scope":"project"}`)

	userRes, _ := config.ScanUserScope(claude)
	projRes, _ := config.ScanProjects(projectRoot)
	combined := &config.Result{Files: append(userRes.Files, projRes.Files...)}
	if _, err := inventory.Sync(conn, combined, claude, projectRoot); err != nil {
		t.Fatal(err)
	}

	// ユーザー設定を統一元にしても、プロジェクト設定は配布先にならない
	if _, err := UnifyPlan(conn, filepath.Join(claude, "settings.json")); err != ErrNoTargets {
		t.Errorf("スコープを跨いで統一してはいけない: %v", err)
	}
}

func TestUnifyRejectsUnknownSource(t *testing.T) {
	conn, _ := fixture(t, map[string]string{"proj-a/CLAUDE.md": "x"})

	if _, err := UnifyPlan(conn, "/nowhere/CLAUDE.md"); err != ErrSourceNotFound {
		t.Errorf("got %v, want ErrSourceNotFound", err)
	}
}

func TestUnifyRejectsWhenNothingToUnify(t *testing.T) {
	conn, root := fixture(t, map[string]string{"proj-a/CLAUDE.md": "only one"})

	if _, err := UnifyPlan(conn, filepath.Join(root, "proj-a", "CLAUDE.md")); err != ErrNoTargets {
		t.Errorf("配布先が無いときは ErrNoTargets: %v", err)
	}
}

// ─── テンプレート ─────────────────────────────────────────────────────────────

func TestCreateAndApplyTemplate(t *testing.T) {
	conn, root := fixture(t, map[string]string{
		"proj-a/CLAUDE.md":             "共通ルール",
		"proj-a/.claude/settings.json": `{"model":"opus"}`,
	})

	tmpl, err := CreateTemplate(conn, "標準構成", "新規プロジェクト用", "proj-a")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.FileCount != 2 {
		t.Fatalf("got %d files, want 2", tmpl.FileCount)
	}

	files, err := TemplateFiles(conn, tmpl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2", len(files))
	}

	// まっさらなプロジェクトへ適用する
	target := filepath.Join(root, "proj-new")
	changes, err := ApplyTemplatePlan(conn, tmpl.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Type != snapshot.ChangeRecreate {
			t.Errorf("新規なので全部 recreate のはず: %+v", c)
		}
	}
	// ドライランでは作られない
	if _, err := os.Stat(filepath.Join(target, "CLAUDE.md")); err == nil {
		t.Error("Planがファイルを作っている")
	}

	result, err := ApplyTemplate(conn, tmpl.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 2 {
		t.Errorf("got %d created, want 2: %+v", result.Created, result)
	}
	if got := read(t, filepath.Join(target, "CLAUDE.md")); got != "共通ルール" {
		t.Errorf("適用されていない: %q", got)
	}
	if got := read(t, filepath.Join(target, ".claude", "settings.json")); got != `{"model":"opus"}` {
		t.Errorf("ネストしたパスが適用されていない: %q", got)
	}
}

func TestApplyTemplateIsUndoable(t *testing.T) {
	conn, root := fixture(t, map[string]string{
		"proj-a/CLAUDE.md": "テンプレの中身",
		"proj-b/CLAUDE.md": "既存の中身",
	})

	tmpl, err := CreateTemplate(conn, "t", "", "proj-a")
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, "proj-b")
	result, err := ApplyTemplate(conn, tmpl.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 {
		t.Errorf("既存ファイルの上書きは updated: %+v", result)
	}
	if read(t, filepath.Join(target, "CLAUDE.md")) != "テンプレの中身" {
		t.Fatal("適用されていない")
	}

	if _, err := snapshot.Restore(conn, result.BackupID); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(target, "CLAUDE.md")); got != "既存の中身" {
		t.Errorf("適用を取り消せていない: %q", got)
	}
}

func TestApplyTemplateSkipsIdenticalFiles(t *testing.T) {
	conn, root := fixture(t, map[string]string{
		"proj-a/CLAUDE.md": "同じ内容",
		"proj-b/CLAUDE.md": "同じ内容",
	})

	tmpl, err := CreateTemplate(conn, "t", "", "proj-a")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ApplyTemplate(conn, tmpl.ID, filepath.Join(root, "proj-b"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || result.Updated != 0 {
		t.Errorf("同内容なら触らないはず: %+v", result)
	}
}

func TestDeleteTemplate(t *testing.T) {
	conn, _ := fixture(t, map[string]string{"proj-a/CLAUDE.md": "x"})

	tmpl, err := CreateTemplate(conn, "t", "", "proj-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteTemplate(conn, tmpl.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := ApplyTemplatePlan(conn, tmpl.ID, t.TempDir()); err != ErrTemplateNotFound {
		t.Errorf("削除後は見つからないはず: %v", err)
	}
	var count int64
	conn.Model(&db.TemplateEntry{}).Where("template_id = ?", tmpl.ID).Count(&count)
	if count != 0 {
		t.Errorf("エントリが残っている: %d", count)
	}
}

func TestCreateTemplateRejectsEmptyProject(t *testing.T) {
	conn, _ := fixture(t, map[string]string{"proj-a/CLAUDE.md": "x"})

	if _, err := CreateTemplate(conn, "t", "", "存在しないプロジェクト"); err == nil {
		t.Error("設定の無いプロジェクトからは作れないはず")
	}
}
