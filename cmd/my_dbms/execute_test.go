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
		{input: "create table users;", want: "tabela users criada"},
		{input: "CREATE TABLE users;", wantErr: true},
		{input: "select * from users", want: "(0 linhas)"},
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
