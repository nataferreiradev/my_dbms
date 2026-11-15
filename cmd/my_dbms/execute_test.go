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
		{input: "select * from users", want: "id | nome\n(0 linhas)"},
		{input: "DROP TABLE users;", wantErr: true},
	}

	for _, s := range steps {
		got, err := execute(db, s.input)
		if (err != nil) != s.wantErr {
			t.Fatalf("execute(%q) erro = %v, wantErr %v", s.input, err, s.wantErr)
		}
		if got != s.want {
			t.Errorf("execute(%q) = %q, want %q", s.input, got, s.want)
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
