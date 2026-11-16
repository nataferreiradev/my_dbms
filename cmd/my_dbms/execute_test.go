package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"my_DBMS/internal/parser"
	"my_DBMS/internal/storage"
)

func TestExecute(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "SELECT * FROM users;", wantErr: true},
		{input: "create table users;", wantErr: true},
		{input: "create table users (id int, nome text);", want: "tabela users criada"},
		{input: "CREATE TABLE users (id INT);", wantErr: true},
		{input: "select * from users", want: "id | nome\n---+-----\n(0 linhas)"},
		{input: `insert into users values (1, "Ana");`, want: "1 linha inserida"},
		{input: `INSERT INTO users VALUES (007, "Silva, Bruno");`, want: "1 linha inserida"},
		{input: "SELECT * FROM users;", want: "id | nome\n---+-------------\n 1 | Ana\n 7 | Silva, Bruno\n(2 linhas)"},
		{input: "SELECT nome FROM users;", want: "nome\n------------\nAna\nSilva, Bruno\n(2 linhas)"},
		{input: "SELECT nome, id, nome FROM users;", want: "nome         | id | nome\n-------------+----+-------------\nAna          |  1 | Ana\nSilva, Bruno |  7 | Silva, Bruno\n(2 linhas)"},
		{input: "SELECT idade FROM users;", wantErr: true},
		{input: "SELECT name, definition FROM _catalog;", want: "name  | definition\n------+-----------------\nusers | id INT,nome TEXT\n(1 linha)"},
		{input: `INSERT INTO _catalog VALUES ("table", "x", "x", "id INT");`, wantErr: true},
		{input: "SELECT name, value FROM _settings WHERE name = 'page_size';", want: "name      | value\n----------+------\npage_size | 4096\n(1 linha)"},
		{input: "SELECT name, rows, pages FROM _tables WHERE rows > 1;", want: "name  | rows | pages\n------+------+------\nusers |    2 |     1\n(1 linha)"},
		{input: "DELETE FROM _tables;", wantErr: true},
		{input: "DROP TABLE nada;", wantErr: true},
		{input: "DROP TABLE users;", want: "tabela users removida"},
		{input: "SELECT * FROM users;", wantErr: true},
		{input: "DESTROY TABLE users;", wantErr: true},
	}

	for _, s := range steps {
		got, err := execute(db, s.input)
		if (err != nil) != s.wantErr {
			t.Fatalf("execute(%q) erro = %v, wantErr %v", s.input, err, s.wantErr)
		}
		if got != s.want {
			t.Errorf("execute(%q) =\n%s\nwant\n%s", s.input, got, s.want)
		}
	}
}

func TestCreateTableInvalidColumns(t *testing.T) {
	inputs := []string{
		"CREATE TABLE t ();",
		"CREATE TABLE t (id);",
		"CREATE TABLE t (id INT,);",
		"CREATE TABLE t (id INT nome TEXT);",
		"CREATE TABLE t (id FLOAT);",
		"CREATE TABLE t (1 INT);",
		"CREATE TABLE t id INT;",
		"CREATE TABLE t (id INT, id TEXT);",
	}

	for _, input := range inputs {
		db, _ := storage.Open(t.TempDir(), 4096)
		if _, err := execute(db, input); err == nil {
			t.Errorf("execute(%q) deveria falhar", input)
		}
	}
}

func TestKeys(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "CREATE TABLE users (id INT PRIMARY KEY, email TEXT UNIQUE, a INT, b INT, unique (a, b));", want: "tabela users criada"},
		{input: "SELECT type, name, definition FROM _catalog;", want: "type        | name            | definition\n------------+-----------------+---------------------------------------\ntable       | users           | id INT NOT NULL,email TEXT,a INT,b INT\nprimary_key | users_pkey      | id\nunique      | users_email_key | email\nunique      | users_a_b_key   | a,b\n(4 linhas)"},
		{input: `INSERT INTO users VALUES (1, "ana@x", 1, 1);`, want: "1 linha inserida"},
		{input: `INSERT INTO users VALUES (01, "bia@x", 2, 2);`, wantErr: true},
		{input: `INSERT INTO users VALUES (2, "ana@x", 2, 2);`, wantErr: true},
		{input: `INSERT INTO users VALUES (2, "bia@x", 1, 1);`, wantErr: true},
		{input: `INSERT INTO users VALUES (2, "bia@x", 1, 2);`, want: "1 linha inserida"},
		{input: "CREATE TABLE pares (a INT, b INT, PRIMARY KEY (a, b));", want: "tabela pares criada"},
		{input: "INSERT INTO pares VALUES (1, 2);", want: "1 linha inserida"},
		{input: "INSERT INTO pares VALUES (2, 1);", want: "1 linha inserida"},
		{input: "INSERT INTO pares VALUES (1, 2);", wantErr: true},
	}

	for _, s := range steps {
		got, err := execute(db, s.input)
		if (err != nil) != s.wantErr {
			t.Fatalf("execute(%q) erro = %v, wantErr %v", s.input, err, s.wantErr)
		}
		if got != s.want {
			t.Errorf("execute(%q) =\n%s\nwant\n%s", s.input, got, s.want)
		}
	}
}

func TestNull(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "CREATE TABLE p (id INT PRIMARY KEY, nome TEXT NOT NULL, apelido TEXT UNIQUE);", want: "tabela p criada"},
		{input: "INSERT INTO p VALUES (1, 'Ana', NULL);", want: "1 linha inserida"},
		{input: "INSERT INTO p VALUES (2, 'Bia', NULL);", want: "1 linha inserida"},
		{input: "INSERT INTO p VALUES (3, NULL, 'x');", wantErr: true},
		{input: "INSERT INTO p VALUES (NULL, 'Caio', 'y');", wantErr: true},
		{input: "INSERT INTO p VALUES (4, 'Dani', 'NULL');", want: "1 linha inserida"},
		{input: "SELECT apelido FROM p;", want: "apelido\n-------\nNULL\nNULL\nNULL\n(3 linhas)"},
	}

	for _, s := range steps {
		got, err := execute(db, s.input)
		if (err != nil) != s.wantErr {
			t.Fatalf("execute(%q) erro = %v, wantErr %v", s.input, err, s.wantErr)
		}
		if got != s.want {
			t.Errorf("execute(%q) =\n%s\nwant\n%s", s.input, got, s.want)
		}
	}
}

func TestInsertColumnsAndRows(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "CREATE TABLE p (id INT PRIMARY KEY, nome TEXT, cidade TEXT);", want: "tabela p criada"},
		{input: "INSERT INTO p VALUES (1, 'Ana', 'Recife'), (2, 'Bia', 'Natal');", want: "2 linhas inseridas"},
		{input: "INSERT INTO p (nome, id) VALUES ('Caio', 3);", want: "1 linha inserida"},
		{input: "INSERT INTO p (cidade) VALUES ('Olinda');", wantErr: true},
		{input: "INSERT INTO p (id, idade) VALUES (4, 30);", wantErr: true},
		{input: "INSERT INTO p (id, id) VALUES (4, 4);", wantErr: true},
		{input: "INSERT INTO p (id, nome) VALUES (4, 'Dani', 'x');", wantErr: true},
		{input: "INSERT INTO p (id) VALUES (4), (3);", wantErr: true},
		{input: "INSERT INTO p (id) VALUES (4), (4);", wantErr: true},
		{input: "SELECT * FROM p;", want: "id | nome | cidade\n---+------+-------\n 1 | Ana  | Recife\n 2 | Bia  | Natal\n 3 | Caio | NULL\n(3 linhas)"},
	}

	for _, s := range steps {
		got, err := execute(db, s.input)
		if (err != nil) != s.wantErr {
			t.Fatalf("execute(%q) erro = %v, wantErr %v", s.input, err, s.wantErr)
		}
		if got != s.want {
			t.Errorf("execute(%q) =\n%s\nwant\n%s", s.input, got, s.want)
		}
	}
}

func TestWhere(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	setup := []string{
		"CREATE TABLE p (id INT, nome TEXT, idade INT, chefe INT);",
		"INSERT INTO p VALUES (1, 'Ana', 30, NULL), (2, 'Bia', 9, 1), (3, 'Caio', 10, 1), (4, 'Dani', NULL, 2);",
	}
	for _, input := range setup {
		if _, err := execute(db, input); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		where   string
		want    string // ids que passam, separados por vírgula
		wantErr bool
	}{
		{where: "id = 2", want: "2"},
		{where: "id<>2", want: "1,3,4"},
		{where: "idade > 9", want: "1,3"}, // numérico: 10 > 9
		{where: "idade >= 10 AND idade <= 30", want: "1,3"},
		{where: "nome < 'C'", want: "1,2"},
		{where: "nome = 'Ana' OR id = 4", want: "1,4"},
		{where: "id = 1 OR id = 2 AND idade = 30", want: "1"},
		{where: "(id = 1 OR id = 2) AND idade = 9", want: "2"},
		{where: "chefe = id - 0", wantErr: true},
		{where: "chefe = 1", want: "2,3"},
		{where: "chefe <> 1", want: "4"}, // NULL <> 1 é desconhecido
		{where: "chefe = NULL", want: ""},
		{where: "chefe IS NULL", want: "1"},
		{where: "idade IS NOT NULL AND chefe IS NOT NULL", want: "2,3"},
		{where: "idade > 5 OR chefe = 2", want: "1,2,3,4"},
		{where: "chefe < id", want: "2,3,4"},
		{where: "1 = 1", want: "1,2,3,4"},
		{where: "'a' = 'b'", want: ""},
		{where: "idade = 'trinta'", wantErr: true},
		{where: "nome = 1", wantErr: true},
		{where: "nome = id", wantErr: true},
		{where: "1 = 'a'", wantErr: true},
		{where: "idade = 1.5", wantErr: true},
		{where: "altura = 1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.where, func(t *testing.T) {
			got, err := execute(db, "SELECT id FROM p WHERE "+tt.where)
			if (err != nil) != tt.wantErr {
				t.Fatalf("erro = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			// Tira cabeçalho, separador e contagem e fica só com os ids.
			lines := strings.Split(got, "\n")
			ids := strings.Join(lines[2:len(lines)-1], ",")
			ids = strings.ReplaceAll(ids, " ", "")
			if ids != tt.want {
				t.Errorf("ids = %q, want %q\n%s", ids, tt.want, got)
			}
		})
	}
}

func TestUpdateDelete(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "CREATE TABLE p (id INT PRIMARY KEY, nome TEXT NOT NULL, idade INT);", want: "tabela p criada"},
		{input: "INSERT INTO p VALUES (1, 'Ana', 30), (2, 'Bia', 9), (3, 'Caio', 10);", want: "3 linhas inseridas"},
		{input: "UPDATE p SET idade = 31 WHERE nome = 'Ana';", want: "1 linha atualizada"},
		{input: "UPDATE p SET idade = NULL, nome = 'Bea' WHERE id = 2;", want: "1 linha atualizada"},
		{input: "UPDATE p SET idade = 0 WHERE id > 99;", want: "0 linhas atualizadas"},
		{input: "UPDATE p SET id = 1 WHERE id = 3;", wantErr: true},
		{input: "UPDATE p SET nome = NULL;", wantErr: true},
		{input: "UPDATE p SET idade = 'x';", wantErr: true},
		{input: "UPDATE p SET altura = 1;", wantErr: true},
		{input: "UPDATE p SET idade = 1, idade = 2;", wantErr: true},
		{input: "SELECT * FROM p;", want: "id | nome | idade\n---+------+------\n 1 | Ana  |    31\n 2 | Bea  |  NULL\n 3 | Caio |    10\n(3 linhas)"},
		{input: "DELETE FROM p WHERE idade IS NULL;", want: "1 linha removida"},
		{input: "DELETE FROM p WHERE nada = 1;", wantErr: true},
		{input: "DELETE FROM p WHERE idade < 0;", want: "0 linhas removidas"},
		{input: "SELECT id FROM p;", want: "id\n--\n 1\n 3\n(2 linhas)"},
		{input: "INSERT INTO p VALUES (2, 'Bia', 9);", want: "1 linha inserida"},
		{input: "DELETE FROM p;", want: "3 linhas removidas"},
		{input: "SELECT * FROM p;", want: "id | nome | idade\n---+------+------\n(0 linhas)"},
		{input: "DELETE FROM _catalog;", wantErr: true},
		{input: "DROP TABLE _catalog;", wantErr: true},
	}

	for _, s := range steps {
		got, err := execute(db, s.input)
		if (err != nil) != s.wantErr {
			t.Fatalf("execute(%q) erro = %v, wantErr %v", s.input, err, s.wantErr)
		}
		if got != s.want {
			t.Errorf("execute(%q) =\n%s\nwant\n%s", s.input, got, s.want)
		}
	}
}

func TestWhereEmptyTableStillValidates(t *testing.T) {
	db, _ := storage.Open(t.TempDir(), 4096)
	execute(db, "CREATE TABLE p (id INT);")

	if _, err := execute(db, "SELECT * FROM p WHERE nada = 1"); err == nil {
		t.Error("coluna inexistente no WHERE deveria dar erro mesmo com a tabela vazia")
	}
}

func TestCreateTableInvalidKeys(t *testing.T) {
	inputs := []string{
		"CREATE TABLE t (id INT PRIMARY);",
		"CREATE TABLE t (id INT KEY);",
		"CREATE TABLE t (id INT UNIQUE UNIQUE);",
		"CREATE TABLE t (id INT PRIMARY KEY nome);",
		"CREATE TABLE t (id INT PRIMARY KEY, nome TEXT PRIMARY KEY);",
		"CREATE TABLE t (id INT, PRIMARY KEY (x));",
		"CREATE TABLE t (id INT, PRIMARY KEY ());",
		"CREATE TABLE t (id INT, PRIMARY KEY id);",
		"CREATE TABLE t (id INT, UNIQUE (id, id));",
		"CREATE TABLE t (id INT, UNIQUE (1));",
		"CREATE TABLE t (PRIMARY KEY (id));",
	}

	for _, input := range inputs {
		db, _ := storage.Open(t.TempDir(), 4096)
		if _, err := execute(db, input); err == nil {
			t.Errorf("execute(%q) deveria falhar", input)
		}
	}
}

func TestInsertInvalid(t *testing.T) {
	inputs := []string{
		`INSERT INTO nada VALUES (1, "Ana");`,
		`INSERT INTO users VALUES (1);`,
		`INSERT INTO users VALUES (1, "Ana", 3);`,
		`INSERT INTO users VALUES ("1", "Ana");`,
		`INSERT INTO users VALUES (1, 2);`,
		`INSERT INTO users VALUES (1, nome);`,
		`INSERT INTO users VALUES (1,, "Ana");`,
		`INSERT INTO users VALUES 1, "Ana";`,
	}

	for _, input := range inputs {
		db, _ := storage.Open(t.TempDir(), 4096)
		if _, err := execute(db, "CREATE TABLE users (id INT, nome TEXT);"); err != nil {
			t.Fatal(err)
		}
		if _, err := execute(db, input); err == nil {
			t.Errorf("execute(%q) deveria falhar", input)
		}
	}
}

func TestIndexes(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	setup := []string{
		"CREATE TABLE p (id INT PRIMARY KEY, nome TEXT, idade INT, cidade TEXT);",
		"INSERT INTO p VALUES (1, 'Ana', 30, 'Recife'), (2, 'Bia', 25, 'Natal'), (3, 'Caio', 30, 'Natal'), (4, NULL, 41, 'Recife');",
	}
	for _, s := range setup {
		if _, err := execute(db, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}

	steps := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "EXPLAIN SELECT * FROM p WHERE id = 2", want: "busca no índice p_pkey da tabela p (id = 2)"},
		{input: "EXPLAIN SELECT * FROM p WHERE 2 < id AND nome <> 'x'", want: "busca no índice p_pkey da tabela p (id > 2)"},
		{input: "EXPLAIN SELECT * FROM p WHERE id = 2 OR id = 3", want: "varredura completa na tabela p"},
		{input: "EXPLAIN SELECT * FROM p WHERE idade = 30", want: "varredura completa na tabela p"},
		{input: "EXPLAIN SELECT * FROM p WHERE idade = 30 AND idade = 'x'", wantErr: true},
		{input: "CREATE INDEX p_idade ON p (idade, cidade);", want: "índice p_idade criado"},
		{input: "EXPLAIN SELECT * FROM p WHERE idade = 30", want: "busca no índice p_idade da tabela p (idade = 30)"},
		{input: "EXPLAIN SELECT * FROM p WHERE idade >= 30 AND idade < 40", want: "busca no índice p_idade da tabela p (idade >= 30 AND idade < 40)"},
		{input: "EXPLAIN DELETE FROM p WHERE cidade = 'Natal' AND idade = 30", want: "busca no índice p_idade da tabela p (idade = 30 AND cidade = 'Natal')"},
		{input: "EXPLAIN UPDATE p SET nome = 'x' WHERE id = 1 AND idade = 30", want: "busca no índice p_pkey da tabela p (id = 1)"},
		// Pelo índice, as linhas saem na ordem da chave (idade, cidade).
		{input: "SELECT nome FROM p WHERE idade = 30", want: "nome\n----\nCaio\nAna\n(2 linhas)"},
		{input: "SELECT nome FROM p WHERE idade >= 30 AND idade < 40 AND nome IS NOT NULL", want: "nome\n----\nCaio\nAna\n(2 linhas)"},
		{input: "UPDATE p SET idade = 31 WHERE idade = 30 AND cidade = 'Natal'", want: "1 linha atualizada"},
		{input: "SELECT id FROM p WHERE idade = 31", want: "id\n--\n 3\n(1 linha)"},
		{input: "DELETE FROM p WHERE idade > 30", want: "2 linhas removidas"},
		{input: "SELECT id FROM p WHERE idade >= 0", want: "id\n--\n 2\n 1\n(2 linhas)"},
		{input: "SELECT name, table_name, type, columns FROM _indexes", want: "name    | table_name | type        | columns\n--------+------------+-------------+-------------\np_pkey  | p          | primary_key | id\np_idade | p          | index       | idade,cidade\n(2 linhas)"},
		{input: "CREATE INDEX p_idade ON p (nome);", wantErr: true},
		{input: "DROP INDEX p_pkey;", wantErr: true},
		{input: "DROP INDEX p_idade;", want: "índice p_idade removido"},
		{input: "EXPLAIN SELECT * FROM p WHERE idade = 30", want: "varredura completa na tabela p"},
		{input: "INSERT INTO p VALUES (2, 'Duda', 20, 'Natal');", wantErr: true},
	}
	for _, s := range steps {
		got, err := execute(db, s.input)
		if (err != nil) != s.wantErr {
			t.Fatalf("execute(%q) erro = %v, wantErr %v", s.input, err, s.wantErr)
		}
		if got != s.want {
			t.Errorf("execute(%q) =\n%s\nwant\n%s", s.input, got, s.want)
		}
	}
}

// TestIndexMatchesFullScan confere que o SELECT pelo índice devolve as mesmas
// linhas que a varredura completa, para várias condições.
func TestIndexMatchesFullScan(t *testing.T) {
	withIndex, _ := storage.Open(t.TempDir(), 1024)
	without, _ := storage.Open(t.TempDir(), 1024)
	for _, db := range []*storage.DB{withIndex, without} {
		execute(db, "CREATE TABLE t (a INT, b TEXT, c INT);")
		var values []string
		for i := range 300 {
			b := fmt.Sprintf("'%c'", 'a'+i%5)
			if i%11 == 0 {
				b = "NULL"
			}
			values = append(values, fmt.Sprintf("(%d, %s, %d)", (i*7)%50-10, b, i))
		}
		if _, err := execute(db, "INSERT INTO t VALUES "+strings.Join(values, ", ")+";"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := execute(withIndex, "CREATE INDEX t_a_b ON t (a, b);"); err != nil {
		t.Fatal(err)
	}

	wheres := []string{
		"a = 3", "a = -10", "a = 100", "a > 30", "a >= 30", "a < -5", "a <= -5",
		"a >= 0 AND a < 10", "10 > a AND a > 0", "a = 3 AND b = 'c'", "a = 3 AND b >= 'b'",
		"a = 3 AND b IS NULL", "a = 3 AND c > 100", "b = 'a'", "a = 3 OR a = 4",
	}
	for _, w := range wheres {
		query := "SELECT c FROM t WHERE " + w + ";"
		got, err := querySelect(withIndex, mustSelect(t, query))
		if err != nil {
			t.Fatal(err)
		}
		want, _ := querySelect(without, mustSelect(t, query))
		if !sameCells(got.Rows, want.Rows) {
			t.Errorf("%s: com índice %d linhas, sem índice %d", w, len(got.Rows), len(want.Rows))
		}
	}
}

func mustSelect(t *testing.T, input string) *parser.Select {
	t.Helper()
	stmt, err := parse(input)
	if err != nil {
		t.Fatal(err)
	}
	return stmt.(*parser.Select)
}

// sameCells compara linhas sem olhar a ordem: sem ORDER BY, a ordem do
// índice e a da tabela podem ser diferentes.
func sameCells(a, b [][]storage.Value) bool {
	key := func(rows [][]storage.Value) []string {
		var keys []string
		for _, r := range rows {
			keys = append(keys, fmt.Sprint(r))
		}
		slices.Sort(keys)
		return keys
	}
	return slices.Equal(key(a), key(b))
}
