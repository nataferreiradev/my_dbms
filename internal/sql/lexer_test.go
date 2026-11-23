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
			input: "p.id=c.pessoa_id",
			want:  []Token{{Identifier, "p.id", 0}, {Operator, "=", 4}, {Identifier, "c.pessoa_id", 5}},
		},
		{
			input: "a left outer join b inner Join c right JOIN d",
			want: []Token{
				{Identifier, "a", 0},
				{Keyword, "LEFT", 2},
				{Keyword, "OUTER", 7},
				{Keyword, "JOIN", 13},
				{Identifier, "b", 18},
				{Keyword, "INNER", 20},
				{Keyword, "JOIN", 26},
				{Identifier, "c", 31},
				{Keyword, "RIGHT", 33},
				{Keyword, "JOIN", 39},
				{Identifier, "d", 44},
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
		"p.",
		"a.b.c",
		"p.1",
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

// FuzzLex confere que nenhuma entrada derruba o lexer e que as posições dos
// tokens ficam dentro da entrada.
// Rode com: go test ./internal/sql -fuzz FuzzLex
func FuzzLex(f *testing.F) {
	for _, s := range []string{
		"SELECT * FROM t WHERE id >= 10 AND nome <> 'it''s';",
		`INSERT INTO t VALUES (1, "a"), (-2, NULL);`,
		"'sem fim",
		"ção = 1",
		"SELECT p.id FROM p LEFT JOIN q ON p.id = q.p_id",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		tokens, err := Lex(input)
		if err != nil {
			return
		}
		n := len([]rune(input))
		for _, tok := range tokens {
			if tok.Pos < 0 || tok.Pos >= n {
				t.Errorf("token %q na posição %d, fora da entrada de %d caracteres", tok.Value, tok.Pos, n)
			}
		}
	})
}
