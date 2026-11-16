package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"my_DBMS/internal/parser"
	"my_DBMS/internal/sql"
	"my_DBMS/internal/storage"
)

// execute roda um comando SQL e devolve o texto para mostrar ao usuário.
func execute(db *storage.DB, input string) (string, error) {
	stmt, err := parse(input)
	if err != nil {
		return "", err
	}
	return run(db, stmt)
}

// parse transforma o texto do comando na AST.
func parse(input string) (parser.Statement, error) {
	tokens, err := sql.Lex(input)
	if err != nil {
		return nil, err
	}
	return parser.Parse(tokens)
}

// run executa um comando já analisado.
func run(db *storage.DB, stmt parser.Statement) (string, error) {
	switch s := stmt.(type) {
	case *parser.CreateTable:
		return createTable(db, s)
	case *parser.Insert:
		return insert(db, s)
	case *parser.Select:
		return selectRows(db, s)
	case *parser.Update:
		return update(db, s)
	case *parser.Delete:
		return deleteRows(db, s)
	case *parser.DropTable:
		if err := db.DropTable(s.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf("tabela %s removida", s.Name), nil
	}
	return "", fmt.Errorf("comando não suportado: %T", stmt)
}

func createTable(db *storage.DB, s *parser.CreateTable) (string, error) {
	columns := make([]storage.Column, len(s.Columns))
	for i, c := range s.Columns {
		columns[i] = storage.Column{Name: c.Name, Type: c.Type, NotNull: c.NotNull}
	}
	keys := make([]storage.Index, len(s.Keys))
	for i, k := range s.Keys {
		keys[i] = storage.Index{Kind: storage.Unique, Columns: k.Columns}
		if k.Primary {
			keys[i].Kind = storage.PrimaryKey
		}
	}

	if err := db.CreateTable(s.Name, columns, keys); err != nil {
		return "", err
	}
	return fmt.Sprintf("tabela %s criada", s.Name), nil
}

func insert(db *storage.DB, s *parser.Insert) (string, error) {
	schema, err := db.TableSchema(s.Table)
	if err != nil {
		return "", err
	}

	// positions diz em que coluna da tabela cai cada valor da linha. Sem
	// lista de colunas, é a ordem da tabela.
	var positions []int
	if s.Columns == nil {
		for i := range schema.Columns {
			positions = append(positions, i)
		}
	}
	for _, name := range s.Columns {
		i := schema.ColumnIndex(name)
		if i < 0 {
			return "", fmt.Errorf("coluna %q não existe na tabela %s", name, s.Table)
		}
		if slices.Contains(positions, i) {
			return "", fmt.Errorf("coluna %q repetida no INSERT", name)
		}
		positions = append(positions, i)
	}

	rows := make([][]storage.Value, len(s.Rows))
	for r, lits := range s.Rows {
		if len(lits) != len(positions) {
			return "", fmt.Errorf("esperava %d valores por linha, mas a linha %d tem %d", len(positions), r+1, len(lits))
		}
		// Coluna fora da lista fica NULL.
		row := make([]storage.Value, len(schema.Columns))
		for i := range row {
			row[i] = storage.Null
		}
		for j, p := range positions {
			col := schema.Columns[p]
			if row[p], err = literalValue(lits[j], col.Type); err != nil {
				return "", fmt.Errorf("coluna %s: %w", col.Name, err)
			}
		}
		rows[r] = row
	}

	if err := db.InsertRows(s.Table, rows); err != nil {
		return "", err
	}
	return count(len(rows), "linha inserida", "linhas inseridas"), nil
}

// update troca os valores das colunas do SET nas linhas que passam no WHERE.
// Primeiro junta todas as linhas que mudam, depois grava: mudar enquanto lê
// faria uma linha que mudou de lugar ser lida (e mudada) de novo.
func update(db *storage.DB, s *parser.Update) (string, error) {
	schema, err := db.TableSchema(s.Table)
	if err != nil {
		return "", err
	}
	where, err := compileWhere(s.Where, schema)
	if err != nil {
		return "", err
	}

	positions := make([]int, len(s.Set))
	values := make([]storage.Value, len(s.Set))
	for i, a := range s.Set {
		p := schema.ColumnIndex(a.Column)
		if p < 0 {
			return "", fmt.Errorf("coluna %q não existe na tabela %s", a.Column, s.Table)
		}
		if slices.Contains(positions[:i], p) {
			return "", fmt.Errorf("coluna %q repetida no SET", a.Column)
		}
		col := schema.Columns[p]
		if values[i], err = literalValue(a.Value, col.Type); err != nil {
			return "", fmt.Errorf("coluna %s: %w", col.Name, err)
		}
		positions[i] = p
	}

	var rids []storage.RID
	var rows [][]storage.Value
	for row, err := range db.Scan(s.Table) {
		if err != nil {
			return "", err
		}
		if where(row.Values) != triTrue {
			continue
		}
		changed := slices.Clone(row.Values)
		for i, p := range positions {
			changed[p] = values[i]
		}
		rids = append(rids, row.RID)
		rows = append(rows, changed)
	}
	if len(rows) > 0 {
		if err := db.UpdateRows(s.Table, rids, rows); err != nil {
			return "", err
		}
	}
	return count(len(rows), "linha atualizada", "linhas atualizadas"), nil
}

// deleteRows apaga as linhas que passam no WHERE. Sem WHERE, apaga todas.
func deleteRows(db *storage.DB, s *parser.Delete) (string, error) {
	schema, err := db.TableSchema(s.Table)
	if err != nil {
		return "", err
	}
	where, err := compileWhere(s.Where, schema)
	if err != nil {
		return "", err
	}

	var rids []storage.RID
	for row, err := range db.Scan(s.Table) {
		if err != nil {
			return "", err
		}
		if where(row.Values) == triTrue {
			rids = append(rids, row.RID)
		}
	}
	if len(rids) > 0 {
		if err := db.DeleteRows(s.Table, rids); err != nil {
			return "", err
		}
	}
	return count(len(rids), "linha removida", "linhas removidas"), nil
}

// count escreve "1 linha inserida" ou "3 linhas inseridas".
func count(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// literalValue confere se o literal serve para o tipo (INT ou TEXT) e devolve
// o valor como ele vai para o disco: NULL, número inteiro normalizado ou o
// texto. NOT NULL é conferido pelo storage.
func literalValue(lit parser.Literal, typ string) (storage.Value, error) {
	switch {
	case lit.Kind == parser.Null:
		return storage.Null, nil
	case typ == "INT" && lit.Kind == parser.Number:
		n, err := strconv.Atoi(lit.Text)
		if err != nil {
			return storage.Value{}, fmt.Errorf("%s não é um número inteiro", lit.Text)
		}
		return storage.Text(strconv.Itoa(n)), nil
	case typ == "TEXT" && lit.Kind == parser.String:
		return storage.Text(lit.Text), nil
	}
	return storage.Value{}, fmt.Errorf("esperava um valor %s, veio %s", typ, showLiteral(lit))
}

// showLiteral escreve o literal como no SQL, com aspas se for texto.
func showLiteral(lit parser.Literal) string {
	if lit.Kind == parser.String {
		return "'" + strings.ReplaceAll(lit.Text, "'", "''") + "'"
	}
	return lit.Text
}

// display é como um valor aparece na saída do SELECT.
func display(v storage.Value) string {
	if v.Null {
		return "NULL"
	}
	return v.Text
}

// selectResult é o resultado de um SELECT antes de virar texto: o REPL
// formata como tabela e o servidor manda as colunas e linhas em JSON.
type selectResult struct {
	Columns []storage.Column
	Rows    [][]storage.Value
}

func selectRows(db *storage.DB, s *parser.Select) (string, error) {
	r, err := querySelect(db, s)
	if err != nil {
		return "", err
	}
	return r.format(), nil
}

// querySelect lê a tabela, filtra pelo WHERE e escolhe as colunas pedidas.
func querySelect(db *storage.DB, s *parser.Select) (*selectResult, error) {
	table, err := db.TableSchema(s.Table)
	if err != nil {
		return nil, err
	}
	where, err := compileWhere(s.Where, table)
	if err != nil {
		return nil, err
	}

	// indexes diz quais colunas da tabela entram no resultado, em ordem.
	var indexes []int
	if s.Columns == nil {
		for i := range table.Columns {
			indexes = append(indexes, i)
		}
	}
	for _, name := range s.Columns {
		i := table.ColumnIndex(name)
		if i < 0 {
			return nil, fmt.Errorf("coluna %q não existe na tabela %s", name, s.Table)
		}
		indexes = append(indexes, i)
	}

	r := &selectResult{}
	for _, i := range indexes {
		r.Columns = append(r.Columns, table.Columns[i])
	}
	for row, err := range db.Scan(s.Table) {
		if err != nil {
			return nil, err
		}
		if where(row.Values) != triTrue {
			continue
		}
		picked := make([]storage.Value, len(indexes))
		for j, i := range indexes {
			picked[j] = row.Values[i]
		}
		r.Rows = append(r.Rows, picked)
	}
	return r, nil
}

// format desenha o resultado como o texto que o REPL imprime.
func (r *selectResult) format() string {
	headers := make([]string, len(r.Columns))
	right := make([]bool, len(r.Columns))
	for i, c := range r.Columns {
		headers[i] = c.Name
		right[i] = c.Type == "INT"
	}
	rows := make([][]string, len(r.Rows))
	for i, row := range r.Rows {
		rows[i] = make([]string, len(row))
		for j, v := range row {
			rows[i][j] = display(v)
		}
	}
	return formatTable(headers, right, rows)
}
