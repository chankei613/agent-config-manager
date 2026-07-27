package api

import (
	"errors"
	"net/http"

	"github.com/chankei613/agent-config-manager/internal/content"
	"github.com/chankei613/agent-config-manager/internal/db"
)

func (s *Server) contentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/content", s.getContent)
	mux.HandleFunc("GET /api/v1/diff", s.getDiff)
}

// getContent は1ファイルの中身を返す。機密種別は自動でマスクされる。
func (s *Server) getContent(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errors.New("path は必須です"))
		return
	}

	kind, err := s.kindForPath(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	file, err := content.Read(path, kind, r.URL.Query().Get("mask") == "true")
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

// getDiff は2ファイルの行単位差分を返す。
func (s *Server) getDiff(w http.ResponseWriter, r *http.Request) {
	left := r.URL.Query().Get("left")
	right := r.URL.Query().Get("right")
	if left == "" || right == "" {
		writeError(w, http.StatusBadRequest, errors.New("left と right は必須です"))
		return
	}

	kind, err := s.kindForPath(left)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	diff, err := content.DiffFiles(left, right, kind)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

// kindForPath はインベントリに載っているファイルだけを対象にする。
// 任意のパスを読ませないための入口チェックでもある。
func (s *Server) kindForPath(path string) (string, error) {
	var row db.ConfigFile
	if err := s.DB.Select("kind").First(&row, "path = ?", path).Error; err != nil {
		return "", errors.New("インベントリに無いファイルです")
	}
	return row.Kind, nil
}
