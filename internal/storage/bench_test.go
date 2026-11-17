package storage

import (
	"strconv"
	"testing"
)

// Benchmarks de linha de base, para comparar quando houver índice. Rode com:
//
//	go test ./internal/storage -run '^$' -bench . -benchmem -benchtime 50x
//
// Em sistemas com /tmp na memória (tmpfs), o fsync sai de graça e os números
// ficam otimistas.

// benchTable cria uma tabela com PRIMARY KEY (id) e n linhas.
func benchTable(b *testing.B, n int) *DB {
	b.Helper()
	db, err := Open(b.TempDir(), 4096)
	if err != nil {
		b.Fatal(err)
	}
	keys := []Index{{Kind: PrimaryKey, Columns: []string{"id"}}}
	if err := db.CreateTable("t", userColumns, keys); err != nil {
		b.Fatal(err)
	}
	rows := make([][]Value, n)
	for i := range rows {
		rows[i] = texts(strconv.Itoa(i), "nome "+strconv.Itoa(i))
	}
	if err := db.InsertRows("t", rows); err != nil {
		b.Fatal(err)
	}
	return db
}

// BenchmarkInsertWithKey mede 10 INSERTs de uma linha cada numa tabela que
// começa com n linhas. A tabela é recriada a cada rodada (fora do tempo
// medido), senão ela cresceria junto com b.N. Sem índice, conferir a chave lê
// a tabela inteira e gravar reescreve o arquivo inteiro: o custo cresce com n.
func BenchmarkInsertWithKey(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				db := benchTable(b, n)
				b.StartTimer()
				for j := range 10 {
					if err := insertOne(db, "t", texts(strconv.Itoa(n+j), "novo")); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// BenchmarkReadTable mede a leitura completa, que hoje é o que todo SELECT faz.
func BenchmarkReadTable(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			db := benchTable(b, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := db.ReadTable("t"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
