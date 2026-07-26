package main

import (
	"errors"
	"os"
	"path/filepath"
)

var errNotReady = errors.New("アプリの初期化が完了していません")

// appDataDir はDBの置き場所。設定ファイルのある場所とは分離する。
func appDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Library", "Application Support", "agent-config-manager")
}

// defaultUserDir はユーザースコープの設定ディレクトリ。
func defaultUserDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// defaultProjectRoots はプロジェクトを探す既定のルート。
// 実在するものだけを返す（存在しないパスを渡してもスキャナは黙って無視するが、
// UIに「どこを見ているか」を出す際に嘘を表示しないため）。
func defaultProjectRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	candidates := []string{
		filepath.Join(home, ".claude", "mcp_mini", "cometinc"),
		filepath.Join(home, ".claude", "comet_mcp"),
		filepath.Join(home, "src"),
		filepath.Join(home, "projects"),
	}

	roots := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			roots = append(roots, c)
		}
	}
	return roots
}
