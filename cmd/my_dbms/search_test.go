package main

import (
	"strings"
	"testing"

	"my_DBMS/internal/embed"
	"my_DBMS/internal/storage"
)

func TestSearch(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	db.Embed = embed.Fake(64)

	steps := []struct {
		input   string
		want    string // a saída tem que conter isto
		wantErr string
	}{
		{input: "CREATE TABLE docs (id INT PRIMARY KEY, titulo TEXT, corpo TEXT) WITH EMBEDDING (titulo, corpo);", want: "tabela docs criada"},
		{input: "SELECT type, name, definition FROM _catalog WHERE type = 'vector';", want: "vector | docs_embedding | titulo,corpo"},
		{input: `INSERT INTO docs VALUES (1, 'Bolo', 'farinha ovos açúcar'), (2, 'Gato', 'o gato dorme no sofá'), (3, 'Carro', 'motor rodas');`, want: "3 linhas inseridas"},
		{input: "SEARCH docs FOR 'onde o gato dorme' TOP 1;", want: "similarity | id | titulo | corpo"},
		{input: "SEARCH docs FOR 'onde o gato dorme' TOP 1;", want: "| Gato   | o gato dorme no sofá\n(1 linha)"},
		{input: "UPDATE docs SET corpo = 'o carro dorme na garagem' WHERE id = 3;", want: "1 linha atualizada"},
		{input: "SEARCH docs FOR 'carro na garagem' TOP 1;", want: "Carro"},
		{input: "DELETE FROM docs WHERE id = 3;", want: "1 linha removida"},
		{input: "SEARCH docs FOR 'garagem';", want: "(2 linhas)"},
		{input: "SEARCH nada FOR 'x';", wantErr: "não existe"},
		{input: "CREATE TABLE p (id INT, nome TEXT);", want: "tabela p criada"},
		{input: "SEARCH p FOR 'x';", wantErr: "não tem índice vetorial"},
		{input: "CREATE VECTOR INDEX p_vec ON p (nome);", want: "índice p_vec criado"},
		{input: "INSERT INTO p VALUES (1, 'Ana');", want: "1 linha inserida"},
		{input: "SEARCH p FOR 'Ana';", want: "|  1 | Ana\n(1 linha)"},
		{input: "DROP INDEX p_vec;", want: "índice p_vec removido"},
	}
	for _, s := range steps {
		got, err := execute(db, s.input)
		if s.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), s.wantErr) {
				t.Errorf("execute(%q): erro %v, queria algo com %q", s.input, err, s.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("execute(%q): %v", s.input, err)
		}
		if !strings.Contains(got, s.want) {
			t.Errorf("execute(%q) =\n%s\nqueria algo com\n%s", s.input, got, s.want)
		}
	}
}

// Sem API configurada, a tabela com embedding pode ser criada, mas o INSERT
// explica o que falta.
func TestSearchWithoutEmbedder(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execute(db, "CREATE TABLE d (t TEXT) WITH EMBEDDING (t);"); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(db, "INSERT INTO d VALUES ('oi');"); err == nil || !strings.Contains(err.Error(), "my_dbms -setup") {
		t.Errorf("INSERT sem API: %v", err)
	}
}
