package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// peopleDB cria pessoas (id INT PRIMARY KEY, nome TEXT, idade INT) com 30
// linhas: idade = id % 10, e nome NULL nos múltiplos de 7.
func peopleDB(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, _ := Open(dir, testPageSize)
	columns := []Column{{Name: "id", Type: "INT"}, {Name: "nome", Type: "TEXT"}, {Name: "idade", Type: "INT"}}
	if err := db.CreateTable("pessoas", columns, []Index{{Kind: PrimaryKey, Columns: []string{"id"}}}); err != nil {
		t.Fatal(err)
	}
	var rows [][]Value
	for i := range 30 {
		nome := Text("p" + strconv.Itoa(i))
		if i%7 == 0 {
			nome = Null
		}
		rows = append(rows, []Value{Text(strconv.Itoa(i)), nome, Text(strconv.Itoa(i % 10))})
	}
	if err := db.InsertRows("pessoas", rows); err != nil {
		t.Fatal(err)
	}
	return db, dir
}

func ids(rows []Row) []string {
	var got []string
	for _, r := range rows {
		got = append(got, r.Values[0].Text)
	}
	return got
}

func TestCreateIndex(t *testing.T) {
	db, dir := peopleDB(t)
	if err := db.CreateIndex("pessoas_idade", "pessoas", []string{"idade", "id"}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		r    IndexRange
		want []string
	}{
		{name: "igual", r: IndexRange{Eq: []Value{Text("3")}}, want: []string{"3", "13", "23"}},
		{name: "igual nas duas colunas", r: IndexRange{Eq: []Value{Text("3"), Text("13")}}, want: []string{"13"}},
		{name: "intervalo na segunda coluna", r: IndexRange{Eq: []Value{Text("3")}, From: &Value{Text: "10"}}, want: []string{"13", "23"}},
		{name: "intervalo", r: IndexRange{From: &Value{Text: "8"}}, want: []string{"8", "18", "28", "9", "19", "29"}},
		{name: "até", r: IndexRange{To: &Value{Text: "0"}}, want: []string{"0", "10", "20"}},
		{name: "nada", r: IndexRange{Eq: []Value{Text("42")}}, want: nil},
	}
	check := func(t *testing.T, db *DB) {
		for _, tt := range tests {
			got := ids(collect(t, db.IndexScan("pessoas", "pessoas_idade", tt.r)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
			}
		}
	}
	check(t, db)

	// O índice sobrevive a reabrir o banco e acompanha INSERT, UPDATE e DELETE.
	db, _ = Open(dir, testPageSize)
	check(t, db)
	insertOne(db, "pessoas", []Value{Text("33"), Null, Text("3")})
	table, _ := db.ReadTable("pessoas")
	db.DeleteRows("pessoas", table.RIDs[13:14]) // id 13
	db.UpdateRows("pessoas", table.RIDs[23:24], [][]Value{{Text("23"), Null, Text("4")}})
	got := ids(collect(t, db.IndexScan("pessoas", "pessoas_idade", IndexRange{Eq: []Value{Text("3")}})))
	if want := []string{"3", "33"}; !reflect.DeepEqual(got, want) {
		t.Errorf("depois das mudanças: %v, want %v", got, want)
	}

	if err := db.DropIndex("pessoas_idade"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pessoas_idade.mari")); err == nil {
		t.Error("o arquivo do índice ficou")
	}
	db, _ = Open(dir, testPageSize)
	if schema, _ := db.TableSchema("pessoas"); len(schema.Indexes) != 1 {
		t.Errorf("índices depois do DROP INDEX = %v", schema.Indexes)
	}
}

func TestCreateIndexErrors(t *testing.T) {
	db, _ := peopleDB(t)
	db.CreateTable("outra", userColumns, nil)
	db.CreateIndex("pessoas_nome", "pessoas", []string{"nome"})

	tests := []struct {
		name, index, table string
		columns            []string
	}{
		{"tabela inexistente", "i", "nada", []string{"id"}},
		{"coluna inexistente", "i", "pessoas", []string{"x"}},
		{"coluna repetida", "i", "pessoas", []string{"id", "id"}},
		{"sem colunas", "i", "pessoas", nil},
		{"nome de tabela", "outra", "pessoas", []string{"id"}},
		{"nome de índice", "pessoas_nome", "pessoas", []string{"id"}},
		{"nome de chave", "pessoas_pkey", "pessoas", []string{"id"}},
		{"nome do sistema", "_i", "pessoas", []string{"id"}},
		{"tabela do sistema", "i", "_catalog", []string{"name"}},
	}
	for _, tt := range tests {
		if err := db.CreateIndex(tt.index, tt.table, tt.columns); err == nil {
			t.Errorf("%s: esperava erro", tt.name)
		}
	}
	if err := db.DropIndex("pessoas_pkey"); err == nil {
		t.Error("DROP INDEX da chave primária: esperava erro")
	}
	if err := db.DropIndex("nada"); err == nil {
		t.Error("DROP INDEX de índice inexistente: esperava erro")
	}
	if err := db.CreateTable("pessoas_nome", userColumns, nil); err == nil {
		t.Error("CREATE TABLE com nome de índice: esperava erro")
	}
}

func TestIndexKeyTooBig(t *testing.T) {
	db, _ := Open(t.TempDir(), testPageSize)
	db.CreateTable("t", userColumns, []Index{{Kind: Unique, Columns: []string{"nome"}}})
	big := make([]byte, 400)
	for i := range big {
		big[i] = 'x'
	}
	if err := db.InsertRows("t", [][]Value{texts("1", "a"), texts("2", string(big))}); err == nil {
		t.Error("esperava erro para chave grande demais para o índice")
	}
	if table, _ := db.ReadTable("t"); len(table.Rows) != 0 {
		t.Errorf("o INSERT que falhou deixou linhas: %v", table.Rows)
	}
}

func TestIndexesTable(t *testing.T) {
	db, _ := peopleDB(t)
	db.CreateIndex("pessoas_nome", "pessoas", []string{"nome", "idade"})
	table, err := db.ReadTable("_indexes")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]Value{
		texts("pessoas_pkey", "pessoas", "primary_key", "id", "1", "1"),
		texts("pessoas_nome", "pessoas", "index", "nome,idade", "1", "1"),
	}
	if !reflect.DeepEqual(table.Rows, want) {
		t.Errorf("_indexes = %v, want %v", table.Rows, want)
	}
}
