package main

import (
	"cmp"
	"fmt"
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

// compileWhere confere a condição contra o schema (as colunas existem e os
// tipos batem) e devolve o predicado. Conferir antes de ler as linhas faz o
// erro aparecer mesmo com a tabela vazia. Sem WHERE, toda linha passa.
func compileWhere(e parser.Expr, schema storage.Schema) (predicate, error) {
	if e == nil {
		return func([]storage.Value) tri { return triTrue }, nil
	}

	switch e := e.(type) {
	case *parser.Logical:
		left, err := compileWhere(e.Left, schema)
		if err != nil {
			return nil, err
		}
		right, err := compileWhere(e.Right, schema)
		if err != nil {
			return nil, err
		}
		if e.Op == "AND" {
			return func(row []storage.Value) tri { return min(left(row), right(row)) }, nil
		}
		return func(row []storage.Value) tri { return max(left(row), right(row)) }, nil

	case *parser.IsNull:
		get, _, err := compileOperand(e.Expr, schema, "")
		if err != nil {
			return nil, err
		}
		return func(row []storage.Value) tri { return toTri(get(row).Null != e.Not) }, nil

	case *parser.Comparison:
		return compileComparison(e, schema)
	}
	return nil, fmt.Errorf("condição inválida no WHERE")
}

// compileComparison resolve "a op b". O tipo da comparação vem da coluna, e
// um literal do outro lado precisa servir para ele. Entre dois literais, vale
// o tipo do primeiro que não for NULL.
func compileComparison(c *parser.Comparison, schema storage.Schema) (predicate, error) {
	typ := cmp.Or(columnType(c.Left, schema), columnType(c.Right, schema),
		literalType(c.Left), literalType(c.Right), "TEXT")

	left, leftType, err := compileOperand(c.Left, schema, typ)
	if err != nil {
		return nil, err
	}
	right, rightType, err := compileOperand(c.Right, schema, typ)
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
func compileOperand(e parser.Expr, schema storage.Schema, typ string) (getter, string, error) {
	switch e := e.(type) {
	case parser.ColumnRef:
		i := schema.ColumnIndex(e.Name)
		if i < 0 {
			return nil, "", fmt.Errorf("coluna %q não existe", e.Name)
		}
		return func(row []storage.Value) storage.Value { return row[i] }, schema.Columns[i].Type, nil

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
func columnType(e parser.Expr, schema storage.Schema) string {
	if ref, ok := e.(parser.ColumnRef); ok {
		if i := schema.ColumnIndex(ref.Name); i >= 0 {
			return schema.Columns[i].Type
		}
	}
	return ""
}

// literalType devolve o tipo natural de um literal: INT para número, TEXT
// para string e "" para NULL ou para o que não é literal.
func literalType(e parser.Expr) string {
	if lit, ok := e.(parser.Literal); ok {
		switch lit.Kind {
		case parser.Number:
			return "INT"
		case parser.String:
			return "TEXT"
		}
	}
	return ""
}

// compareValues devolve -1, 0 ou 1. INT compara como número (senão "10"
// viria antes de "9"), TEXT compara byte a byte.
func compareValues(a, b storage.Value, typ string) int {
	if typ == "INT" {
		x, _ := strconv.Atoi(a.Text)
		y, _ := strconv.Atoi(b.Text)
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
