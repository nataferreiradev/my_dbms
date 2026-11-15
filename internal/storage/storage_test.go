package storage

import (
	"reflect"
	"testing"
)

var userColumns = []Column{{Name: "id", Type: "INT"}, {Name: "nome", Type: "TEXT"}}

func TestCreateAndReadTable(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := db.CreateTable("users", userColumns); err != nil {
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
			if err := db.CreateTable(tt.table, tt.columns); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

func TestCreateTableTwice(t *testing.T) {
	db, _ := Open(t.TempDir())
	db.CreateTable("users", userColumns)

	if err := db.CreateTable("users", userColumns); err == nil {
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
			if err := db.CreateTable("t", tt.columns); err != nil {
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
