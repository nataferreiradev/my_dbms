package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const tableExtension = ".marina"

// DB guarda cada tabela como um arquivo dentro de dir.
type DB struct {
	dir string
}

type Column struct {
	Name string
	Type string
}

type Table struct {
	Columns []Column
}

func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DB{dir: dir}, nil
}

// CreateTable cria o arquivo da tabela com o schema na primeira linha,
// no formato "id INT,nome TEXT".
func (db *DB) CreateTable(name string, columns []Column) error {
	if name == "" {
		return errors.New("nome da tabela não pode ser vazio")
	}
	if len(columns) == 0 {
		return errors.New("tabela precisa de pelo menos uma coluna")
	}

	seen := map[string]bool{}
	parts := make([]string, len(columns))
	for i, c := range columns {
		if seen[c.Name] {
			return fmt.Errorf("coluna %q repetida", c.Name)
		}
		seen[c.Name] = true
		parts[i] = c.Name + " " + c.Type
	}

	file, err := os.OpenFile(db.tablePath(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("tabela %q já existe", name)
	}
	if err != nil {
		return err
	}
	if _, err := file.WriteString(strings.Join(parts, ",") + "\n"); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (db *DB) ReadTable(name string) (*Table, error) {
	data, err := os.ReadFile(db.tablePath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("tabela %q não existe", name)
	}
	if err != nil {
		return nil, err
	}

	header, _, _ := strings.Cut(string(data), "\n")
	if header == "" {
		return nil, fmt.Errorf("tabela %q sem schema", name)
	}

	var table Table
	for _, part := range strings.Split(header, ",") {
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
