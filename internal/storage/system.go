package storage

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Além do catálogo, o banco tem tabelas do sistema que não existem em disco:
// são montadas na hora da leitura, a partir do estado do banco, como as
// views pg_settings e pg_tables do PostgreSQL. Dá para consultá-las com
// SELECT e WHERE como qualquer tabela, mas não para alterá-las.
const (
	settingsName = "_settings"
	tablesName   = "_tables"
	indexesName  = "_indexes"
)

// settingsSchema tem um parâmetro do banco por linha. O valor é TEXT porque
// cada parâmetro tem o seu tipo.
var settingsSchema = Schema{Columns: []Column{
	{Name: "name", Type: "TEXT"},
	{Name: "value", Type: "TEXT"},
	{Name: "description", Type: "TEXT"},
}}

// tablesSchema tem uma linha por tabela gravada em disco, com o que ela
// ocupa.
var tablesSchema = Schema{Columns: []Column{
	{Name: "name", Type: "TEXT"},
	{Name: "rows", Type: "INT"},
	{Name: "pages", Type: "INT"},
	{Name: "bytes", Type: "INT"},
}}

// indexesSchema tem uma linha por índice, com o tamanho e a altura da
// árvore.
var indexesSchema = Schema{Columns: []Column{
	{Name: "name", Type: "TEXT"},
	{Name: "table_name", Type: "TEXT"},
	{Name: "type", Type: "TEXT"},
	{Name: "columns", Type: "TEXT"},
	{Name: "pages", Type: "INT"},
	{Name: "height", Type: "INT"},
}}

// isVirtual diz se a tabela do sistema é montada na memória, sem arquivo.
func isVirtual(name string) bool {
	return name == settingsName || name == tablesName || name == indexesName
}

// systemRows monta as linhas de uma tabela do sistema que não existe em
// disco. ok é false para as outras tabelas.
func (db *DB) systemRows(name string) (rows [][]Value, ok bool, err error) {
	switch name {
	case settingsName:
		return db.settingsRows(), true, nil
	case tablesName:
		rows, err := db.tablesRows()
		return rows, true, err
	case indexesName:
		rows, err := db.indexesRows()
		return rows, true, err
	}
	return nil, false, nil
}

func (db *DB) settingsRows() [][]Value {
	return [][]Value{
		{Text("data_dir"), Text(db.dir), Text("pasta dos arquivos das tabelas")},
		{Text("page_size"), Text(strconv.Itoa(db.pageSize)), Text("tamanho das páginas em bytes, gravado no banco quando ele é criado")},
		{Text("format_version"), Text(strconv.Itoa(fileVersion)), Text("versão do formato dos arquivos .mari")},
	}
}

// tablesRows conta, para cada tabela em disco, as linhas e as páginas de
// dados (sem a página 0). bytes é o tamanho do arquivo.
func (db *DB) tablesRows() ([][]Value, error) {
	t := db.begin()
	defer t.close()
	var rows [][]Value
	for _, name := range slices.Sorted(maps.Keys(db.tables)) {
		if isVirtual(name) {
			continue
		}
		count := 0
		err := t.scanRecords(name, func(RID, []byte) (bool, error) {
			count++
			return true, nil
		})
		if err != nil {
			return nil, err
		}
		pages, _ := t.pageCount(name)
		rows = append(rows, []Value{Text(name), itoa(count), itoa(pages - 1), itoa(pages * db.pageSize)})
	}
	return rows, nil
}

// indexesRows lista os índices de todas as tabelas, com as páginas (sem a 0)
// e a altura da B+tree.
func (db *DB) indexesRows() ([][]Value, error) {
	t := db.begin()
	defer t.close()
	var rows [][]Value
	for _, table := range slices.Sorted(maps.Keys(db.tables)) {
		for _, idx := range db.tables[table].Indexes {
			pages, err := t.pageCount(idx.Name)
			if err != nil {
				return nil, err
			}
			height, err := t.btreeHeight(idx.Name)
			if err != nil {
				return nil, err
			}
			rows = append(rows, []Value{Text(idx.Name), Text(table), Text(string(idx.Kind)),
				Text(strings.Join(idx.Columns, ",")), itoa(pages - 1), itoa(height)})
		}
		// O índice vetorial não é árvore: não tem altura.
		if v := db.tables[table].Vector; v != nil {
			pages, err := t.pageCount(v.Name)
			if err != nil {
				return nil, err
			}
			rows = append(rows, []Value{Text(v.Name), Text(table), Text(string(v.Kind)),
				Text(strings.Join(v.Columns, ",")), itoa(pages - 1), Null})
		}
	}
	return rows, nil
}

func itoa(n int) Value {
	return Text(strconv.Itoa(n))
}
