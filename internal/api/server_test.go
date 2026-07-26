package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/inventory"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newFixture は「CLAUDE.mdが3プロジェクトで2種類に割れている」状態のサーバーを返す。
func newFixture(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	conn, err := db.Init(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	// CLAUDE.md は3プロジェクトにあり、proj-c だけ内容が乖離している
	writeFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "shared rules")
	writeFile(t, filepath.Join(root, "proj-b", "CLAUDE.md"), "shared rules")
	writeFile(t, filepath.Join(root, "proj-c", "CLAUDE.md"), "drifted rules")
	// settings.json は2プロジェクトにあり内容が揃っている（比較対象だが乖離なし）
	writeFile(t, filepath.Join(root, "proj-a", ".claude", "settings.json"), `{"model":"opus"}`)
	writeFile(t, filepath.Join(root, "proj-b", ".claude", "settings.json"), `{"model":"opus"}`)

	s := New(conn, "", []string{root})
	if _, err := s.Rescan(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)
	return srv, root
}

func getJSON[T any](t *testing.T, srv *httptest.Server, path string) T {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s -> %s", path, resp.Status)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

func TestSummaryEndpoint(t *testing.T) {
	srv, _ := newFixture(t)

	summary := getJSON[inventory.Summary](t, srv, "/api/v1/summary")
	if summary.TotalFiles != 5 {
		t.Errorf("got %d files, want 5 (CLAUDE.md×3 + settings.json×2)", summary.TotalFiles)
	}
	if summary.ProjectCount != 3 {
		t.Errorf("got %d projects, want 3", summary.ProjectCount)
	}
	if len(summary.DivergedKinds) != 1 || summary.DivergedKinds[0] != "claude_md" {
		t.Errorf("claude_md が割れているはず: %v", summary.DivergedKinds)
	}
}

func TestMatrixEndpoint(t *testing.T) {
	srv, _ := newFixture(t)

	matrix := getJSON[inventory.Matrix](t, srv, "/api/v1/matrix")
	if len(matrix.Projects) != 3 {
		t.Errorf("got %d projects, want 3: %v", len(matrix.Projects), matrix.Projects)
	}
	if len(matrix.Cells) == 0 {
		t.Fatal("cells が空")
	}
}

func TestDriftEndpointFiltersToDivergedOnly(t *testing.T) {
	srv, _ := newFixture(t)

	// 複数箇所に存在する設定（CLAUDE.md と settings.json）が比較対象になる
	all := getJSON[[]inventory.Drift](t, srv, "/api/v1/drift")
	diverged := getJSON[[]inventory.Drift](t, srv, "/api/v1/drift?diverged=true")

	if len(all) != 2 {
		t.Fatalf("比較対象は CLAUDE.md と settings.json の2件: %+v", all)
	}
	// 割れているものが先頭
	if !all[0].Diverged || all[1].Diverged {
		t.Errorf("乖離しているものを先に並べるはず: %+v", all)
	}

	if len(diverged) != 1 {
		t.Fatalf("割れているのは claude_md だけ: %+v", diverged)
	}
	if diverged[0].Kind != "claude_md" || diverged[0].Identity != "CLAUDE.md" {
		t.Errorf("got kind=%q identity=%q", diverged[0].Kind, diverged[0].Identity)
	}
	// 多数派（2件）が先頭に来る
	if diverged[0].Groups[0].Count != 2 {
		t.Errorf("先頭は多数派のはず: %+v", diverged[0].Groups)
	}
}

// 1箇所にしかない設定は比較対象にならない
func TestDriftEndpointExcludesSingleLocationConfigs(t *testing.T) {
	srv, root := newFixture(t)
	writeFile(t, filepath.Join(root, "proj-c", ".claude", "settings.local.json"), `{"only":"here"}`)

	resp, err := srv.Client().Post(srv.URL+"/api/v1/rescan", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	reports := getJSON[[]inventory.Drift](t, srv, "/api/v1/drift?kind=settings_local")
	if len(reports) != 0 {
		t.Errorf("1箇所にしかない設定は比較しようがない: %+v", reports)
	}
}

func TestDriftEndpointFiltersByKind(t *testing.T) {
	srv, _ := newFixture(t)

	reports := getJSON[[]inventory.Drift](t, srv, "/api/v1/drift?kind=settings")
	if len(reports) != 1 || reports[0].Kind != "settings" {
		t.Fatalf("settings だけ返るはず: %+v", reports)
	}
	if reports[0].Diverged {
		t.Error("settings.json は2プロジェクトで同内容なので乖離なし")
	}
}

func TestListFilesFiltersByProject(t *testing.T) {
	srv, _ := newFixture(t)

	rows := getJSON[[]db.ConfigFile](t, srv, "/api/v1/files?project=proj-a")
	if len(rows) != 2 {
		t.Fatalf("proj-a は CLAUDE.md と settings.json の2件: %+v", rows)
	}
	for _, row := range rows {
		if row.Project != "proj-a" {
			t.Errorf("他プロジェクトが混ざっている: %+v", row)
		}
	}
}

func TestOrphansEndpoint(t *testing.T) {
	conn, err := db.Init(filepath.Join(t.TempDir(), "orphan.db"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(claude, "agents")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	s := New(conn, claude, nil)
	if _, err := s.Rescan(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	orphans := getJSON[[]db.ConfigFile](t, srv, "/api/v1/orphans")
	if len(orphans) != 1 {
		t.Fatalf("リンク切れ1件が返るはず: %+v", orphans)
	}
	if !orphans[0].Broken {
		t.Error("broken フラグが立っていない")
	}
}

// 再スキャンで変更が反映されること（UIの「再読み込み」ボタン相当）
func TestRescanEndpointPicksUpChanges(t *testing.T) {
	srv, root := newFixture(t)

	// proj-c を多数派に揃える → 乖離が解消されるはず
	writeFile(t, filepath.Join(root, "proj-c", "CLAUDE.md"), "shared rules")

	resp, err := srv.Client().Post(srv.URL+"/api/v1/rescan", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rescan -> %s", resp.Status)
	}

	var stats inventory.SyncStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats.Updated != 1 {
		t.Errorf("1件更新されるはず: %+v", stats)
	}

	diverged := getJSON[[]inventory.Drift](t, srv, "/api/v1/drift?diverged=true")
	if len(diverged) != 0 {
		t.Errorf("内容を揃えたので乖離は解消するはず: %+v", diverged)
	}
}
