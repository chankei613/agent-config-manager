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
)

func TestSnapshotLifecycle(t *testing.T) {
	srv, root := newFixture(t)
	path := filepath.Join(root, "proj-a", "CLAUDE.md")

	// 作成
	body, _ := json.Marshal(map[string]string{"label": "v1", "note": "初期状態"})
	resp, err := srv.Client().Post(srv.URL+"/api/v1/snapshots", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %s", resp.Status)
	}

	var snap db.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.FileCount == 0 {
		t.Fatal("ファイルが1件も保存されていない")
	}

	// 一覧
	snaps := getJSON[[]db.Snapshot](t, srv, "/api/v1/snapshots")
	if len(snaps) != 1 {
		t.Errorf("got %d snapshots, want 1", len(snaps))
	}

	// 中身の一覧（内容そのものは返さない）
	entries := getJSON[[]snapshot.Entry](t, srv, "/api/v1/snapshots/1/entries")
	if len(entries) != snap.FileCount {
		t.Errorf("got %d entries, want %d", len(entries), snap.FileCount)
	}

	// 書き換えてから plan を見る（この時点ではまだ何も戻らない）
	if err := os.WriteFile(path, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes := getJSON[[]snapshot.Change](t, srv, "/api/v1/snapshots/1/plan")
	restores := 0
	for _, c := range changes {
		if c.Type == snapshot.ChangeRestore {
			restores++
		}
	}
	if restores != 1 {
		t.Errorf("1件が書き戻し対象になるはず: %+v", changes)
	}
	if content, _ := os.ReadFile(path); string(content) != "edited" {
		t.Error("planはファイルを書き換えてはいけない")
	}

	// 復元
	restoreResp, err := srv.Client().Post(srv.URL+"/api/v1/snapshots/1/restore", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restoreResp.Body.Close()
	if restoreResp.StatusCode != http.StatusOK {
		t.Fatalf("restore: %s", restoreResp.Status)
	}

	var result snapshot.RestoreResult
	if err := json.NewDecoder(restoreResp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Restored != 1 {
		t.Errorf("got %d restored, want 1", result.Restored)
	}
	// 取り消せるように自動バックアップが取られていること
	if result.BackupID == 0 {
		t.Error("自動バックアップIDが返っていない")
	}
	if content, _ := os.ReadFile(path); string(content) != "shared rules" {
		t.Errorf("復元されていない: %q", content)
	}
}

func TestPlanRejectsUnknownSnapshot(t *testing.T) {
	srv, _ := newFixture(t)

	resp, err := srv.Client().Get(srv.URL + "/api/v1/snapshots/999/plan")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("got %s, want 404", resp.Status)
	}
}

func TestDeleteSnapshot(t *testing.T) {
	srv, _ := newFixture(t)

	body, _ := json.Marshal(map[string]string{"label": "v1"})
	created, err := srv.Client().Post(srv.URL+"/api/v1/snapshots", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	created.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/snapshots/1", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %s, want 204", resp.Status)
	}

	if snaps := getJSON[[]db.Snapshot](t, srv, "/api/v1/snapshots"); len(snaps) != 0 {
		t.Errorf("削除後は空のはず: %+v", snaps)
	}
}
