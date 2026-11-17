package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// newTestFile cria um arquivo com n páginas de tabela, cada uma cheia de um
// byte diferente, e devolve o banco para mexer nele.
func newTestFile(t *testing.T, n int) *DB {
	t.Helper()
	db := &DB{dir: t.TempDir(), pageSize: testPageSize}
	var pages [][]byte
	for i := range n {
		pages = append(pages, filled(byte('a'+i)))
	}
	if err := createFile(db.tablePath("t"), testPageSize, pages...); err != nil {
		t.Fatal(err)
	}
	return db
}

func filled(b byte) []byte {
	return bytes.Repeat([]byte{b}, testPageSize)
}

func readFilePage(t *testing.T, db *DB, n int) []byte {
	t.Helper()
	data, err := os.ReadFile(db.tablePath("t"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < (n+1)*testPageSize {
		return nil
	}
	return data[n*testPageSize : (n+1)*testPageSize]
}

func TestTxCommit(t *testing.T) {
	db := newTestFile(t, 2)
	tx := db.begin()
	tx.write("t", 1, filled('x'))
	if _, page, err := tx.allocate("t", pageHeap); err != nil {
		t.Fatal(err)
	} else {
		copy(page[pageHeaderSize:], "nova")
	}
	// Antes do commit, a transação já enxerga as mudanças, e o disco não.
	if p, _ := tx.page("t", 1); p[0] != 'x' {
		t.Error("a transação não enxerga a própria mudança")
	}
	if p := readFilePage(t, db, 1); p[0] != 'a' {
		t.Error("a mudança chegou ao disco antes do commit")
	}
	if err := tx.commit(); err != nil {
		t.Fatal(err)
	}

	if p := readFilePage(t, db, 1); p[0] != 'x' {
		t.Error("commit não gravou a página alterada")
	}
	if p := readFilePage(t, db, 3); p == nil || string(p[pageHeaderSize:pageHeaderSize+4]) != "nova" {
		t.Error("commit não gravou a página nova")
	}
	if _, err := os.Stat(filepath.Join(db.dir, journalName)); err == nil {
		t.Error("o journal ficou depois do commit")
	}
}

func TestTxRollback(t *testing.T) {
	db := newTestFile(t, 2)
	tx := db.begin()
	tx.write("t", 1, filled('x'))
	tx.allocate("t", pageHeap)
	tx.close()

	if p := readFilePage(t, db, 1); p[0] != 'a' {
		t.Error("close sem commit gravou a página")
	}
	if p := readFilePage(t, db, 3); p != nil {
		t.Error("close sem commit aumentou o arquivo")
	}
}

// TestRecoverJournal simula uma queda no meio do commit: o journal já foi
// gravado e só parte das páginas novas chegou ao arquivo.
func TestRecoverJournal(t *testing.T) {
	db := newTestFile(t, 2)
	tx := db.begin()
	tx.write("t", 1, filled('x'))
	tx.write("t", 2, filled('y'))
	tx.allocate("t", pageHeap)
	journal, err := tx.journal([]string{"t"})
	if err != nil {
		t.Fatal(err)
	}
	tx.close()

	os.WriteFile(filepath.Join(db.dir, journalName), journal, 0o644)
	f, _ := os.OpenFile(db.tablePath("t"), os.O_RDWR, 0)
	f.WriteAt(filled('x'), 1*testPageSize) // a página 1 chegou, a 2 não
	f.WriteAt(filled('z'), 3*testPageSize) // e a página nova também
	f.Close()

	if err := recoverJournal(db.dir, testPageSize); err != nil {
		t.Fatal(err)
	}
	if p := readFilePage(t, db, 1); !bytes.Equal(p, filled('a')) {
		t.Error("a página 1 não voltou ao original")
	}
	if p := readFilePage(t, db, 2); !bytes.Equal(p, filled('b')) {
		t.Error("a página 2 mudou")
	}
	if p := readFilePage(t, db, 3); p != nil {
		t.Error("o arquivo não voltou ao tamanho original")
	}
	if _, err := os.Stat(filepath.Join(db.dir, journalName)); err == nil {
		t.Error("o journal ficou depois da recuperação")
	}
}

// TestRecoverIncompleteJournal simula uma queda enquanto o journal era
// gravado: o checksum não bate, e os arquivos (que não foram tocados) ficam
// como estão.
func TestRecoverIncompleteJournal(t *testing.T) {
	db := newTestFile(t, 2)
	tx := db.begin()
	tx.write("t", 1, filled('x'))
	journal, _ := tx.journal([]string{"t"})
	tx.close()

	// Corta o journal, e "estraga" o arquivo de propósito para ver que a
	// recuperação não mexe nele.
	os.WriteFile(filepath.Join(db.dir, journalName), journal[:len(journal)-10], 0o644)
	f, _ := os.OpenFile(db.tablePath("t"), os.O_RDWR, 0)
	f.WriteAt(filled('q'), 2*testPageSize)
	f.Close()

	if err := recoverJournal(db.dir, testPageSize); err != nil {
		t.Fatal(err)
	}
	if p := readFilePage(t, db, 2); !bytes.Equal(p, filled('q')) {
		t.Error("journal incompleto foi aplicado")
	}
	if _, err := os.Stat(filepath.Join(db.dir, journalName)); err == nil {
		t.Error("o journal incompleto ficou")
	}
}

// TestOpenRecovers confere que o Open desfaz um commit interrompido antes de
// ler o catálogo.
func TestOpenRecovers(t *testing.T) {
	dir := t.TempDir()
	db, _ := Open(dir, testPageSize)
	db.CreateTable("t", userColumns, nil)
	insertOne(db, "t", texts("1", "Ana"))

	// Um INSERT que "caiu" depois de gravar o journal e a página nova.
	tx := db.begin()
	rec, _ := encodeRecord(userColumns, texts("2", "Bia"))
	if _, err := tx.insertRecord("t", rec); err != nil {
		t.Fatal(err)
	}
	journal, _ := tx.journal([]string{"t"})
	tx.apply([]string{"t"})
	tx.close()
	os.WriteFile(filepath.Join(dir, journalName), journal, 0o644)

	db, err := Open(dir, testPageSize)
	if err != nil {
		t.Fatal(err)
	}
	table, _ := db.ReadTable("t")
	if len(table.Rows) != 1 {
		t.Errorf("linhas depois da recuperação = %v, want só a primeira", table.Rows)
	}
}
