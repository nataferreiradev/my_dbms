package storage

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	Name    string
	Type    string
	NotNull bool
}

// Value é o conteúdo de uma célula: um texto ou NULL. Números também são
// guardados como texto, já normalizados.
type Value struct {
	Text string
	Null bool
}

// Null é o valor NULL do SQL.
var Null = Value{Null: true}

// Text cria um valor que não é NULL.
func Text(s string) Value {
	return Value{Text: s}
}

// KeyKind diz que tipo de chave é. O valor é o que vai na coluna type do
// catálogo.
type KeyKind string

const (
	PrimaryKey KeyKind = "primary_key"
	Unique     KeyKind = "unique"
)

// Key é uma restrição de unicidade sobre uma ou mais colunas. O Name é
// gerado pelo CreateTable: <tabela>_pkey ou <tabela>_<colunas>_key.
type Key struct {
	Name    string
	Kind    KeyKind
	Columns []string
}

type Schema struct {
	Columns []Column
	Keys    []Key
}

type Table struct {
	Schema
	Rows [][]Value
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
//	primary_key,users_pkey,users,id
//
// Uma chave sempre vem depois da linha da tabela dela.
func (db *DB) loadCatalog() error {
	rows, err := db.readRows(catalogName, len(catalogSchema.Columns))
	if err != nil {
		return err
	}
	for _, row := range rows {
		kind, name, tableName, def := row[0], row[1], row[2], row[3]
		switch kind {
		case kindTable:
			columns, err := parseColumns(def)
			if err != nil {
				return fmt.Errorf("catálogo, tabela %q: %w", name, err)
			}
			db.tables[name] = Schema{Columns: columns}
		case string(PrimaryKey), string(Unique):
			schema, ok := db.tables[tableName]
			if !ok {
				return fmt.Errorf("catálogo corrompido: chave %q aponta para a tabela %q, que não existe", name, tableName)
			}
			key := Key{Name: name, Kind: KeyKind(kind), Columns: strings.Split(def, ",")}
			for _, c := range key.Columns {
				if columnIndex(schema.Columns, c) < 0 {
					return fmt.Errorf("catálogo corrompido: chave %q usa a coluna %q, que não existe", name, c)
				}
			}
			schema.Keys = append(schema.Keys, key)
			db.tables[tableName] = schema
		default:
			return fmt.Errorf("catálogo corrompido: tipo de objeto %q desconhecido", kind)
		}
	}
	return nil
}

func (db *DB) CreateTable(name string, columns []Column, keys []Key) error {
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
	keys, err := nameKeys(name, columns, keys)
	if err != nil {
		return err
	}
	// Coluna de chave primária nunca é NULL, como no SQL padrão.
	columns = slices.Clone(columns)
	for _, k := range keys {
		if k.Kind == PrimaryKey {
			for _, c := range k.Columns {
				columns[columnIndex(columns, c)].NotNull = true
			}
		}
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

	records := [][]string{{kindTable, name, name, formatColumns(columns)}}
	for _, k := range keys {
		records = append(records, []string{string(k.Kind), k.Name, name, strings.Join(k.Columns, ",")})
	}
	if err := db.appendRows(catalogName, records); err != nil {
		os.Remove(path)
		return err
	}
	db.tables[name] = Schema{Columns: columns, Keys: keys}
	return nil
}

// nameKeys confere as chaves da tabela e devolve uma cópia com o Name de
// cada uma preenchido.
func nameKeys(table string, columns []Column, keys []Key) ([]Key, error) {
	named := make([]Key, len(keys))
	seen := map[string]bool{}
	for i, k := range keys {
		if len(k.Columns) == 0 {
			return nil, errors.New("chave sem colunas")
		}
		inKey := map[string]bool{}
		for _, c := range k.Columns {
			if columnIndex(columns, c) < 0 {
				return nil, fmt.Errorf("a chave usa a coluna %q, que não existe", c)
			}
			if inKey[c] {
				return nil, fmt.Errorf("coluna %q repetida na chave", c)
			}
			inKey[c] = true
		}

		switch k.Kind {
		case PrimaryKey:
			k.Name = table + "_pkey"
		case Unique:
			k.Name = table + "_" + strings.Join(k.Columns, "_") + "_key"
		default:
			return nil, fmt.Errorf("tipo de chave desconhecido %q", k.Kind)
		}
		switch {
		case seen[k.Name] && k.Kind == PrimaryKey:
			return nil, errors.New("tabela só pode ter uma chave primária")
		case seen[k.Name]:
			return nil, fmt.Errorf("chave UNIQUE (%s) repetida", strings.Join(k.Columns, ", "))
		}
		seen[k.Name] = true
		named[i] = k
	}
	return named, nil
}

// TableSchema devolve as colunas e chaves da tabela, sem ler os registros.
func (db *DB) TableSchema(name string) (Schema, error) {
	schema, ok := db.tables[name]
	if !ok {
		return Schema{}, fmt.Errorf("tabela %q não existe", name)
	}
	return schema, nil
}

// ColumnIndex devolve a posição da coluna, ou -1 se ela não existe.
func (s Schema) ColumnIndex(name string) int {
	return columnIndex(s.Columns, name)
}

// InsertRows acrescenta registros no fim da tabela: ou entram todos, ou
// nenhum. Os tipos dos valores já devem estar validados; NOT NULL e chaves
// são conferidos aqui.
func (db *DB) InsertRows(name string, rows [][]Value) error {
	if isSystem(name) {
		return fmt.Errorf("tabela %q é do sistema e só pode ser lida", name)
	}
	schema, err := db.TableSchema(name)
	if err != nil {
		return err
	}
	if err := checkRows(schema, rows); err != nil {
		return err
	}

	if len(schema.Keys) > 0 {
		existing, err := db.readRows(name, len(schema.Columns))
		if err != nil {
			return err
		}
		if err := checkKeys(schema, append(decodeRows(existing), rows...)); err != nil {
			return err
		}
	}

	encoded := make([][]string, len(rows))
	for i, row := range rows {
		encoded[i] = encodeRow(row)
	}
	return db.appendRows(name, encoded)
}

// ReplaceRows troca todos os registros da tabela por rows. É o que DELETE e
// UPDATE usam: leem a tabela, mudam na memória e gravam de volta. NOT NULL e
// chaves são conferidos antes de gravar.
func (db *DB) ReplaceRows(name string, rows [][]Value) error {
	if isSystem(name) {
		return fmt.Errorf("tabela %q é do sistema e só pode ser lida", name)
	}
	schema, err := db.TableSchema(name)
	if err != nil {
		return err
	}
	if err := checkRows(schema, rows); err != nil {
		return err
	}
	if err := checkKeys(schema, rows); err != nil {
		return err
	}

	encoded := make([][]string, len(rows))
	for i, row := range rows {
		encoded[i] = encodeRow(row)
	}
	return db.writeRows(name, encoded)
}

// DropTable apaga a tabela: as linhas dela e das chaves dela no catálogo, e
// depois o arquivo de dados.
func (db *DB) DropTable(name string) error {
	if isSystem(name) {
		return fmt.Errorf("tabela %q é do sistema e não pode ser apagada", name)
	}
	if _, err := db.TableSchema(name); err != nil {
		return err
	}

	catalog, err := db.readRows(catalogName, len(catalogSchema.Columns))
	if err != nil {
		return err
	}
	kept := slices.DeleteFunc(catalog, func(row []string) bool { return row[2] == name })
	if err := db.writeRows(catalogName, kept); err != nil {
		return err
	}
	delete(db.tables, name)

	if err := os.Remove(db.tablePath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("tabela removida do catálogo, mas o arquivo ficou: %w", err)
	}
	return nil
}

// checkRows confere o número de valores e o NOT NULL de cada linha.
func checkRows(schema Schema, rows [][]Value) error {
	for _, row := range rows {
		if len(row) != len(schema.Columns) {
			return fmt.Errorf("a tabela tem %d colunas, mas vieram %d valores", len(schema.Columns), len(row))
		}
		for i, c := range schema.Columns {
			if c.NotNull && row[i].Null {
				return fmt.Errorf("a coluna %s não aceita NULL", c.Name)
			}
		}
	}
	return nil
}

// checkKeys confere que nenhuma chave se repete entre as linhas. Sem índice
// ainda, quem chama passa a tabela inteira. Como no SQL, NULL não é igual a
// nada: uma chave UNIQUE aceita várias linhas com NULL.
func checkKeys(schema Schema, rows [][]Value) error {
	for _, k := range schema.Keys {
		positions := make([]int, len(k.Columns))
		for i, c := range k.Columns {
			positions[i] = schema.ColumnIndex(c)
		}

		seen := map[string]bool{}
		for _, row := range rows {
			values := pick(row, positions)
			if slices.Contains(values, Null) {
				continue
			}
			texts := make([]string, len(values))
			for i, v := range values {
				texts[i] = v.Text
			}
			// %q deixa ("a,b", "c") diferente de ("a", "b,c").
			id := fmt.Sprintf("%q", texts)
			if seen[id] {
				return fmt.Errorf("valor duplicado viola a chave %s: (%s) = (%s) já existe",
					k.Name, strings.Join(k.Columns, ", "), strings.Join(texts, ", "))
			}
			seen[id] = true
		}
	}
	return nil
}

// pick devolve os valores de row nas posições pedidas.
func pick(row []Value, positions []int) []Value {
	values := make([]Value, len(positions))
	for i, p := range positions {
		values[i] = row[p]
	}
	return values
}

// columnIndex devolve a posição da coluna, ou -1 se ela não existe.
func columnIndex(columns []Column, name string) int {
	return slices.IndexFunc(columns, func(c Column) bool { return c.Name == name })
}

func (db *DB) ReadTable(name string) (*Table, error) {
	schema, err := db.TableSchema(name)
	if err != nil {
		return nil, err
	}
	rows, err := db.readRows(name, len(schema.Columns))
	if err != nil {
		return nil, err
	}
	return &Table{Schema: schema, Rows: decodeRows(rows)}, nil
}

// No disco, NULL é gravado como \N. Um texto que começa com \ ganha mais uma
// \ na frente, para o texto "\N" digitado pelo usuário não virar NULL.
func encodeRow(row []Value) []string {
	fields := make([]string, len(row))
	for i, v := range row {
		switch {
		case v.Null:
			fields[i] = `\N`
		case strings.HasPrefix(v.Text, `\`):
			fields[i] = `\` + v.Text
		default:
			fields[i] = v.Text
		}
	}
	return fields
}

// decodeRows faz o caminho inverso de encodeRow para várias linhas.
func decodeRows(rows [][]string) [][]Value {
	values := make([][]Value, len(rows))
	for r, row := range rows {
		values[r] = make([]Value, len(row))
		for i, field := range row {
			if field == `\N` {
				values[r][i] = Null
			} else {
				values[r][i] = Text(strings.TrimPrefix(field, `\`))
			}
		}
	}
	return values
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
	data, err := encodeCSV(rows)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(db.tablePath(name), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// writeRows troca o conteúdo de um arquivo de tabela. Grava num arquivo
// temporário e renomeia por cima do original: se o programa cair no meio,
// o original continua inteiro.
func (db *DB) writeRows(name string, rows [][]string) error {
	data, err := encodeCSV(rows)
	if err != nil {
		return err
	}
	path := db.tablePath(name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// encodeCSV escreve os registros em CSV.
func encodeCSV(rows [][]string) ([]byte, error) {
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
	return buf.Bytes(), w.Error()
}

// formatColumns escreve as colunas como vão para o catálogo:
// "id INT NOT NULL,nome TEXT".
func formatColumns(columns []Column) string {
	parts := make([]string, len(columns))
	for i, c := range columns {
		parts[i] = c.Name + " " + c.Type
		if c.NotNull {
			parts[i] += " NOT NULL"
		}
	}
	return strings.Join(parts, ",")
}

// parseColumns faz o caminho inverso de formatColumns.
func parseColumns(def string) ([]Column, error) {
	var columns []Column
	for _, part := range strings.Split(def, ",") {
		f := strings.Fields(part)
		switch {
		case len(f) == 2:
			columns = append(columns, Column{Name: f[0], Type: f[1]})
		case len(f) == 4 && f[2] == "NOT" && f[3] == "NULL":
			columns = append(columns, Column{Name: f[0], Type: f[1], NotNull: true})
		default:
			return nil, fmt.Errorf("coluna mal formada %q", part)
		}
	}
	return columns, nil
}
