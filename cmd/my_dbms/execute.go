package main

import (
	"fmt"

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
	case match(tokens, "CREATE", "TABLE", sql.Identifier):
		name := tokens[2].Value
		if err := db.CreateTable(name); err != nil {
			return "", err
		}
		return fmt.Sprintf("tabela %s criada", name), nil

	case match(tokens, "SELECT", "*", "FROM", sql.Identifier):
		data, err := db.ReadTable(tokens[3].Value)
		if err != nil {
			return "", err
		}
		if len(data) == 0 {
			return "(0 linhas)", nil
		}
		return string(data), nil
	}

	return "", fmt.Errorf("comando não suportado: %s", input)
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
