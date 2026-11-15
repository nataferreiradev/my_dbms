package parser

import (
	"reflect"
	"strings"
	"testing"

	"my_DBMS/internal/sql"
)

func parse(t *testing.T, input string) (Statement, error) {
	t.Helper()
	tokens, err := sql.Lex(input)
	if err != nil {
		t.Fatalf("Lex(%q): %v", input, err)
	}
	return Parse(tokens)
}

func TestParse(t *testing.T) {
	tests := []struct {
		input string
		want  Statement
	}{
		{
			input: "CREATE TABLE t (id INT PRIMARY KEY, email TEXT UNIQUE, a INT, b INT, UNIQUE (a, b));",
			want: &CreateTable{
				Name: "t",
				Columns: []ColumnDef{
					{Name: "id", Type: "INT"},
					{Name: "email", Type: "TEXT"},
					{Name: "a", Type: "INT"},
					{Name: "b", Type: "INT"},
				},
				Keys: []KeyDef{
					{Primary: true, Columns: []string{"id"}},
					{Columns: []string{"email"}},
					{Columns: []string{"a", "b"}},
				},
			},
		},
		{
			input: `INSERT INTO t VALUES (1, 'Ana', -2.5)`,
			want: &Insert{Table: "t", Values: []Literal{
				{Kind: Number, Text: "1"},
				{Kind: String, Text: "Ana"},
				{Kind: Number, Text: "-2.5"},
			}},
		},
		{
			input: "SELECT * FROM t;",
			want:  &Select{Table: "t"},
		},
		{
			input: "select nome, id from t",
			want:  &Select{Table: "t", Columns: []string{"nome", "id"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parse(t, tt.input)
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		input   string
		wantErr string
	}{
		{input: ";", wantErr: "comando vazio"},
		{input: "DESTROY t", wantErr: "não suportado"},
		{input: "CREATE TABLE t;", wantErr: `esperava "(", mas o comando acabou`},
		{input: "CREATE TABLE t (id FLOAT)", wantErr: `posição 20: esperava um tipo (INT ou TEXT), veio "FLOAT"`},
		{input: "CREATE TABLE t (id INT nome TEXT)", wantErr: `esperava "," ou ")"`},
		{input: "CREATE TABLE t (id INT PRIMARY)", wantErr: `esperava "KEY"`},
		{input: "CREATE TABLE t (id INT,)", wantErr: "esperava um nome"},
		{input: "CREATE TABLE t ()", wantErr: "esperava um nome"},
		{input: "CREATE TABLE t (id INT, PRIMARY KEY id)", wantErr: `esperava "("`},
		{input: "INSERT INTO t VALUES (1, nome)", wantErr: "esperava um valor"},
		{input: "INSERT INTO t VALUES 1", wantErr: `esperava "("`},
		{input: "SELECT FROM t", wantErr: "esperava um nome"},
		{input: "SELECT * FROM t t", wantErr: "esperava o fim do comando"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parse(t, tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("erro = %v, queria algo com %q", err, tt.wantErr)
			}
		})
	}
}
