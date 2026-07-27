package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chankei613/agent-config-manager/internal/sync"
)

// syncRoutes は統一・テンプレート系。いずれも書き込みを伴うため、
// 対応する plan エンドポイントを必ず用意している。
func (s *Server) syncRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/unify/plan", s.unifyPlan)
	mux.HandleFunc("POST /api/v1/unify", s.unify)

	mux.HandleFunc("GET /api/v1/templates", s.listTemplates)
	mux.HandleFunc("POST /api/v1/templates", s.createTemplate)
	mux.HandleFunc("GET /api/v1/templates/{id}/files", s.templateFiles)
	mux.HandleFunc("POST /api/v1/templates/{id}/plan", s.applyTemplatePlan)
	mux.HandleFunc("POST /api/v1/templates/{id}/apply", s.applyTemplate)
	mux.HandleFunc("DELETE /api/v1/templates/{id}", s.deleteTemplate)
}

func (s *Server) unifyPlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourcePath string `json:"source_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	changes, err := sync.UnifyPlan(s.DB, body.SourcePath)
	if err != nil {
		writeError(w, statusForSyncErr(err), err)
		return
	}
	writeJSON(w, http.StatusOK, changes)
}

func (s *Server) unify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourcePath string `json:"source_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	result, err := sync.Unify(s.DB, body.SourcePath)
	if err != nil {
		writeError(w, statusForSyncErr(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := sync.ListTemplates(s.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Note    string `json:"note"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if body.Name == "" || body.Project == "" {
		writeError(w, http.StatusBadRequest, errors.New("name と project は必須です"))
		return
	}

	tmpl, err := sync.CreateTemplate(s.DB, body.Name, body.Note, body.Project)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, tmpl)
}

func (s *Server) templateFiles(w http.ResponseWriter, r *http.Request) {
	id, err := snapshotID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	files, err := sync.TemplateFiles(s.DB, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) applyTemplatePlan(w http.ResponseWriter, r *http.Request) {
	id, target, ok := templateTarget(w, r)
	if !ok {
		return
	}

	changes, err := sync.ApplyTemplatePlan(s.DB, id, target)
	if err != nil {
		writeError(w, statusForSyncErr(err), err)
		return
	}
	writeJSON(w, http.StatusOK, changes)
}

func (s *Server) applyTemplate(w http.ResponseWriter, r *http.Request) {
	id, target, ok := templateTarget(w, r)
	if !ok {
		return
	}

	result, err := sync.ApplyTemplate(s.DB, id, target)
	if err != nil {
		writeError(w, statusForSyncErr(err), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := snapshotID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if err := sync.DeleteTemplate(s.DB, id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func templateTarget(w http.ResponseWriter, r *http.Request) (uint, string, bool) {
	id, err := snapshotID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return 0, "", false
	}

	var body struct {
		TargetDir string `json:"target_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return 0, "", false
	}
	if body.TargetDir == "" {
		writeError(w, http.StatusBadRequest, errors.New("target_dir は必須です"))
		return 0, "", false
	}
	return id, body.TargetDir, true
}

func statusForSyncErr(err error) int {
	switch {
	case errors.Is(err, sync.ErrSourceNotFound), errors.Is(err, sync.ErrTemplateNotFound):
		return http.StatusNotFound
	case errors.Is(err, sync.ErrNoTargets):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
