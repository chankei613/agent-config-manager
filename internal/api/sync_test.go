package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/snapshot"
	"github.com/chankei613/agent-config-manager/internal/sync"
)

func postBody(t *testing.T, srvURL string, client *http.Client, path string, body any) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(body)
	resp, err := client.Post(srvURL+path, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// 乖離しているCLAUDE.mdを、多数派の内容に揃える一連の流れ
func TestUnifyEndpointResolvesDrift(t *testing.T) {
	srv, root := newFixture(t)
	source := filepath.Join(root, "proj-a", "CLAUDE.md")
	drifted := filepath.Join(root, "proj-c", "CLAUDE.md")

	// 実行前は乖離している
	if diverged := getJSON[[]interface{}](t, srv, "/api/v1/drift?diverged=true"); len(diverged) != 1 {
		t.Fatalf("前提: CLAUDE.mdが乖離しているはず: %d", len(diverged))
	}

	// plan（この時点では書き換えない）
	planResp := postBody(t, srv.URL, srv.Client(), "/api/v1/unify/plan", map[string]string{"source_path": source})
	changes := decode[[]snapshot.Change](t, planResp)

	writes := 0
	for _, c := range changes {
		if c.Type == snapshot.ChangeRestore {
			writes++
		}
	}
	if writes != 1 {
		t.Errorf("proj-c の1件が書き換え対象のはず: %+v", changes)
	}
	if content, _ := os.ReadFile(drifted); string(content) != "drifted rules" {
		t.Error("planがファイルを書き換えている")
	}

	// 実行
	resp := postBody(t, srv.URL, srv.Client(), "/api/v1/unify", map[string]string{"source_path": source})
	result := decode[sync.UnifyResult](t, resp)
	if result.Updated != 1 {
		t.Errorf("got %d updated, want 1: %+v", result.Updated, result)
	}
	if result.BackupID == 0 {
		t.Error("自動バックアップIDが返っていない")
	}
	if content, _ := os.ReadFile(drifted); string(content) != "shared rules" {
		t.Errorf("統一されていない: %q", content)
	}

	// 再スキャンすれば乖離が解消していること
	rescan, err := srv.Client().Post(srv.URL+"/api/v1/rescan", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	rescan.Body.Close()

	if diverged := getJSON[[]interface{}](t, srv, "/api/v1/drift?diverged=true"); len(diverged) != 0 {
		t.Errorf("統一後は乖離が解消するはず: %d", len(diverged))
	}
}

func TestUnifyRejectsUnknownSource(t *testing.T) {
	srv, _ := newFixture(t)

	resp := postBody(t, srv.URL, srv.Client(), "/api/v1/unify/plan", map[string]string{"source_path": "/nowhere"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("got %s, want 404", resp.Status)
	}
}

func TestTemplateLifecycle(t *testing.T) {
	srv, root := newFixture(t)

	created := postBody(t, srv.URL, srv.Client(), "/api/v1/templates", map[string]string{
		"name": "標準構成", "note": "新規用", "project": "proj-a",
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create: %s", created.Status)
	}
	tmpl := decode[db.Template](t, created)
	if tmpl.FileCount != 2 {
		t.Fatalf("proj-a は CLAUDE.md と settings.json の2件: %d", tmpl.FileCount)
	}

	if list := getJSON[[]db.Template](t, srv, "/api/v1/templates"); len(list) != 1 {
		t.Errorf("got %d templates, want 1", len(list))
	}

	files := getJSON[[]sync.TemplateFile](t, srv, "/api/v1/templates/1/files")
	// .claude 配下のパスが保たれていること（適用先を間違えないため）
	hasNested := false
	for _, f := range files {
		if f.RelPath == ".claude/settings.json" {
			hasNested = true
		}
	}
	if !hasNested {
		t.Errorf(".claude/ を含む相対パスで保存されるべき: %+v", files)
	}

	// まっさらなディレクトリへ適用
	target := filepath.Join(root, "proj-new")
	planResp := postBody(t, srv.URL, srv.Client(), "/api/v1/templates/1/plan", map[string]string{"target_dir": target})
	changes := decode[[]snapshot.Change](t, planResp)
	if len(changes) != 2 {
		t.Fatalf("got %d changes, want 2", len(changes))
	}
	if _, err := os.Stat(filepath.Join(target, "CLAUDE.md")); err == nil {
		t.Error("planがファイルを作っている")
	}

	applyResp := postBody(t, srv.URL, srv.Client(), "/api/v1/templates/1/apply", map[string]string{"target_dir": target})
	result := decode[sync.ApplyResult](t, applyResp)
	if result.Created != 2 {
		t.Errorf("got %d created, want 2: %+v", result.Created, result)
	}
	if _, err := os.Stat(filepath.Join(target, ".claude", "settings.json")); err != nil {
		t.Error(".claude/settings.json が正しい位置に作られていない")
	}
}

func TestApplyTemplateRequiresTargetDir(t *testing.T) {
	srv, _ := newFixture(t)

	created := postBody(t, srv.URL, srv.Client(), "/api/v1/templates", map[string]string{
		"name": "t", "project": "proj-a",
	})
	created.Body.Close()

	resp := postBody(t, srv.URL, srv.Client(), "/api/v1/templates/1/apply", map[string]string{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("got %s, want 400", resp.Status)
	}
}
