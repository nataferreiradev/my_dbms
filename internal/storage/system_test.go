package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, 2048)

	table, err := db.ReadTable("_settings")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range table.Rows {
		got[row[0].Text] = row[1].Text
	}
	want := map[string]string{"data_dir": dir, "page_size": "2048", "format_version": "1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("_settings = %v, want %v", got, want)
	}
}

func TestTables(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, testPageSize)
	db.CreateTable("users", userColumns, []Key{{Kind: PrimaryKey, Columns: []string{"id"}}})
	db.CreateTable("vazia", userColumns, nil)

	// Cada registro tem 50 bytes + 4 do slot: cabem 18 por página, e 50
	// linhas ocupam 3 páginas.
	var rows [][]Value
	for i := range 50 {
		rows = append(rows, texts(strconv.Itoa(i), strings.Repeat("x", 40)))
	}
	if err := db.InsertRows("users", rows); err != nil {
		t.Fatal(err)
	}

	table, err := db.ReadTable("_tables")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]Value{
		texts("_catalog", "3", "1", "2048"),
		texts("users", "50", "3", "4096"),
		texts("vazia", "0", "0", "1024"),
	}
	if !reflect.DeepEqual(table.Rows, want) {
		t.Errorf("_tables = %v, want %v", table.Rows, want)
	}

	// bytes tem que bater com o tamanho real do arquivo.
	for _, row := range table.Rows {
		info, err := os.Stat(filepath.Join(dir, row[0].Text+".marina"))
		if err != nil {
			t.Fatal(err)
		}
		if strconv.FormatInt(info.Size(), 10) != row[3].Text {
			t.Errorf("%s: bytes = %s, arquivo tem %d", row[0].Text, row[3].Text, info.Size())
		}
	}
}

func TestSystemTablesReadOnly(t *testing.T) {
	for _, name := range []string{"_settings", "_tables"} {
		t.Run(name, func(t *testing.T) {
			db, _ := Open(t.TempDir(), testPageSize)
			if err := db.CreateTable(name, userColumns, nil); err == nil {
				t.Error("CreateTable: esperava erro")
			}
			if err := db.InsertRows(name, [][]Value{texts("a", "b", "c")}); err == nil {
				t.Error("InsertRows: esperava erro")
			}
			if err := db.ReplaceRows(name, nil); err == nil {
				t.Error("ReplaceRows: esperava erro")
			}
			if err := db.DropTable(name); err == nil {
				t.Error("DropTable: esperava erro")
			}
		})
	}
}
