package storage

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// Além do catálogo, o banco tem tabelas do sistema que não existem em disco:
// são montadas na hora da leitura, a partir do estado do banco, como as
// views pg_settings e pg_tables do PostgreSQL. Dá para consultá-las com
// SELECT e WHERE como qualquer tabela, mas não para alterá-las.
const (
	settingsName = "_settings"
	tablesName   = "_tables"
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

func (db *DB) settingsRows() [][]Value {
	return [][]Value{
		{Text("data_dir"), Text(db.dir), Text("pasta dos arquivos das tabelas")},
		{Text("page_size"), Text(strconv.Itoa(db.pageSize)), Text("tamanho das páginas em bytes, gravado no banco quando ele é criado")},
		{Text("format_version"), Text(strconv.Itoa(fileVersion)), Text("versão do formato dos arquivos .marina")},
	}
}

// tablesRows conta, para cada tabela em disco, as linhas e as páginas de
// dados (sem a página 0). bytes é o tamanho do arquivo.
func (db *DB) tablesRows() ([][]Value, error) {
	var rows [][]Value
	for _, name := range slices.Sorted(maps.Keys(db.tables)) {
		if name == settingsName || name == tablesName {
			continue
		}
		pages, err := readPages(db.tablePath(name), db.pageSize)
		if err != nil {
			return nil, fmt.Errorf("tabela %q: %w", name, err)
		}
		count := 0
		for i, page := range pages {
			records, err := pageRecords(page)
			if err != nil {
				return nil, fmt.Errorf("tabela %q corrompida, página %d: %w", name, i+1, err)
			}
			count += len(records)
		}
		size := (len(pages) + 1) * db.pageSize
		rows = append(rows, []Value{Text(name), Text(strconv.Itoa(count)), Text(strconv.Itoa(len(pages))), Text(strconv.Itoa(size))})
	}
	return rows, nil
}
