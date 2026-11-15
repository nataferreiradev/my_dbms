package storage

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testPageSize é o menor tamanho aceito, para encher páginas com poucas
// linhas.
const testPageSize = 1024

var userColumns = []Column{{Name: "id", Type: "INT"}, {Name: "nome", Type: "TEXT"}}

// texts monta uma linha só com textos, sem NULL.
func texts(fields ...string) []Value {
	row := make([]Value, len(fields))
	for i, f := range fields {
		row[i] = Text(f)
	}
	return row
}

func insertOne(db *DB, table string, row []Value) error {
	return db.InsertRows(table, [][]Value{row})
}

func TestCreateAndReadTable(t *testing.T) {
	db, err := Open(t.TempDir(), testPageSize)
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
		{name: "tipo desconhecido", table: "users", columns: []Column{{Name: "id", Type: "FLOAT"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir(), testPageSize)
			if err := db.CreateTable(tt.table, tt.columns, nil); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

func TestCreateTableTwice(t *testing.T) {
	db, _ := Open(t.TempDir(), testPageSize)
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
		{name: "INT nos extremos", columns: userColumns, rows: [][]Value{texts("-9223372036854775808", "a"), texts("9223372036854775807", "b"), texts("0", "c")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir(), testPageSize)
			if err := db.CreateTable("t", tt.columns, nil); err != nil {
				t.Fatal(err)
			}
			for _, row := range tt.rows {
				if err := insertOne(db, "t", row); err != nil {
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
			db, _ := Open(t.TempDir(), testPageSize)
			db.CreateTable("t", columns, nil)
			if err := insertOne(db, tt.table, tt.row); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

func TestInsertRowsBatch(t *testing.T) {
	keys := []Key{{Kind: PrimaryKey, Columns: []string{"id"}}}
	tests := []struct {
		name     string
		rows     [][]Value
		wantErr  bool
		wantRows int
	}{
		{name: "lote válido", rows: [][]Value{texts("2", "Bia"), texts("3", "Caio")}, wantRows: 3},
		{name: "repetido dentro do lote", rows: [][]Value{texts("2", "Bia"), texts("2", "Caio")}, wantErr: true, wantRows: 1},
		{name: "repetido com a tabela", rows: [][]Value{texts("2", "Bia"), texts("1", "Caio")}, wantErr: true, wantRows: 1},
		{name: "valores a menos no meio", rows: [][]Value{texts("2", "Bia"), texts("3")}, wantErr: true, wantRows: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir(), testPageSize)
			db.CreateTable("t", userColumns, keys)
			insertOne(db, "t", texts("1", "Ana"))

			err := db.InsertRows("t", tt.rows)
			if (err != nil) != tt.wantErr {
				t.Fatalf("InsertRows erro = %v, wantErr %v", err, tt.wantErr)
			}
			table, _ := db.ReadTable("t")
			if len(table.Rows) != tt.wantRows {
				t.Errorf("linhas = %v, want %d", table.Rows, tt.wantRows)
			}
		})
	}
}

func TestReplaceRows(t *testing.T) {
	keys := []Key{{Kind: PrimaryKey, Columns: []string{"id"}}}
	tests := []struct {
		name    string
		rows    [][]Value
		wantErr bool
	}{
		{name: "troca", rows: [][]Value{texts("2", "Bia"), {Text("3"), Null}}},
		{name: "esvazia", rows: nil},
		{name: "chave repetida", rows: [][]Value{texts("2", "Bia"), texts("2", "Caio")}, wantErr: true},
		{name: "NULL na chave primária", rows: [][]Value{{Null, Text("Bia")}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir(), testPageSize)
			db.CreateTable("t", userColumns, keys)
			before := [][]Value{texts("1", "Ana")}
			db.InsertRows("t", before)

			err := db.ReplaceRows("t", tt.rows)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ReplaceRows erro = %v, wantErr %v", err, tt.wantErr)
			}
			want := tt.rows
			if tt.wantErr {
				want = before
			}
			table, _ := db.ReadTable("t")
			if len(table.Rows) != len(want) || (len(want) > 0 && !reflect.DeepEqual(table.Rows, want)) {
				t.Errorf("linhas = %v, want %v", table.Rows, want)
			}
		})
	}
}

func TestDropTable(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, testPageSize)
	db.CreateTable("users", userColumns, []Key{{Kind: PrimaryKey, Columns: []string{"id"}}})
	db.CreateTable("outra", userColumns, nil)
	insertOne(db, "users", texts("1", "Ana"))

	if err := db.DropTable("users"); err != nil {
		t.Fatalf("DropTable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "users.marina")); err == nil {
		t.Error("o arquivo da tabela deveria ter sido apagado")
	}

	// Reabrir confere que o catálogo no disco ficou sem a tabela e a chave.
	db, err := Open(dir, testPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReadTable("users"); err == nil {
		t.Error("tabela apagada ainda existe")
	}
	catalog, _ := db.ReadTable("_catalog")
	if want := [][]Value{texts("table", "outra", "outra", "id INT,nome TEXT")}; !reflect.DeepEqual(catalog.Rows, want) {
		t.Errorf("catálogo = %v, want %v", catalog.Rows, want)
	}

	// O nome fica livre para uma tabela nova.
	if err := db.CreateTable("users", userColumns, nil); err != nil {
		t.Errorf("recriar tabela apagada: %v", err)
	}
}

func TestDropTableErrors(t *testing.T) {
	db, _ := Open(t.TempDir(), testPageSize)

	if err := db.DropTable("nada"); err == nil {
		t.Error("esperava erro ao apagar tabela inexistente")
	}
	if err := db.DropTable("_catalog"); err == nil {
		t.Error("esperava erro ao apagar o catálogo")
	}
	if err := db.ReplaceRows("_catalog", nil); err == nil {
		t.Error("esperava erro ao regravar o catálogo")
	}
}

func TestTableSchema(t *testing.T) {
	db, _ := Open(t.TempDir(), testPageSize)
	db.CreateTable("users", userColumns, nil)

	schema, err := db.TableSchema("users")
	if err != nil {
		t.Fatal(err)
	}
	if got := schema.ColumnIndex("nome"); got != 1 {
		t.Errorf("ColumnIndex(nome) = %d, want 1", got)
	}
	if got := schema.ColumnIndex("x"); got != -1 {
		t.Errorf("ColumnIndex(x) = %d, want -1", got)
	}
	if _, err := db.TableSchema("nada"); err == nil {
		t.Error("esperava erro para tabela inexistente")
	}
}

func TestReadMissingTable(t *testing.T) {
	db, _ := Open(t.TempDir(), testPageSize)

	if _, err := db.ReadTable("nada"); err == nil {
		t.Error("esperava erro ao ler tabela inexistente")
	}
}

func TestCatalogSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, testPageSize)
	columns := []Column{{Name: "id", Type: "INT", NotNull: true}, {Name: "nome", Type: "TEXT"}}
	if err := db.CreateTable("users", columns, nil); err != nil {
		t.Fatal(err)
	}
	if err := insertOne(db, "users", []Value{Text("1"), Null}); err != nil {
		t.Fatal(err)
	}

	db, err := Open(dir, testPageSize)
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
	db, _ := Open(t.TempDir(), testPageSize)
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
	db, _ := Open(t.TempDir(), testPageSize)

	if err := db.CreateTable("_catalog", userColumns, nil); err == nil {
		t.Error("esperava erro ao criar tabela com nome reservado")
	}
	if err := insertOne(db, "_catalog", texts("table", "x", "x", "id INT")); err == nil {
		t.Error("esperava erro ao inserir no catálogo")
	}
}

func TestCreateTableOrphanFile(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, testPageSize)
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
	db, _ := Open(dir, testPageSize)
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
	db, _ = Open(dir, testPageSize)
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
			db, _ := Open(dir, testPageSize)
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
			db, _ := Open(t.TempDir(), testPageSize)
			if err := db.CreateTable("t", columns, keys); err != nil {
				t.Fatal(err)
			}
			if err := insertOne(db, "t", texts("1", "x", "y")); err != nil {
				t.Fatal(err)
			}
			if err := insertOne(db, "t", []Value{Text("9"), Text("n"), Null}); err != nil {
				t.Fatal(err)
			}

			err := insertOne(db, "t", tt.row)
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
		catalog [][]Value
	}{
		{name: "tipo desconhecido", catalog: [][]Value{catalogRow("view", "v", "v", "x")}},
		{name: "coluna sem tipo", catalog: [][]Value{catalogRow("table", "users", "users", "id")}},
		{name: "coluna com sobra", catalog: [][]Value{catalogRow("table", "users", "users", "id INT NULL")}},
		{name: "chave sem tabela", catalog: [][]Value{catalogRow("primary_key", "users_pkey", "users", "id")}},
		{name: "chave com coluna inexistente", catalog: [][]Value{
			catalogRow("table", "users", "users", "id INT"),
			catalogRow("unique", "users_x_key", "users", "x"),
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			db := &DB{dir: dir, pageSize: testPageSize}
			if err := db.writeRows(catalogName, catalogSchema.Columns, tt.catalog); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir, testPageSize); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

// rawFile monta um arquivo .marina byte a byte: a página 0 com a versão e o
// page_size pedidos, seguida de rest.
func rawFile(version, pageSize uint16, rest ...[]byte) []byte {
	data := make([]byte, testPageSize)
	copy(data, "MRNA")
	binary.LittleEndian.PutUint16(data[4:], version)
	binary.LittleEndian.PutUint16(data[6:], pageSize)
	for _, r := range rest {
		data = append(data, r...)
	}
	return data
}

func TestOpenBadCatalogFile(t *testing.T) {
	badHeader := newPage(testPageSize)
	binary.LittleEndian.PutUint16(badHeader[0:], 1000) // 1000 slots não cabem
	badRecord := newPage(testPageSize)
	addRecord(badRecord, []byte{0}) // só o bitmap: falta o primeiro TEXT

	tests := []struct {
		name string
		data []byte
	}{
		{name: "vazio", data: nil},
		{name: "formato antigo, em CSV", data: []byte("table,users,users,id INT\n")},
		{name: "versão desconhecida", data: rawFile(2, testPageSize)},
		{name: "page_size diferente", data: rawFile(1, 2048)},
		{name: "página cortada", data: rawFile(1, testPageSize, make([]byte, 10))},
		{name: "cabeçalho da página inválido", data: rawFile(1, testPageSize, badHeader)},
		{name: "registro cortado", data: rawFile(1, testPageSize, badRecord)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "_catalog.marina"), tt.data, 0o644)
			if _, err := Open(dir, testPageSize); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

func TestOpenPageSize(t *testing.T) {
	for _, size := range []int{0, 512, 1000, 3000, 65536} {
		if _, err := Open(t.TempDir(), size); err == nil {
			t.Errorf("Open com page_size %d: esperava erro", size)
		}
	}

	dir := t.TempDir()
	if _, err := Open(dir, 2048); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, 4096); err == nil {
		t.Error("esperava erro ao abrir com page_size diferente do banco")
	}
	if _, err := Open(dir, 2048); err != nil {
		t.Errorf("abrir de novo com o mesmo page_size: %v", err)
	}
}

func TestManyPages(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, testPageSize)
	db.CreateTable("t", userColumns, nil)

	var rows [][]Value
	for i := range 100 {
		rows = append(rows, []Value{Text(strings.Repeat("9", i%10+1)), Text(strings.Repeat("x", i))})
	}
	// Em dois INSERTs, para o segundo continuar a última página do primeiro.
	if err := db.InsertRows("t", rows[:30]); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertRows("t", rows[30:]); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "t.marina")
	info, _ := os.Stat(path)
	if info.Size()%testPageSize != 0 || info.Size()/testPageSize < 5 {
		t.Errorf("arquivo com %d bytes: esperava várias páginas de %d", info.Size(), testPageSize)
	}

	db, _ = Open(dir, testPageSize)
	table, err := db.ReadTable("t")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(table.Rows, rows) {
		t.Errorf("linhas lidas diferentes das gravadas: %d de %d", len(table.Rows), len(rows))
	}

	// Regravar com menos linhas encolhe o arquivo.
	if err := db.ReplaceRows("t", rows[:1]); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Size() != 2*testPageSize {
		t.Errorf("arquivo com %d bytes, want %d (cabeçalho + 1 página)", info.Size(), 2*testPageSize)
	}
}

func TestRecordSizeLimit(t *testing.T) {
	// Uma coluna TEXT: 1 byte de bitmap + 2 de tamanho + o texto. O máximo é
	// a página menos o cabeçalho dela (4) e um slot (4): 1016 bytes.
	columns := []Column{{Name: "s", Type: "TEXT"}}
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "cabe exato", size: 1013},
		{name: "um byte a mais", size: 1014, wantErr: true},
		{name: "muito maior", size: 5000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, _ := Open(t.TempDir(), testPageSize)
			db.CreateTable("t", columns, nil)
			insertOne(db, "t", texts("a"))

			err := db.InsertRows("t", [][]Value{texts("b"), texts(strings.Repeat("x", tt.size))})
			if (err != nil) != tt.wantErr {
				t.Fatalf("InsertRows erro = %v, wantErr %v", err, tt.wantErr)
			}
			want := 3
			if tt.wantErr {
				want = 1
			}
			table, _ := db.ReadTable("t")
			if len(table.Rows) != want {
				t.Errorf("%d linhas, want %d", len(table.Rows), want)
			}
		})
	}
}
