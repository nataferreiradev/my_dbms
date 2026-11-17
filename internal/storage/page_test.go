package storage

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRecordRoundTrip(t *testing.T) {
	nine := make([]Column, 9)
	for i := range nine {
		nine[i] = Column{Name: string(rune('a' + i)), Type: "TEXT"}
	}

	tests := []struct {
		name    string
		columns []Column
		row     []Value
	}{
		{name: "INT e TEXT", columns: userColumns, row: texts("1", "Ana")},
		{name: "NULL", columns: userColumns, row: []Value{Null, Null}},
		{name: "texto vazio", columns: userColumns, row: texts("0", "")},
		{name: "INT negativo", columns: userColumns, row: texts("-42", "x")},
		{name: "caracteres especiais", columns: userColumns, row: texts("1", "a,b \"c\"\n\\N ção")},
		{name: "texto longo", columns: userColumns, row: texts("1", strings.Repeat("x", 300))},
		{name: "bitmap de 2 bytes", columns: nine, row: append(texts("1", "2", "3", "4", "5", "6", "7", "8"), Null)},
		{name: "REAL e BOOLEAN", columns: realBoolColumns, row: texts("1.5", "TRUE")},
		{name: "REAL negativo e FALSE", columns: realBoolColumns, row: texts("-0.25", "FALSE")},
		{name: "REAL inteiro", columns: realBoolColumns, row: texts("3", "TRUE")},
		{name: "REAL grande", columns: realBoolColumns, row: texts("123456789.125", "FALSE")},
		{name: "REAL e BOOLEAN NULL", columns: realBoolColumns, row: []Value{Null, Null}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := encodeRecord(tt.columns, tt.row)
			if err != nil {
				t.Fatalf("encodeRecord: %v", err)
			}
			got, err := decodeRecord(tt.columns, rec)
			if err != nil {
				t.Fatalf("decodeRecord: %v", err)
			}
			if !reflect.DeepEqual(got, tt.row) {
				t.Errorf("decodeRecord = %v, want %v", got, tt.row)
			}
		})
	}
}

// O exemplo do docs/estrutura.md: (1, 'Ana', NULL).
func TestEncodeRecordBytes(t *testing.T) {
	columns := []Column{{Name: "id", Type: "INT"}, {Name: "nome", Type: "TEXT"}, {Name: "email", Type: "TEXT"}}
	rec, err := encodeRecord(columns, []Value{Text("1"), Text("Ana"), Null})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x04,                   // bitmap: a coluna 2 é NULL
		1, 0, 0, 0, 0, 0, 0, 0, // id = 1
		3, 'A', 'n', 'a', // nome = "Ana"
	}
	if !bytes.Equal(rec, want) {
		t.Errorf("encodeRecord = % x, want % x", rec, want)
	}
}

func TestEncodeRecordErrors(t *testing.T) {
	tests := []struct {
		name    string
		columns []Column
		row     []Value
	}{
		{name: "INT com texto", columns: userColumns, row: texts("abc", "x")},
		{name: "INT fora do int64", columns: userColumns, row: texts("99999999999999999999", "x")},
		{name: "tipo desconhecido", columns: []Column{{Name: "f", Type: "FLOAT"}}, row: texts("1.5")},
		{name: "REAL com texto", columns: realBoolColumns, row: texts("abc", "TRUE")},
		{name: "BOOLEAN com texto", columns: realBoolColumns, row: texts("1", "SIM")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := encodeRecord(tt.columns, tt.row); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

func TestDecodeRecordErrors(t *testing.T) {
	tests := []struct {
		name string
		rec  []byte
	}{
		{name: "vazio", rec: nil},
		{name: "INT cortado", rec: []byte{0, 1, 0, 0}},
		{name: "TEXT cortado", rec: []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 5, 'a'}},
		{name: "TEXT sem tamanho", rec: []byte{0, 1, 0, 0, 0, 0, 0, 0, 0}},
		{name: "bytes sobrando", rec: []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 'a', 'b'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeRecord(userColumns, tt.rec); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

var realBoolColumns = []Column{{Name: "preco", Type: "REAL"}, {Name: "ativo", Type: "BOOLEAN"}}

func TestDecodeRealBoolErrors(t *testing.T) {
	// record monta um registro com o float64 f e o byte b do BOOLEAN.
	record := func(f float64, b byte) []byte {
		return append(binary.LittleEndian.AppendUint64([]byte{0}, math.Float64bits(f)), b)
	}
	tests := []struct {
		name string
		rec  []byte
	}{
		{name: "REAL cortado", rec: []byte{0, 1, 2, 3}},
		{name: "BOOLEAN faltando", rec: record(1.5, 0)[:9]},
		{name: "BOOLEAN com 2", rec: record(1.5, 2)},
		{name: "REAL NaN", rec: record(math.NaN(), 1)},
		{name: "REAL infinito", rec: record(math.Inf(-1), 1)},
		{name: "REAL -0", rec: record(math.Copysign(0, -1), 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeRecord(realBoolColumns, tt.rec); err == nil {
				t.Error("esperava erro")
			}
		})
	}
}

func TestFormatReal(t *testing.T) {
	tests := map[float64]string{
		1.5:                  "1.5",
		1500:                 "1500",
		0.1:                  "0.1",
		-2.25:                "-2.25",
		math.Copysign(0, -1): "0",
		1e21:                 "1000000000000000000000",
		0.000001:             "0.000001",
		123456789.123456789:  "123456789.12345679",
	}
	for f, want := range tests {
		if got := FormatReal(f); got != want {
			t.Errorf("FormatReal(%v) = %q, want %q", f, got, want)
		}
	}
}

func TestPageFill(t *testing.T) {
	page := newPage(testPageSize, pageHeap)
	var added [][]byte
	for i := 0; ; i++ {
		rec := bytes.Repeat([]byte{byte(i)}, 10)
		if _, ok := addRecord(page, rec); !ok {
			break
		}
		added = append(added, rec)
	}

	// Cada registro gasta 10 bytes + 4 do slot: (1024 - 10) / 14 = 72.
	if len(added) != 72 {
		t.Errorf("couberam %d registros, want 72", len(added))
	}
	got, err := pageRecords(page)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, added) {
		t.Errorf("pageRecords devolveu %d registros diferentes dos gravados", len(got))
	}
}

// TestPageSlotsAreStable confere o que dá estabilidade ao RID: apagar,
// encolher, crescer e compactar nunca mudam o número do slot de uma linha.
func TestPageSlotsAreStable(t *testing.T) {
	page := newPage(testPageSize, pageHeap)
	want := map[int][]byte{}
	for i := range 40 {
		rec := bytes.Repeat([]byte{byte('a' + i%26)}, 20)
		slot, ok := addRecord(page, rec)
		if !ok || slot != i {
			t.Fatalf("addRecord = %d, %v; want %d, true", slot, ok, i)
		}
		want[i] = rec
	}

	// Apaga os pares: viram slots mortos (nil) e buracos.
	for i := 0; i < 40; i += 2 {
		deleteRecord(page, i)
		delete(want, i)
	}
	// Encolhe o 1 e cresce o 3 além do espaço contínuo: só cabe compactando.
	want[1] = []byte("curto")
	if !updateRecord(page, 1, want[1]) {
		t.Fatal("updateRecord para menor falhou")
	}
	want[3] = bytes.Repeat([]byte{'z'}, 300)
	if !updateRecord(page, 3, want[3]) {
		t.Fatal("updateRecord para maior falhou, mesmo com espaço nos buracos")
	}
	// Um registro novo reaproveita o primeiro slot morto.
	if slot, ok := addRecord(page, []byte("novo")); !ok || slot != 0 {
		t.Fatalf("addRecord = %d, %v; want 0 (slot morto), true", slot, ok)
	}
	want[0] = []byte("novo")

	records, err := pageRecords(page)
	if err != nil {
		t.Fatal(err)
	}
	for i, rec := range records {
		if !bytes.Equal(rec, want[i]) {
			t.Errorf("slot %d = %q, want %q", i, rec, want[i])
		}
	}

	// Maior que todo o espaço livre: não cabe e a página não muda.
	before := bytes.Clone(page)
	if updateRecord(page, 5, make([]byte, testPageSize)) {
		t.Error("updateRecord aceitou um registro maior que a página")
	}
	if !bytes.Equal(page, before) {
		t.Error("updateRecord que falhou mexeu na página")
	}
}

func TestBuildPage(t *testing.T) {
	records := [][]byte{[]byte("a"), []byte("bb"), []byte("ccc")}
	page, ok := buildPage(testPageSize, pageLeaf, 7, records)
	if !ok {
		t.Fatal("buildPage não coube")
	}
	got, _ := pageRecords(page)
	if pageKind(page) != pageLeaf || pageLink(page) != 7 || !reflect.DeepEqual(got, records) {
		t.Errorf("página = tipo %d, link %d, %q", pageKind(page), pageLink(page), got)
	}
	if _, ok := buildPage(testPageSize, pageLeaf, 0, [][]byte{make([]byte, testPageSize)}); ok {
		t.Error("buildPage aceitou registros maiores que a página")
	}
}

func TestPageRecordsErrors(t *testing.T) {
	tooManySlots := newPage(testPageSize, pageHeap)
	tooManySlots[2], tooManySlots[3] = 0xff, 0x00 // 255 slots, mas nenhum registro

	outside := newPage(testPageSize, pageHeap)
	addRecord(outside, []byte("abc"))
	outside[pageHeaderSize+2], outside[pageHeaderSize+3] = 0xff, 0x00 // tamanho do slot 0 passa do fim da página

	for name, page := range map[string][]byte{"slots demais": tooManySlots, "slot para fora": outside} {
		if _, err := pageRecords(page); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

func TestCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.mari")
	if err := createFile(path, testPageSize, newPage(testPageSize, pageHeap)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if size, err := readHeader(data); err != nil || size != testPageSize || len(data) != 2*testPageSize {
		t.Errorf("arquivo com %d bytes, readHeader = %d, %v", len(data), size, err)
	}
	if err := createFile(path, testPageSize); err == nil {
		t.Error("createFile sobrescreveu um arquivo que já existia")
	}
}

// fuzzColumns cobre todos os tipos e mais de 8 colunas, para o bitmap de
// NULL ter dois bytes. REAL e BOOLEAN ficam no fim, para o caso guardado em
// testdata continuar batendo nas mesmas colunas TEXT.
var fuzzColumns = []Column{
	{Name: "a", Type: "INT"}, {Name: "b", Type: "TEXT"}, {Name: "c", Type: "INT"},
	{Name: "d", Type: "TEXT"}, {Name: "e", Type: "INT"}, {Name: "f", Type: "TEXT"},
	{Name: "g", Type: "INT"}, {Name: "h", Type: "TEXT"}, {Name: "i", Type: "TEXT"},
	{Name: "j", Type: "REAL"}, {Name: "k", Type: "BOOLEAN"},
}

// FuzzDecodeRecord confere que bytes quaisquer dão erro, nunca panic, e que
// um registro que decodifica volta aos mesmos bytes quando gravado de novo.
// Rode com: go test ./internal/storage -fuzz FuzzDecodeRecord
func FuzzDecodeRecord(f *testing.F) {
	row := []Value{Text("1"), Text("Ana"), Null, Text(""), Text("-9"), Null, Text("0"), Text("x"), Text("é"), Text("-1.5"), Text("TRUE")}
	rec, err := encodeRecord(fuzzColumns, row)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(rec)
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		row, err := decodeRecord(fuzzColumns, data)
		if err != nil {
			return
		}
		again, err := encodeRecord(fuzzColumns, row)
		if err != nil {
			t.Fatalf("decodificou %x, mas não grava de volta: %v", data, err)
		}
		if !bytes.Equal(again, data) {
			t.Errorf("ida e volta diferente: %x → %x", data, again)
		}
	})
}

// FuzzPageRecords confere que uma página corrompida dá erro, nunca panic, e
// que os registros devolvidos ficam dentro da página.
// Rode com: go test ./internal/storage -fuzz FuzzPageRecords
func FuzzPageRecords(f *testing.F) {
	page := newPage(minPageSize, pageHeap)
	addRecord(page, []byte("abc"))
	addRecord(page, []byte("de"))
	f.Add(page)
	f.Add(newPage(minPageSize, pageLeaf))

	f.Fuzz(func(t *testing.T, data []byte) {
		// O pager sempre entrega páginas do tamanho certo.
		page := make([]byte, minPageSize)
		copy(page, data)
		records, err := pageRecords(page)
		if err != nil {
			return
		}
		for i, rec := range records {
			if len(rec) > len(page) {
				t.Errorf("registro %d com %d bytes numa página de %d", i, len(rec), len(page))
			}
		}
	})
}
