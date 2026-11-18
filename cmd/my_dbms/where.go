package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"my_DBMS/internal/parser"
	"my_DBMS/internal/storage"
)

// tri é a lógica de três valores do SQL: comparar com NULL não dá verdadeiro
// nem falso, dá desconhecido. Com a ordem falso < desconhecido < verdadeiro,
// AND é o mínimo dos dois lados e OR é o máximo. O WHERE só deixa passar
// triTrue.
type tri int

const (
	triFalse tri = iota
	triUnknown
	triTrue
)

func toTri(b bool) tri {
	if b {
		return triTrue
	}
	return triFalse
}

// predicate diz se uma linha satisfaz a condição.
type predicate func(row []storage.Value) tri

// getter tira um valor da linha: uma coluna ou um literal fixo.
type getter func(row []storage.Value) storage.Value

// compileWhere confere a condição contra o escopo (as colunas existem e os
// tipos batem) e devolve o predicado. Conferir antes de ler as linhas faz o
// erro aparecer mesmo com a tabela vazia. Sem WHERE, toda linha passa.
func compileWhere(e parser.Expr, sc scope) (predicate, error) {
	if e == nil {
		return func([]storage.Value) tri { return triTrue }, nil
	}

	switch e := e.(type) {
	case *parser.Logical:
		left, err := compileWhere(e.Left, sc)
		if err != nil {
			return nil, err
		}
		right, err := compileWhere(e.Right, sc)
		if err != nil {
			return nil, err
		}
		if e.Op == "AND" {
			return func(row []storage.Value) tri { return min(left(row), right(row)) }, nil
		}
		return func(row []storage.Value) tri { return max(left(row), right(row)) }, nil

	case *parser.IsNull:
		get, _, err := compileOperand(e.Expr, sc, "")
		if err != nil {
			return nil, err
		}
		return func(row []storage.Value) tri { return toTri(get(row).Null != e.Not) }, nil

	case *parser.Comparison:
		return compileComparison(e, sc)
	}
	return nil, fmt.Errorf("condição inválida no WHERE")
}

// compileComparison resolve "a op b". O tipo da comparação vem da coluna, e
// um literal do outro lado precisa servir para ele. Entre dois literais, vale
// o tipo do primeiro que não for NULL.
func compileComparison(c *parser.Comparison, sc scope) (predicate, error) {
	typ := cmp.Or(columnType(c.Left, sc), columnType(c.Right, sc),
		literalType(c.Left), literalType(c.Right), "TEXT")

	left, leftType, err := compileOperand(c.Left, sc, typ)
	if err != nil {
		return nil, err
	}
	right, rightType, err := compileOperand(c.Right, sc, typ)
	if err != nil {
		return nil, err
	}
	if leftType != rightType {
		return nil, fmt.Errorf("não dá para comparar %s com %s", leftType, rightType)
	}

	return func(row []storage.Value) tri {
		a, b := left(row), right(row)
		if a.Null || b.Null {
			return triUnknown
		}
		return toTri(holds(c.Op, compareValues(a, b, typ)))
	}, nil
}

// compileOperand devolve o getter de um lado da comparação e o tipo dele.
// Um literal é convertido para typ uma vez só, aqui.
func compileOperand(e parser.Expr, sc scope, typ string) (getter, string, error) {
	switch e := e.(type) {
	case parser.ColumnRef:
		i, err := sc.find(e)
		if err != nil {
			return nil, "", err
		}
		return func(row []storage.Value) storage.Value { return row[i] }, sc.columns[i].Type, nil

	case parser.Literal:
		if typ == "" {
			typ = cmp.Or(literalType(e), "TEXT")
		}
		v, err := literalValue(e, typ)
		if err != nil {
			return nil, "", fmt.Errorf("WHERE: %w", err)
		}
		return func([]storage.Value) storage.Value { return v }, typ, nil
	}
	return nil, "", fmt.Errorf("operando inválido no WHERE")
}

// columnType devolve o tipo da coluna, ou "" se e não for uma coluna
// conhecida.
func columnType(e parser.Expr, sc scope) string {
	if ref, ok := e.(parser.ColumnRef); ok {
		if i, err := sc.find(ref); err == nil {
			return sc.columns[i].Type
		}
	}
	return ""
}

// literalType devolve o tipo natural de um literal: INT para número inteiro,
// REAL para número com ponto, TEXT para string, BOOLEAN para TRUE e FALSE e
// "" para NULL ou para o que não é literal.
func literalType(e parser.Expr) string {
	if lit, ok := e.(parser.Literal); ok {
		switch lit.Kind {
		case parser.Number:
			if strings.Contains(lit.Text, ".") {
				return "REAL"
			}
			return "INT"
		case parser.String:
			return "TEXT"
		case parser.Bool:
			return "BOOLEAN"
		}
	}
	return ""
}

// compareValues devolve -1, 0 ou 1. INT e REAL comparam como número (senão
// "10" viria antes de "9"). TEXT compara byte a byte, e BOOLEAN também:
// "FALSE" < "TRUE", que é a ordem do SQL.
func compareValues(a, b storage.Value, typ string) int {
	switch typ {
	case "INT":
		x, _ := strconv.Atoi(a.Text)
		y, _ := strconv.Atoi(b.Text)
		return cmp.Compare(x, y)
	case "REAL":
		x, _ := strconv.ParseFloat(a.Text, 64)
		y, _ := strconv.ParseFloat(b.Text, 64)
		return cmp.Compare(x, y)
	}
	return strings.Compare(a.Text, b.Text)
}

// holds diz se o resultado de compareValues satisfaz o operador.
func holds(op string, c int) bool {
	switch op {
	case "=":
		return c == 0
	case "<>":
		return c != 0
	case "<":
		return c < 0
	case ">":
		return c > 0
	case "<=":
		return c <= 0
	case ">=":
		return c >= 0
	}
	return false
}

// scope são as colunas que um comando enxerga: as de uma tabela ou, com JOIN,
// as de várias, uma depois da outra, na ordem do FROM. tables[i] é a tabela
// da coluna i. A linha que o predicado recebe segue a mesma ordem.
type scope struct {
	tables  []string
	columns []storage.Column
}

// tableScope é o escopo de uma tabela só.
func tableScope(table string, schema storage.Schema) scope {
	s := scope{columns: schema.Columns}
	for range schema.Columns {
		s.tables = append(s.tables, table)
	}
	return s
}

// join devolve o escopo com as colunas de other depois das de s. Concat
// copia: um append direto poderia escrever no array das colunas do schema,
// que é compartilhado com o catálogo.
func (s scope) join(other scope) scope {
	return scope{
		tables:  slices.Concat(s.tables, other.tables),
		columns: slices.Concat(s.columns, other.columns),
	}
}

// find devolve a posição da coluna. Um nome sem tabela precisa existir em uma
// tabela só do escopo; se estiver em duas, é ambíguo. Como uma tabela não
// repete nomes de coluna nem aparece duas vezes no FROM, dois achados são
// sempre de tabelas diferentes.
func (s scope) find(ref parser.ColumnRef) (int, error) {
	found := -1
	for i, c := range s.columns {
		if c.Name != ref.Name || (ref.Table != "" && s.tables[i] != ref.Table) {
			continue
		}
		if found >= 0 {
			return -1, fmt.Errorf("coluna %q é ambígua: use tabela.coluna (%s.%s ou %s.%s)",
				ref.Name, s.tables[found], ref.Name, s.tables[i], ref.Name)
		}
		found = i
	}
	switch {
	case found >= 0:
		return found, nil
	case ref.Table != "" && !slices.Contains(s.tables, ref.Table):
		return -1, fmt.Errorf("tabela %q não está no FROM", ref.Table)
	}
	return -1, fmt.Errorf("coluna %q não existe", ref.String())
}
