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
