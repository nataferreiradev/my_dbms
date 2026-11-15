package storage

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	tableExtension = ".marina"
	catalogName    = "_catalog"
	kindTable      = "table"
)

// catalogSchema é o schema do próprio catálogo. Ele não aparece como linha
// do catálogo: fica fixo no código, como o sqlite_schema do SQLite.
var catalogSchema = Schema{Columns: []Column{
	{Name: "type", Type: "TEXT"},
	{Name: "name", Type: "TEXT"},
	{Name: "table_name", Type: "TEXT"},
	{Name: "definition", Type: "TEXT"},
}}

// DB guarda cada tabela como um arquivo CSV dentro de dir, só com os
// registros. O que descreve os objetos do banco fica no catálogo
// (_catalog.marina), que é carregado para a memória no Open.
type DB struct {
	dir    string
	tables map[string]Schema
}

type Column struct {
	Name string
	Type string
}

type Schema struct {
	Columns []Column
}

type Table struct {
	Schema
	Rows [][]string
}

func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db := &DB{dir: dir, tables: map[string]Schema{catalogName: catalogSchema}}

	// Banco novo: começa com o catálogo vazio.
	file, err := os.OpenFile(db.tablePath(catalogName), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	file.Close()

	if err := db.loadCatalog(); err != nil {
		return nil, err
	}
	return db, nil
}

// loadCatalog lê o catálogo para a memória. Cada linha é um objeto do banco,
// e a coluna type diz qual é:
//
//	type,name,table_name,definition
//	table,users,users,"id INT,nome TEXT"
func (db *DB) loadCatalog() error {
	rows, err := db.readRows(catalogName, len(catalogSchema.Columns))
	if err != nil {
		return err
	}
	for _, row := range rows {
		kind, name, def := row[0], row[1], row[3]
		switch kind {
		case kindTable:
			columns, err := parseColumns(def)
			if err != nil {
				return fmt.Errorf("catálogo, tabela %q: %w", name, err)
			}
			db.tables[name] = Schema{Columns: columns}
		default:
			return fmt.Errorf("catálogo corrompido: tipo de objeto %q desconhecido", kind)
		}
	}
	return nil
}

func (db *DB) CreateTable(name string, columns []Column) error {
	if name == "" {
		return errors.New("nome da tabela não pode ser vazio")
	}
	if isSystem(name) {
		return fmt.Errorf("nome %q reservado: nomes começando com _ são do sistema", name)
	}
	if _, ok := db.tables[name]; ok {
		return fmt.Errorf("tabela %q já existe", name)
	}
	if len(columns) == 0 {
		return errors.New("tabela precisa de pelo menos uma coluna")
	}

	seen := map[string]bool{}
	for _, c := range columns {
		if seen[c.Name] {
			return fmt.Errorf("coluna %q repetida", c.Name)
		}
		seen[c.Name] = true
	}

	// Primeiro o arquivo de dados, depois o catálogo: se algo falhar no meio,
	// sobra no máximo um arquivo que o catálogo não enxerga.
	path := db.tablePath(name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("o arquivo %s já existe, mas não está no catálogo", path)
	}
	if err != nil {
		return err
	}
	file.Close()

	record := []string{kindTable, name, name, formatColumns(columns)}
	if err := db.appendRows(catalogName, [][]string{record}); err != nil {
		os.Remove(path)
		return err
	}
	db.tables[name] = Schema{Columns: columns}
	return nil
}

// InsertRow acrescenta um registro no fim da tabela. Os valores já devem
// estar validados contra o schema.
func (db *DB) InsertRow(name string, values []string) error {
	if isSystem(name) {
		return fmt.Errorf("tabela %q é do sistema e só pode ser lida", name)
	}
	schema, ok := db.tables[name]
	if !ok {
		return fmt.Errorf("tabela %q não existe", name)
	}
	if len(values) != len(schema.Columns) {
		return fmt.Errorf("tabela %q tem %d colunas, mas vieram %d valores", name, len(schema.Columns), len(values))
	}
	return db.appendRows(name, [][]string{values})
}

func (db *DB) ReadTable(name string) (*Table, error) {
	schema, ok := db.tables[name]
	if !ok {
		return nil, fmt.Errorf("tabela %q não existe", name)
	}
	rows, err := db.readRows(name, len(schema.Columns))
	if err != nil {
		return nil, err
	}
	return &Table{Schema: schema, Rows: rows}, nil
}

func (db *DB) tablePath(name string) string {
	return filepath.Join(db.dir, name+tableExtension)
}

// isSystem diz se o nome é de um objeto do sistema, como o catálogo.
func isSystem(name string) bool {
	return strings.HasPrefix(name, "_")
}

// readRows lê todos os registros de um arquivo de tabela. Registro com
// número de campos diferente de fields é erro.
func (db *DB) readRows(name string, fields int) ([][]string, error) {
	file, err := os.Open(db.tablePath(name))
	if err != nil {
		return nil, err
	}
	defer file.Close()

	r := csv.NewReader(file)
	r.FieldsPerRecord = fields
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("tabela %q corrompida: %w", name, err)
	}
	return rows, nil
}

// appendRows acrescenta registros no fim de um arquivo de tabela, com uma
// única escrita.
func (db *DB) appendRows(name string, rows [][]string) error {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	for _, row := range rows {
		// O csv escreve um único campo vazio como linha vazia, e o leitor pula
		// linhas vazias: o registro sumiria. Com aspas ele vira "" e sobrevive.
		if len(row) == 1 && row[0] == "" {
			w.Flush()
			buf.WriteString("\"\"\n")
			continue
		}
		w.Write(row)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	file, err := os.OpenFile(db.tablePath(name), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := file.Write(buf.Bytes()); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// formatColumns escreve as colunas como vão para o catálogo: "id INT,nome TEXT".
func formatColumns(columns []Column) string {
	parts := make([]string, len(columns))
	for i, c := range columns {
		parts[i] = c.Name + " " + c.Type
	}
	return strings.Join(parts, ",")
}

// parseColumns faz o caminho inverso de formatColumns.
func parseColumns(def string) ([]Column, error) {
	var columns []Column
	for _, part := range strings.Split(def, ",") {
		name, typ, ok := strings.Cut(part, " ")
		if !ok {
			return nil, fmt.Errorf("coluna mal formada %q", part)
		}
		columns = append(columns, Column{Name: name, Type: typ})
	}
	return columns, nil
}
