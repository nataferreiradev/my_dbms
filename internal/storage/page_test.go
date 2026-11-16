package storage

import (
	"bytes"
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

func TestPageFill(t *testing.T) {
	page := newPage(testPageSize)
	var added [][]byte
	for i := 0; ; i++ {
		rec := bytes.Repeat([]byte{byte(i)}, 10)
		if !addRecord(page, rec) {
			break
		}
		added = append(added, rec)
	}

	// Cada registro gasta 10 bytes + 4 do slot: (1024 - 4) / 14 = 72.
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

func TestPageRecordsErrors(t *testing.T) {
	tooManySlots := newPage(testPageSize)
	tooManySlots[0], tooManySlots[1] = 0xff, 0x00 // 255 slots, mas nenhum registro

	outside := newPage(testPageSize)
	addRecord(outside, []byte("abc"))
	outside[6], outside[7] = 0xff, 0x00 // tamanho do slot 0 passa do fim da página

	for name, page := range map[string][]byte{"slots demais": tooManySlots, "slot para fora": outside} {
		if _, err := pageRecords(page); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
}

func TestWritePagesLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.mari")
	if err := writePages(path, testPageSize, [][]byte{newPage(testPageSize)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("o arquivo temporário ficou para trás: %v", err)
	}
	pages, err := readPages(path, testPageSize)
	if err != nil || len(pages) != 1 {
		t.Errorf("readPages = %d páginas, %v; want 1 página", len(pages), err)
	}
}

// fuzzColumns cobre os dois tipos e mais de 8 colunas, para o bitmap de NULL
// ter dois bytes.
var fuzzColumns = []Column{
	{Name: "a", Type: "INT"}, {Name: "b", Type: "TEXT"}, {Name: "c", Type: "INT"},
	{Name: "d", Type: "TEXT"}, {Name: "e", Type: "INT"}, {Name: "f", Type: "TEXT"},
	{Name: "g", Type: "INT"}, {Name: "h", Type: "TEXT"}, {Name: "i", Type: "TEXT"},
}

// FuzzDecodeRecord confere que bytes quaisquer dão erro, nunca panic, e que
// um registro que decodifica volta aos mesmos bytes quando gravado de novo.
// Rode com: go test ./internal/storage -fuzz FuzzDecodeRecord
func FuzzDecodeRecord(f *testing.F) {
	row := []Value{Text("1"), Text("Ana"), Null, Text(""), Text("-9"), Null, Text("0"), Text("x"), Text("é")}
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
	page := newPage(minPageSize)
	addRecord(page, []byte("abc"))
	addRecord(page, []byte("de"))
	f.Add(page)
	f.Add(newPage(minPageSize))

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
