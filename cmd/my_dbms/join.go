package main

import (
	"fmt"
	"slices"
	"strings"

	"my_DBMS/internal/parser"
	"my_DBMS/internal/storage"
)

// joinRows lê as tabelas e junta da esquerda para a direita: a tabela do FROM
// com o primeiro JOIN, esse resultado com o segundo, e assim por diante. Cada
// tabela é lida inteira para a memória uma vez, porque o nested loop percorre
// o lado de dentro de novo para cada linha de fora, e porque assim só uma
// transação de leitura fica aberta por vez.
func (q *selectQuery) joinRows(db *storage.DB) ([][]storage.Value, error) {
	from, err := db.ReadTable(q.stmt.Table)
	if err != nil {
		return nil, err
	}
	rows, width := from.Rows, len(q.schema.Columns)
	for i, j := range q.stmt.Joins {
		right, err := db.ReadTable(j.Table)
		if err != nil {
			return nil, err
		}
		rows = nestedLoop(j.Kind, rows, right.Rows, width, q.widths[i], q.ons[i])
		width += q.widths[i]
	}
	return rows, nil
}

// nestedLoop junta cada linha de left com cada linha de right e fica com os
// pares em que o ON dá verdadeiro (NULL = NULL dá desconhecido e não casa).
// Cada linha do resultado são as colunas de left seguidas das de right.
//
//   - INNER: só os pares que casam.
//   - LEFT: também as linhas de left sem par, com NULL nas colunas de right.
//   - RIGHT: também as linhas de right sem par, com NULL nas colunas de left.
//     Elas vêm no fim, depois dos pares, na ordem de right.
//
// Sem ORDER BY, os pares saem na ordem de left e, para cada linha de left, na
// ordem de right.
func nestedLoop(kind string, left, right [][]storage.Value, leftWidth, rightWidth int, on predicate) [][]storage.Value {
	var out [][]storage.Value
	rightMatched := make([]bool, len(right))
	// pair é reaproveitado para testar o ON; só os pares que entram no
	// resultado ganham uma cópia.
	pair := make([]storage.Value, 0, leftWidth+rightWidth)
	for _, l := range left {
		matched := false
		for k, r := range right {
			pair = append(append(pair[:0], l...), r...)
			if on(pair) != triTrue {
				continue
			}
			out = append(out, slices.Clone(pair))
			matched = true
			rightMatched[k] = true
		}
		if !matched && kind == "LEFT" {
			out = append(out, slices.Concat(l, nulls(rightWidth)))
		}
	}
	if kind == "RIGHT" {
		for k, r := range right {
			if !rightMatched[k] {
				out = append(out, slices.Concat(nulls(leftWidth), r))
			}
		}
	}
	return out
}

// nulls devolve n valores NULL.
func nulls(n int) []storage.Value {
	row := make([]storage.Value, n)
	for i := range row {
		row[i] = storage.Null
	}
	return row
}

// explainJoin descreve um SELECT com JOIN. Por enquanto todo JOIN é nested
// loop com varredura completa dos dois lados: o choosePlan olha o WHERE com o
// schema de uma tabela só e não sabe escolher índice para um JOIN.
func explainJoin(db *storage.DB, s *parser.Select) (string, error) {
	if _, err := compileSelect(db, s); err != nil {
		return "", err
	}
	lines := []string{"varredura completa na tabela " + s.Table}
	for _, j := range s.Joins {
		lines = append(lines, fmt.Sprintf("%s JOIN por nested loop: varredura completa na tabela %s", j.Kind, j.Table))
	}
	return strings.Join(lines, "\n"), nil
}
