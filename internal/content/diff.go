package content

import "strings"

// LineType は差分の各行の種類。
type LineType string

const (
	LineSame LineType = "same"
	LineAdd  LineType = "add" // 右側にだけある
	LineDel  LineType = "del" // 左側にだけある
)

// Line は差分1行。
type Line struct {
	Type LineType `json:"type"`
	// LeftNo / RightNo は行番号（該当しない側は0）。
	LeftNo  int    `json:"left_no"`
	RightNo int    `json:"right_no"`
	Text    string `json:"text"`
}

// Diff は2ファイルの差分。
type Diff struct {
	LeftPath  string `json:"left_path"`
	RightPath string `json:"right_path"`
	Lines     []Line `json:"lines"`
	Added     int    `json:"added"`
	Deleted   int    `json:"deleted"`
	// Masked はどちらかの内容をマスクしたか。
	Masked    bool `json:"masked"`
	Identical bool `json:"identical"`
}

// DiffFiles は2つのファイルを読んで行単位の差分を返す。
// 秘密はマスクしてから比較するので、差分表示にキーが露出しない。
func DiffFiles(leftPath, rightPath, kind string) (*Diff, error) {
	left, err := Read(leftPath, kind, false)
	if err != nil {
		return nil, err
	}
	right, err := Read(rightPath, kind, false)
	if err != nil {
		return nil, err
	}

	diff := &Diff{
		LeftPath:  leftPath,
		RightPath: rightPath,
		Masked:    left.Masked || right.Masked,
		Lines:     DiffLines(left.Text, right.Text),
	}

	for _, l := range diff.Lines {
		switch l.Type {
		case LineAdd:
			diff.Added++
		case LineDel:
			diff.Deleted++
		}
	}
	diff.Identical = diff.Added == 0 && diff.Deleted == 0

	return diff, nil
}

// DiffLines は行単位のLCS差分を返す。
// 設定ファイルは数十〜数百行なので、素直なO(n*m)のDPで十分。
func DiffLines(leftText, rightText string) []Line {
	left := splitLines(leftText)
	right := splitLines(rightText)

	lcs := buildLCSTable(left, right)

	lines := make([]Line, 0, len(left)+len(right))
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		switch {
		case left[i] == right[j]:
			lines = append(lines, Line{Type: LineSame, LeftNo: i + 1, RightNo: j + 1, Text: left[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			lines = append(lines, Line{Type: LineDel, LeftNo: i + 1, Text: left[i]})
			i++
		default:
			lines = append(lines, Line{Type: LineAdd, RightNo: j + 1, Text: right[j]})
			j++
		}
	}
	for ; i < len(left); i++ {
		lines = append(lines, Line{Type: LineDel, LeftNo: i + 1, Text: left[i]})
	}
	for ; j < len(right); j++ {
		lines = append(lines, Line{Type: LineAdd, RightNo: j + 1, Text: right[j]})
	}

	return lines
}

// buildLCSTable は後ろ向きに最長共通部分列の長さ表を作る。
func buildLCSTable(left, right []string) [][]int {
	table := make([][]int, len(left)+1)
	for i := range table {
		table[i] = make([]int, len(right)+1)
	}

	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	return table
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	// 末尾の改行で空行が増えないようにする
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}
