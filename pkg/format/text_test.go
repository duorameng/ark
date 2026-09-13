package format

import (
	"strings"
	"testing"
)

func TestStringWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"舱位 ID", 7},             // 舱(2) + 位(2) + ' '(1) + I(1) + D(1) = 7
		{"装载优先级", 10},           // 5 * 2 = 10
		{"极少变动 (冷数据)", 17},      // 4*2 + 1 + 1 + 3*2 + 1 = 17
		{"极少变动 (根级同级文件)", 23}, // 4*2 + 1 + 1 + 6*2 + 1 = 23
		{"中频变动", 8},              // 4 * 2 = 8
		{"[watchover]", 11},        // 11
		{"10.2 KB", 7},             // 7
	}

	for _, tt := range tests {
		got := StringWidth(tt.input)
		if got != tt.want {
			t.Errorf("StringWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestPadRightAndLeft(t *testing.T) {
	s := "中频变动" // 视觉宽度 8
	paddedR := PadRight(s, 12)
	if StringWidth(paddedR) != 12 {
		t.Errorf("PadRight width = %d, want 12", StringWidth(paddedR))
	}
	if paddedR != "中频变动    " {
		t.Errorf("PadRight content = %q", paddedR)
	}

	paddedL := PadLeft(s, 12)
	if StringWidth(paddedL) != 12 {
		t.Errorf("PadLeft width = %d, want 12", StringWidth(paddedL))
	}
	if paddedL != "    中频变动" {
		t.Errorf("PadLeft content = %q", paddedL)
	}
}

func TestTableAlignment(t *testing.T) {
	tbl := NewTable("舱位 ID", "冷热变动特征", "文件数", "预估大小")
	tbl.SetAlignment(2, AlignRight)
	tbl.SetAlignment(3, AlignRight)
	tbl.AddRow("[watchover]", "极少变动 (冷数据)", "1", "317 B")
	tbl.AddRow("[root_files]", "极少变动 (根级同级文件)", "4", "10.2 KB")
	tbl.AddRow("[ddns]", "中频变动", "4", "2.7 KB")

	lines := tbl.RenderLines()
	if len(lines) != 6 { // header + sep + 3 rows + sep = 6
		t.Fatalf("expected 6 lines, got %d", len(lines))
	}

	// 检查每一行（除最后非补齐列外）是否对齐
	// 第一列最大宽度: max("舱位 ID"(7), "[watchover]"(11), "[root_files]"(12), "[ddns]"(6)) = 12
	// 第二列最大宽度: max("冷热变动特征"(12), "极少变动 (冷数据)"(17), "极少变动 (根级同级文件)"(23), "中频变动"(8)) = 23
	// 第三列最大宽度: max("文件数"(6), "1"(1), "4"(1)) = 6
	// 第四列最大宽度: max("预估大小"(8), "317 B"(5), "10.2 KB"(7), "2.7 KB"(6)) = 8
	// spacing = 2
	// 总宽度 = 12 + 2 + 23 + 2 + 6 + 2 + 8 = 55
	wantSep := strings.Repeat("-", 55)
	if lines[1] != wantSep {
		t.Errorf("separator line 1 = %q, want %q", lines[1], wantSep)
	}
	if lines[5] != wantSep {
		t.Errorf("separator line 5 = %q, want %q", lines[5], wantSep)
	}

	for i, line := range lines {
		t.Logf("Line %d: %s", i, line)
	}
}
