package main

import (
	"strconv"
	"strings"
	"testing"

	"my_DBMS/internal/storage"
)

// BenchmarkSelectByKey mede um SELECT que acha uma linha pela chave primária.
// Sem índice, é uma varredura completa: o custo cresce com o tamanho da
// tabela. Com índice, deve ficar quase constante. Rode com:
//
//	go test ./cmd/my_dbms -run '^$' -bench . -benchmem
func BenchmarkSelectByKey(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			db, err := storage.Open(b.TempDir(), 4096)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := execute(db, "CREATE TABLE t (id INT PRIMARY KEY, nome TEXT);"); err != nil {
				b.Fatal(err)
			}
			values := make([]string, n)
			for i := range values {
				values[i] = "(" + strconv.Itoa(i) + ", 'nome')"
			}
			if _, err := execute(db, "INSERT INTO t VALUES "+strings.Join(values, ", ")+";"); err != nil {
				b.Fatal(err)
			}

			query := "SELECT nome FROM t WHERE id = " + strconv.Itoa(n/2) + ";"
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := execute(db, query); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
