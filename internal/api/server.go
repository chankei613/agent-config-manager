// Package api はインベントリを読むためのHTTP APIを提供する。
// Phase 5でWailsに載せる際は、同じ Server のメソッドをバインディングとして
// 再利用できるよう、ハンドラとロジックを分けてある。
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/chankei613/agent-config-manager/internal/config"
	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/inventory"
	"github.com/chankei613/agent-config-manager/internal/snapshot"
	"gorm.io/gorm"
)

type Server struct {
	DB *gorm.DB
	// Roots はスキャン対象。Rescan で使う。
	UserDir      string
	ProjectRoots []string
}

func New(conn *gorm.DB, userDir string, projectRoots []string) *Server {
	return &Server{DB: conn, UserDir: userDir, ProjectRoots: projectRoots}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/summary", s.getSummary)
	mux.HandleFunc("GET /api/v1/matrix", s.getMatrix)
	mux.HandleFunc("GET /api/v1/drift", s.getDrift)
	mux.HandleFunc("GET /api/v1/orphans", s.getOrphans)
	mux.HandleFunc("GET /api/v1/files", s.listFiles)
	mux.HandleFunc("POST /api/v1/rescan", s.rescan)

	mux.HandleFunc("GET /api/v1/snapshots", s.listSnapshots)
	mux.HandleFunc("POST /api/v1/snapshots", s.createSnapshot)
	mux.HandleFunc("GET /api/v1/snapshots/{id}/entries", s.snapshotEntries)
	mux.HandleFunc("GET /api/v1/snapshots/{id}/plan", s.planRestore)
	mux.HandleFunc("POST /api/v1/snapshots/{id}/restore", s.restoreSnapshot)
	mux.HandleFunc("DELETE /api/v1/snapshots/{id}", s.deleteSnapshot)

	s.syncRoutes(mux)

	return mux
}

func snapshotID(r *http.Request) (uint, error) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	return uint(id), err
}

func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request) {
	snaps, err := snapshot.List(s.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) createSnapshot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
		Note  string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	snap, err := snapshot.Create(s.DB, body.Label, body.Note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, snap)
}

func (s *Server) snapshotEntries(w http.ResponseWriter, r *http.Request) {
	id, err := snapshotID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	entries, err := snapshot.Entries(s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// planRestore は「復元したら何が起きるか」を、何も書き換えずに返す。
func (s *Server) planRestore(w http.ResponseWriter, r *http.Request) {
	id, err := snapshotID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	changes, err := snapshot.Plan(s.DB, id)
	if err != nil {
		writeError(w, statusForSnapshotErr(err), err)
		return
	}
	writeJSON(w, http.StatusOK, changes)
}

func (s *Server) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := snapshotID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	result, err := snapshot.Restore(s.DB, id)
	if err != nil {
		writeError(w, statusForSnapshotErr(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := snapshotID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if err := snapshot.Delete(s.DB, id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func statusForSnapshotErr(err error) int {
	if errors.Is(err, snapshot.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func (s *Server) getSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := inventory.BuildSummary(s.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) getMatrix(w http.ResponseWriter, r *http.Request) {
	matrix, err := inventory.BuildMatrix(s.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, matrix)
}

func (s *Server) getDrift(w http.ResponseWriter, r *http.Request) {
	var kinds []string
	if raw := r.URL.Query().Get("kind"); raw != "" {
		kinds = strings.Split(raw, ",")
	}

	// divergedのみ=trueなら、割れているものだけ返す（UIの既定表示用）
	onlyDiverged := r.URL.Query().Get("diverged") == "true"

	reports, err := inventory.DriftReport(s.DB, kinds...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if onlyDiverged {
		filtered := make([]inventory.Drift, 0, len(reports))
		for _, rep := range reports {
			if rep.Diverged {
				filtered = append(filtered, rep)
			}
		}
		reports = filtered
	}

	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) getOrphans(w http.ResponseWriter, r *http.Request) {
	orphans, err := inventory.Orphans(s.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, orphans)
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	q := s.DB.Model(&db.ConfigFile{})

	if kind := r.URL.Query().Get("kind"); kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if project := r.URL.Query().Get("project"); project != "" {
		q = q.Where("project = ?", project)
	}
	if r.URL.Query().Get("via_symlink") == "true" {
		q = q.Where("via_symlink = ?", true)
	}

	var rows []db.ConfigFile
	if err := q.Order("kind asc, project asc, path asc").Find(&rows).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// rescan は登録済みルートを再走査してインベントリを更新する。
// 設定ファイルへの書き込みは一切行わない（読み取り専用）。
func (s *Server) rescan(w http.ResponseWriter, r *http.Request) {
	stats, err := s.Rescan()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// Rescan はHTTPから切り離した本体。Wailsバインディングからも直接呼べる。
func (s *Server) Rescan() (*inventory.SyncStats, error) {
	var all []config.File
	var errs []string
	var covered []string

	if s.UserDir != "" {
		res, err := config.ScanUserScope(s.UserDir)
		if err != nil {
			return nil, err
		}
		all = append(all, res.Files...)
		errs = append(errs, res.Errors...)
		covered = append(covered, s.UserDir)
	}

	if len(s.ProjectRoots) > 0 {
		res, err := config.ScanProjects(s.ProjectRoots...)
		if err != nil {
			return nil, err
		}
		all = append(all, res.Files...)
		errs = append(errs, res.Errors...)
		covered = append(covered, s.ProjectRoots...)
	}

	return Sync(s.DB, &config.Result{Files: all, Errors: errs}, covered...)
}

// Sync は inventory.Sync の薄いラッパー（テストから差し替えやすくするため）。
var Sync = inventory.Sync

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
