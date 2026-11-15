package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"my_DBMS/internal/sql"
	"my_DBMS/internal/storage"
)

// execute roda um comando SQL e devolve o texto para mostrar ao usuário.
func execute(db *storage.DB, input string) (string, error) {
	tokens, err := sql.Lex(input)
	if err != nil {
		return "", err
	}
	if n := len(tokens); n > 0 && tokens[n-1].Value == ";" {
		tokens = tokens[:n-1]
	}

	n := len(tokens)
	switch {
	case n > 3 && match(tokens[:3], "CREATE", "TABLE", sql.Identifier):
		return createTable(db, tokens[2].Value, tokens[3:])

	case n > 4 && match(tokens[:4], "INSERT", "INTO", sql.Identifier, "VALUES"):
		return insert(db, tokens[2].Value, tokens[4:])

	case n > 3 && match(tokens[:1], "SELECT") && match(tokens[n-2:], "FROM", sql.Identifier):
		return selectRows(db, tokens[n-1].Value, tokens[1:n-2])
	}

	return "", fmt.Errorf("comando não suportado: %s", input)
}

// createTable roda CREATE TABLE <nome> (coluna TIPO, ...). Depois do tipo a
// coluna pode ter PRIMARY KEY ou UNIQUE, e chaves de várias colunas entram
// como itens da lista: PRIMARY KEY (a, b), UNIQUE (c, d).
func createTable(db *storage.DB, name string, list []sql.Token) (string, error) {
	items, err := parseList(list)
	if err != nil {
		return "", err
	}

	var columns []storage.Column
	var keys []storage.Key
	for _, item := range items {
		if kind, rest, ok := keyPrefix(item); ok {
			cols, err := parseNames(rest)
			if err != nil {
				return "", fmt.Errorf("chave mal formada %q: %w", tokensText(item), err)
			}
			keys = append(keys, storage.Key{Kind: kind, Columns: cols})
			continue
		}

		if len(item) < 2 || item[0].Type != sql.Identifier {
			return "", fmt.Errorf("coluna mal formada %q, use nome TIPO", tokensText(item))
		}
		col, typ := item[0].Value, item[1].Value
		if typ != "INT" && typ != "TEXT" {
			return "", fmt.Errorf("tipo inválido para a coluna %s: %q (use INT ou TEXT)", col, typ)
		}
		if len(item) > 2 {
			kind, rest, ok := keyPrefix(item[2:])
			if !ok || len(rest) > 0 {
				return "", fmt.Errorf("coluna mal formada %q, depois do tipo só cabe PRIMARY KEY ou UNIQUE", tokensText(item))
			}
			keys = append(keys, storage.Key{Kind: kind, Columns: []string{col}})
		}
		columns = append(columns, storage.Column{Name: col, Type: typ})
	}

	if err := db.CreateTable(name, columns, keys); err != nil {
		return "", err
	}
	return fmt.Sprintf("tabela %s criada", name), nil
}

// keyPrefix reconhece PRIMARY KEY ou UNIQUE no começo dos tokens e devolve
// o tipo da chave e o que sobrou.
func keyPrefix(tokens []sql.Token) (storage.KeyKind, []sql.Token, bool) {
	switch {
	case len(tokens) >= 2 && match(tokens[:2], "PRIMARY", "KEY"):
		return storage.PrimaryKey, tokens[2:], true
	case len(tokens) >= 1 && match(tokens[:1], "UNIQUE"):
		return storage.Unique, tokens[1:], true
	}
	return "", nil, false
}

// parseNames lê "(a, b, ...)" em que cada item é um nome de coluna.
func parseNames(tokens []sql.Token) ([]string, error) {
	items, err := parseList(tokens)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(items))
	for i, item := range items {
		if len(item) != 1 || item[0].Type != sql.Identifier {
			return nil, fmt.Errorf("esperava nome de coluna, veio %q", tokensText(item))
		}
		names[i] = item[0].Value
	}
	return names, nil
}

// insert roda INSERT INTO <nome> VALUES (valor, ...).
func insert(db *storage.DB, name string, list []sql.Token) (string, error) {
	items, err := parseList(list)
	if err != nil {
		return "", err
	}
	table, err := db.ReadTable(name)
	if err != nil {
		return "", err
	}
	if len(items) != len(table.Columns) {
		return "", fmt.Errorf("tabela %s tem %d colunas, mas vieram %d valores", name, len(table.Columns), len(items))
	}

	values := make([]string, len(items))
	for i, item := range items {
		col := table.Columns[i]
		if len(item) != 1 || item[0].Type != sql.Literal {
			return "", fmt.Errorf("valor inválido para a coluna %s: %q", col.Name, tokensText(item))
		}
		values[i], err = literalValue(item[0].Value, col)
		if err != nil {
			return "", err
		}
	}

	if err := db.InsertRow(name, values); err != nil {
		return "", err
	}
	return "1 linha inserida", nil
}

// literalValue confere se o literal serve para a coluna e devolve o valor
// como ele vai para o disco: número normalizado ou texto sem as aspas.
func literalValue(lit string, col storage.Column) (string, error) {
	quoted := strings.HasPrefix(lit, `"`)
	switch {
	case col.Type == "INT" && !quoted:
		n, err := strconv.Atoi(lit)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(n), nil
	case col.Type == "TEXT" && quoted:
		return lit[1 : len(lit)-1], nil
	}
	return "", fmt.Errorf("a coluna %s é %s, o valor %s não serve", col.Name, col.Type, lit)
}

// selectRows roda SELECT * FROM <nome> ou SELECT a, b FROM <nome>.
func selectRows(db *storage.DB, name string, list []sql.Token) (string, error) {
	table, err := db.ReadTable(name)
	if err != nil {
		return "", err
	}

	// indexes diz quais colunas da tabela entram no resultado, em ordem.
	var indexes []int
	if len(list) == 1 && list[0].Value == "*" {
		for i := range table.Columns {
			indexes = append(indexes, i)
		}
	} else {
		items, err := splitCommas(list)
		if err != nil {
			return "", err
		}
		for _, item := range items {
			if len(item) != 1 || item[0].Type != sql.Identifier {
				return "", fmt.Errorf("coluna inválida no SELECT: %q", tokensText(item))
			}
			i := slices.IndexFunc(table.Columns, func(c storage.Column) bool { return c.Name == item[0].Value })
			if i < 0 {
				return "", fmt.Errorf("coluna %q não existe na tabela %s", item[0].Value, name)
			}
			indexes = append(indexes, i)
		}
	}

	headers := make([]string, len(indexes))
	for j, i := range indexes {
		headers[j] = table.Columns[i].Name
	}
	rows := make([][]string, len(table.Rows))
	for r, row := range table.Rows {
		rows[r] = make([]string, len(indexes))
		for j, i := range indexes {
			rows[r][j] = row[i]
		}
	}
	return formatTable(headers, rows), nil
}

// parseList lê "(a, b c, ...)" e devolve os itens separados por vírgula.
func parseList(tokens []sql.Token) ([][]sql.Token, error) {
	n := len(tokens)
	if n < 2 || tokens[0].Value != "(" || tokens[n-1].Value != ")" {
		return nil, errors.New("esperava uma lista entre parênteses")
	}
	return splitCommas(tokens[1 : n-1])
}

// splitCommas separa os tokens nas vírgulas que estão fora de parênteses,
// para "id INT, PRIMARY KEY (a, b)" virar dois itens. Item vazio é erro.
func splitCommas(tokens []sql.Token) ([][]sql.Token, error) {
	items := [][]sql.Token{nil}
	depth := 0
	for _, tok := range tokens {
		if tok.Type == sql.Symbol {
			switch tok.Value {
			case "(":
				depth++
			case ")":
				depth--
			case ",":
				if depth == 0 {
					items = append(items, nil)
					continue
				}
			}
		}
		last := len(items) - 1
		items[last] = append(items[last], tok)
	}
	for _, item := range items {
		if len(item) == 0 {
			return nil, errors.New("lista com item vazio")
		}
	}
	return items, nil
}

// tokensText junta os tokens de volta em texto, para mensagens de erro.
func tokensText(tokens []sql.Token) string {
	words := make([]string, len(tokens))
	for i, t := range tokens {
		words[i] = t.Value
	}
	return strings.Join(words, " ")
}

// match compara os tokens com um padrão. Cada item do padrão é um valor
// exato (string) ou um tipo de token (sql.TokenType).
func match(tokens []sql.Token, pattern ...any) bool {
	if len(tokens) != len(pattern) {
		return false
	}
	for i, p := range pattern {
		switch p := p.(type) {
		case string:
			if tokens[i].Value != p {
				return false
			}
		case sql.TokenType:
			if tokens[i].Type != p {
				return false
			}
		}
	}
	return true
}
