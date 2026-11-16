package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	tableExtension = ".mari"
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

// DB guarda cada tabela e cada índice como um arquivo de páginas dentro de
// dir (o formato está em page.go). O que descreve os objetos do banco fica no
// catálogo (_catalog.mari), que é carregado para a memória no Open.
//
// Toda operação roda numa transação (tx.go): ou grava tudo, ou nada. Várias
// leituras podem rodar ao mesmo tempo, mas uma escrita precisa rodar
// sozinha; quem chama garante isso.
type DB struct {
	dir      string
	pageSize int
	tables   map[string]Schema
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

// IndexKind diz que tipo de índice é. O valor é o que vai na coluna type do
// catálogo.
type IndexKind string

const (
	PrimaryKey IndexKind = "primary_key" // chave primária: única e NOT NULL
	Unique     IndexKind = "unique"      // chave única
	PlainIndex IndexKind = "index"       // índice criado com CREATE INDEX
)

// Index é um índice B+tree sobre uma ou mais colunas, guardado no arquivo
// <Name>.mari. Toda chave (PRIMARY KEY ou UNIQUE) tem um índice, que é o que
// confere a unicidade. O Name das chaves é gerado pelo CreateTable:
// <tabela>_pkey ou <tabela>_<colunas>_key.
type Index struct {
	Name    string
	Kind    IndexKind
	Columns []string
}

// Unique diz se o índice impede valores repetidos.
func (i Index) Unique() bool {
	return i.Kind != PlainIndex
}

type Schema struct {
	Columns []Column
	Indexes []Index
}

// Row é uma linha lida de uma tabela, com o endereço dela.
type Row struct {
	RID    RID
	Values []Value
}

type Table struct {
	Schema
	Rows [][]Value
	RIDs []RID // RIDs[i] é o endereço de Rows[i]
}

// Open abre o banco em dir. Num banco novo, cria a pasta e o catálogo vazio.
// O pageSize vale para o banco inteiro e fica gravado nos arquivos: abrir um
// banco existente com outro valor é erro. Se um commit ficou pela metade
// (o programa caiu no meio), ele é desfeito aqui.
func Open(dir string, pageSize int) (*DB, error) {
	if err := checkPageSize(pageSize); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := recoverJournal(dir, pageSize); err != nil {
		return nil, err
	}
	db := &DB{dir: dir, pageSize: pageSize, tables: map[string]Schema{
		catalogName:  catalogSchema,
		settingsName: settingsSchema,
		tablesName:   tablesSchema,
		indexesName:  indexesSchema,
	}}

	path := db.tablePath(catalogName)
	data := make([]byte, 8)
	f, err := os.Open(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Banco novo: começa com o catálogo vazio.
		if err := createFile(path, pageSize); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		_, err := f.Read(data)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("catálogo %s: %w", path, err)
		}
		size, err := readHeader(data)
		if err != nil {
			return nil, fmt.Errorf("catálogo %s: %w", path, err)
		}
		if size != pageSize {
			return nil, fmt.Errorf("o banco foi criado com page_size = %d e não pode ser aberto com %d", size, pageSize)
		}
	}

	if err := db.loadCatalog(); err != nil {
		return nil, err
	}
	return db, nil
}

// loadCatalog lê o catálogo para a memória. Cada linha é um objeto do banco,
// e a coluna type diz qual é:
//
//	type        | name       | table_name | definition
//	table       | users      | users      | id INT,nome TEXT
//	primary_key | users_pkey | users      | id
//	index       | users_nome | users      | nome
//
// Um índice sempre vem depois da linha da tabela dele.
func (db *DB) loadCatalog() error {
	t := db.begin()
	defer t.close()
	return t.scanRecords(catalogName, func(_ RID, rec []byte) (bool, error) {
		row, err := decodeRecord(catalogSchema.Columns, rec)
		if err != nil {
			return false, fmt.Errorf("catálogo corrompido: %w", err)
		}
		kind, name, tableName, def := row[0].Text, row[1].Text, row[2].Text, row[3].Text
		switch kind {
		case kindTable:
			columns, err := parseColumns(def)
			if err != nil {
				return false, fmt.Errorf("catálogo, tabela %q: %w", name, err)
			}
			db.tables[name] = Schema{Columns: columns}
		case string(PrimaryKey), string(Unique), string(PlainIndex):
			schema, ok := db.tables[tableName]
			if !ok {
				return false, fmt.Errorf("catálogo corrompido: índice %q aponta para a tabela %q, que não existe", name, tableName)
			}
			idx := Index{Name: name, Kind: IndexKind(kind), Columns: strings.Split(def, ",")}
			for _, c := range idx.Columns {
				if columnIndex(schema.Columns, c) < 0 {
					return false, fmt.Errorf("catálogo corrompido: índice %q usa a coluna %q, que não existe", name, c)
				}
			}
			schema.Indexes = append(schema.Indexes, idx)
			db.tables[tableName] = schema
		default:
			return false, fmt.Errorf("catálogo corrompido: tipo de objeto %q desconhecido", kind)
		}
		return true, nil
	})
}

// CreateTable cria a tabela e um índice para cada chave (PRIMARY KEY ou
// UNIQUE) em keys. O Name das chaves é preenchido aqui.
func (db *DB) CreateTable(name string, columns []Column, keys []Index) error {
	if name == "" {
		return errors.New("nome da tabela não pode ser vazio")
	}
	if isSystem(name) {
		return fmt.Errorf("nome %q reservado: nomes começando com _ são do sistema", name)
	}
	if _, ok := db.tables[name]; ok {
		return fmt.Errorf("tabela %q já existe", name)
	}
	if table, ok := db.indexTable(name); ok {
		return fmt.Errorf("já existe um índice %q (da tabela %s)", name, table)
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
		if c.Type != "INT" && c.Type != "TEXT" {
			return fmt.Errorf("coluna %q: tipo %q desconhecido (use INT ou TEXT)", c.Name, c.Type)
		}
	}
	keys, err := nameKeys(name, columns, keys)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if db.nameInUse(k.Name) {
			return fmt.Errorf("o nome %q da chave já é usado por outra tabela ou índice", k.Name)
		}
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

	// Primeiro os arquivos, depois o catálogo: se algo falhar no meio, sobram
	// no máximo arquivos que o catálogo não enxerga.
	var created []string
	removeCreated := func() {
		for _, n := range created {
			os.Remove(db.tablePath(n))
		}
	}
	err = createFile(db.tablePath(name), db.pageSize)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("o arquivo %s já existe, mas não está no catálogo", db.tablePath(name))
	}
	if err != nil {
		return err
	}
	created = append(created, name)
	for _, k := range keys {
		err := db.newIndexFile(k.Name)
		if errors.Is(err, fs.ErrExist) {
			err = fmt.Errorf("o arquivo %s já existe, mas não está no catálogo", db.tablePath(k.Name))
		}
		if err != nil {
			removeCreated()
			return err
		}
		created = append(created, k.Name)
	}

	t := db.begin()
	defer t.close()
	records := [][]Value{catalogRow(kindTable, name, name, formatColumns(columns))}
	for _, k := range keys {
		records = append(records, catalogRow(string(k.Kind), k.Name, name, strings.Join(k.Columns, ",")))
	}
	if err := t.appendCatalog(records...); err != nil {
		removeCreated()
		return err
	}
	if err := t.commit(); err != nil {
		removeCreated()
		return err
	}
	db.tables[name] = Schema{Columns: columns, Indexes: keys}
	return nil
}

// catalogRow monta uma linha do catálogo.
func catalogRow(kind, name, table, def string) []Value {
	return []Value{Text(kind), Text(name), Text(table), Text(def)}
}

// appendCatalog acrescenta linhas ao catálogo.
func (t *tx) appendCatalog(rows ...[]Value) error {
	for _, row := range rows {
		rec, err := encodeRecord(catalogSchema.Columns, row)
		if err != nil {
			return err
		}
		if _, err := t.insertRecord(catalogName, rec); err != nil {
			return err
		}
	}
	return nil
}

// deleteCatalog apaga as linhas do catálogo para as quais drop diz true.
func (t *tx) deleteCatalog(drop func(row []Value) bool) error {
	var rids []RID
	err := t.scanRecords(catalogName, func(rid RID, rec []byte) (bool, error) {
		row, err := decodeRecord(catalogSchema.Columns, rec)
		if err != nil {
			return false, err
		}
		if drop(row) {
			rids = append(rids, rid)
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	for _, rid := range rids {
		if err := t.deleteRecord(catalogName, rid); err != nil {
			return err
		}
	}
	return nil
}

// nameKeys confere as chaves da tabela e devolve uma cópia com o Name de
// cada uma preenchido.
func nameKeys(table string, columns []Column, keys []Index) ([]Index, error) {
	named := make([]Index, len(keys))
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
		k.Columns = slices.Clone(k.Columns)
		named[i] = k
	}
	return named, nil
}

// TableSchema devolve as colunas e índices da tabela, sem ler os registros.
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

// writableSchema devolve o schema de uma tabela que aceita escrita.
func (db *DB) writableSchema(name string) (Schema, error) {
	if isSystem(name) {
		return Schema{}, fmt.Errorf("tabela %q é do sistema e só pode ser lida", name)
	}
	return db.TableSchema(name)
}

// InsertRows acrescenta registros à tabela: ou entram todos, ou nenhum. Os
// tipos dos valores já devem estar validados; NOT NULL e chaves são
// conferidos aqui.
func (db *DB) InsertRows(name string, rows [][]Value) error {
	schema, err := db.writableSchema(name)
	if err != nil {
		return err
	}
	if err := checkRows(schema, rows); err != nil {
		return err
	}

	t := db.begin()
	defer t.close()
	for _, row := range rows {
		rec, err := encodeRecord(schema.Columns, row)
		if err != nil {
			return err
		}
		if err := t.checkUnique(schema, row); err != nil {
			return err
		}
		rid, err := t.insertRecord(name, rec)
		if err != nil {
			return err
		}
		if err := t.addToIndexes(schema, rid, row); err != nil {
			return err
		}
	}
	return t.commit()
}

// UpdateRows troca as linhas dos endereços rids pelas de rows (rows[i] vai
// para rids[i]). Ou muda tudo, ou nada. NOT NULL e chaves são conferidos
// aqui.
func (db *DB) UpdateRows(name string, rids []RID, rows [][]Value) error {
	if len(rids) != len(rows) {
		return fmt.Errorf("UpdateRows: %d endereços para %d linhas", len(rids), len(rows))
	}
	schema, err := db.writableSchema(name)
	if err != nil {
		return err
	}
	if err := checkRows(schema, rows); err != nil {
		return err
	}

	t := db.begin()
	defer t.close()
	// Primeiro tira as linhas antigas dos índices, depois põe as novas. Assim
	// trocar valores entre linhas (a = 1 vira 2 e a = 2 vira 1) não esbarra
	// na chave de uma linha que também está mudando.
	for _, rid := range rids {
		old, err := t.row(name, schema, rid)
		if err != nil {
			return err
		}
		if err := t.removeFromIndexes(schema, rid, old); err != nil {
			return err
		}
	}
	for i, row := range rows {
		rec, err := encodeRecord(schema.Columns, row)
		if err != nil {
			return err
		}
		if err := t.checkUnique(schema, row); err != nil {
			return err
		}
		rid, err := t.updateRecord(name, rids[i], rec)
		if err != nil {
			return err
		}
		if err := t.addToIndexes(schema, rid, row); err != nil {
			return err
		}
	}
	return t.commit()
}

// DeleteRows apaga as linhas dos endereços rids.
func (db *DB) DeleteRows(name string, rids []RID) error {
	schema, err := db.writableSchema(name)
	if err != nil {
		return err
	}
	t := db.begin()
	defer t.close()
	for _, rid := range rids {
		row, err := t.row(name, schema, rid)
		if err != nil {
			return err
		}
		if err := t.removeFromIndexes(schema, rid, row); err != nil {
			return err
		}
		if err := t.deleteRecord(name, rid); err != nil {
			return err
		}
	}
	return t.commit()
}

// row lê e decodifica a linha do endereço rid.
func (t *tx) row(name string, schema Schema, rid RID) ([]Value, error) {
	rec, err := t.record(name, rid)
	if err != nil {
		return nil, err
	}
	row, err := decodeRecord(schema.Columns, rec)
	if err != nil {
		return nil, fmt.Errorf("tabela %q corrompida, linha %s: %w", name, rid, err)
	}
	return row, nil
}

// DropTable apaga a tabela: as linhas dela e dos índices dela no catálogo, e
// depois os arquivos.
func (db *DB) DropTable(name string) error {
	if isSystem(name) {
		return fmt.Errorf("tabela %q é do sistema e não pode ser apagada", name)
	}
	schema, err := db.TableSchema(name)
	if err != nil {
		return err
	}

	t := db.begin()
	defer t.close()
	if err := t.deleteCatalog(func(row []Value) bool { return row[2].Text == name }); err != nil {
		return err
	}
	if err := t.commit(); err != nil {
		return err
	}
	delete(db.tables, name)

	var errs []error
	for _, n := range append([]string{name}, indexNames(schema)...) {
		if err := os.Remove(db.tablePath(n)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("tabela removida do catálogo, mas sobraram arquivos: %w", errors.Join(errs...))
	}
	return nil
}

func indexNames(schema Schema) []string {
	var names []string
	for _, idx := range schema.Indexes {
		names = append(names, idx.Name)
	}
	return names
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

// Scan percorre as linhas da tabela, na ordem em que estão no arquivo:
//
//	for row, err := range db.Scan("users") { ... }
//
// Um erro vem como último item.
func (db *DB) Scan(name string) iter.Seq2[Row, error] {
	return func(yield func(Row, error) bool) {
		schema, err := db.TableSchema(name)
		if err != nil {
			yield(Row{}, err)
			return
		}
		if rows, ok, err := db.systemRows(name); ok || err != nil {
			if err != nil {
				yield(Row{}, err)
				return
			}
			for _, r := range rows {
				if !yield(Row{Values: r}, nil) {
					return
				}
			}
			return
		}

		t := db.begin()
		defer t.close()
		err = t.scanRecords(name, func(rid RID, rec []byte) (bool, error) {
			row, err := decodeRecord(schema.Columns, rec)
			if err != nil {
				return false, fmt.Errorf("tabela %q corrompida, linha %s: %w", name, rid, err)
			}
			return yield(Row{RID: rid, Values: row}, nil), nil
		})
		if err != nil {
			yield(Row{}, err)
		}
	}
}

// ReadTable lê a tabela inteira para a memória.
func (db *DB) ReadTable(name string) (*Table, error) {
	schema, err := db.TableSchema(name)
	if err != nil {
		return nil, err
	}
	table := &Table{Schema: schema}
	for row, err := range db.Scan(name) {
		if err != nil {
			return nil, err
		}
		table.Rows = append(table.Rows, row.Values)
		table.RIDs = append(table.RIDs, row.RID)
	}
	return table, nil
}

func (db *DB) tablePath(name string) string {
	return filepath.Join(db.dir, name+tableExtension)
}

// isSystem diz se o nome é de um objeto do sistema, como o catálogo.
func isSystem(name string) bool {
	return strings.HasPrefix(name, "_")
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
