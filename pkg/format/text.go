package format

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// RuneWidth 返回单个字符在等宽终端中的视觉显示列宽 (基于 Unicode East Asian Width 国际标准)
func RuneWidth(r rune) int {
	return runewidth.RuneWidth(r)
}

// StringWidth 计算字符串在等宽终端下的真实视觉列宽 (精准处理 CJK 汉字、Emoji、全角字符与 ASCII 混排)
func StringWidth(s string) int {
	return runewidth.StringWidth(s)
}

// PadRight 对字符串进行右补空格 (左对齐，视觉宽度达到 targetWidth)
func PadRight(s string, targetWidth int) string {
	return runewidth.FillRight(s, targetWidth)
}

// PadLeft 对字符串进行左补空格 (右对齐，视觉宽度达到 targetWidth)
func PadLeft(s string, targetWidth int) string {
	return runewidth.FillLeft(s, targetWidth)
}

// FormatBytes 将字节数转换为人类友好的可读字符串 (如 317 B, 10.2 KB, 819.9 MB, 2.4 GB)
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Alignment 对齐方式
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
)

// Table 用于在等宽终端中自适应计算 CJK 字符真实视觉宽度并生成 100% 对齐的表格
type Table struct {
	headers []string
	aligns  []Alignment
	rows    [][]string
	spacing int
}

// NewTable 创建一个新表格
func NewTable(headers ...string) *Table {
	return &Table{
		headers: headers,
		aligns:  make([]Alignment, len(headers)),
		spacing: 2,
	}
}

// SetSpacing 设置列与列之间的空格数 (默认 2)
func (t *Table) SetSpacing(spacing int) *Table {
	if spacing >= 1 {
		t.spacing = spacing
	}
	return t
}

// SetAlignment 设置某一列的对齐方式 (左对齐或右对齐)
func (t *Table) SetAlignment(colIndex int, align Alignment) *Table {
	if colIndex >= 0 && colIndex < len(t.aligns) {
		t.aligns[colIndex] = align
	}
	return t
}

// AddRow 添加一行数据
func (t *Table) AddRow(cols ...string) *Table {
	row := make([]string, len(cols))
	copy(row, cols)
	t.rows = append(t.rows, row)
	return t
}

// RenderLines 渲染表格为各行字符串 (包含表头、分割线、数据行和底部分割线)
func (t *Table) RenderLines() []string {
	numCols := len(t.headers)
	if numCols == 0 {
		return nil
	}

	// 1. 统计每一列的最大视觉宽度
	colWidths := make([]int, numCols)
	for i, h := range t.headers {
		w := StringWidth(h)
		if w > colWidths[i] {
			colWidths[i] = w
		}
	}
	for _, row := range t.rows {
		for i := 0; i < numCols && i < len(row); i++ {
			w := StringWidth(row[i])
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	// 2. 格式化单行的辅助函数
	formatRow := func(cells []string) string {
		var sb strings.Builder
		for i := 0; i < numCols; i++ {
			val := ""
			if i < len(cells) {
				val = cells[i]
			}
			targetW := colWidths[i]

			// 如果是最后一列且左对齐，可直接输出无需尾部补空
			isLast := i == numCols-1
			var padded string
			align := AlignLeft
			if i < len(t.aligns) {
				align = t.aligns[i]
			}

			if isLast && align == AlignLeft {
				padded = val
			} else {
				if align == AlignRight {
					padded = PadLeft(val, targetW)
				} else {
					padded = PadRight(val, targetW)
				}
			}

			sb.WriteString(padded)
			if !isLast {
				sb.WriteString(strings.Repeat(" ", t.spacing))
			}
		}
		return sb.String()
	}

	// 3. 计算整行总视觉宽度以生成分割线
	totalWidth := 0
	for i, w := range colWidths {
		totalWidth += w
		if i < numCols-1 {
			totalWidth += t.spacing
		}
	}

	var lines []string
	// 表头
	lines = append(lines, formatRow(t.headers))
	// 分割线
	sep := strings.Repeat("-", totalWidth)
	lines = append(lines, sep)
	// 数据行
	for _, row := range t.rows {
		lines = append(lines, formatRow(row))
	}
	// 底部分割线
	lines = append(lines, sep)

	return lines
}

// Render 渲染表格为完整字符串 (以换行符结尾)
func (t *Table) Render() string {
	lines := t.RenderLines()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

