package storage

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const tableExtension = ".marina"

// DB guarda cada tabela como um arquivo CSV dentro de dir. A primeira linha
// é o schema ("id INT,nome TEXT") e as outras são os registros.
type DB struct {
	dir string
}

type Column struct {
	Name string
	Type string
}

type Table struct {
	Columns []Column
	Rows    [][]string
}

func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DB{dir: dir}, nil
}

func (db *DB) CreateTable(name string, columns []Column) error {
	if name == "" {
		return errors.New("nome da tabela não pode ser vazio")
	}
	if len(columns) == 0 {
		return errors.New("tabela precisa de pelo menos uma coluna")
	}

	seen := map[string]bool{}
	header := make([]string, len(columns))
	for i, c := range columns {
		if seen[c.Name] {
			return fmt.Errorf("coluna %q repetida", c.Name)
		}
		seen[c.Name] = true
		header[i] = c.Name + " " + c.Type
	}

	file, err := os.OpenFile(db.tablePath(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("tabela %q já existe", name)
	}
	if err != nil {
		return err
	}
	return writeRecord(file, header)
}

// InsertRow acrescenta um registro no fim da tabela. Os valores já devem
// estar validados contra o schema.
func (db *DB) InsertRow(name string, values []string) error {
	file, err := os.OpenFile(db.tablePath(name), os.O_APPEND|os.O_WRONLY, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("tabela %q não existe", name)
	}
	if err != nil {
		return err
	}
	return writeRecord(file, values)
}

func (db *DB) ReadTable(name string) (*Table, error) {
	file, err := os.Open(db.tablePath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("tabela %q não existe", name)
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("tabela %q corrompida: %w", name, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("tabela %q sem schema", name)
	}

	table := Table{Rows: records[1:]}
	for _, part := range records[0] {
		colName, colType, ok := strings.Cut(part, " ")
		if !ok {
			return nil, fmt.Errorf("tabela %q: coluna mal formada %q", name, part)
		}
		table.Columns = append(table.Columns, Column{Name: colName, Type: colType})
	}
	return &table, nil
}

func (db *DB) tablePath(name string) string {
	return filepath.Join(db.dir, name+tableExtension)
}

// writeRecord grava um registro CSV e fecha o arquivo.
func writeRecord(file *os.File, record []string) error {
	w := csv.NewWriter(file)
	// O csv escreve um único campo vazio como linha vazia, e o leitor pula
	// linhas vazias: o registro sumiria. Com aspas ele vira "" e sobrevive.
	if len(record) == 1 && record[0] == "" {
		_, err := file.WriteString("\"\"\n")
		if err != nil {
			file.Close()
			return err
		}
		return file.Close()
	}
	w.Write(record)
	w.Flush()
	if err := w.Error(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
