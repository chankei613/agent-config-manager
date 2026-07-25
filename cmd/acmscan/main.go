// cmd/acmscan は Phase 1 のスキャナを実環境に対して走らせる確認用CLI。
// UI（Phase 5）が出来るまでの動作確認手段であり、読み取りしかしない。
//
//	go run ./cmd/acmscan -user ~/.claude -projects /path/to/cometinc
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chankei613/agent-config-manager/internal/config"
	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/inventory"
)

func main() {
	userDir := flag.String("user", defaultUserDir(), "ユーザースコープの .claude ディレクトリ")
	projectRoots := flag.String("projects", "", "プロジェクトを含むルート（カンマ区切り）")
	dbPath := flag.String("db", "acm.db", "SQLiteファイル")
	flag.Parse()

	conn, err := db.Init(*dbPath)
	if err != nil {
		log.Fatalf("db init: %v", err)
	}

	var covered []string
	var all []config.File
	var scanErrors []string

	if *userDir != "" {
		res, err := config.ScanUserScope(*userDir)
		if err != nil {
			log.Fatalf("scan user scope: %v", err)
		}
		all = append(all, res.Files...)
		scanErrors = append(scanErrors, res.Errors...)
		covered = append(covered, *userDir)
	}

	if *projectRoots != "" {
		roots := strings.Split(*projectRoots, ",")
		res, err := config.ScanProjects(roots...)
		if err != nil {
			log.Fatalf("scan projects: %v", err)
		}
		all = append(all, res.Files...)
		scanErrors = append(scanErrors, res.Errors...)
		covered = append(covered, roots...)
	}

	combined := &config.Result{Files: all, Errors: scanErrors}
	stats, err := inventory.Sync(conn, combined, covered...)
	if err != nil {
		log.Fatalf("sync: %v", err)
	}

	report(all, stats, scanErrors)

	if orphans, err := inventory.Orphans(conn); err == nil && len(orphans) > 0 {
		fmt.Printf("\n孤児（リンク切れ）%d件:\n", len(orphans))
		for _, o := range orphans {
			fmt.Printf("  %s → %s\n", o.Path, o.SymlinkTarget)
		}
	}

	// 「設定が散逸している」ことの可視化 — 同じ種別で内容が割れているもの
	for _, kind := range []config.Kind{config.KindClaudeMD, config.KindSettings, config.KindMCPConfig} {
		groups, err := inventory.Duplicates(conn, string(kind))
		if err != nil || len(groups) <= 1 {
			continue
		}
		fmt.Printf("\n%s が %d 通りに分かれています:\n", kind, len(groups))
		for _, files := range groups {
			labels := make([]string, 0, len(files))
			for _, f := range files {
				label := f.Project
				if label == "" {
					label = "(user)"
				}
				labels = append(labels, label)
			}
			sort.Strings(labels)
			fmt.Printf("  - %s\n", strings.Join(labels, ", "))
		}
	}
}

func report(files []config.File, stats *inventory.SyncStats, errs []string) {
	byKind := map[config.Kind]int{}
	byProject := map[string]int{}
	symlinks := 0

	for _, f := range files {
		byKind[f.Kind]++
		if f.Project != "" {
			byProject[f.Project]++
		}
		if f.IsSymlink {
			symlinks++
		}
	}

	fmt.Printf("設定ファイル %d 件（追加 %d / 更新 %d / 削除 %d）\n",
		stats.Total, stats.Added, stats.Updated, stats.Removed)

	fmt.Println("\n種別ごと:")
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Printf("  %-16s %d\n", k, byKind[config.Kind(k)])
	}

	if symlinks > 0 {
		fmt.Printf("\nうちシンボリックリンク: %d 件（実体を壊さないよう書き込み時は要注意）\n", symlinks)
	}

	if len(byProject) > 0 {
		fmt.Printf("\n設定を持つプロジェクト: %d\n", len(byProject))
		names := make([]string, 0, len(byProject))
		for p := range byProject {
			names = append(names, p)
		}
		sort.Strings(names)
		for _, p := range names {
			fmt.Printf("  %-24s %d\n", p, byProject[p])
		}
	}

	if len(errs) > 0 {
		fmt.Printf("\n読めなかった場所 %d 件（スキャンは継続）:\n", len(errs))
		for i, e := range errs {
			if i >= 5 {
				fmt.Printf("  ... 他 %d 件\n", len(errs)-5)
				break
			}
			fmt.Printf("  %s\n", e)
		}
	}
}

func defaultUserDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}
