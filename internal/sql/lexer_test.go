package sql

import (
	"reflect"
	"testing"
)

func TestLex(t *testing.T) {
	tests := []struct {
		input string
		want  []Token
	}{
		{
			input: "select *   FROM users;",
			want: []Token{
				{Keyword, "SELECT", 0},
				{Operator, "*", 1},
				{Keyword, "FROM", 2},
				{Identifier, "users", 3},
				{Symbol, ";", 4},
			},
		},
		{
			input: "CREATE TABLE pessoas;",
			want: []Token{
				{Keyword, "CREATE", 0},
				{Keyword, "TABLE", 1},
				{Identifier, "pessoas", 2},
				{Symbol, ";", 3},
			},
		},
		{
			input: `SELECT * FROM t WHERE nome = "ana maria" AND idade >= 18`,
			want: []Token{
				{Keyword, "SELECT", 0},
				{Operator, "*", 1},
				{Keyword, "FROM", 2},
				{Identifier, "t", 3},
				{Keyword, "WHERE", 4},
				{Identifier, "nome", 5},
				{Operator, "=", 6},
				{Literal, `"ana maria"`, 7},
				{Keyword, "AND", 8},
				{Identifier, "idade", 9},
				{Operator, ">=", 10},
				{Literal, "18", 11},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Lex(tt.input)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("\n got: %v\nwant: %v", got, tt.want)
			}
		})
	}
}

func TestLexInvalid(t *testing.T) {
	for _, input := range []string{"SELECT * FROM 1tabela", "SELECT @x", "SELECT a-b"} {
		if _, err := Lex(input); err == nil {
			t.Errorf("Lex(%q): esperava erro, não veio nenhum", input)
		}
	}
}
