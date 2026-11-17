package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// tx é uma transação: toda operação no banco abre uma, lê e altera páginas
// por ela e no fim chama commit (ou só close, para desistir).
//
// As páginas alteradas ficam na memória até o commit. Por isso desistir é de
// graça: se um INSERT de 10 linhas falha na sétima, nada chegou ao disco. O
// commit grava as páginas usando um rollback journal, como o SQLite:
//
//  1. copia a versão original de cada página que vai mudar para o arquivo
//     _journal, com um checksum no fim, e faz o fsync;
//  2. grava as páginas novas nos arquivos e faz o fsync deles;
//  3. apaga o _journal. Esse é o momento do commit.
//
// Se o programa cair no passo 2, o próximo Open acha o _journal e devolve as
// páginas originais (recoverJournal). Se cair no passo 1, o checksum não
// bate, os arquivos ainda não foram tocados e o _journal é só descartado.
//
// Os arquivos ficam abertos só durante a transação. Ler pode acontecer em
// várias transações ao mesmo tempo; escrever, só em uma (quem garante isso é
// quem chama, como a trava do servidor).
type tx struct {
	db    *DB
	files map[string]*txFile
}

// txFile é um arquivo .mari aberto por uma transação.
type txFile struct {
	f     *os.File
	pages int            // páginas no disco, contando a 0
	size  int            // páginas contando as criadas nesta transação
	dirty map[int][]byte // páginas alteradas, pelo número
}

const (
	journalName  = "_journal"
	journalMagic = "MRNJ"
)

func (db *DB) begin() *tx {
	return &tx{db: db, files: map[string]*txFile{}}
}

// file abre o arquivo da tabela ou do índice name, na primeira vez que a
// transação precisa dele, e confere o cabeçalho.
func (t *tx) file(name string) (*txFile, error) {
	if tf, ok := t.files[name]; ok {
		return tf, nil
	}
	f, err := os.OpenFile(t.db.tablePath(name), os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	header := make([]byte, 8)
	if _, err := f.ReadAt(header, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", name, errors.New("arquivo sem cabeçalho"))
	}
	size, err := readHeader(header)
	if err == nil && size != t.db.pageSize {
		err = fmt.Errorf("arquivo com páginas de %d bytes, mas o banco usa %d", size, t.db.pageSize)
	}
	if err == nil && info.Size()%int64(t.db.pageSize) != 0 {
		err = fmt.Errorf("tamanho de %d bytes não é múltiplo da página (%d)", info.Size(), t.db.pageSize)
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	pages := int(info.Size() / int64(t.db.pageSize))
	tf := &txFile{f: f, pages: pages, size: pages, dirty: map[int][]byte{}}
	t.files[name] = tf
	return tf, nil
}

// pageCount devolve quantas páginas o arquivo tem, contando a 0 e as criadas
// nesta transação.
func (t *tx) pageCount(name string) (int, error) {
	tf, err := t.file(name)
	if err != nil {
		return 0, err
	}
	return tf.size, nil
}

// page lê a página n (n ≥ 1). Uma página alterada nesta transação vem da
// memória. Quem alterar os bytes precisa chamar write para a mudança valer.
func (t *tx) page(name string, n int) ([]byte, error) {
	tf, err := t.file(name)
	if err != nil {
		return nil, err
	}
	if n < 1 || n >= tf.size {
		return nil, fmt.Errorf("%s: página %d não existe (o arquivo tem %d)", name, n, tf.size-1)
	}
	if p, ok := tf.dirty[n]; ok {
		return p, nil
	}
	p := make([]byte, t.db.pageSize)
	if _, err := tf.f.ReadAt(p, int64(n)*int64(t.db.pageSize)); err != nil {
		return nil, fmt.Errorf("%s: página %d: %w", name, n, err)
	}
	return p, nil
}

// write marca a página n como alterada; ela vai para o disco no commit.
func (t *tx) write(name string, n int, page []byte) error {
	tf, err := t.file(name)
	if err != nil {
		return err
	}
	tf.dirty[n] = page
	return nil
}

// allocate cria uma página vazia do tipo kind no fim do arquivo e devolve o
// número dela.
func (t *tx) allocate(name string, kind byte) (int, []byte, error) {
	tf, err := t.file(name)
	if err != nil {
		return 0, nil, err
	}
	n := tf.size
	tf.size++
	page := newPage(t.db.pageSize, kind)
	tf.dirty[n] = page
	return n, page, nil
}

// close fecha os arquivos. Sem commit antes, as alterações são descartadas.
func (t *tx) close() {
	for _, tf := range t.files {
		tf.f.Close()
	}
	t.files = nil
}

// commit grava as páginas alteradas, protegidas pelo journal, e fecha a
// transação.
func (t *tx) commit() error {
	defer t.close()
	var names []string
	for _, name := range slices.Sorted(maps.Keys(t.files)) {
		if len(t.files[name].dirty) > 0 {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}

	journal, err := t.journal(names)
	if err != nil {
		return err
	}
	journalPath := filepath.Join(t.db.dir, journalName)
	if err := writeSynced(journalPath, journal); err != nil {
		os.Remove(journalPath)
		return fmt.Errorf("journal: %w", err)
	}
	if err := syncDir(t.db.dir); err != nil {
		return err
	}

	if err := t.apply(names); err != nil {
		// Os arquivos podem ter ficado pela metade: devolve o que o journal
		// guardou.
		t.close()
		if rerr := recoverJournal(t.db.dir, t.db.pageSize); rerr != nil {
			return fmt.Errorf("%w (e a recuperação também falhou: %v; reabra o banco)", err, rerr)
		}
		return err
	}

	if err := os.Remove(journalPath); err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	return syncDir(t.db.dir)
}

// apply grava as páginas alteradas nos arquivos e faz o fsync de cada um.
func (t *tx) apply(names []string) error {
	for _, name := range names {
		tf := t.files[name]
		for _, n := range slices.Sorted(maps.Keys(tf.dirty)) {
			if _, err := tf.f.WriteAt(tf.dirty[n], int64(n)*int64(t.db.pageSize)); err != nil {
				return fmt.Errorf("%s: página %d: %w", name, n, err)
			}
		}
		if err := tf.f.Sync(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// journal monta o conteúdo do _journal:
//
//	"MRNJ" | nº de arquivos | por arquivo: nome | páginas originais |
//	nº de páginas salvas | (nº da página | conteúdo original)... | crc32
//
// Os números são uvarint, e o crc32 (little endian) cobre tudo o que vem
// antes dele. Páginas novas (depois do fim original) não precisam de cópia:
// voltar atrás é cortar o arquivo no tamanho original.
func (t *tx) journal(names []string) ([]byte, error) {
	data := []byte(journalMagic)
	data = binary.AppendUvarint(data, uint64(len(names)))
	for _, name := range names {
		tf := t.files[name]
		var old []int
		for n := range tf.dirty {
			if n < tf.pages {
				old = append(old, n)
			}
		}
		slices.Sort(old)

		data = binary.AppendUvarint(data, uint64(len(name)))
		data = append(data, name...)
		data = binary.AppendUvarint(data, uint64(tf.pages))
		data = binary.AppendUvarint(data, uint64(len(old)))
		for _, n := range old {
			p := make([]byte, t.db.pageSize)
			if _, err := tf.f.ReadAt(p, int64(n)*int64(t.db.pageSize)); err != nil {
				return nil, fmt.Errorf("%s: página %d: %w", name, n, err)
			}
			data = binary.AppendUvarint(data, uint64(n))
			data = append(data, p...)
		}
	}
	return binary.LittleEndian.AppendUint32(data, crc32.ChecksumIEEE(data)), nil
}

// journalFile é o que o journal guardou de um arquivo.
type journalFile struct {
	name  string
	pages int
	saved map[int][]byte
}

// parseJournal lê o _journal. Um erro quer dizer que ele está incompleto.
func parseJournal(data []byte, pageSize int) ([]journalFile, error) {
	if len(data) < len(journalMagic)+4 || string(data[:len(journalMagic)]) != journalMagic {
		return nil, errors.New("journal sem cabeçalho")
	}
	body, sum := data[:len(data)-4], binary.LittleEndian.Uint32(data[len(data)-4:])
	if crc32.ChecksumIEEE(body) != sum {
		return nil, errors.New("checksum do journal não bate")
	}

	pos := len(journalMagic)
	next := func() (int, error) {
		v, n := binary.Uvarint(body[pos:])
		if n <= 0 || v > uint64(len(body)) {
			return 0, errors.New("journal mal formado")
		}
		pos += n
		return int(v), nil
	}
	count, err := next()
	if err != nil {
		return nil, err
	}
	var files []journalFile
	for range count {
		size, err := next()
		if err != nil || pos+size > len(body) {
			return nil, errors.New("journal mal formado")
		}
		jf := journalFile{name: string(body[pos : pos+size]), saved: map[int][]byte{}}
		pos += size
		if jf.pages, err = next(); err != nil {
			return nil, err
		}
		saved, err := next()
		if err != nil {
			return nil, err
		}
		for range saved {
			n, err := next()
			if err != nil || pos+pageSize > len(body) {
				return nil, errors.New("journal mal formado")
			}
			jf.saved[n] = body[pos : pos+pageSize]
			pos += pageSize
		}
		files = append(files, jf)
	}
	return files, nil
}

// recoverJournal desfaz um commit que não terminou: se existe um _journal
// completo, devolve as páginas originais e corta os arquivos no tamanho
// original. Um _journal incompleto é só apagado, porque os arquivos não
// chegaram a ser tocados.
func recoverJournal(dir string, pageSize int) error {
	path := filepath.Join(dir, journalName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	files, err := parseJournal(data, pageSize)
	if err == nil {
		for _, jf := range files {
			if err := restoreFile(filepath.Join(dir, jf.name+tableExtension), pageSize, jf); err != nil {
				return fmt.Errorf("recuperando %s pelo journal: %w", jf.name, err)
			}
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(dir)
}

func restoreFile(path string, pageSize int, jf journalFile) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	for n, p := range jf.saved {
		if _, err := f.WriteAt(p, int64(n)*int64(pageSize)); err != nil {
			return err
		}
	}
	if err := f.Truncate(int64(jf.pages) * int64(pageSize)); err != nil {
		return err
	}
	return f.Sync()
}
