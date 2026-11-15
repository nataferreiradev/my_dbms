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

// InsertRow acrescenta um registro no fim da tabela. Os tipos dos valores
// já devem estar validados; NOT NULL e chaves são conferidos aqui.
func (db *DB) InsertRow(name string, values []Value) error {
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
	for i, c := range schema.Columns {
		if c.NotNull && values[i].Null {
			return fmt.Errorf("a coluna %s não aceita NULL", c.Name)
		}
	}

	if len(schema.Keys) > 0 {
		rows, err := db.readRows(name, len(schema.Columns))
		if err != nil {
			return err
		}
		// Sem índice ainda: cada chave é conferida varrendo a tabela inteira.
		for _, k := range schema.Keys {
			if err := checkKey(schema.Columns, k, decodeRows(rows), values); err != nil {
				return err
			}
		}
	}
	return db.appendRows(name, [][]string{encodeRow(values)})
}

// checkKey devolve erro se alguma linha já tem, nas colunas da chave, os
// mesmos valores que values. Como no SQL, NULL não é igual a nada: uma chave
// UNIQUE aceita várias linhas com NULL.
func checkKey(columns []Column, k Key, rows [][]Value, values []Value) error {
	positions := make([]int, len(k.Columns))
	for i, c := range k.Columns {
		positions[i] = columnIndex(columns, c)
	}
	want := pick(values, positions)
	if slices.Contains(want, Null) {
		return nil
	}
	for _, row := range rows {
		if slices.Equal(pick(row, positions), want) {
			texts := make([]string, len(want))
			for i, v := range want {
				texts[i] = v.Text
			}
			return fmt.Errorf("valor duplicado viola a chave %s: (%s) = (%s) já existe",
				k.Name, strings.Join(k.Columns, ", "), strings.Join(texts, ", "))
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
	schema, ok := db.tables[name]
	if !ok {
		return nil, fmt.Errorf("tabela %q não existe", name)
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
