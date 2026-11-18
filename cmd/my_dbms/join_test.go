package main

import (
	"strings"
	"testing"

	"my_DBMS/internal/storage"
)

func TestJoin(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	setup := []string{
		"CREATE TABLE pessoas (id INT PRIMARY KEY, nome TEXT NOT NULL, cidade_id INT);",
		"INSERT INTO pessoas VALUES (1, 'Ana', 10), (2, 'Bia', 20), (3, 'Caio', NULL);",
		"CREATE TABLE pedidos (id INT PRIMARY KEY, pessoa_id INT, item TEXT);",
		// 103 não tem pessoa e 104 aponta para uma pessoa que não existe.
		"INSERT INTO pedidos VALUES (100, 1, 'livro'), (101, 1, 'caneta'), (102, 2, 'livro'), (103, NULL, 'mesa'), (104, 9, 'lápis');",
		"CREATE TABLE cidades (id INT PRIMARY KEY, nome TEXT);",
		"INSERT INTO cidades VALUES (10, 'Recife'), (30, 'Natal');",
		"CREATE TABLE vazia (id INT);",
	}
	for _, s := range setup {
		if _, err := execute(db, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}

	steps := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name:  "inner",
			input: "SELECT pessoas.nome, pedidos.item FROM pessoas JOIN pedidos ON pessoas.id = pedidos.pessoa_id",
			want:  "nome | item\n-----+-------\nAna  | livro\nAna  | caneta\nBia  | livro\n(3 linhas)",
		},
		{
			name:  "inner escrito por extenso",
			input: "SELECT nome, item FROM pessoas INNER JOIN pedidos ON pedidos.pessoa_id = pessoas.id",
			want:  "nome | item\n-----+-------\nAna  | livro\nAna  | caneta\nBia  | livro\n(3 linhas)",
		},
		{
			name:  "left preenche NULL à direita",
			input: "SELECT nome, item FROM pessoas LEFT JOIN pedidos ON pessoas.id = pedidos.pessoa_id",
			want:  "nome | item\n-----+-------\nAna  | livro\nAna  | caneta\nBia  | livro\nCaio | NULL\n(4 linhas)",
		},
		{
			name:  "right preenche NULL à esquerda, inclusive chave NULL",
			input: "SELECT nome, item FROM pessoas RIGHT OUTER JOIN pedidos ON pessoas.id = pedidos.pessoa_id",
			want:  "nome | item\n-----+-------\nAna  | livro\nAna  | caneta\nBia  | livro\nNULL | mesa\nNULL | lápis\n(5 linhas)",
		},
		{
			name:  "WHERE vê os NULL do LEFT",
			input: "SELECT nome FROM pessoas LEFT JOIN pedidos ON pessoas.id = pedidos.pessoa_id WHERE pedidos.id IS NULL",
			want:  "nome\n----\nCaio\n(1 linha)",
		},
		{
			name:  "condição no ON não tira linhas do LEFT",
			input: "SELECT nome, item FROM pessoas LEFT JOIN pedidos ON pessoas.id = pedidos.pessoa_id AND item = 'caneta'",
			want:  "nome | item\n-----+-------\nAna  | caneta\nBia  | NULL\nCaio | NULL\n(3 linhas)",
		},
		{
			name:  "condição no WHERE tira",
			input: "SELECT nome, item FROM pessoas LEFT JOIN pedidos ON pessoas.id = pedidos.pessoa_id WHERE item = 'caneta'",
			want:  "nome | item\n-----+-------\nAna  | caneta\n(1 linha)",
		},
		{
			name:  "* qualifica só os nomes repetidos",
			input: "SELECT * FROM pessoas JOIN cidades ON cidade_id = cidades.id",
			want: "pessoas.id | pessoas.nome | cidade_id | cidades.id | cidades.nome\n" +
				"-----------+--------------+-----------+------------+-------------\n" +
				"         1 | Ana          |        10 |         10 | Recife\n" +
				"(1 linha)",
		},
		{
			name:  "right com cidade sem pessoa",
			input: "SELECT pessoas.nome, cidades.nome FROM pessoas RIGHT JOIN cidades ON pessoas.cidade_id = cidades.id",
			want:  "pessoas.nome | cidades.nome\n-------------+-------------\nAna          | Recife\nNULL         | Natal\n(2 linhas)",
		},
		{
			name:  "vários JOINs da esquerda para a direita",
			input: "SELECT pessoas.nome, item, cidades.nome FROM pessoas JOIN pedidos ON pessoas.id = pessoa_id LEFT JOIN cidades ON cidade_id = cidades.id",
			want:  "pessoas.nome | item   | cidades.nome\n-------------+--------+-------------\nAna          | livro  | Recife\nAna          | caneta | Recife\nBia          | livro  | NULL\n(3 linhas)",
		},
		{
			name:  "left com tabela vazia",
			input: "SELECT pessoas.id, vazia.id FROM pessoas LEFT JOIN vazia ON pessoas.id = vazia.id",
			want:  "pessoas.id | vazia.id\n-----------+---------\n         1 |     NULL\n         2 |     NULL\n         3 |     NULL\n(3 linhas)",
		},
		{
			name:  "inner com tabela vazia",
			input: "SELECT * FROM vazia JOIN pessoas ON vazia.id = pessoas.id",
			want:  "vazia.id | pessoas.id | nome | cidade_id\n---------+------------+------+----------\n(0 linhas)",
		},
		{
			name:  "explain",
			input: "EXPLAIN SELECT * FROM pessoas LEFT JOIN pedidos ON pessoas.id = pessoa_id WHERE pessoas.id = 1",
			want:  "varredura completa na tabela pessoas\nLEFT JOIN por nested loop: varredura completa na tabela pedidos",
		},
		{name: "coluna ambígua no SELECT", input: "SELECT id FROM pessoas JOIN pedidos ON pessoas.id = pessoa_id", wantErr: `coluna "id" é ambígua`},
		{name: "coluna ambígua no ON", input: "SELECT * FROM pessoas JOIN pedidos ON id = pessoa_id", wantErr: `coluna "id" é ambígua`},
		{name: "coluna ambígua no WHERE com tabela vazia", input: "SELECT * FROM vazia JOIN pedidos ON vazia.id = pedidos.id WHERE id = 1", wantErr: `coluna "id" é ambígua`},
		{name: "tabela inexistente", input: "SELECT * FROM pessoas JOIN nada ON pessoas.id = nada.id", wantErr: `tabela "nada" não existe`},
		{name: "mesma tabela duas vezes", input: "SELECT * FROM pessoas JOIN pessoas ON pessoas.id = pessoas.id", wantErr: "aparece duas vezes"},
		{name: "tipos diferentes no ON", input: "SELECT * FROM pessoas JOIN pedidos ON pessoas.nome = pedidos.id", wantErr: "não dá para comparar TEXT com INT"},
		{name: "ON vê só as tabelas anteriores", input: "SELECT * FROM pessoas JOIN pedidos ON pessoas.id = cidades.id JOIN cidades ON cidades.id = 1", wantErr: `tabela "cidades" não está no FROM`},
		{name: "coluna de outra tabela", input: "SELECT pedidos.nome FROM pessoas JOIN pedidos ON pessoas.id = pessoa_id", wantErr: `coluna "pedidos.nome" não existe`},
		{name: "explain confere as colunas", input: "EXPLAIN SELECT id FROM pessoas JOIN pedidos ON pessoas.id = pessoa_id", wantErr: "ambígua"},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			got, err := execute(db, s.input)
			if s.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), s.wantErr) {
					t.Fatalf("erro = %v, queria algo com %q", err, s.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if got != s.want {
				t.Errorf("\n got:\n%s\nwant:\n%s", got, s.want)
			}
		})
	}
}

func TestNestedLoop(t *testing.T) {
	v := storage.Text
	left := [][]storage.Value{{v("1")}, {v("2")}, {storage.Null}}
	right := [][]storage.Value{{v("2")}, {v("3")}, {v("1")}, {storage.Null}}
	// O ON é left = right, comparando o texto das duas colunas.
	on := func(row []storage.Value) tri {
		if row[0].Null || row[1].Null {
			return triUnknown
		}
		return toTri(row[0].Text == row[1].Text)
	}

	tests := []struct {
		kind string
		want string
	}{
		{kind: "INNER", want: "1=1 2=2"},
		{kind: "LEFT", want: "1=1 2=2 NULL=NULL"},
		{kind: "RIGHT", want: "1=1 2=2 NULL=3 NULL=NULL"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			var pairs []string
			for _, row := range nestedLoop(tt.kind, left, right, 1, 1, on) {
				if len(row) != 2 {
					t.Fatalf("linha com %d colunas, queria 2", len(row))
				}
				pairs = append(pairs, display(row[0])+"="+display(row[1]))
			}
			if got := strings.Join(pairs, " "); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
