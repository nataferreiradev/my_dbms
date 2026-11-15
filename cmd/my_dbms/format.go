package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// formatTable desenha o resultado de um SELECT como texto alinhado:
//
//	id | nome
//	---+-----
//	 1 | Ana
//	(1 linha)
//
// As colunas marcadas em right (as numéricas) ficam alinhadas à direita,
// cabeçalho incluído. A largura conta runas, não bytes, para acentos não
// desalinharem.
func formatTable(headers []string, right []bool, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				b.WriteString(" | ")
			}
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell))
			if right[i] {
				b.WriteString(pad)
			}
			b.WriteString(cell)
			// A última coluna não leva espaço no fim.
			if !right[i] && i < len(cells)-1 {
				b.WriteString(pad)
			}
		}
		b.WriteString("\n")
	}

	writeRow(headers)
	for i, w := range widths {
		if i > 0 {
			b.WriteString("-+-")
		}
		b.WriteString(strings.Repeat("-", w))
	}
	b.WriteString("\n")
	for _, row := range rows {
		writeRow(row)
	}

	if len(rows) == 1 {
		b.WriteString("(1 linha)")
	} else {
		fmt.Fprintf(&b, "(%d linhas)", len(rows))
	}
	return b.String()
}
