package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
)

// Cada índice é uma B+tree num arquivo .mari próprio, com as páginas no
// mesmo formato com slots das tabelas:
//
//   - Folha (pageLeaf): as entradas do índice (chave codificada + RID, veja
//     key.go), em ordem, uma por slot. O link aponta para a próxima folha,
//     então percorrer um intervalo é andar pelas folhas, sem voltar à raiz.
//   - Nó interno (pageInternal): registros "chave | página filha (uint32)", em
//     ordem. A filha do registro i tem as entradas menores que a chave i (e
//     maiores ou iguais à chave i-1); o link aponta para a filha mais à
//     direita, com as entradas maiores ou iguais à última chave.
//
// A raiz é sempre a página 1. Quando ela enche, o conteúdo desce para duas
// páginas novas e ela vira um nó interno com uma chave só; assim ninguém
// precisa guardar onde a raiz está.
//
// Apagar uma entrada nunca junta páginas: uma folha pode até ficar vazia. O
// PostgreSQL também não junta no DELETE comum; quem recupera esse espaço lá é
// o VACUUM.
const rootPage = 1

// maxEntrySize é a maior entrada aceita num índice. Com no máximo um quarto
// da página por entrada, dividir uma página cheia sempre dá duas metades que
// cabem, mesmo nos nós internos, que guardam a chave mais 4 bytes.
func maxEntrySize(pageSize int) int {
	return (pageSize-pageHeaderSize)/4 - slotSize - 4
}

// newIndexFile cria o arquivo de um índice vazio: só a raiz, como folha.
func (db *DB) newIndexFile(name string) error {
	return createFile(db.tablePath(name), db.pageSize, newPage(db.pageSize, pageLeaf))
}

// btreeInsert põe a entrada no índice name.
func (t *tx) btreeInsert(name string, entry []byte) error {
	if len(entry) > maxEntrySize(t.db.pageSize) {
		return fmt.Errorf("índice %s: chave de %d bytes é grande demais (máximo %d)", name, len(entry)-ridSize, maxEntrySize(t.db.pageSize)-ridSize)
	}
	_, _, err := t.insertInto(name, rootPage, entry)
	return err
}

// insertInto põe a entrada na subárvore que começa na página n. Se a página
// dividiu, devolve a chave que separa as duas metades e o número da página
// nova, que ficou com a metade direita.
func (t *tx) insertInto(name string, n int, entry []byte) (sep []byte, right int, err error) {
	page, records, err := t.node(name, n)
	if err != nil {
		return nil, 0, err
	}
	link := pageLink(page)

	if pageKind(page) == pageLeaf {
		i := sort.Search(len(records), func(i int) bool { return bytes.Compare(records[i], entry) >= 0 })
		if i < len(records) && bytes.Equal(records[i], entry) {
			return nil, 0, fmt.Errorf("índice %s corrompido: entrada repetida", name)
		}
		return t.store(name, n, pageLeaf, link, slices.Insert(records, i, entry))
	}

	i := childIndex(records, entry)
	child := int(childAt(records, link, i))
	sep, right, err = t.insertInto(name, child, entry)
	if err != nil || right == 0 {
		return nil, 0, err
	}
	// A filha dividiu: as entradas menores que sep continuam nela, e o
	// ponteiro que apontava para ela passa a apontar para a página nova.
	if i == len(records) {
		link = uint32(right)
	} else {
		records[i] = internalRecord(nodeKey(records[i]), right)
	}
	return t.store(name, n, pageInternal, link, slices.Insert(records, i, internalRecord(sep, child)))
}

// store grava os registros na página n. Se não couberem, divide a página em
// duas e devolve a chave separadora e a página nova, para o pai registrar.
func (t *tx) store(name string, n int, kind byte, link uint32, records [][]byte) ([]byte, int, error) {
	size := t.db.pageSize
	if page, ok := buildPage(size, kind, link, records); ok {
		return nil, 0, t.write(name, n, page)
	}

	mid := splitPoint(records)
	var sep []byte
	var left, right [][]byte
	var leftLink uint32 // o link da metade esquerda; o da direita é o antigo
	if kind == pageLeaf {
		// A primeira entrada da direita separa as metades; ela continua na
		// folha, porque as folhas guardam todas as entradas.
		left, right = records[:mid], records[mid:]
		sep = slices.Clone(right[0])
	} else {
		// Num nó interno, a chave do meio sobe para o pai, e a filha dela
		// vira a filha mais à direita da metade esquerda.
		left, right = records[:mid], records[mid+1:]
		sep = slices.Clone(nodeKey(records[mid]))
		leftLink = nodeChild(records[mid])
	}

	if n == rootPage {
		// A raiz fica no lugar: as duas metades vão para páginas novas.
		l, _, err := t.allocate(name, kind)
		if err != nil {
			return nil, 0, err
		}
		r, _, err := t.allocate(name, kind)
		if err != nil {
			return nil, 0, err
		}
		if kind == pageLeaf {
			leftLink = uint32(r)
		}
		lp, _ := buildPage(size, kind, leftLink, left)
		rp, _ := buildPage(size, kind, link, right)
		root, _ := buildPage(size, pageInternal, uint32(r), [][]byte{internalRecord(sep, l)})
		for p, page := range map[int][]byte{l: lp, r: rp, rootPage: root} {
			if err := t.write(name, p, page); err != nil {
				return nil, 0, err
			}
		}
		return nil, 0, nil
	}

	r, _, err := t.allocate(name, kind)
	if err != nil {
		return nil, 0, err
	}
	if kind == pageLeaf {
		leftLink = uint32(r)
	}
	lp, _ := buildPage(size, kind, leftLink, left)
	rp, _ := buildPage(size, kind, link, right)
	if err := t.write(name, n, lp); err != nil {
		return nil, 0, err
	}
	return sep, r, t.write(name, r, rp)
}

// splitPoint escolhe onde dividir os registros: no meio, contando bytes.
func splitPoint(records [][]byte) int {
	half := recordsSize(records) / 2
	used := 0
	for i, rec := range records {
		used += len(rec) + slotSize
		if used >= half {
			return min(max(i, 1), len(records)-2)
		}
	}
	return len(records) / 2
}

// btreeDelete tira a entrada do índice name.
func (t *tx) btreeDelete(name string, entry []byte) error {
	n := rootPage
	for {
		page, records, err := t.node(name, n)
		if err != nil {
			return err
		}
		if pageKind(page) == pageInternal {
			n = int(childAt(records, pageLink(page), childIndex(records, entry)))
			continue
		}
		i := sort.Search(len(records), func(i int) bool { return bytes.Compare(records[i], entry) >= 0 })
		if i == len(records) || !bytes.Equal(records[i], entry) {
			return fmt.Errorf("índice %s corrompido: falta a entrada da linha %s", name, entryRID(entry))
		}
		leaf, _ := buildPage(t.db.pageSize, pageLeaf, pageLink(page), slices.Delete(records, i, i+1))
		return t.write(name, n, leaf)
	}
}

// cursor percorre as entradas de um índice em ordem, a partir de um ponto.
type cursor struct {
	t       *tx
	name    string
	records [][]byte // entradas da folha atual
	link    uint32   // a próxima folha
	i       int
}

// btreeSeek devolve um cursor na primeira entrada maior ou igual a key.
func (t *tx) btreeSeek(name string, key []byte) (*cursor, error) {
	n := rootPage
	for {
		page, records, err := t.node(name, n)
		if err != nil {
			return nil, err
		}
		if pageKind(page) == pageInternal {
			n = int(childAt(records, pageLink(page), childIndex(records, key)))
			continue
		}
		c := &cursor{t: t, name: name, records: records, link: pageLink(page)}
		c.i = sort.Search(len(records), func(i int) bool { return bytes.Compare(records[i], key) >= 0 })
		return c, c.skipEmpty()
	}
}

func (c *cursor) valid() bool   { return c.i < len(c.records) }
func (c *cursor) entry() []byte { return c.records[c.i] }

// next avança para a próxima entrada, passando para a próxima folha quando
// a atual acaba.
func (c *cursor) next() error {
	c.i++
	return c.skipEmpty()
}

// skipEmpty segue o link das folhas até achar uma entrada ou chegar ao fim.
func (c *cursor) skipEmpty() error {
	for c.i >= len(c.records) && c.link != 0 {
		page, records, err := c.t.node(c.name, int(c.link))
		if err != nil {
			return err
		}
		if pageKind(page) != pageLeaf {
			return fmt.Errorf("índice %s corrompido: o link de uma folha aponta para a página %d, que não é folha", c.name, c.link)
		}
		c.records, c.link, c.i = records, pageLink(page), 0
	}
	return nil
}

// btreeHeight conta os níveis da árvore, da raiz até as folhas.
func (t *tx) btreeHeight(name string) (int, error) {
	n, height := rootPage, 1
	for {
		page, records, err := t.node(name, n)
		if err != nil {
			return 0, err
		}
		if pageKind(page) == pageLeaf {
			return height, nil
		}
		n = int(childAt(records, pageLink(page), 0))
		height++
	}
}

// node lê uma página da B+tree e os registros dela. Os registros são cópias,
// para poderem ser rearranjados sem mexer na página.
func (t *tx) node(name string, n int) ([]byte, [][]byte, error) {
	page, err := t.page(name, n)
	if err != nil {
		return nil, nil, err
	}
	kind := pageKind(page)
	if kind != pageLeaf && kind != pageInternal {
		return nil, nil, fmt.Errorf("índice %s corrompido: página %d é do tipo %d", name, n, kind)
	}
	records, err := pageRecords(page)
	if err != nil {
		return nil, nil, fmt.Errorf("índice %s corrompido, página %d: %w", name, n, err)
	}
	for i, rec := range records {
		if rec == nil || (kind == pageInternal && len(rec) < 5) {
			return nil, nil, fmt.Errorf("índice %s corrompido, página %d: registro %d inválido", name, n, i)
		}
		records[i] = slices.Clone(rec)
	}
	return page, records, nil
}

// internalRecord monta o registro de um nó interno: a chave e a filha.
func internalRecord(key []byte, child int) []byte {
	return binary.LittleEndian.AppendUint32(slices.Clone(key), uint32(child))
}

func nodeKey(rec []byte) []byte   { return rec[:len(rec)-4] }
func nodeChild(rec []byte) uint32 { return binary.LittleEndian.Uint32(rec[len(rec)-4:]) }

// childIndex devolve qual filha de um nó interno tem a chave key: a do
// primeiro registro com chave maior que key, ou len(records) para a filha
// mais à direita.
func childIndex(records [][]byte, key []byte) int {
	return sort.Search(len(records), func(i int) bool { return bytes.Compare(key, nodeKey(records[i])) < 0 })
}

// childAt devolve a página da filha i de um nó interno.
func childAt(records [][]byte, link uint32, i int) uint32 {
	if i == len(records) {
		return link
	}
	return nodeChild(records[i])
}
