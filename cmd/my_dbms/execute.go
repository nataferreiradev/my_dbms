package main

import (
	"fmt"
	"slices"
	"strconv"

	"my_DBMS/internal/parser"
	"my_DBMS/internal/sql"
	"my_DBMS/internal/storage"
)

// execute roda um comando SQL e devolve o texto para mostrar ao usuário.
func execute(db *storage.DB, input string) (string, error) {
	tokens, err := sql.Lex(input)
	if err != nil {
		return "", err
	}
	stmt, err := parser.Parse(tokens)
	if err != nil {
		return "", err
	}

	switch s := stmt.(type) {
	case *parser.CreateTable:
		return createTable(db, s)
	case *parser.Insert:
		return insert(db, s)
	case *parser.Select:
		return selectRows(db, s)
	}
	return "", fmt.Errorf("comando não suportado: %s", input)
}

func createTable(db *storage.DB, s *parser.CreateTable) (string, error) {
	columns := make([]storage.Column, len(s.Columns))
	for i, c := range s.Columns {
		columns[i] = storage.Column{Name: c.Name, Type: c.Type, NotNull: c.NotNull}
	}
	keys := make([]storage.Key, len(s.Keys))
	for i, k := range s.Keys {
		keys[i] = storage.Key{Kind: storage.Unique, Columns: k.Columns}
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
			if row[p], err = literalValue(lits[j], schema.Columns[p]); err != nil {
				return "", err
			}
		}
		rows[r] = row
	}

	if err := db.InsertRows(s.Table, rows); err != nil {
		return "", err
	}
	return count(len(rows), "linha inserida", "linhas inseridas"), nil
}

// count escreve "1 linha inserida" ou "3 linhas inseridas".
func count(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// literalValue confere se o literal serve para a coluna e devolve o valor
// como ele vai para o disco: NULL, número inteiro normalizado ou o texto.
// NOT NULL é conferido pelo storage.
func literalValue(lit parser.Literal, col storage.Column) (storage.Value, error) {
	switch {
	case lit.Kind == parser.Null:
		return storage.Null, nil
	case col.Type == "INT" && lit.Kind == parser.Number:
		if n, err := strconv.Atoi(lit.Text); err == nil {
			return storage.Text(strconv.Itoa(n)), nil
		}
	case col.Type == "TEXT" && lit.Kind == parser.String:
		return storage.Text(lit.Text), nil
	}
	return storage.Value{}, fmt.Errorf("a coluna %s é %s, o valor %s não serve", col.Name, col.Type, lit.Text)
}

// display é como um valor aparece na saída do SELECT.
func display(v storage.Value) string {
	if v.Null {
		return "NULL"
	}
	return v.Text
}

func selectRows(db *storage.DB, s *parser.Select) (string, error) {
	table, err := db.ReadTable(s.Table)
	if err != nil {
		return "", err
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
			return "", fmt.Errorf("coluna %q não existe na tabela %s", name, s.Table)
		}
		indexes = append(indexes, i)
	}

	headers := make([]string, len(indexes))
	for j, i := range indexes {
		headers[j] = table.Columns[i].Name
	}
	rows := make([][]string, len(table.Rows))
	for r, row := range table.Rows {
		rows[r] = make([]string, len(indexes))
		for j, i := range indexes {
			rows[r][j] = display(row[i])
		}
	}
	return formatTable(headers, rows), nil
}
