package storage

import (
	"bytes"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
)

func TestKeyOrder(t *testing.T) {
	// Cada lista está em ordem crescente; a codificação tem que manter.
	ints := []Value{Null, Text(strconv.Itoa(math.MinInt64)), Text("-100"), Text("-1"), Text("0"), Text("1"), Text("255"), Text("256"), Text(strconv.Itoa(math.MaxInt64))}
	texts := []Value{Null, Text(""), Text("\x00"), Text("\x00\x00"), Text("\x00\x01"), Text("a"), Text("a\x00"), Text("ab"), Text("b"), Text("é")}

	for typ, values := range map[string][]Value{"INT": ints, "TEXT": texts} {
		for i := 1; i < len(values); i++ {
			a, _ := appendKey(nil, typ, values[i-1])
			b, _ := appendKey(nil, typ, values[i])
			if bytes.Compare(a, b) >= 0 {
				t.Errorf("%s: %q deveria vir antes de %q (%x, %x)", typ, values[i-1].Text, values[i].Text, a, b)
			}
		}
	}

	// Chaves de várias colunas não misturam o fim de uma com o começo da
	// outra.
	types := []string{"TEXT", "TEXT"}
	ab, _ := encodeKey(types, []Value{Text("a"), Text("bc")})
	abc, _ := encodeKey(types, []Value{Text("ab"), Text("c")})
	if bytes.Compare(ab, abc) >= 0 || hasKey(abc, ab[:3]) {
		t.Errorf(`("a", "bc") = %x deveria vir antes de ("ab", "c") = %x`, ab, abc)
	}
}

// newTestIndex cria um índice vazio num banco de páginas pequenas, para a
// árvore crescer rápido.
func newTestIndex(t *testing.T) *DB {
	t.Helper()
	db := &DB{dir: t.TempDir(), pageSize: testPageSize}
	if err := db.newIndexFile("i"); err != nil {
		t.Fatal(err)
	}
	return db
}

// entry monta uma entrada de índice com uma chave INT.
func entry(n int, rid RID) []byte {
	key, _ := encodeKey([]string{"INT"}, []Value{Text(strconv.Itoa(n))})
	return appendRID(key, rid)
}

// allEntries percorre o índice inteiro pelas folhas.
func allEntries(t *testing.T, tx *tx) [][]byte {
	t.Helper()
	var got [][]byte
	c, err := tx.btreeSeek("i", nil)
	for ; err == nil && c.valid(); err = c.next() {
		got = append(got, c.entry())
	}
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestBtree(t *testing.T) {
	db := newTestIndex(t)
	tx := db.begin()
	defer tx.close()

	// 2000 entradas em ordem aleatória, com chaves repetidas (RIDs
	// diferentes), como num índice não único.
	var want [][]byte
	for _, i := range rand.New(rand.NewPCG(1, 2)).Perm(2000) {
		e := entry(i/2, RID{uint32(i), uint16(i % 7)})
		if err := tx.btreeInsert("i", e); err != nil {
			t.Fatal(err)
		}
		want = append(want, e)
	}
	slices.SortFunc(want, bytes.Compare)

	if got := allEntries(t, tx); !slices.EqualFunc(got, want, bytes.Equal) {
		t.Fatalf("o índice tem %d entradas fora de ordem ou faltando (want %d)", len(got), len(want))
	}
	height, err := tx.btreeHeight("i")
	if err != nil || height < 3 {
		t.Errorf("altura = %d, %v; esperava pelo menos 3 níveis com 2000 entradas em páginas de 1 KB", height, err)
	}

	// Seek por uma chave sem o RID cai na primeira entrada com ela.
	key, _ := encodeKey([]string{"INT"}, []Value{Text("500")})
	c, err := tx.btreeSeek("i", key)
	if err != nil || !c.valid() || !hasKey(c.entry(), key) || entryRID(c.entry()).Page != 1000 {
		t.Errorf("seek(500) = %x", c.entry())
	}

	// Apaga metade; o resto continua em ordem, e as apagadas somem.
	for i, e := range want {
		if i%2 == 0 {
			if err := tx.btreeDelete("i", e); err != nil {
				t.Fatal(err)
			}
		}
	}
	var kept [][]byte
	for i, e := range want {
		if i%2 == 1 {
			kept = append(kept, e)
		}
	}
	if got := allEntries(t, tx); !slices.EqualFunc(got, kept, bytes.Equal) {
		t.Errorf("depois de apagar: %d entradas, want %d", len(got), len(kept))
	}
	if err := tx.btreeDelete("i", want[0]); err == nil {
		t.Error("apagar uma entrada que não existe deveria dar erro")
	}
	if err := tx.btreeInsert("i", want[1]); err == nil {
		t.Error("inserir uma entrada repetida deveria dar erro")
	}
}

func TestBtreePersists(t *testing.T) {
	db := newTestIndex(t)
	tx := db.begin()
	for i := range 500 {
		tx.btreeInsert("i", entry(i, RID{1, uint16(i)}))
	}
	if err := tx.commit(); err != nil {
		t.Fatal(err)
	}

	tx = db.begin()
	defer tx.close()
	if got := allEntries(t, tx); len(got) != 500 {
		t.Errorf("depois do commit: %d entradas, want 500", len(got))
	}
}

func TestBtreeEntryTooBig(t *testing.T) {
	db := newTestIndex(t)
	tx := db.begin()
	defer tx.close()
	big := appendRID(bytes.Repeat([]byte{'x'}, maxEntrySize(testPageSize)), RID{})
	if err := tx.btreeInsert("i", big); err == nil {
		t.Error("esperava erro para entrada maior que o máximo")
	}
	// Entradas do tamanho máximo ainda dividem bem.
	for i := range 50 {
		e := appendRID(bytes.Repeat([]byte{byte(i)}, maxEntrySize(testPageSize)-ridSize), RID{1, uint16(i)})
		if err := tx.btreeInsert("i", e); err != nil {
			t.Fatalf("entrada %d: %v", i, err)
		}
	}
	if got := allEntries(t, tx); len(got) != 50 {
		t.Errorf("%d entradas, want 50", len(got))
	}
}
