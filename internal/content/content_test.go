package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// APIキーが画面に出てはいけない。キー名は残して値だけ伏せる。
func TestMaskHidesSecretValuesButKeepsKeys(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		mustHide   string
		mustRemain string
	}{
		{
			"JSONのapiKey",
			`{"apiKey": "sk-live-abcdef1234567890", "model": "opus"}`,
			"sk-live-abcdef1234567890",
			"apiKey",
		},
		{
			"JSONのtoken",
			`{"GITHUB_TOKEN": "ghp_aaaaaaaaaaaaaaaaaaaa"}`,
			"ghp_aaaaaaaaaaaaaaaaaaaa",
			"GITHUB_TOKEN",
		},
		{
			"env形式",
			"ANTHROPIC_API_KEY=sk-ant-secret-value-here\nDEBUG=true",
			"sk-ant-secret-value-here",
			"ANTHROPIC_API_KEY",
		},
		{
			"Authorizationヘッダ",
			`{"header": "Bearer abcdefghijklmnopqrstuvwxyz"}`,
			"abcdefghijklmnopqrstuvwxyz",
			"Bearer",
		},
		{
			"裸のAPIキー形式",
			"my key is sk-proj-abcdefghijklmnopqrst here",
			"sk-proj-abcdefghijklmnopqrst",
			"my key is",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			masked, didMask := Mask(tc.input)
			if !didMask {
				t.Fatalf("マスクされていない: %q", masked)
			}
			if strings.Contains(masked, tc.mustHide) {
				t.Errorf("秘密が残っている: %q", masked)
			}
			if !strings.Contains(masked, tc.mustRemain) {
				t.Errorf("キー名まで消えている（何が設定されているか分からなくなる）: %q", masked)
			}
		})
	}
}

func TestMaskLeavesOrdinaryContentAlone(t *testing.T) {
	input := "# プロジェクト指示\n\n- タスクは上から順に実行する\n"

	masked, didMask := Mask(input)
	if didMask {
		t.Errorf("秘密が無いのにマスクした: %q", masked)
	}
	if masked != input {
		t.Errorf("内容が変わっている: %q", masked)
	}
}

func TestReadMasksSensitiveKindsAutomatically(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "settings.local.json", `{"apiKey":"sk-secret-aaaaaaaaaaaaaaaa"}`)

	file, err := Read(path, "settings_local", false)
	if err != nil {
		t.Fatal(err)
	}
	if !file.Masked {
		t.Error("settings_local は指定しなくてもマスクされるべき")
	}
	if strings.Contains(file.Text, "sk-secret-aaaaaaaaaaaaaaaa") {
		t.Errorf("秘密が残っている: %q", file.Text)
	}
}

func TestReadDoesNotMaskOrdinaryKinds(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "CLAUDE.md", "# 行動指針\n上から実行する\n")

	file, err := Read(path, "claude_md", false)
	if err != nil {
		t.Fatal(err)
	}
	if file.Masked {
		t.Error("通常のドキュメントをマスクする必要はない")
	}
	if file.Lines != 3 {
		t.Errorf("got %d lines, want 3", file.Lines)
	}
}

// 明示的に指定すれば種別に関わらずマスクできる
func TestReadAlwaysMask(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "CLAUDE.md", "TOKEN=abcdefghijklmnop\n")

	file, err := Read(path, "claude_md", true)
	if err != nil {
		t.Fatal(err)
	}
	if !file.Masked {
		t.Error("alwaysMask=true ならマスクされるべき")
	}
}

func TestReadTruncatesHugeFiles(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "big.md", strings.Repeat("a", maxDisplaySize+100))

	file, err := Read(path, "claude_md", false)
	if err != nil {
		t.Fatal(err)
	}
	if !file.Truncated {
		t.Error("巨大ファイルは打ち切るべき")
	}
	if len(file.Text) > maxDisplaySize {
		t.Errorf("打ち切られていない: %d bytes", len(file.Text))
	}
}

// ─── 差分 ─────────────────────────────────────────────────────────────────────

func TestDiffLinesDetectsChanges(t *testing.T) {
	left := "共通ルール\n古い項目\n末尾\n"
	right := "共通ルール\n新しい項目\n末尾\n"

	lines := DiffLines(left, right)

	var adds, dels, sames []string
	for _, l := range lines {
		switch l.Type {
		case LineAdd:
			adds = append(adds, l.Text)
		case LineDel:
			dels = append(dels, l.Text)
		case LineSame:
			sames = append(sames, l.Text)
		}
	}

	if len(sames) != 2 {
		t.Errorf("共通行は2行のはず: %v", sames)
	}
	if len(dels) != 1 || dels[0] != "古い項目" {
		t.Errorf("削除行が違う: %v", dels)
	}
	if len(adds) != 1 || adds[0] != "新しい項目" {
		t.Errorf("追加行が違う: %v", adds)
	}
}

func TestDiffLinesOnIdenticalContent(t *testing.T) {
	text := "同じ\n内容\n"

	lines := DiffLines(text, text)
	for _, l := range lines {
		if l.Type != LineSame {
			t.Errorf("同一内容に差分が出ている: %+v", l)
		}
	}
}

func TestDiffLinesTracksLineNumbers(t *testing.T) {
	lines := DiffLines("a\nb\n", "a\nc\n")

	for _, l := range lines {
		switch l.Type {
		case LineSame:
			if l.LeftNo != 1 || l.RightNo != 1 {
				t.Errorf("共通行の行番号が違う: %+v", l)
			}
		case LineDel:
			if l.LeftNo != 2 || l.RightNo != 0 {
				t.Errorf("削除行は左の行番号だけ持つ: %+v", l)
			}
		case LineAdd:
			if l.RightNo != 2 || l.LeftNo != 0 {
				t.Errorf("追加行は右の行番号だけ持つ: %+v", l)
			}
		}
	}
}

func TestDiffFilesCountsAndMasks(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a/settings.local.json", "{\n\"apiKey\":\"sk-aaaaaaaaaaaaaaaaaaaa\",\n\"model\":\"opus\"\n}")
	b := write(t, dir, "b/settings.local.json", "{\n\"apiKey\":\"sk-bbbbbbbbbbbbbbbbbbbb\",\n\"model\":\"sonnet\"\n}")

	diff, err := DiffFiles(a, b, "settings_local")
	if err != nil {
		t.Fatal(err)
	}
	if !diff.Masked {
		t.Error("機密種別の差分はマスクされるべき")
	}
	for _, l := range diff.Lines {
		if strings.Contains(l.Text, "sk-aaaa") || strings.Contains(l.Text, "sk-bbbb") {
			t.Errorf("差分にキーが露出している: %q", l.Text)
		}
	}
	// apiKey行は両方マスクされて同一になるので、差分は model 行だけ
	if diff.Added != 1 || diff.Deleted != 1 {
		t.Errorf("差分はmodel行の1組だけのはず: +%d -%d", diff.Added, diff.Deleted)
	}
}

func TestDiffFilesIdentical(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a/CLAUDE.md", "同じ内容\n")
	b := write(t, dir, "b/CLAUDE.md", "同じ内容\n")

	diff, err := DiffFiles(a, b, "claude_md")
	if err != nil {
		t.Fatal(err)
	}
	if !diff.Identical {
		t.Errorf("同一内容なので identical のはず: +%d -%d", diff.Added, diff.Deleted)
	}
}
