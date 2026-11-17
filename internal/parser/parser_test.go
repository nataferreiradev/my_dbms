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
			input: "CREATE TABLE t (id INT NOT NULL UNIQUE, nome TEXT PRIMARY KEY NOT NULL)",
			want: &CreateTable{
				Name: "t",
				Columns: []ColumnDef{
					{Name: "id", Type: "INT", NotNull: true},
					{Name: "nome", Type: "TEXT", NotNull: true},
				},
				Keys: []KeyDef{
					{Columns: []string{"id"}},
					{Primary: true, Columns: []string{"nome"}},
				},
			},
		},
		{
			input: `INSERT INTO t VALUES (1, 'Ana', -2.5, null)`,
			want: &Insert{Table: "t", Rows: [][]Literal{{
				{Kind: Number, Text: "1"},
				{Kind: String, Text: "Ana"},
				{Kind: Number, Text: "-2.5"},
				{Kind: Null, Text: "NULL"},
			}}},
		},
		{
			input: "CREATE TABLE t (preco REAL NOT NULL, ativo BOOLEAN)",
			want: &CreateTable{
				Name: "t",
				Columns: []ColumnDef{
					{Name: "preco", Type: "REAL", NotNull: true},
					{Name: "ativo", Type: "BOOLEAN"},
				},
			},
		},
		{
			input: "INSERT INTO t VALUES (true, FALSE)",
			want: &Insert{Table: "t", Rows: [][]Literal{{
				{Kind: Bool, Text: "TRUE"},
				{Kind: Bool, Text: "FALSE"},
			}}},
		},
		{
			input: "SELECT * FROM t WHERE ativo = TRUE",
			want:  &Select{Table: "t", Where: &Comparison{Op: "=", Left: ColumnRef{"ativo"}, Right: Literal{Bool, "TRUE"}}},
		},
		{
			input: `INSERT INTO t (nome, id) VALUES ('Ana', 1), ('Bia', 2)`,
			want: &Insert{Table: "t", Columns: []string{"nome", "id"}, Rows: [][]Literal{
				{{Kind: String, Text: "Ana"}, {Kind: Number, Text: "1"}},
				{{Kind: String, Text: "Bia"}, {Kind: Number, Text: "2"}},
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
		{
			input: "UPDATE t SET nome = 'Bia', idade = NULL WHERE id = 2",
			want: &Update{
				Table: "t",
				Set: []Assignment{
					{Column: "nome", Value: Literal{String, "Bia"}},
					{Column: "idade", Value: Literal{Null, "NULL"}},
				},
				Where: &Comparison{Op: "=", Left: ColumnRef{"id"}, Right: Literal{Number, "2"}},
			},
		},
		{
			input: "update t set a=1",
			want:  &Update{Table: "t", Set: []Assignment{{Column: "a", Value: Literal{Number, "1"}}}},
		},
		{
			input: "DELETE FROM t WHERE a IS NULL;",
			want:  &Delete{Table: "t", Where: &IsNull{Expr: ColumnRef{"a"}}},
		},
		{
			input: "DELETE FROM t",
			want:  &Delete{Table: "t"},
		},
		{
			input: "DROP TABLE t;",
			want:  &DropTable{Name: "t"},
		},
		{
			// AND liga mais forte que OR.
			input: "SELECT * FROM t WHERE a = 1 OR b > 'x' AND c IS NOT NULL",
			want: &Select{Table: "t", Where: &Logical{
				Op:   "OR",
				Left: &Comparison{Op: "=", Left: ColumnRef{"a"}, Right: Literal{Number, "1"}},
				Right: &Logical{
					Op:    "AND",
					Left:  &Comparison{Op: ">", Left: ColumnRef{"b"}, Right: Literal{String, "x"}},
					Right: &IsNull{Expr: ColumnRef{"c"}, Not: true},
				},
			}},
		},
		{
			input: "SELECT * FROM t WHERE (a<>1 OR b IS NULL) AND 2<=a",
			want: &Select{Table: "t", Where: &Logical{
				Op: "AND",
				Left: &Logical{
					Op:    "OR",
					Left:  &Comparison{Op: "<>", Left: ColumnRef{"a"}, Right: Literal{Number, "1"}},
					Right: &IsNull{Expr: ColumnRef{"b"}},
				},
				Right: &Comparison{Op: "<=", Left: Literal{Number, "2"}, Right: ColumnRef{"a"}},
			}},
		},
		{
			input: "SELECT * FROM t WHERE a = 1 AND b = 2 AND c = 3",
			want: &Select{Table: "t", Where: &Logical{
				Op: "AND",
				Left: &Logical{
					Op:    "AND",
					Left:  &Comparison{Op: "=", Left: ColumnRef{"a"}, Right: Literal{Number, "1"}},
					Right: &Comparison{Op: "=", Left: ColumnRef{"b"}, Right: Literal{Number, "2"}},
				},
				Right: &Comparison{Op: "=", Left: ColumnRef{"c"}, Right: Literal{Number, "3"}},
			}},
		},
		{
			input: "CREATE INDEX t_nome ON t (nome, id);",
			want:  &CreateIndex{Name: "t_nome", Table: "t", Columns: []string{"nome", "id"}},
		},
		{input: "DROP INDEX t_nome", want: &DropIndex{Name: "t_nome"}},
		{
			input: "EXPLAIN SELECT * FROM t WHERE id = 1",
			want:  &Explain{Statement: &Select{Table: "t", Where: &Comparison{Op: "=", Left: ColumnRef{"id"}, Right: Literal{Number, "1"}}}},
		},
		{input: "explain delete from t", want: &Explain{Statement: &Delete{Table: "t"}}},
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
		{input: "CREATE TABLE t (id FLOAT)", wantErr: `posição 20: esperava um tipo (INT, REAL, TEXT ou BOOLEAN), veio "FLOAT"`},
		{input: "CREATE TABLE t (id INT nome TEXT)", wantErr: `esperava "," ou ")"`},
		{input: "CREATE TABLE t (id INT PRIMARY)", wantErr: `esperava "KEY"`},
		{input: "CREATE TABLE t (id INT NOT)", wantErr: `esperava "NULL"`},
		{input: "CREATE TABLE t (id INT,)", wantErr: "esperava um nome"},
		{input: "CREATE TABLE t ()", wantErr: "esperava um nome"},
		{input: "CREATE TABLE t (id INT, PRIMARY KEY id)", wantErr: `esperava "("`},
		{input: "INSERT INTO t VALUES (1, nome)", wantErr: "esperava um valor"},
		{input: "INSERT INTO t VALUES 1", wantErr: `esperava "("`},
		{input: "INSERT INTO t VALUES (1),", wantErr: `esperava "("`},
		{input: "INSERT INTO t (1) VALUES (1)", wantErr: "esperava um nome"},
		{input: "INSERT INTO t () VALUES (1)", wantErr: "esperava um nome"},
		{input: "SELECT FROM t", wantErr: "esperava um nome"},
		{input: "SELECT * FROM t t", wantErr: "esperava o fim do comando"},
		{input: "SELECT * FROM t WHERE", wantErr: "esperava uma coluna ou um valor"},
		{input: "SELECT * FROM t WHERE a", wantErr: "esperava um operador de comparação"},
		{input: "SELECT * FROM t WHERE a * 1", wantErr: "esperava um operador de comparação"},
		{input: "SELECT * FROM t WHERE a = ", wantErr: "esperava uma coluna ou um valor"},
		{input: "SELECT * FROM t WHERE a = 1 AND", wantErr: "esperava uma coluna ou um valor"},
		{input: "SELECT * FROM t WHERE (a = 1", wantErr: `esperava ")"`},
		{input: "SELECT * FROM t WHERE a IS 1", wantErr: `esperava "NULL"`},
		{input: "SELECT * FROM t WHERE a = 1 b = 2", wantErr: "esperava o fim do comando"},
		{input: "UPDATE t", wantErr: `esperava "SET"`},
		{input: "UPDATE t SET", wantErr: "esperava um nome"},
		{input: "UPDATE t SET a 1", wantErr: `esperava "="`},
		{input: "UPDATE t SET a = b", wantErr: "esperava um valor"},
		{input: "UPDATE t SET a = 1,", wantErr: "esperava um nome"},
		{input: "DELETE t", wantErr: `esperava "FROM"`},
		{input: "DELETE FROM t WHERE", wantErr: "esperava uma coluna ou um valor"},
		{input: "DROP t", wantErr: `esperava "TABLE"`},
		{input: "DROP TABLE t, u", wantErr: "esperava o fim do comando"},
		{input: "CREATE INDEX i t (a)", wantErr: `esperava "ON"`},
		{input: "CREATE INDEX i ON t", wantErr: `esperava "("`},
		{input: "CREATE INDEX ON t (a)", wantErr: "esperava um nome"},
		{input: "DROP INDEX", wantErr: "esperava um nome"},
		{input: "EXPLAIN", wantErr: "esperava SELECT, UPDATE ou DELETE"},
		{input: "EXPLAIN DROP TABLE t", wantErr: "esperava SELECT, UPDATE ou DELETE"},
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

// FuzzParse confere que nenhum comando derruba o parser: ou sai uma AST, ou
// um erro.
// Rode com: go test ./internal/parser -fuzz FuzzParse
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"CREATE TABLE p (id INT PRIMARY KEY, nome TEXT NOT NULL, UNIQUE (nome));",
		"INSERT INTO p (nome, id) VALUES ('a', 1), ('b', 2);",
		"SELECT a, b FROM p WHERE (a > 1 OR b IS NULL) AND a <> 3;",
		"UPDATE p SET a = 1, b = NULL WHERE a = 2",
		"DELETE FROM p",
		"DROP TABLE p;",
		"CREATE INDEX i ON p (a, b);",
		"DROP INDEX i",
		"EXPLAIN SELECT * FROM p WHERE a >= 1 AND a < 9",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		tokens, err := sql.Lex(input)
		if err != nil {
			return
		}
		stmt, err := Parse(tokens)
		if err == nil && stmt == nil {
			t.Errorf("Parse(%q) sem erro e sem comando", input)
		}
	})
}
