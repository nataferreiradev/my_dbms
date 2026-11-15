package sql

import (
	"reflect"
	"strings"
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
				{Operator, "*", 7},
				{Keyword, "FROM", 11},
				{Identifier, "users", 16},
				{Symbol, ";", 21},
			},
		},
		{
			input: "CREATE TABLE pessoas;",
			want: []Token{
				{Keyword, "CREATE", 0},
				{Keyword, "TABLE", 7},
				{Identifier, "pessoas", 13},
				{Symbol, ";", 20},
			},
		},
		{
			input: `SELECT * FROM t WHERE nome = "ana maria" AND idade >= 18`,
			want: []Token{
				{Keyword, "SELECT", 0},
				{Operator, "*", 7},
				{Keyword, "FROM", 9},
				{Identifier, "t", 14},
				{Keyword, "WHERE", 16},
				{Identifier, "nome", 22},
				{Operator, "=", 27},
				{String, "ana maria", 29},
				{Keyword, "AND", 41},
				{Identifier, "idade", 45},
				{Operator, ">=", 51},
				{Number, "18", 54},
			},
		},
		{
			input: "id=1",
			want:  []Token{{Identifier, "id", 0}, {Operator, "=", 2}, {Number, "1", 3}},
		},
		{
			input: "a<>b",
			want:  []Token{{Identifier, "a", 0}, {Operator, "<>", 1}, {Identifier, "b", 3}},
		},
		{
			input: "x>=-1",
			want:  []Token{{Identifier, "x", 0}, {Operator, ">=", 1}, {Number, "-1", 3}},
		},
		{
			input: "(1.5,-0.25)",
			want: []Token{
				{Symbol, "(", 0},
				{Number, "1.5", 1},
				{Symbol, ",", 4},
				{Number, "-0.25", 5},
				{Symbol, ")", 10},
			},
		},
		{
			input: `'Ana','it''s'`,
			want:  []Token{{String, "Ana", 0}, {Symbol, ",", 5}, {String, "it's", 6}},
		},
		{
			input: `"ele disse ""oi""" ''`,
			want:  []Token{{String, `ele disse "oi"`, 0}, {String, "", 19}},
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
	inputs := []string{
		"SELECT * FROM 1tabela",
		"SELECT @x",
		"SELECT a-b",
		"'aberta",
		`"aberta''`,
		"1.",
		".5",
		"é = 1",
	}
	for _, input := range inputs {
		if _, err := Lex(input); err == nil {
			t.Errorf("Lex(%q): esperava erro, não veio nenhum", input)
		}
	}
}

func TestLexErrorPosition(t *testing.T) {
	// A posição no erro conta caracteres a partir de 1, com acento valendo um.
	_, err := Lex("SELECT 'é', @x")
	if err == nil || !strings.Contains(err.Error(), "posição 13") {
		t.Errorf("erro = %v, queria a posição 13", err)
	}
}
