package main

import (
	"testing"

	"my_DBMS/internal/storage"
)

func TestExecute(t *testing.T) {
	db, err := storage.Open(t.TempDir())
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
		{input: "SELECT * FROM users;", want: "id | nome\n---+-------------\n1  | Ana\n7  | Silva, Bruno\n(2 linhas)"},
		{input: "SELECT nome FROM users;", want: "nome\n------------\nAna\nSilva, Bruno\n(2 linhas)"},
		{input: "SELECT nome, id, nome FROM users;", want: "nome         | id | nome\n-------------+----+-------------\nAna          | 1  | Ana\nSilva, Bruno | 7  | Silva, Bruno\n(2 linhas)"},
		{input: "SELECT idade FROM users;", wantErr: true},
		{input: "SELECT name, definition FROM _catalog;", want: "name  | definition\n------+-----------------\nusers | id INT,nome TEXT\n(1 linha)"},
		{input: `INSERT INTO _catalog VALUES ("table", "x", "x", "id INT");`, wantErr: true},
		{input: "DROP TABLE users;", wantErr: true},
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
		db, _ := storage.Open(t.TempDir())
		if _, err := execute(db, input); err == nil {
			t.Errorf("execute(%q) deveria falhar", input)
		}
	}
}

func TestKeys(t *testing.T) {
	db, err := storage.Open(t.TempDir())
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
	db, err := storage.Open(t.TempDir())
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
		db, _ := storage.Open(t.TempDir())
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
		db, _ := storage.Open(t.TempDir())
		if _, err := execute(db, "CREATE TABLE users (id INT, nome TEXT);"); err != nil {
			t.Fatal(err)
		}
		if _, err := execute(db, input); err == nil {
			t.Errorf("execute(%q) deveria falhar", input)
		}
	}
}
