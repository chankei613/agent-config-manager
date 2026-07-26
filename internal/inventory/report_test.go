package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chankei613/agent-config-manager/internal/config"
	"gorm.io/gorm"
)

// seedScan はプロジェクト群を作ってスキャン結果をDBへ入れる。
func seedScan(t *testing.T, files map[string]string) *gorm.DB {
	t.Helper()

	conn := newTestDB(t)
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(root, rel), content)
	}
	if _, err := Sync(conn, scanProjects(t, root), root); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestBuildMatrix(t *testing.T) {
	conn := seedScan(t, map[string]string{
		"proj-a/CLAUDE.md":                   "rules",
		"proj-a/.claude/settings.local.json": "{}",
		"proj-b/CLAUDE.md":                   "rules",
		"proj-c/CLAUDE.md":                   "different rules",
	})

	matrix, err := BuildMatrix(conn)
	if err != nil {
		t.Fatal(err)
	}

	if len(matrix.Projects) != 3 {
		t.Errorf("got %d projects, want 3: %v", len(matrix.Projects), matrix.Projects)
	}
	if len(matrix.Kinds) != 2 {
		t.Errorf("got %d kinds, want 2: %v", len(matrix.Kinds), matrix.Kinds)
	}

	// proj-a と proj-b の CLAUDE.md は同内容 → 同じハッシュ
	var hashA, hashB, hashC string
	for _, cell := range matrix.Cells {
		if cell.Kind != string(config.KindClaudeMD) {
			continue
		}
		switch cell.Project {
		case "proj-a":
			hashA = cell.Hash
		case "proj-b":
			hashB = cell.Hash
		case "proj-c":
			hashC = cell.Hash
		}
	}
	if hashA == "" || hashA != hashB {
		t.Errorf("proj-a と proj-b は同内容なので同じハッシュのはず: %q vs %q", hashA, hashB)
	}
	if hashC == hashA {
		t.Error("proj-c は内容が違うので別ハッシュのはず")
	}
}

// 1つのセルに別内容のファイルが複数ある場合、代表ハッシュは出さない
func TestMatrixCellWithMixedContentHasNoHash(t *testing.T) {
	conn := seedScan(t, map[string]string{
		"proj-a/.claude/agents/one.md": "first",
		"proj-a/.claude/agents/two.md": "second",
	})

	matrix, err := BuildMatrix(conn)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, cell := range matrix.Cells {
		if cell.Kind == string(config.KindAgent) && cell.Project == "proj-a" {
			found = true
			if cell.Count != 2 {
				t.Errorf("got count %d, want 2", cell.Count)
			}
			if cell.Hash != "" {
				t.Error("内容が2種類あるセルに代表ハッシュを出してはいけない")
			}
		}
	}
	if !found {
		t.Error("agent のセルが見つからない")
	}
}

func TestDriftReportRanksMajorityFirst(t *testing.T) {
	conn := seedScan(t, map[string]string{
		"proj-a/CLAUDE.md": "shared",
		"proj-b/CLAUDE.md": "shared",
		"proj-c/CLAUDE.md": "shared",
		"proj-d/CLAUDE.md": "drifted",
	})

	reports, err := DriftReport(conn, string(config.KindClaudeMD))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(reports))
	}

	drift := reports[0]
	if !drift.Diverged {
		t.Fatal("4プロジェクトで内容が割れているので diverged のはず")
	}
	if len(drift.Groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(drift.Groups))
	}
	// 多数派が先頭（＝統一先の候補）
	if drift.Groups[0].Count != 3 {
		t.Errorf("先頭は多数派(3件)のはず、got %d", drift.Groups[0].Count)
	}
	if drift.Groups[1].Count != 1 {
		t.Errorf("2番目は少数派(1件)のはず、got %d", drift.Groups[1].Count)
	}
	if len(drift.Groups[1].Projects) != 1 || drift.Groups[1].Projects[0] != "proj-d" {
		t.Errorf("乖離しているのは proj-d のはず: %v", drift.Groups[1].Projects)
	}
}

// 別々のWorker定義153件のように「内容が違って当たり前のファイル群」を
// 分裂と誤検知しないこと。比較単位は種別ではなく相対パス。
func TestDriftReportDoesNotFlagDistinctFilesOfSameKind(t *testing.T) {
	conn := seedScan(t, map[string]string{
		"proj-a/.claude/agents/architect.md": "アーキテクトの定義",
		"proj-a/.claude/agents/reviewer.md":  "レビュアーの定義",
		"proj-a/.claude/agents/writer.md":    "ライターの定義",
	})

	reports, err := DriftReport(conn, string(config.KindAgent))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 0 {
		t.Errorf("それぞれ別の定義なので比較対象にならないはず: %+v", reports)
	}

	summary, err := BuildSummary(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.DivergedKinds) != 0 {
		t.Errorf("agent を分裂扱いしてはいけない: %v", summary.DivergedKinds)
	}
}

// 同じ名前のWorker定義が複数プロジェクトにあって中身が違う場合は検出する
func TestDriftReportFlagsSameNamedFileAcrossProjects(t *testing.T) {
	conn := seedScan(t, map[string]string{
		"proj-a/.claude/agents/architect.md": "v1 の定義",
		"proj-b/.claude/agents/architect.md": "v2 の定義",
		"proj-c/.claude/agents/architect.md": "v1 の定義",
		// 別名の定義は比較対象外
		"proj-a/.claude/agents/reviewer.md": "レビュアー",
	})

	reports, err := DriftReport(conn, string(config.KindAgent))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("architect.md だけが比較対象: %+v", reports)
	}

	drift := reports[0]
	if drift.Identity != "agents/architect.md" {
		t.Errorf("got identity %q, want agents/architect.md", drift.Identity)
	}
	if !drift.Diverged {
		t.Error("中身が割れているので diverged のはず")
	}
	if drift.Groups[0].Count != 2 {
		t.Errorf("多数派は2件のはず: %+v", drift.Groups)
	}
}

// ユーザー全体設定とプロジェクト設定は役割が違うので比較しない。
// （~/.claude/settings.local.json は全体の既定値、プロジェクト側は個別の上書き）
func TestDriftReportDoesNotCompareAcrossScopes(t *testing.T) {
	conn := newTestDB(t)

	userDir := t.TempDir()
	claude := filepath.Join(userDir, ".claude")
	writeFile(t, filepath.Join(claude, "settings.local.json"), `{"scope":"user"}`)

	projectRoot := t.TempDir()
	writeFile(t, filepath.Join(projectRoot, "proj-a", ".claude", "settings.local.json"), `{"scope":"project"}`)

	userRes, err := config.ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}
	projRes := scanProjects(t, projectRoot)

	combined := &config.Result{Files: append(userRes.Files, projRes.Files...)}
	if _, err := Sync(conn, combined, claude, projectRoot); err != nil {
		t.Fatal(err)
	}

	reports, err := DriftReport(conn, string(config.KindSettingsLocal))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 0 {
		t.Errorf("スコープが違う設定を比較してはいけない: %+v", reports)
	}
}

func TestDriftReportNotDivergedWhenUnified(t *testing.T) {
	conn := seedScan(t, map[string]string{
		"proj-a/CLAUDE.md": "same",
		"proj-b/CLAUDE.md": "same",
	})

	reports, err := DriftReport(conn, string(config.KindClaudeMD))
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Diverged {
		t.Errorf("内容が揃っているので diverged=false のはず: %+v", reports)
	}
}

func TestBuildSummary(t *testing.T) {
	conn := seedScan(t, map[string]string{
		"proj-a/CLAUDE.md":                   "rules",
		"proj-b/CLAUDE.md":                   "drifted",
		"proj-a/.claude/settings.local.json": "{}",
	})

	summary, err := BuildSummary(conn)
	if err != nil {
		t.Fatal(err)
	}

	if summary.TotalFiles != 3 {
		t.Errorf("got %d files, want 3", summary.TotalFiles)
	}
	if summary.ProjectCount != 2 {
		t.Errorf("got %d projects, want 2", summary.ProjectCount)
	}
	if summary.ByKind[string(config.KindClaudeMD)] != 2 {
		t.Errorf("claude_md の件数が合わない: %v", summary.ByKind)
	}
	if len(summary.DivergedKinds) != 1 || summary.DivergedKinds[0] != string(config.KindClaudeMD) {
		t.Errorf("claude_md が割れているはず: %v", summary.DivergedKinds)
	}
}

func TestSummaryCountsOrphansAndSymlinkedFiles(t *testing.T) {
	conn := newTestDB(t)
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	realWorkers := filepath.Join(dir, "obsidian", "workers")

	writeFile(t, filepath.Join(realWorkers, "worker-a.md"), "a")
	if err := os.MkdirAll(filepath.Join(claude, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realWorkers, filepath.Join(claude, "agents")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(claude, "skills")); err != nil {
		t.Fatal(err)
	}

	res, err := config.ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(conn, res, claude); err != nil {
		t.Fatal(err)
	}

	summary, err := BuildSummary(conn)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ViaSymlink != 1 {
		t.Errorf("親がリンクのファイルは1件のはず: %d", summary.ViaSymlink)
	}
	if summary.Orphans != 1 {
		t.Errorf("リンク切れは1件のはず: %d", summary.Orphans)
	}
}
