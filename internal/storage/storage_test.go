package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var userColumns = []Column{{Name: "id", Type: "INT"}, {Name: "nome", Type: "TEXT"}}

func TestCreateAndReadTable(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := db.CreateTable("users", userColumns, nil); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	table, err := db.ReadTable("users")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	if !reflect.DeepEqual(table.Columns, userColumns) {
		t.Errorf("colunas = %v, want %v", table.Columns, userColumns)
	}
}

func TestCreateTableErrors(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		columns []Column
	}{
		{name: "nome vazio", table: "", columns: userColumns},
		{name: "sem colunas", table: "users", columns: nil},
		{name: "coluna repetida", table: "users", columns: []Column{{"id", "INT"}, {"id", "TEXT"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir())
			if err := db.CreateTable(tt.table, tt.columns, nil); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

func TestCreateTableTwice(t *testing.T) {
	db, _ := Open(t.TempDir())
	db.CreateTable("users", userColumns, nil)

	if err := db.CreateTable("users", userColumns, nil); err == nil {
		t.Error("esperava erro ao criar tabela que já existe")
	}
}

func TestInsertRow(t *testing.T) {
	tests := []struct {
		name    string
		columns []Column
		rows    [][]string
	}{
		{name: "simples", columns: userColumns, rows: [][]string{{"1", "Ana"}, {"2", "Bruno"}}},
		{name: "vírgula e aspas", columns: userColumns, rows: [][]string{{"1", `Silva, "Ana"`}}},
		{name: "campo único vazio", columns: []Column{{"s", "TEXT"}}, rows: [][]string{{""}, {"x"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir())
			if err := db.CreateTable("t", tt.columns, nil); err != nil {
				t.Fatal(err)
			}
			for _, row := range tt.rows {
				if err := db.InsertRow("t", row); err != nil {
					t.Fatalf("InsertRow(%q): %v", row, err)
				}
			}

			table, err := db.ReadTable("t")
			if err != nil {
				t.Fatalf("ReadTable: %v", err)
			}
			if !reflect.DeepEqual(table.Rows, tt.rows) {
				t.Errorf("linhas = %q, want %q", table.Rows, tt.rows)
			}
		})
	}
}

func TestInsertRowMissingTable(t *testing.T) {
	db, _ := Open(t.TempDir())

	if err := db.InsertRow("nada", []string{"1"}); err == nil {
		t.Error("esperava erro ao inserir em tabela inexistente")
	}
}

func TestReadMissingTable(t *testing.T) {
	db, _ := Open(t.TempDir())

	if _, err := db.ReadTable("nada"); err == nil {
		t.Error("esperava erro ao ler tabela inexistente")
	}
}

func TestCatalogSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir)
	if err := db.CreateTable("users", userColumns, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertRow("users", []string{"1", "Ana"}); err != nil {
		t.Fatal(err)
	}

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open de novo: %v", err)
	}
	table, err := db.ReadTable("users")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	if !reflect.DeepEqual(table.Columns, userColumns) {
		t.Errorf("colunas = %v, want %v", table.Columns, userColumns)
	}
	if want := [][]string{{"1", "Ana"}}; !reflect.DeepEqual(table.Rows, want) {
		t.Errorf("linhas = %q, want %q", table.Rows, want)
	}
}

func TestReadCatalog(t *testing.T) {
	db, _ := Open(t.TempDir())
	db.CreateTable("users", userColumns, nil)

	table, err := db.ReadTable("_catalog")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	want := [][]string{{"table", "users", "users", "id INT,nome TEXT"}}
	if !reflect.DeepEqual(table.Rows, want) {
		t.Errorf("linhas = %q, want %q", table.Rows, want)
	}
}

func TestSystemNames(t *testing.T) {
	db, _ := Open(t.TempDir())

	if err := db.CreateTable("_catalog", userColumns, nil); err == nil {
		t.Error("esperava erro ao criar tabela com nome reservado")
	}
	if err := db.InsertRow("_catalog", []string{"table", "x", "x", "id INT"}); err == nil {
		t.Error("esperava erro ao inserir no catálogo")
	}
}

func TestCreateTableOrphanFile(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir)
	os.WriteFile(filepath.Join(dir, "users.marina"), []byte("1,Ana\n"), 0o644)

	if err := db.CreateTable("users", userColumns, nil); err == nil {
		t.Error("esperava erro: arquivo existe fora do catálogo")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "users.marina")); string(data) != "1,Ana\n" {
		t.Errorf("arquivo órfão foi alterado: %q", data)
	}
}

func TestCreateTableKeys(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir)
	columns := []Column{{"id", "INT"}, {"email", "TEXT"}, {"a", "INT"}, {"b", "INT"}}
	keys := []Key{
		{Kind: PrimaryKey, Columns: []string{"id"}},
		{Kind: Unique, Columns: []string{"email"}},
		{Kind: Unique, Columns: []string{"a", "b"}},
	}
	if err := db.CreateTable("users", columns, keys); err != nil {
		t.Fatal(err)
	}

	want := []Key{
		{Name: "users_pkey", Kind: PrimaryKey, Columns: []string{"id"}},
		{Name: "users_email_key", Kind: Unique, Columns: []string{"email"}},
		{Name: "users_a_b_key", Kind: Unique, Columns: []string{"a", "b"}},
	}
	db, _ = Open(dir)
	table, err := db.ReadTable("users")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(table.Keys, want) {
		t.Errorf("chaves = %v, want %v", table.Keys, want)
	}
}

func TestCreateTableKeyErrors(t *testing.T) {
	tests := []struct {
		name string
		keys []Key
	}{
		{name: "sem colunas", keys: []Key{{Kind: Unique}}},
		{name: "coluna inexistente", keys: []Key{{Kind: Unique, Columns: []string{"x"}}}},
		{name: "coluna repetida", keys: []Key{{Kind: Unique, Columns: []string{"id", "id"}}}},
		{name: "duas primárias", keys: []Key{{Kind: PrimaryKey, Columns: []string{"id"}}, {Kind: PrimaryKey, Columns: []string{"nome"}}}},
		{name: "unique repetida", keys: []Key{{Kind: Unique, Columns: []string{"id"}}, {Kind: Unique, Columns: []string{"id"}}}},
		{name: "tipo desconhecido", keys: []Key{{Kind: "foreign_key", Columns: []string{"id"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			db, _ := Open(dir)
			if err := db.CreateTable("users", userColumns, tt.keys); err == nil {
				t.Fatal("esperava erro")
			}
			if _, err := os.Stat(filepath.Join(dir, "users.marina")); err == nil {
				t.Error("tabela inválida não deveria criar arquivo")
			}
		})
	}
}

func TestInsertRowKeys(t *testing.T) {
	columns := []Column{{"id", "INT"}, {"a", "TEXT"}, {"b", "TEXT"}}
	keys := []Key{
		{Kind: PrimaryKey, Columns: []string{"id"}},
		{Kind: Unique, Columns: []string{"a", "b"}},
	}
	tests := []struct {
		name    string
		row     []string
		wantErr bool
	}{
		{name: "id repetido", row: []string{"1", "z", "z"}, wantErr: true},
		{name: "par (a, b) repetido", row: []string{"2", "x", "y"}, wantErr: true},
		{name: "só a repetido", row: []string{"2", "x", "z"}},
		{name: "só b repetido", row: []string{"2", "z", "y"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir())
			if err := db.CreateTable("t", columns, keys); err != nil {
				t.Fatal(err)
			}
			if err := db.InsertRow("t", []string{"1", "x", "y"}); err != nil {
				t.Fatal(err)
			}

			err := db.InsertRow("t", tt.row)
			if (err != nil) != tt.wantErr {
				t.Fatalf("InsertRow(%q) erro = %v, wantErr %v", tt.row, err, tt.wantErr)
			}
			want := 2
			if tt.wantErr {
				want = 1
			}
			table, _ := db.ReadTable("t")
			if len(table.Rows) != want {
				t.Errorf("linhas = %q, want %d", table.Rows, want)
			}
		})
	}
}

func TestOpenCorruptCatalog(t *testing.T) {
	tests := []struct {
		name    string
		catalog string
	}{
		{name: "tipo desconhecido", catalog: "view,v,v,x\n"},
		{name: "campos a menos", catalog: "table,users\n"},
		{name: "coluna sem tipo", catalog: "table,users,users,id\n"},
		{name: "chave sem tabela", catalog: "primary_key,users_pkey,users,id\n"},
		{name: "chave com coluna inexistente", catalog: "table,users,users,id INT\nunique,users_x_key,users,x\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "_catalog.marina"), []byte(tt.catalog), 0o644)
			if _, err := Open(dir); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}
