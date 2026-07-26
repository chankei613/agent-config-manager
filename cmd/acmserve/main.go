// cmd/acmserve はインベントリ参照APIをlocalhostで提供する。
// Phase 5のUIはこのAPIを叩く。Wailsに載せる際は同じ api.Server を
// バインディングとして使い回すため、ここはHTTPの口を用意するだけ。
//
//	go run ./cmd/acmserve -projects /path/to/projects
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chankei613/agent-config-manager/internal/api"
	"github.com/chankei613/agent-config-manager/internal/db"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8430", "待ち受けアドレス（localhost限定）")
	userDir := flag.String("user", defaultUserDir(), "ユーザースコープの .claude ディレクトリ")
	projectRoots := flag.String("projects", "", "プロジェクトを含むルート（カンマ区切り）")
	dbPath := flag.String("db", "acm.db", "SQLiteファイル")
	flag.Parse()

	conn, err := db.Init(*dbPath)
	if err != nil {
		log.Fatalf("db init: %v", err)
	}

	var roots []string
	if *projectRoots != "" {
		roots = strings.Split(*projectRoots, ",")
	}

	server := api.New(conn, *userDir, roots)

	// 起動時に1回スキャンして、すぐ中身のある状態にする
	stats, err := server.Rescan()
	if err != nil {
		log.Fatalf("initial scan: %v", err)
	}
	log.Printf("scanned %d config files (added %d / updated %d / removed %d)",
		stats.Total, stats.Added, stats.Updated, stats.Removed)

	log.Printf("agent-config-manager API listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}

func defaultUserDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}
