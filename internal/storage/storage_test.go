package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var userColumns = []Column{{Name: "id", Type: "INT"}, {Name: "nome", Type: "TEXT"}}

// texts monta uma linha só com textos, sem NULL.
func texts(fields ...string) []Value {
	row := make([]Value, len(fields))
	for i, f := range fields {
		row[i] = Text(f)
	}
	return row
}

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
		{name: "coluna repetida", table: "users", columns: []Column{{Name: "id", Type: "INT"}, {Name: "id", Type: "TEXT"}}},
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
		rows    [][]Value
	}{
		{name: "simples", columns: userColumns, rows: [][]Value{texts("1", "Ana"), texts("2", "Bruno")}},
		{name: "vírgula e aspas", columns: userColumns, rows: [][]Value{texts("1", `Silva, "Ana"`)}},
		{name: "campo único vazio", columns: []Column{{Name: "s", Type: "TEXT"}}, rows: [][]Value{texts(""), texts("x")}},
		{name: "NULL", columns: userColumns, rows: [][]Value{{Text("1"), Null}, {Null, Text("")}}},
		{name: "texto parecido com NULL", columns: userColumns, rows: [][]Value{texts("1", `\N`), texts("2", `\\x`), texts("3", `\`)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir())
			if err := db.CreateTable("t", tt.columns, nil); err != nil {
				t.Fatal(err)
			}
			for _, row := range tt.rows {
				if err := db.InsertRow("t", row); err != nil {
					t.Fatalf("InsertRow(%v): %v", row, err)
				}
			}

			table, err := db.ReadTable("t")
			if err != nil {
				t.Fatalf("ReadTable: %v", err)
			}
			if !reflect.DeepEqual(table.Rows, tt.rows) {
				t.Errorf("linhas = %v, want %v", table.Rows, tt.rows)
			}
		})
	}
}

func TestInsertRowErrors(t *testing.T) {
	columns := []Column{{Name: "id", Type: "INT", NotNull: true}, {Name: "nome", Type: "TEXT"}}
	tests := []struct {
		name  string
		table string
		row   []Value
	}{
		{name: "tabela inexistente", table: "nada", row: texts("1", "Ana")},
		{name: "valores a menos", table: "t", row: texts("1")},
		{name: "NULL em NOT NULL", table: "t", row: []Value{Null, Text("Ana")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir())
			db.CreateTable("t", columns, nil)
			if err := db.InsertRow(tt.table, tt.row); err == nil {
				t.Error("esperava erro")
			}
		})
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
	columns := []Column{{Name: "id", Type: "INT", NotNull: true}, {Name: "nome", Type: "TEXT"}}
	if err := db.CreateTable("users", columns, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertRow("users", []Value{Text("1"), Null}); err != nil {
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
	if !reflect.DeepEqual(table.Columns, columns) {
		t.Errorf("colunas = %v, want %v", table.Columns, columns)
	}
	if want := [][]Value{{Text("1"), Null}}; !reflect.DeepEqual(table.Rows, want) {
		t.Errorf("linhas = %v, want %v", table.Rows, want)
	}
}

func TestReadCatalog(t *testing.T) {
	db, _ := Open(t.TempDir())
	db.CreateTable("users", userColumns, []Key{{Kind: PrimaryKey, Columns: []string{"id"}}})

	table, err := db.ReadTable("_catalog")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	want := [][]Value{
		texts("table", "users", "users", "id INT NOT NULL,nome TEXT"),
		texts("primary_key", "users_pkey", "users", "id"),
	}
	if !reflect.DeepEqual(table.Rows, want) {
		t.Errorf("linhas = %v, want %v", table.Rows, want)
	}
}

func TestSystemNames(t *testing.T) {
	db, _ := Open(t.TempDir())

	if err := db.CreateTable("_catalog", userColumns, nil); err == nil {
		t.Error("esperava erro ao criar tabela com nome reservado")
	}
	if err := db.InsertRow("_catalog", texts("table", "x", "x", "id INT")); err == nil {
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
	columns := []Column{{Name: "id", Type: "INT"}, {Name: "email", Type: "TEXT"}, {Name: "a", Type: "INT"}, {Name: "b", Type: "INT"}}
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
	if !table.Columns[0].NotNull {
		t.Error("coluna da chave primária deveria ser NOT NULL")
	}
	if columns[0].NotNull {
		t.Error("CreateTable alterou o slice de colunas de quem chamou")
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
	columns := []Column{{Name: "id", Type: "INT"}, {Name: "a", Type: "TEXT"}, {Name: "b", Type: "TEXT"}}
	keys := []Key{
		{Kind: PrimaryKey, Columns: []string{"id"}},
		{Kind: Unique, Columns: []string{"a", "b"}},
	}
	tests := []struct {
		name    string
		row     []Value
		wantErr bool
	}{
		{name: "id repetido", row: texts("1", "z", "z"), wantErr: true},
		{name: "par (a, b) repetido", row: texts("2", "x", "y"), wantErr: true},
		{name: "só a repetido", row: texts("2", "x", "z")},
		{name: "só b repetido", row: texts("2", "z", "y")},
		{name: "id NULL", row: []Value{Null, Text("z"), Text("z")}, wantErr: true},
		{name: "NULL na unique não colide", row: []Value{Text("2"), Text("n"), Null}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir())
			if err := db.CreateTable("t", columns, keys); err != nil {
				t.Fatal(err)
			}
			if err := db.InsertRow("t", texts("1", "x", "y")); err != nil {
				t.Fatal(err)
			}
			if err := db.InsertRow("t", []Value{Text("9"), Text("n"), Null}); err != nil {
				t.Fatal(err)
			}

			err := db.InsertRow("t", tt.row)
			if (err != nil) != tt.wantErr {
				t.Fatalf("InsertRow(%v) erro = %v, wantErr %v", tt.row, err, tt.wantErr)
			}
			want := 3
			if tt.wantErr {
				want = 2
			}
			table, _ := db.ReadTable("t")
			if len(table.Rows) != want {
				t.Errorf("linhas = %v, want %d", table.Rows, want)
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
		{name: "coluna com sobra", catalog: "table,users,users,id INT NULL\n"},
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
