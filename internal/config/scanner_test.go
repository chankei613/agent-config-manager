package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		rel  string
		want Kind
	}{
		{"CLAUDE.md", KindClaudeMD},
		{"settings.json", KindSettings},
		{"settings.local.json", KindSettingsLocal},
		{"claude_desktop_config.json", KindMCPConfig},
		{".mcp.json", KindMCPConfig},
		{"agents/engineering-backend-architect.md", KindAgent},
		{"commands/deploy.md", KindCommand},
		{"skills/dataviz/SKILL.md", KindSkill},
		{"skills/dataviz/references/palette.md", KindOther},
		{"agents/README.txt", KindOther},
		{"history.jsonl", KindOther},
	}

	for _, tc := range cases {
		if got := Classify(tc.rel); got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestSensitiveKinds(t *testing.T) {
	if !KindSettingsLocal.IsSensitive() {
		t.Error("settings.local.json must be treated as sensitive (APIキーが入りうる)")
	}
	if KindClaudeMD.IsSensitive() {
		t.Error("CLAUDE.md should not be marked sensitive")
	}
}

// writeFile はフィクスチャを作る。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findFile(files []File, relPath string) *File {
	for i := range files {
		if files[i].RelPath == relPath {
			return &files[i]
		}
	}
	return nil
}

func TestScanUserScope(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")

	writeFile(t, filepath.Join(claude, "CLAUDE.md"), "# 行動指針")
	writeFile(t, filepath.Join(claude, "settings.json"), `{"model":"opus"}`)
	writeFile(t, filepath.Join(claude, "agents", "worker-a.md"), "worker a")
	writeFile(t, filepath.Join(claude, "skills", "dataviz", "SKILL.md"), "skill")
	// 走査対象外に落ちるべきもの
	writeFile(t, filepath.Join(claude, "history.jsonl"), "{}")
	writeFile(t, filepath.Join(claude, "cache", "big.json"), "{}")
	writeFile(t, filepath.Join(claude, "sessions", "s1.json"), "{}")

	res, err := ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Files) != 4 {
		t.Fatalf("got %d tracked files, want 4: %+v", len(res.Files), res.Files)
	}
	for _, f := range res.Files {
		if f.Scope != ScopeUser {
			t.Errorf("%s: got scope %q, want user", f.RelPath, f.Scope)
		}
		if f.Hash == "" {
			t.Errorf("%s: expected a content hash", f.RelPath)
		}
	}

	if got := findFile(res.Files, "agents/worker-a.md"); got == nil || got.Kind != KindAgent {
		t.Errorf("agents/worker-a.md was not classified as an agent: %+v", got)
	}
	if findFile(res.Files, "cache/big.json") != nil {
		t.Error("cache/ must be skipped")
	}
	if findFile(res.Files, "history.jsonl") != nil {
		t.Error("history.jsonl must not be tracked")
	}
}

func TestScanProjectsPicksUpRootLevelConfig(t *testing.T) {
	root := t.TempDir()

	// プロジェクト1: ルート直下のCLAUDE.mdと.claude配下の両方
	writeFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "# proj a")
	writeFile(t, filepath.Join(root, "proj-a", ".claude", "settings.local.json"), `{"env":{}}`)
	writeFile(t, filepath.Join(root, "proj-a", ".mcp.json"), `{"mcpServers":{}}`)
	// プロジェクト2: 設定なし
	if err := os.MkdirAll(filepath.Join(root, "proj-b", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 隠しディレクトリはプロジェクトとして扱わない
	writeFile(t, filepath.Join(root, ".hidden", "CLAUDE.md"), "no")

	res, err := ScanProjects(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Files) != 3 {
		t.Fatalf("got %d files, want 3: %+v", len(res.Files), res.Files)
	}
	for _, f := range res.Files {
		if f.Project != "proj-a" {
			t.Errorf("got project %q, want proj-a (%s)", f.Project, f.RelPath)
		}
		if f.Scope != ScopeProject {
			t.Errorf("%s: got scope %q, want project", f.RelPath, f.Scope)
		}
	}
}

// workers は ~/.claude/agents → obsidian のシンボリックリンク。
// リンクであることを記録しないと、上書き時に実体を壊す。
func TestScanRecordsSymlinks(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	real := filepath.Join(dir, "obsidian", "workers")

	writeFile(t, filepath.Join(real, "worker-a.md"), "the real worker definition")
	if err := os.MkdirAll(filepath.Join(claude, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(claude, "agents", "worker-a.md")
	if err := os.Symlink(filepath.Join(real, "worker-a.md"), link); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(res.Files))
	}

	f := res.Files[0]
	if !f.IsSymlink {
		t.Error("expected the entry to be flagged as a symlink")
	}
	if f.SymlinkTarget == "" {
		t.Error("expected the symlink target to be recorded")
	}
	if f.Broken {
		t.Error("link resolves, so it must not be flagged broken")
	}
	// リンク先の実体を読んでハッシュ・サイズが取れていること
	if f.Hash == "" || f.Size == 0 {
		t.Errorf("expected the target's content to be hashed: %+v", f)
	}
}

// 実環境では ~/.claude/agents 自体が Obsidian の workers ディレクトリへのリンク。
// filepath.WalkDir はディレクトリへのリンクを辿らないため、素直に書くと
// Worker定義が丸ごと見えなくなる（実際に一度取りこぼした）。その回帰テスト。
func TestScanFollowsSymlinkedDirectory(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	realWorkers := filepath.Join(dir, "obsidian", "workers")

	writeFile(t, filepath.Join(realWorkers, "worker-a.md"), "definition a")
	writeFile(t, filepath.Join(realWorkers, "worker-b.md"), "definition b")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realWorkers, filepath.Join(claude, "agents")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Files) != 2 {
		t.Fatalf("got %d files, want 2 — リンク先ディレクトリの中身を辿れていない: %+v", len(res.Files), res.Files)
	}
	for _, f := range res.Files {
		if f.Kind != KindAgent {
			t.Errorf("%s: got kind %q, want agent", f.RelPath, f.Kind)
		}
		if !f.ViaSymlink {
			t.Errorf("%s: 親がリンクなので via_symlink が立つべき（書き込むと実体に届く）", f.RelPath)
		}
		if f.RealPath == "" || !strings.Contains(f.RealPath, "obsidian") {
			t.Errorf("%s: 実体パスが記録されていない: %q", f.RelPath, f.RealPath)
		}
		if f.Hash == "" {
			t.Errorf("%s: リンク先の内容がハッシュされていない", f.RelPath)
		}
	}
}

// ディレクトリへのリンクが切れている場合（実環境の ~/.claude/agents がこれ）。
// 「不明なファイル」として捨てると、配下の定義が全滅していることに気付けない。
func TestScanReportsDanglingDirectorySymlink(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	// 存在しないディレクトリを指すリンク
	if err := os.Symlink(filepath.Join(dir, "moved-away", "workers"), filepath.Join(claude, "agents")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1 — リンク切れが黙って捨てられている: %+v", len(res.Files), res.Files)
	}

	f := res.Files[0]
	if !f.Broken {
		t.Error("expected the dangling link to be flagged broken")
	}
	if f.Kind != KindAgent {
		t.Errorf("got kind %q, want agent (ディレクトリ名から推測すべき)", f.Kind)
	}
	if f.SymlinkTarget == "" {
		t.Error("expected the intended target to be recorded so it can be repaired")
	}
}

// plugins/ 配下はマーケットプレイスのベンダーファイルなので数えない
func TestScanSkipsPluginVendorFiles(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")

	writeFile(t, filepath.Join(claude, "settings.json"), `{}`)
	writeFile(t, filepath.Join(claude, "plugins", "marketplaces", "official", "github", ".mcp.json"), `{}`)
	writeFile(t, filepath.Join(claude, "plugins", "cache", "figma", "2.2.81", ".mcp.json"), `{}`)

	res, err := ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1 (plugins配下は除外): %+v", len(res.Files), res.Files)
	}
	if res.Files[0].Kind != KindSettings {
		t.Errorf("got kind %q, want settings", res.Files[0].Kind)
	}
}

// リンクがループしていても走査が止まること
func TestScanSurvivesSymlinkLoop(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	nested := filepath.Join(claude, "agents")

	writeFile(t, filepath.Join(nested, "worker-a.md"), "a")
	if err := os.Symlink(claude, filepath.Join(nested, "loop")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	done := make(chan *Result, 1)
	go func() {
		res, _ := ScanUserScope(claude)
		done <- res
	}()

	select {
	case res := <-done:
		if len(res.Files) == 0 {
			t.Error("expected the real file to still be found")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("scan did not terminate on a symlink loop")
	}
}

func TestScanDetectsBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(filepath.Join(claude, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(claude, "agents", "gone.md")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist.md"), link); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := ScanUserScope(claude)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(res.Files))
	}
	if !res.Files[0].Broken {
		t.Error("a dangling symlink must be reported as broken (孤児検出の土台)")
	}
}

// 同じ内容のファイルは同じハッシュになる。プロジェクト横断の差分検出の前提。
func TestIdenticalContentSharesHash(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proj-a", "CLAUDE.md"), "same content")
	writeFile(t, filepath.Join(root, "proj-b", "CLAUDE.md"), "same content")
	writeFile(t, filepath.Join(root, "proj-c", "CLAUDE.md"), "different content")

	res, err := ScanProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 3 {
		t.Fatalf("got %d files, want 3", len(res.Files))
	}

	byProject := map[string]string{}
	for _, f := range res.Files {
		byProject[f.Project] = f.Hash
	}
	if byProject["proj-a"] != byProject["proj-b"] {
		t.Error("identical files must hash identically")
	}
	if byProject["proj-a"] == byProject["proj-c"] {
		t.Error("differing files must hash differently")
	}
}

func TestScanMissingDirectoryIsNotAnError(t *testing.T) {
	res, err := ScanUserScope(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("scanning a missing .claude should not fail: %v", err)
	}
	if len(res.Files) != 0 || len(res.Errors) != 0 {
		t.Errorf("expected an empty result, got %+v", res)
	}
}
