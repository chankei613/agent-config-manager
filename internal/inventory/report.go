package inventory

import (
	"sort"

	"github.com/chankei613/agent-config-manager/internal/db"
	"gorm.io/gorm"
)

// Matrix は「種別 × プロジェクト」でどこに何があるかを示す表。
// 「また .claude フォルダを探している」状態を一目で終わらせるための中心ビュー。
type Matrix struct {
	Kinds    []string     `json:"kinds"`    // 行（設定種別）
	Projects []string     `json:"projects"` // 列（"" はユーザースコープ = 全プロジェクト共通）
	Cells    []MatrixCell `json:"cells"`
}

type MatrixCell struct {
	Kind    string `json:"kind"`
	Project string `json:"project"`
	Count   int    `json:"count"`
	// Hash が空でないなら、そのセルの内容は1種類に揃っている。
	// 複数ファイルが別内容なら空になる。
	Hash string `json:"hash"`
}

// BuildMatrix はインベントリ全体から種別×プロジェクトの表を組み立てる。
func BuildMatrix(conn *gorm.DB) (*Matrix, error) {
	var rows []db.ConfigFile
	if err := conn.Order("kind asc, project asc").Find(&rows).Error; err != nil {
		return nil, err
	}

	type key struct{ kind, project string }
	counts := map[key]int{}
	hashes := map[key]map[string]bool{}
	kindSet := map[string]bool{}
	projectSet := map[string]bool{}

	for _, row := range rows {
		k := key{row.Kind, row.Project}
		counts[k]++
		if hashes[k] == nil {
			hashes[k] = map[string]bool{}
		}
		hashes[k][row.Hash] = true
		kindSet[row.Kind] = true
		projectSet[row.Project] = true
	}

	matrix := &Matrix{
		Kinds:    sortedKeys(kindSet),
		Projects: sortedKeys(projectSet),
		Cells:    []MatrixCell{},
	}

	for k, count := range counts {
		cell := MatrixCell{Kind: k.kind, Project: k.project, Count: count}
		// そのセルの内容が1種類に揃っているときだけハッシュを載せる
		if len(hashes[k]) == 1 {
			for h := range hashes[k] {
				cell.Hash = h
			}
		}
		matrix.Cells = append(matrix.Cells, cell)
	}

	sort.Slice(matrix.Cells, func(i, j int) bool {
		if matrix.Cells[i].Kind != matrix.Cells[j].Kind {
			return matrix.Cells[i].Kind < matrix.Cells[j].Kind
		}
		return matrix.Cells[i].Project < matrix.Cells[j].Project
	})

	return matrix, nil
}

// DriftGroup は同一内容のファイルの集まり。
type DriftGroup struct {
	Hash     string   `json:"hash"`
	Count    int      `json:"count"`
	Projects []string `json:"projects"` // "" はユーザースコープ
	Paths    []string `json:"paths"`
}

// Drift は「同じ設定が複数の場所にあり、内容が割れている」状態のレポート。
//
// 比較単位は種別ではなく **Identity（同じ相対パスを持つ設定）** であることが重要。
// 種別でまとめてしまうと、153個ある Worker 定義のように
// 「そもそも内容が違って当たり前のファイル群」を分裂と誤検知する。
// 比較すべきなのは「同じ役割の設定が複数プロジェクトに存在し、中身がズレている」ケースだけ。
type Drift struct {
	Kind string `json:"kind"`
	// Identity は比較単位（例: "CLAUDE.md", "agents/foo.md"）。
	Identity string `json:"identity"`
	// Groups は内容ごとのまとまり。多い順に並ぶので、先頭が「多数派＝統一先の候補」。
	Groups []DriftGroup `json:"groups"`
	// Diverged は2種類以上に割れているかどうか。
	Diverged bool `json:"diverged"`
}

// DriftReport は「複数の場所に存在する同一設定」の乖離状況を返す。
// kinds が空なら全種別を対象にする。
// 1箇所にしか存在しない設定は比較対象が無いためレポートに含めない。
func DriftReport(conn *gorm.DB, kinds ...string) ([]Drift, error) {
	q := conn.Model(&db.ConfigFile{})
	if len(kinds) > 0 {
		q = q.Where("kind IN ?", kinds)
	}

	var rows []db.ConfigFile
	if err := q.Order("kind asc, rel_path asc, project asc").Find(&rows).Error; err != nil {
		return nil, err
	}

	// スコープも比較単位に含める。ユーザー全体設定（~/.claude/settings.local.json）と
	// プロジェクト固有設定は役割が違うので、内容が異なっていて当然であり比較してはいけない。
	// 結果として比較はプロジェクト間でのみ行われる（ユーザースコープは1箇所しか無いため）。
	type key struct{ scope, kind, identity string }
	byIdentity := map[key][]db.ConfigFile{}
	for _, row := range rows {
		k := key{row.Scope, row.Kind, row.RelPath}
		byIdentity[k] = append(byIdentity[k], row)
	}

	reports := make([]Drift, 0, len(byIdentity))
	for k, files := range byIdentity {
		// 1箇所にしか無いものは「揃っている/割れている」を判定しようがない
		if len(files) < 2 {
			continue
		}
		reports = append(reports, buildDrift(k.kind, k.identity, files))
	}

	// 割れているものを先に見せる。次いで種別・識別子順で安定させる。
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Diverged != reports[j].Diverged {
			return reports[i].Diverged
		}
		if reports[i].Kind != reports[j].Kind {
			return reports[i].Kind < reports[j].Kind
		}
		return reports[i].Identity < reports[j].Identity
	})

	return reports, nil
}

func buildDrift(kind, identity string, files []db.ConfigFile) Drift {
	byHash := map[string][]db.ConfigFile{}
	for _, f := range files {
		byHash[f.Hash] = append(byHash[f.Hash], f)
	}

	groups := make([]DriftGroup, 0, len(byHash))
	for hash, members := range byHash {
		group := DriftGroup{Hash: hash, Count: len(members)}
		for _, m := range members {
			group.Projects = append(group.Projects, m.Project)
			group.Paths = append(group.Paths, m.Path)
		}
		sort.Strings(group.Projects)
		sort.Strings(group.Paths)
		groups = append(groups, group)
	}

	// 多数派を先頭に。同数ならハッシュ順で安定させる。
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Hash < groups[j].Hash
	})

	return Drift{Kind: kind, Identity: identity, Groups: groups, Diverged: len(groups) > 1}
}

// Summary はダッシュボード用の要約。
type Summary struct {
	TotalFiles   int            `json:"total_files"`
	ByKind       map[string]int `json:"by_kind"`
	ProjectCount int            `json:"project_count"`
	// ViaSymlink はここへ書くと別の場所の実体が変わるファイル数（書き込み時の注意対象）
	ViaSymlink int `json:"via_symlink"`
	Orphans    int `json:"orphans"`
	// DivergedKinds は内容が2種類以上に割れている種別
	DivergedKinds []string `json:"diverged_kinds"`
}

func BuildSummary(conn *gorm.DB) (*Summary, error) {
	var rows []db.ConfigFile
	if err := conn.Find(&rows).Error; err != nil {
		return nil, err
	}

	summary := &Summary{ByKind: map[string]int{}, DivergedKinds: []string{}}
	projects := map[string]bool{}

	for _, row := range rows {
		summary.TotalFiles++
		summary.ByKind[row.Kind]++
		if row.Project != "" {
			projects[row.Project] = true
		}
		if row.ViaSymlink {
			summary.ViaSymlink++
		}
		if row.Broken {
			summary.Orphans++
		}
	}
	summary.ProjectCount = len(projects)

	drifts, err := DriftReport(conn)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, d := range drifts {
		if d.Diverged && !seen[d.Kind] {
			seen[d.Kind] = true
			summary.DivergedKinds = append(summary.DivergedKinds, d.Kind)
		}
	}
	sort.Strings(summary.DivergedKinds)

	return summary, nil
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
