package main

import (
	"fmt"
	"iter"
	"strings"

	"my_DBMS/internal/parser"
	"my_DBMS/internal/storage"
)

// plan é como as linhas de uma tabela vão ser lidas para um comando: a
// tabela inteira (varredura completa) ou só um pedaço de um índice. Nos dois
// casos o WHERE inteiro ainda é aplicado em cada linha; o índice só evita ler
// as linhas que com certeza não passariam.
type plan struct {
	table string
	index *storage.Index // nil: varredura completa
	r     storage.IndexRange
	used  []string // as condições que o índice resolve, para o EXPLAIN
}

// rows devolve as linhas que o plano lê.
func (p plan) rows(db *storage.DB) iter.Seq2[storage.Row, error] {
	if p.index == nil {
		return db.Scan(p.table)
	}
	return db.IndexScan(p.table, p.index.Name, p.r)
}

// String descreve o plano, como o EXPLAIN mostra.
func (p plan) String() string {
	if p.index == nil {
		return "varredura completa na tabela " + p.table
	}
	return fmt.Sprintf("busca no índice %s da tabela %s (%s)", p.index.Name, p.table, strings.Join(p.used, " AND "))
}

// cond é uma comparação "coluna op valor" tirada do WHERE, já com o valor
// convertido para o tipo da coluna.
type cond struct {
	column string
	op     string
	value  storage.Value
	text   string
}

// choosePlan escolhe entre a varredura completa e um índice. Só servem as
// comparações ligadas por AND no nível de cima do WHERE: com OR, uma linha
// pode passar por um lado que o índice não cobre.
//
// Um índice (a, b, c) serve quando o WHERE fixa as primeiras colunas com = e,
// opcionalmente, limita a seguinte com <, <=, > ou >=. Ganha o índice que
// fixa mais colunas, e um intervalo vale menos que um =. Empate fica com o
// primeiro índice da tabela (a chave primária, se houver).
func choosePlan(table string, schema storage.Schema, where parser.Expr) plan {
	conds := conditions(where, schema)
	best, bestScore := plan{table: table}, 0
	for i := range schema.Indexes {
		idx := &schema.Indexes[i]
		p := plan{table: table, index: idx}
		score := 0
		for _, col := range idx.Columns {
			if c, ok := findCond(conds, col, "="); ok {
				p.r.Eq = append(p.r.Eq, c.value)
				p.used = append(p.used, c.text)
				score += 2
				continue
			}
			for _, c := range conds {
				switch {
				case c.column != col:
				case (c.op == ">" || c.op == ">=") && p.r.From == nil:
					p.r.From = &c.value
					p.used = append(p.used, c.text)
				case (c.op == "<" || c.op == "<=") && p.r.To == nil:
					p.r.To = &c.value
					p.used = append(p.used, c.text)
				}
			}
			if p.r.From != nil || p.r.To != nil {
				score++
			}
			break
		}
		if score > bestScore {
			best, bestScore = p, score
		}
	}
	return best
}

func findCond(conds []cond, column, op string) (cond, bool) {
	for _, c := range conds {
		if c.column == column && c.op == op {
			return c, true
		}
	}
	return cond{}, false
}

// conditions junta as comparações "coluna op literal" ligadas por AND no
// nível de cima do WHERE. "5 < id" vira "id > 5". Ficam de fora <>, NULL e
// valores que não servem para o tipo da coluna (o compileWhere já reclamou
// deles antes).
func conditions(e parser.Expr, schema storage.Schema) []cond {
	switch e := e.(type) {
	case *parser.Logical:
		if e.Op != "AND" {
			return nil
		}
		return append(conditions(e.Left, schema), conditions(e.Right, schema)...)
	case *parser.Comparison:
		col, colOK := e.Left.(parser.ColumnRef)
		lit, litOK := e.Right.(parser.Literal)
		op := e.Op
		if !colOK || !litOK {
			col, colOK = e.Right.(parser.ColumnRef)
			lit, litOK = e.Left.(parser.Literal)
			op = flip[op]
		}
		i := schema.ColumnIndex(col.Name)
		if !colOK || !litOK || i < 0 || op == "<>" || op == "" || lit.Kind == parser.Null {
			return nil
		}
		v, err := literalValue(lit, schema.Columns[i].Type)
		if err != nil {
			return nil
		}
		return []cond{{column: col.Name, op: op, value: v, text: col.Name + " " + op + " " + showLiteral(lit)}}
	}
	return nil
}

// flip é o operador com os lados trocados: 5 < id é o mesmo que id > 5.
var flip = map[string]string{"=": "=", "<>": "<>", "<": ">", ">": "<", "<=": ">=", ">=": "<="}
