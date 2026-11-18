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
	case *parser.CreateIndex:
		if err := db.CreateIndex(s.Name, s.Table, s.Columns); err != nil {
			return "", err
		}
		return fmt.Sprintf("índice %s criado", s.Name), nil
	case *parser.DropIndex:
		if err := db.DropIndex(s.Name); err != nil {
			return "", err
		}
		return fmt.Sprintf("índice %s removido", s.Name), nil
	case *parser.Explain:
		return explain(db, s)
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
	where, err := compileWhere(s.Where, tableScope(s.Table, schema))
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
	for row, err := range choosePlan(s.Table, schema, s.Where).rows(db) {
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
	where, err := compileWhere(s.Where, tableScope(s.Table, schema))
	if err != nil {
		return "", err
	}

	var rids []storage.RID
	for row, err := range choosePlan(s.Table, schema, s.Where).rows(db) {
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

// explain mostra como um SELECT, UPDATE ou DELETE buscaria as linhas, sem
// rodar o comando.
func explain(db *storage.DB, s *parser.Explain) (string, error) {
	var table string
	var where parser.Expr
	switch st := s.Statement.(type) {
	case *parser.Select:
		table, where = st.Table, st.Where
	case *parser.Update:
		table, where = st.Table, st.Where
	case *parser.Delete:
		table, where = st.Table, st.Where
	default:
		return "", fmt.Errorf("EXPLAIN não suporta %T", s.Statement)
	}
	schema, err := db.TableSchema(table)
	if err != nil {
		return "", err
	}
	if _, err := compileWhere(where, tableScope(table, schema)); err != nil {
		return "", err
	}
	return choosePlan(table, schema, where).String(), nil
}

// count escreve "1 linha inserida" ou "3 linhas inseridas".
func count(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// literalValue confere se o literal serve para o tipo da coluna e devolve o
// valor como ele vai para o disco: NULL, número normalizado ("007" vira "7",
// "1.50" vira "1.5"), TRUE/FALSE ou o texto. NOT NULL é conferido pelo
// storage.
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
	case typ == "REAL" && lit.Kind == parser.Number:
		f, err := strconv.ParseFloat(lit.Text, 64)
		if err != nil {
			// O lexer só deixa passar dígitos, então o erro aqui é número
			// grande demais para um float64.
			return storage.Value{}, fmt.Errorf("%s não cabe num REAL", lit.Text)
		}
		return storage.Text(storage.FormatReal(f)), nil
	case typ == "BOOLEAN" && lit.Kind == parser.Bool:
		return storage.Text(lit.Text), nil
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

// querySelect lê as linhas pelo plano escolhido, filtra pelo WHERE e escolhe
// as colunas pedidas.
func querySelect(db *storage.DB, s *parser.Select) (*selectResult, error) {
	table, err := db.TableSchema(s.Table)
	if err != nil {
		return nil, err
	}
	sc := tableScope(s.Table, table)
	where, err := compileWhere(s.Where, sc)
	if err != nil {
		return nil, err
	}
	indexes, err := projection(sc, s.Columns)
	if err != nil {
		return nil, err
	}

	r := &selectResult{Columns: resultColumns(sc, indexes)}
	for row, err := range choosePlan(s.Table, table, s.Where).rows(db) {
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

// projection diz quais colunas do escopo entram no resultado, em ordem.
// columns nil é o *: todas, na ordem do escopo.
func projection(sc scope, columns []parser.ColumnRef) ([]int, error) {
	var indexes []int
	if columns == nil {
		for i := range sc.columns {
			indexes = append(indexes, i)
		}
	}
	for _, ref := range columns {
		i, err := sc.find(ref)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, i)
	}
	return indexes, nil
}

// resultColumns monta as colunas do resultado. O nome ganha a tabela na
// frente (pessoas.id) só quando outra coluna do resultado, de outra tabela,
// tem o mesmo nome; sem isso, o cabeçalho de um SELECT com JOIN teria dois
// "id" sem dizer qual é qual. NOT NULL não é copiado, porque um JOIN externo
// preenche com NULL até coluna NOT NULL.
func resultColumns(sc scope, indexes []int) []storage.Column {
	columns := make([]storage.Column, len(indexes))
	for j, i := range indexes {
		c := sc.columns[i]
		name := c.Name
		for _, k := range indexes {
			if sc.columns[k].Name == c.Name && sc.tables[k] != sc.tables[i] {
				name = sc.tables[i] + "." + c.Name
				break
			}
		}
		columns[j] = storage.Column{Name: name, Type: c.Type}
	}
	return columns
}

// format desenha o resultado como o texto que o REPL imprime.
func (r *selectResult) format() string {
	headers := make([]string, len(r.Columns))
	right := make([]bool, len(r.Columns))
	for i, c := range r.Columns {
		headers[i] = c.Name
		right[i] = c.Type == "INT" || c.Type == "REAL"
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
