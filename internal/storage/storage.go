package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const tableExtension = ".marina"

// DB guarda cada tabela como um arquivo dentro de dir.
type DB struct {
	dir string
}

func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &DB{dir: dir}, nil
}

func (db *DB) CreateTable(name string) error {
	if name == "" {
		return errors.New("nome da tabela não pode ser vazio")
	}

	file, err := os.OpenFile(db.tablePath(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("tabela %q já existe", name)
	}
	if err != nil {
		return err
	}
	return file.Close()
}

func (db *DB) ReadTable(name string) ([]byte, error) {
	data, err := os.ReadFile(db.tablePath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("tabela %q não existe", name)
	}
	return data, err
}

func (db *DB) tablePath(name string) string {
	return filepath.Join(db.dir, name+tableExtension)
}
