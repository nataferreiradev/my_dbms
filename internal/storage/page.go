package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
)

// Um arquivo .marina é uma sequência de páginas de pageSize bytes. A página 0
// é o cabeçalho do arquivo, e as outras guardam os registros:
//
//	página 0:  "MRNA" | versão (uint16) | page_size (uint16) | zeros
//	página N:  nº de slots (uint16) | início dos registros (uint16) | slot 0 | slot 1 | ...
//	           espaço livre
//	           ... | registro 1 | registro 0
//
// Cada slot tem 4 bytes: offset e tamanho do registro na página (uint16). Os
// slots crescem do começo da página e os registros do fim, e a página está
// cheia quando os dois se encontram. Todos os inteiros são little endian.
const (
	fileMagic      = "MRNA"
	fileVersion    = 1
	pageHeaderSize = 4
	slotSize       = 4

	// Os offsets dentro da página são uint16, por isso o máximo é 32 KB.
	minPageSize = 1024
	maxPageSize = 32768
)

// checkPageSize confere se o tamanho de página é uma potência de 2 entre
// minPageSize e maxPageSize.
func checkPageSize(size int) error {
	if size < minPageSize || size > maxPageSize || size&(size-1) != 0 {
		return fmt.Errorf("page_size %d inválido: precisa ser potência de 2 entre %d e %d", size, minPageSize, maxPageSize)
	}
	return nil
}

// encodeRecord grava uma linha em bytes, guiada pelos tipos das colunas:
//
//	bitmap de NULL | valor 0 | valor 1 | ...
//
// O bitmap tem um bit por coluna (o bit i%8 do byte i/8), ligado quando o
// valor é NULL; um NULL não ocupa mais nada. INT ocupa 8 bytes (int64) e
// TEXT é o tamanho em uvarint seguido dos bytes do texto.
func encodeRecord(columns []Column, row []Value) ([]byte, error) {
	rec := make([]byte, (len(columns)+7)/8)
	for i, c := range columns {
		v := row[i]
		if v.Null {
			rec[i/8] |= 1 << (i % 8)
			continue
		}
		switch c.Type {
		case "INT":
			n, err := strconv.ParseInt(v.Text, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("coluna %s: %q não é um INT", c.Name, v.Text)
			}
			rec = binary.LittleEndian.AppendUint64(rec, uint64(n))
		case "TEXT":
			rec = binary.AppendUvarint(rec, uint64(len(v.Text)))
			rec = append(rec, v.Text...)
		default:
			return nil, fmt.Errorf("coluna %s: tipo %q desconhecido", c.Name, c.Type)
		}
	}
	return rec, nil
}

// decodeRecord faz o caminho inverso de encodeRecord.
func decodeRecord(columns []Column, rec []byte) ([]Value, error) {
	pos := (len(columns) + 7) / 8
	if len(rec) < pos {
		return nil, errors.New("registro menor que o bitmap de NULL")
	}
	row := make([]Value, len(columns))
	for i, c := range columns {
		if rec[i/8]&(1<<(i%8)) != 0 {
			row[i] = Null
			continue
		}
		switch c.Type {
		case "INT":
			if len(rec)-pos < 8 {
				return nil, fmt.Errorf("coluna %s: INT cortado", c.Name)
			}
			n := int64(binary.LittleEndian.Uint64(rec[pos:]))
			row[i] = Text(strconv.FormatInt(n, 10))
			pos += 8
		case "TEXT":
			size, n := binary.Uvarint(rec[pos:])
			if n <= 0 || size > uint64(len(rec)-pos-n) {
				return nil, fmt.Errorf("coluna %s: TEXT cortado", c.Name)
			}
			pos += n
			row[i] = Text(string(rec[pos : pos+int(size)]))
			pos += int(size)
		default:
			return nil, fmt.Errorf("coluna %s: tipo %q desconhecido", c.Name, c.Type)
		}
	}
	if pos != len(rec) {
		return nil, fmt.Errorf("sobraram %d bytes no fim do registro", len(rec)-pos)
	}
	return row, nil
}

// newPage devolve uma página de dados vazia: sem slots, e com os registros
// começando no fim.
func newPage(size int) []byte {
	page := make([]byte, size)
	binary.LittleEndian.PutUint16(page[2:], uint16(size))
	return page
}

// addRecord grava rec na página, se couber junto com um slot novo. O
// registro vai para o fim do espaço livre.
func addRecord(page, rec []byte) bool {
	count := int(binary.LittleEndian.Uint16(page[0:]))
	start := int(binary.LittleEndian.Uint16(page[2:]))
	slot := pageHeaderSize + count*slotSize
	if start-len(rec) < slot+slotSize {
		return false
	}

	start -= len(rec)
	copy(page[start:], rec)
	binary.LittleEndian.PutUint16(page[slot:], uint16(start))
	binary.LittleEndian.PutUint16(page[slot+2:], uint16(len(rec)))
	binary.LittleEndian.PutUint16(page[0:], uint16(count+1))
	binary.LittleEndian.PutUint16(page[2:], uint16(start))
	return true
}

// pageRecords devolve os registros da página, na ordem dos slots.
func pageRecords(page []byte) ([][]byte, error) {
	count := int(binary.LittleEndian.Uint16(page[0:]))
	start := int(binary.LittleEndian.Uint16(page[2:]))
	if pageHeaderSize+count*slotSize > start || start > len(page) {
		return nil, fmt.Errorf("cabeçalho da página inválido: %d slots e registros a partir do byte %d", count, start)
	}

	records := make([][]byte, count)
	for i := range records {
		slot := pageHeaderSize + i*slotSize
		offset := int(binary.LittleEndian.Uint16(page[slot:]))
		size := int(binary.LittleEndian.Uint16(page[slot+2:]))
		if offset < start || offset+size > len(page) {
			return nil, fmt.Errorf("slot %d aponta para fora da área de registros", i)
		}
		records[i] = page[offset : offset+size]
	}
	return records, nil
}

// readHeader confere a página 0 e devolve o page_size gravado nela.
func readHeader(data []byte) (int, error) {
	if len(data) < 8 || string(data[:4]) != fileMagic {
		return 0, errors.New("não é um arquivo do my_dbms (pode ser do formato antigo, em CSV)")
	}
	if v := binary.LittleEndian.Uint16(data[4:]); v != fileVersion {
		return 0, fmt.Errorf("versão %d do formato não é suportada", v)
	}
	return int(binary.LittleEndian.Uint16(data[6:])), nil
}

// readPages lê um arquivo .marina e devolve as páginas de dados, sem a
// página 0.
func readPages(path string, pageSize int) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	size, err := readHeader(data)
	if err != nil {
		return nil, err
	}
	if size != pageSize {
		return nil, fmt.Errorf("arquivo com páginas de %d bytes, mas o banco usa %d", size, pageSize)
	}
	if len(data)%pageSize != 0 {
		return nil, fmt.Errorf("tamanho de %d bytes não é múltiplo da página (%d)", len(data), pageSize)
	}

	var pages [][]byte
	for off := pageSize; off < len(data); off += pageSize {
		pages = append(pages, data[off:off+pageSize])
	}
	return pages, nil
}

// writePages troca o conteúdo de um arquivo .marina pela página 0 e pelas
// páginas de dados. Grava num arquivo temporário e renomeia por cima do
// original: se o programa cair no meio, o original continua inteiro.
func writePages(path string, pageSize int, pages [][]byte) error {
	data := make([]byte, pageSize, pageSize*(len(pages)+1))
	copy(data, fileMagic)
	binary.LittleEndian.PutUint16(data[4:], fileVersion)
	binary.LittleEndian.PutUint16(data[6:], uint16(pageSize))
	for _, p := range pages {
		data = append(data, p...)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
