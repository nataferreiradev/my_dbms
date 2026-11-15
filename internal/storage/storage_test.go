package storage

import "testing"

func TestCreateAndReadTable(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := db.CreateTable("users"); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	data, err := db.ReadTable("users")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("tabela nova deveria estar vazia, veio %d bytes", len(data))
	}
}

func TestCreateTableTwice(t *testing.T) {
	db, _ := Open(t.TempDir())
	db.CreateTable("users")

	if err := db.CreateTable("users"); err == nil {
		t.Error("esperava erro ao criar tabela que já existe")
	}
}

func TestCreateTableEmptyName(t *testing.T) {
	db, _ := Open(t.TempDir())

	if err := db.CreateTable(""); err == nil {
		t.Error("esperava erro para nome vazio")
	}
}

func TestReadMissingTable(t *testing.T) {
	db, _ := Open(t.TempDir())

	if _, err := db.ReadTable("nada"); err == nil {
		t.Error("esperava erro ao ler tabela inexistente")
	}
}
