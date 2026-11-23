package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"slices"
	"strings"
)

// indexTable devolve a tabela do índice name, se ele existir. Tabelas e
// índices dividem os nomes, como no PostgreSQL, porque cada um é um arquivo
// <nome>.mari na mesma pasta.
func (db *DB) indexTable(name string) (string, bool) {
	for table, schema := range db.tables {
		if slices.Contains(indexNames(schema), name) {
			return table, true
		}
	}
	return "", false
}

func (db *DB) nameInUse(name string) bool {
	_, isTable := db.tables[name]
	_, isIndex := db.indexTable(name)
	return isTable || isIndex
}

// indexColumns devolve as posições e os tipos das colunas do índice.
func indexColumns(schema Schema, idx Index) ([]int, []string) {
	positions := make([]int, len(idx.Columns))
	types := make([]string, len(idx.Columns))
	for i, c := range idx.Columns {
		positions[i] = schema.ColumnIndex(c)
		types[i] = schema.Columns[positions[i]].Type
	}
	return positions, types
}

// indexEntry monta a entrada da linha no índice: a chave e o RID.
func indexEntry(schema Schema, idx Index, rid RID, row []Value) ([]byte, error) {
	positions, types := indexColumns(schema, idx)
	key, err := encodeKey(types, pick(row, positions))
	if err != nil {
		return nil, fmt.Errorf("índice %s: %w", idx.Name, err)
	}
	return appendRID(key, rid), nil
}

func (t *tx) addToIndexes(schema Schema, rid RID, row []Value) error {
	for _, idx := range schema.Indexes {
		entry, err := indexEntry(schema, idx, rid, row)
		if err != nil {
			return err
		}
		if err := t.btreeInsert(idx.Name, entry); err != nil {
			return err
		}
	}
	return nil
}

func (t *tx) removeFromIndexes(schema Schema, rid RID, row []Value) error {
	for _, idx := range schema.Indexes {
		entry, err := indexEntry(schema, idx, rid, row)
		if err != nil {
			return err
		}
		if err := t.btreeDelete(idx.Name, entry); err != nil {
			return err
		}
	}
	return nil
}

// checkUnique confere, nos índices das chaves, se a linha repetiria uma
// chave que já existe. É uma busca na árvore, sem ler a tabela. Como no
// SQL, NULL não é igual a nada: uma chave com NULL nunca se repete.
func (t *tx) checkUnique(schema Schema, row []Value) error {
	for _, idx := range schema.Indexes {
		if !idx.Unique() {
			continue
		}
		positions, types := indexColumns(schema, idx)
		values := pick(row, positions)
		if slices.Contains(values, Null) {
			continue
		}
		key, err := encodeKey(types, values)
		if err != nil {
			return fmt.Errorf("índice %s: %w", idx.Name, err)
		}
		c, err := t.btreeSeek(idx.Name, key)
		if err != nil {
			return err
		}
		if c.valid() && hasKey(c.entry(), key) {
			texts := make([]string, len(values))
			for i, v := range values {
				texts[i] = v.Text
			}
			return fmt.Errorf("valor duplicado viola a chave %s: (%s) = (%s) já existe",
				idx.Name, strings.Join(idx.Columns, ", "), strings.Join(texts, ", "))
		}
	}
	return nil
}

// CreateIndex cria um índice (não único) sobre as colunas da tabela e
// preenche com as linhas que ela já tem.
func (db *DB) CreateIndex(name, table string, columns []string) error {
	if isSystem(name) || isSystem(table) {
		return errors.New("nomes começando com _ são do sistema: não dá para criar índice com eles nem neles")
	}
	if db.nameInUse(name) {
		return fmt.Errorf("já existe uma tabela ou índice chamado %q", name)
	}
	schema, err := db.TableSchema(table)
	if err != nil {
		return err
	}
	if err := checkIndexColumns(schema, table, columns); err != nil {
		return err
	}
	idx := Index{Name: name, Kind: PlainIndex, Columns: slices.Clone(columns)}

	err = db.newIndexFile(name)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("o arquivo %s já existe, mas não está no catálogo", db.tablePath(name))
	}
	if err != nil {
		return err
	}

	t := db.begin()
	defer t.close()
	err = t.scanRecords(table, func(rid RID, rec []byte) (bool, error) {
		row, err := decodeRecord(schema.Columns, rec)
		if err != nil {
			return false, fmt.Errorf("tabela %q corrompida, linha %s: %w", table, rid, err)
		}
		entry, err := indexEntry(schema, idx, rid, row)
		if err != nil {
			return false, err
		}
		return true, t.btreeInsert(name, entry)
	})
	if err == nil {
		err = t.appendCatalog(catalogRow(string(PlainIndex), name, table, strings.Join(columns, ",")))
	}
	if err == nil {
		err = t.commit()
	}
	if err != nil {
		t.close()
		os.Remove(db.tablePath(name))
		return err
	}
	schema.Indexes = append(slices.Clone(schema.Indexes), idx)
	db.tables[table] = schema
	return nil
}

// DropIndex apaga um índice criado com CREATE INDEX. Os índices das chaves só
// saem junto com a tabela.
func (db *DB) DropIndex(name string) error {
	table, ok := db.indexTable(name)
	if !ok {
		return fmt.Errorf("índice %q não existe", name)
	}
	schema := db.tables[table]
	kind := VectorIndex
	i := slices.IndexFunc(schema.Indexes, func(idx Index) bool { return idx.Name == name })
	if i >= 0 {
		kind = schema.Indexes[i].Kind
	}
	if kind != PlainIndex && kind != VectorIndex {
		return fmt.Errorf("o índice %s é da chave %s da tabela %s e só sai com a tabela", name, kind, table)
	}

	t := db.begin()
	defer t.close()
	err := t.deleteCatalog(func(row []Value) bool { return row[0].Text == string(kind) && row[1].Text == name })
	if err != nil {
		return err
	}
	if err := t.commit(); err != nil {
		return err
	}
	if kind == VectorIndex {
		schema.Vector = nil
	} else {
		schema.Indexes = slices.Delete(slices.Clone(schema.Indexes), i, i+1)
	}
	db.tables[table] = schema
	if err := os.Remove(db.tablePath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("índice removido do catálogo, mas o arquivo ficou: %w", err)
	}
	return nil
}

// IndexRange diz quais linhas buscar num índice. Eq tem os valores das
// primeiras colunas do índice, em ordem; From e To, quando não são nil,
// limitam a coluna seguinte (os dois inclusive). É um filtro grosso: pode
// devolver linhas a mais (com NULL, por exemplo), e quem chama ainda aplica a
// condição completa em cada linha.
type IndexRange struct {
	Eq       []Value
	From, To *Value
}

// IndexScan percorre, em ordem de chave, as linhas da tabela que o índice
// acha para o intervalo r.
func (db *DB) IndexScan(table, index string, r IndexRange) iter.Seq2[Row, error] {
	return func(yield func(Row, error) bool) {
		schema, err := db.TableSchema(table)
		if err != nil {
			yield(Row{}, err)
			return
		}
		i := slices.IndexFunc(schema.Indexes, func(idx Index) bool { return idx.Name == index })
		if i < 0 {
			yield(Row{}, fmt.Errorf("a tabela %s não tem o índice %q", table, index))
			return
		}
		idx := schema.Indexes[i]
		_, types := indexColumns(schema, idx)
		n := len(r.Eq)
		if n > len(types) || (n == len(types) && (r.From != nil || r.To != nil)) {
			yield(Row{}, fmt.Errorf("intervalo com colunas demais para o índice %s", index))
			return
		}

		prefix, err := encodeKey(types, r.Eq)
		start, end := prefix, []byte(nil)
		if err == nil && r.From != nil {
			start, err = appendKey(slices.Clone(prefix), types[n], *r.From)
		}
		if err == nil && r.To != nil {
			end, err = appendKey(slices.Clone(prefix), types[n], *r.To)
		}
		if err != nil {
			yield(Row{}, fmt.Errorf("índice %s: %w", index, err))
			return
		}

		t := db.begin()
		defer t.close()
		c, err := t.btreeSeek(index, start)
		for ; err == nil && c.valid(); err = c.next() {
			e := c.entry()
			if !hasKey(e, prefix) || (end != nil && bytes.Compare(e, end) > 0 && !hasKey(e, end)) {
				return
			}
			rid := entryRID(e)
			row, err := t.row(table, schema, rid)
			if err != nil {
				yield(Row{}, fmt.Errorf("índice %s aponta para %s: %w", index, rid, err))
				return
			}
			if !yield(Row{RID: rid, Values: row}, nil) {
				return
			}
		}
		if err != nil {
			yield(Row{}, err)
		}
	}
}
