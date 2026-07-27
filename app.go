package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/chankei613/agent-config-manager/internal/api"
	"github.com/chankei613/agent-config-manager/internal/content"
	"github.com/chankei613/agent-config-manager/internal/db"
	"github.com/chankei613/agent-config-manager/internal/inventory"
	"github.com/chankei613/agent-config-manager/internal/snapshot"
	acmsync "github.com/chankei613/agent-config-manager/internal/sync"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gorm.io/gorm"
)

// App はWailsのバインディング。実処理は internal/api.Server が持っており、
// ここはWails固有の初期化とエラー通知だけを担当する。
// 同じ Server を cmd/acmserve のHTTP APIも使っているので、UIとAPIで挙動がズレない。
type App struct {
	ctx    context.Context
	conn   *gorm.DB
	server *api.Server
	ready  bool
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	dataDir := appDataDir()
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		runtime.LogErrorf(ctx, "data dir error: %s", err)
		return
	}

	conn, err := db.Init(filepath.Join(dataDir, "agent-config-manager.db"))
	if err != nil {
		runtime.LogErrorf(ctx, "db init error: %s", err)
		return
	}
	a.conn = conn

	a.server = api.New(conn, defaultUserDir(), defaultProjectRoots())
	a.ready = true

	// 初回スキャンは起動をブロックしないよう別goroutineで走らせる。
	// フロントは mount 時にデータを読むが、それはこのスキャンより先に終わりうる。
	// 完了イベントを出さないと「0件」の画面が出たまま更新されない。
	go func() {
		if _, err := a.server.Rescan(); err != nil {
			runtime.LogErrorf(ctx, "initial scan error: %s", err)
			runtime.EventsEmit(ctx, "scan:failed", err.Error())
			return
		}
		runtime.EventsEmit(ctx, "scan:complete")
	}()
}

func (a *App) shutdown(ctx context.Context) {
	if a.conn == nil {
		return
	}
	if sqlDB, err := a.conn.DB(); err == nil {
		sqlDB.Close()
	}
}

// ─── フロントエンドへ公開するメソッド ──────────────────────────────────────────

func (a *App) GetSummary() (*inventory.Summary, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return inventory.BuildSummary(a.conn)
}

func (a *App) GetMatrix() (*inventory.Matrix, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return inventory.BuildMatrix(a.conn)
}

// GetDrift は乖離レポートを返す。onlyDiverged=true なら割れているものだけ。
func (a *App) GetDrift(onlyDiverged bool) ([]inventory.Drift, error) {
	if !a.ready {
		return nil, errNotReady
	}

	reports, err := inventory.DriftReport(a.conn)
	if err != nil {
		return nil, err
	}
	if !onlyDiverged {
		return reports, nil
	}

	filtered := make([]inventory.Drift, 0, len(reports))
	for _, r := range reports {
		if r.Diverged {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func (a *App) GetOrphans() ([]db.ConfigFile, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return inventory.Orphans(a.conn)
}

// GetFiles は種別・プロジェクトで絞り込んだファイル一覧を返す（空文字は絞り込みなし）。
func (a *App) GetFiles(kind, project string) ([]db.ConfigFile, error) {
	if !a.ready {
		return nil, errNotReady
	}

	q := a.conn.Model(&db.ConfigFile{})
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if project != "" {
		q = q.Where("project = ?", project)
	}

	var rows []db.ConfigFile
	err := q.Order("kind asc, project asc, path asc").Find(&rows).Error
	return rows, err
}

// Rescan は再走査する。設定ファイルへの書き込みは一切行わない。
func (a *App) Rescan() (*inventory.SyncStats, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return a.server.Rescan()
}

// ─── スナップショット（バージョン管理） ────────────────────────────────────────

func (a *App) ListSnapshots() ([]db.Snapshot, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return snapshot.List(a.conn)
}

func (a *App) CreateSnapshot(label, note string) (*db.Snapshot, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return snapshot.Create(a.conn, label, note)
}

func (a *App) GetSnapshotEntries(id int) ([]snapshot.Entry, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return snapshot.Entries(a.conn, uint(id))
}

// PlanRestore は復元で何が起きるかを、何も書き換えずに返す。
// UIは必ずこれを見せてから RestoreSnapshot を呼ぶこと。
func (a *App) PlanRestore(id int) ([]snapshot.Change, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return snapshot.Plan(a.conn, uint(id))
}

// RestoreSnapshot は復元する。実行前に自動バックアップが取られるので取り消せる。
func (a *App) RestoreSnapshot(id int) (*snapshot.RestoreResult, error) {
	if !a.ready {
		return nil, errNotReady
	}

	result, err := snapshot.Restore(a.conn, uint(id))
	if err != nil {
		return nil, err
	}

	// 書き戻した内容をインベントリに反映する
	if _, err := a.server.Rescan(); err != nil {
		runtime.LogErrorf(a.ctx, "rescan after restore failed: %s", err)
	}
	return result, nil
}

func (a *App) DeleteSnapshot(id int) error {
	if !a.ready {
		return errNotReady
	}
	return snapshot.Delete(a.conn, uint(id))
}

// ─── 統一・テンプレート（Phase 4） ────────────────────────────────────────────

// PlanUnify は統一で何が起きるかを、何も書き換えずに返す。
func (a *App) PlanUnify(sourcePath string) ([]snapshot.Change, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return acmsync.UnifyPlan(a.conn, sourcePath)
}

// Unify は sourcePath の内容を、同じ設定を持つ他の場所へ配る。
func (a *App) Unify(sourcePath string) (*acmsync.UnifyResult, error) {
	if !a.ready {
		return nil, errNotReady
	}

	result, err := acmsync.Unify(a.conn, sourcePath)
	if err != nil {
		return nil, err
	}
	if _, err := a.server.Rescan(); err != nil {
		runtime.LogErrorf(a.ctx, "rescan after unify failed: %s", err)
	}
	return result, nil
}

func (a *App) ListTemplates() ([]db.Template, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return acmsync.ListTemplates(a.conn)
}

func (a *App) CreateTemplate(name, note, project string) (*db.Template, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return acmsync.CreateTemplate(a.conn, name, note, project)
}

func (a *App) GetTemplateFiles(id int) ([]acmsync.TemplateFile, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return acmsync.TemplateFiles(a.conn, uint(id))
}

func (a *App) PlanApplyTemplate(id int, targetDir string) ([]snapshot.Change, error) {
	if !a.ready {
		return nil, errNotReady
	}
	return acmsync.ApplyTemplatePlan(a.conn, uint(id), targetDir)
}

func (a *App) ApplyTemplate(id int, targetDir string) (*acmsync.ApplyResult, error) {
	if !a.ready {
		return nil, errNotReady
	}

	result, err := acmsync.ApplyTemplate(a.conn, uint(id), targetDir)
	if err != nil {
		return nil, err
	}
	if _, err := a.server.Rescan(); err != nil {
		runtime.LogErrorf(a.ctx, "rescan after apply failed: %s", err)
	}
	return result, nil
}

func (a *App) DeleteTemplate(id int) error {
	if !a.ready {
		return errNotReady
	}
	return acmsync.DeleteTemplate(a.conn, uint(id))
}

// ─── 中身の表示・差分 ──────────────────────────────────────────────────────────

// GetContent は1ファイルの中身を返す。
// 機密種別（settings.local.json / MCP設定）は自動でマスクされる。
func (a *App) GetContent(path string, alwaysMask bool) (*content.File, error) {
	if !a.ready {
		return nil, errNotReady
	}

	kind, err := a.kindForPath(path)
	if err != nil {
		return nil, err
	}
	return content.Read(path, kind, alwaysMask)
}

// GetDiff は2ファイルの行単位差分を返す。乖離の中身を確認するために使う。
func (a *App) GetDiff(left, right string) (*content.Diff, error) {
	if !a.ready {
		return nil, errNotReady
	}

	kind, err := a.kindForPath(left)
	if err != nil {
		return nil, err
	}
	return content.DiffFiles(left, right, kind)
}

// kindForPath はインベントリに載っているファイルに限定する（任意パスを読ませない）。
func (a *App) kindForPath(path string) (string, error) {
	var row db.ConfigFile
	if err := a.conn.Select("kind").First(&row, "path = ?", path).Error; err != nil {
		return "", errors.New("インベントリに無いファイルです")
	}
	return row.Kind, nil
}

// GetScanRoots は今どこを見ているかをUIに示す。
func (a *App) GetScanRoots() map[string]interface{} {
	if !a.ready {
		return map[string]interface{}{"user": "", "projects": []string{}}
	}
	return map[string]interface{}{
		"user":     a.server.UserDir,
		"projects": a.server.ProjectRoots,
	}
}
