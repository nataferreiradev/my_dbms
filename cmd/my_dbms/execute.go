package main

import (
	"errors"
	"fmt"
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

	switch {
	case len(tokens) > 3 && match(tokens[:3], "CREATE", "TABLE", sql.Identifier):
		name := tokens[2].Value
		columns, err := parseColumns(tokens[3:])
		if err != nil {
			return "", err
		}
		if err := db.CreateTable(name, columns); err != nil {
			return "", err
		}
		return fmt.Sprintf("tabela %s criada", name), nil

	case match(tokens, "SELECT", "*", "FROM", sql.Identifier):
		table, err := db.ReadTable(tokens[3].Value)
		if err != nil {
			return "", err
		}
		names := make([]string, len(table.Columns))
		for i, c := range table.Columns {
			names[i] = c.Name
		}
		return strings.Join(names, " | ") + "\n(0 linhas)", nil
	}

	return "", fmt.Errorf("comando não suportado: %s", input)
}

// parseColumns lê a lista "(nome TIPO, nome TIPO, ...)" do CREATE TABLE.
func parseColumns(tokens []sql.Token) ([]storage.Column, error) {
	n := len(tokens)
	if n < 2 || tokens[0].Value != "(" || tokens[n-1].Value != ")" {
		return nil, errors.New("esperava lista de colunas entre parênteses")
	}

	// Cada coluna ocupa "nome TIPO ," e a última não tem vírgula,
	// então n colunas dão 3n-1 tokens.
	inner := tokens[1 : n-1]
	if len(inner)%3 != 2 {
		return nil, errors.New("lista de colunas mal formada, use (nome TIPO, ...)")
	}

	var columns []storage.Column
	for i := 0; i < len(inner); i += 3 {
		name, typ := inner[i], inner[i+1]
		if name.Type != sql.Identifier {
			return nil, fmt.Errorf("nome de coluna inválido: %q", name.Value)
		}
		if typ.Value != "INT" && typ.Value != "TEXT" {
			return nil, fmt.Errorf("tipo inválido para a coluna %s: %q (use INT ou TEXT)", name.Value, typ.Value)
		}
		if i+2 < len(inner) && inner[i+2].Value != "," {
			return nil, fmt.Errorf("esperava vírgula depois da coluna %s", name.Value)
		}
		columns = append(columns, storage.Column{Name: name.Value, Type: typ.Value})
	}
	return columns, nil
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
