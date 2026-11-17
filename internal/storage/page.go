package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// Um arquivo .mari é uma sequência de páginas de pageSize bytes. A página 0
// é o cabeçalho do arquivo, e as outras guardam registros:
//
//	página 0:  "MRNA" | versão (uint16) | page_size (uint16) | zeros
//	página N:  tipo (1 byte) | livre (1 byte) | nº de slots (uint16) |
//	           início dos registros (uint16) | link (uint32) | slot 0 | slot 1 | ...
//	           espaço livre
//	           ... | registro 1 | registro 0
//
// Cada slot tem 4 bytes: offset e tamanho do registro na página (uint16). Os
// slots crescem do começo da página e os registros do fim, e a página está
// cheia quando os dois se encontram. Todos os inteiros do cabeçalho são
// little endian.
//
// O tipo diz o que a página guarda. Numa página de tabela (pageHeap), o
// número do slot faz parte do endereço da linha (RID), então ele nunca muda:
// apagar uma linha marca o slot como morto (tamanho 0), e compactar a página
// move os registros sem renumerar os slots. As páginas de índice (pageLeaf e
// pageInternal) guardam as entradas da B+tree em ordem, e o link aponta para
// outra página (veja btree.go).
const (
	fileMagic      = "MRNA"
	fileVersion    = 2
	pageHeaderSize = 10
	slotSize       = 4

	// Os offsets dentro da página são uint16, por isso o máximo é 32 KB.
	minPageSize = 1024
	maxPageSize = 32768
)

// Tipos de página.
const (
	pageHeap     byte = 1 // registros de uma tabela
	pageLeaf     byte = 2 // folha da B+tree; link = próxima folha
	pageInternal byte = 3 // nó interno da B+tree; link = filho mais à direita
)

// checkPageSize confere se o tamanho de página é uma potência de 2 entre
// minPageSize e maxPageSize.
func checkPageSize(size int) error {
	if size < minPageSize || size > maxPageSize || size&(size-1) != 0 {
		return fmt.Errorf("page_size %d inválido: precisa ser potência de 2 entre %d e %d", size, minPageSize, maxPageSize)
	}
	return nil
}

// maxRecordSize é o maior registro que cabe numa página vazia, com o slot.
func maxRecordSize(pageSize int) int {
	return pageSize - pageHeaderSize - slotSize
}

// encodeRecord grava uma linha em bytes, guiada pelos tipos das colunas:
//
//	bitmap de NULL | valor 0 | valor 1 | ...
//
// O bitmap tem um bit por coluna (o bit i%8 do byte i/8), ligado quando o
// valor é NULL; um NULL não ocupa mais nada. INT ocupa 8 bytes (int64), REAL
// ocupa 8 bytes (float64), BOOLEAN ocupa 1 byte (0 ou 1) e TEXT é o tamanho
// em uvarint seguido dos bytes do texto.
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
		case "REAL":
			f, err := strconv.ParseFloat(v.Text, 64)
			if err != nil {
				return nil, fmt.Errorf("coluna %s: %q não é um REAL", c.Name, v.Text)
			}
			rec = binary.LittleEndian.AppendUint64(rec, math.Float64bits(f))
		case "BOOLEAN":
			switch v.Text {
			case "TRUE":
				rec = append(rec, 1)
			case "FALSE":
				rec = append(rec, 0)
			default:
				return nil, fmt.Errorf("coluna %s: %q não é um BOOLEAN", c.Name, v.Text)
			}
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
	// Os bits que sobram no último byte do bitmap não são de coluna nenhuma:
	// ligados, o registro está corrompido.
	if extra := len(columns) % 8; extra != 0 && rec[pos-1]>>extra != 0 {
		return nil, errors.New("bitmap de NULL com bits além da última coluna")
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
		case "REAL":
			if len(rec)-pos < 8 {
				return nil, fmt.Errorf("coluna %s: REAL cortado", c.Name)
			}
			f := math.Float64frombits(binary.LittleEndian.Uint64(rec[pos:]))
			// NaN, infinito e -0 nunca são gravados (veja FormatReal).
			if math.IsNaN(f) || math.IsInf(f, 0) || (f == 0 && math.Signbit(f)) {
				return nil, fmt.Errorf("coluna %s: REAL inválido", c.Name)
			}
			row[i] = Text(FormatReal(f))
			pos += 8
		case "BOOLEAN":
			if len(rec)-pos < 1 {
				return nil, fmt.Errorf("coluna %s: BOOLEAN cortado", c.Name)
			}
			switch rec[pos] {
			case 0:
				row[i] = Text("FALSE")
			case 1:
				row[i] = Text("TRUE")
			default:
				return nil, fmt.Errorf("coluna %s: BOOLEAN com o byte %d", c.Name, rec[pos])
			}
			pos++
		case "TEXT":
			size, n := binary.Uvarint(rec[pos:])
			if n <= 0 || size > uint64(len(rec)-pos-n) {
				return nil, fmt.Errorf("coluna %s: TEXT cortado", c.Name)
			}
			// O Uvarint aceita bytes a mais (80 00 também é 0); o encodeRecord
			// nunca grava assim, então é corrupção.
			if n != len(binary.AppendUvarint(nil, size)) {
				return nil, fmt.Errorf("coluna %s: tamanho do TEXT mal codificado", c.Name)
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

// newPage devolve uma página vazia do tipo kind: sem slots, e com os
// registros começando no fim.
func newPage(size int, kind byte) []byte {
	page := make([]byte, size)
	page[0] = kind
	binary.LittleEndian.PutUint16(page[4:], uint16(size))
	return page
}

func pageKind(page []byte) byte         { return page[0] }
func slotCount(page []byte) int         { return int(binary.LittleEndian.Uint16(page[2:])) }
func recordsStart(page []byte) int      { return int(binary.LittleEndian.Uint16(page[4:])) }
func pageLink(page []byte) uint32       { return binary.LittleEndian.Uint32(page[6:]) }
func setPageLink(page []byte, n uint32) { binary.LittleEndian.PutUint32(page[6:], n) }

// slot devolve o offset e o tamanho do registro no slot i.
func slot(page []byte, i int) (offset, size int) {
	at := pageHeaderSize + i*slotSize
	return int(binary.LittleEndian.Uint16(page[at:])), int(binary.LittleEndian.Uint16(page[at+2:]))
}

func setSlot(page []byte, i, offset, size int) {
	at := pageHeaderSize + i*slotSize
	binary.LittleEndian.PutUint16(page[at:], uint16(offset))
	binary.LittleEndian.PutUint16(page[at+2:], uint16(size))
}

// freeSpace devolve o espaço livre contínuo (entre os slots e os registros) e
// o total, somando os buracos deixados por registros apagados ou encolhidos.
func freeSpace(page []byte) (contiguous, total int) {
	count := slotCount(page)
	contiguous = recordsStart(page) - pageHeaderSize - count*slotSize
	used := 0
	for i := range count {
		_, size := slot(page, i)
		used += size
	}
	total = len(page) - pageHeaderSize - count*slotSize - used
	return contiguous, total
}

// addRecord grava rec na página e devolve o slot dele. Reaproveita um slot
// morto, se houver, e compacta a página quando o espaço livre está
// espalhado em buracos. false quer dizer que não coube.
func addRecord(page, rec []byte) (int, bool) {
	count := slotCount(page)
	i := count
	need := len(rec) + slotSize
	if pageKind(page) == pageHeap {
		for j := range count {
			if _, size := slot(page, j); size == 0 {
				i, need = j, len(rec)
				break
			}
		}
	}
	contiguous, total := freeSpace(page)
	if total < need {
		return 0, false
	}
	if contiguous < need {
		compactPage(page)
	}

	start := recordsStart(page) - len(rec)
	copy(page[start:], rec)
	binary.LittleEndian.PutUint16(page[4:], uint16(start))
	if i == count {
		binary.LittleEndian.PutUint16(page[2:], uint16(count+1))
	}
	setSlot(page, i, start, len(rec))
	return i, true
}

// deleteRecord marca o slot como morto. Os bytes do registro viram um buraco,
// que a próxima compactação recupera.
func deleteRecord(page []byte, i int) {
	setSlot(page, i, 0, 0)
}

// updateRecord troca o registro do slot i por rec, sem mudar o número do
// slot. false quer dizer que não coube na página, e ela fica como estava.
func updateRecord(page []byte, i int, rec []byte) bool {
	offset, size := slot(page, i)
	if len(rec) <= size {
		// Cabe no lugar do antigo; a sobra vira buraco.
		copy(page[offset:], rec)
		setSlot(page, i, offset, len(rec))
		return true
	}
	if _, total := freeSpace(page); total+size < len(rec) {
		return false
	}
	setSlot(page, i, 0, 0)
	if contiguous, _ := freeSpace(page); contiguous < len(rec) {
		compactPage(page)
	}
	start := recordsStart(page) - len(rec)
	copy(page[start:], rec)
	binary.LittleEndian.PutUint16(page[4:], uint16(start))
	setSlot(page, i, start, len(rec))
	return true
}

// compactPage junta os registros no fim da página, eliminando os buracos.
// Os slots continuam com os mesmos números; só os offsets mudam.
func compactPage(page []byte) {
	count := slotCount(page)
	records := make([][]byte, count)
	for i := range count {
		offset, size := slot(page, i)
		if size > 0 {
			records[i] = append([]byte(nil), page[offset:offset+size]...)
		}
	}
	start := len(page)
	for i, rec := range records {
		if rec == nil {
			continue
		}
		start -= len(rec)
		copy(page[start:], rec)
		setSlot(page, i, start, len(rec))
	}
	binary.LittleEndian.PutUint16(page[4:], uint16(start))
}

// pageRecords devolve os registros da página, indexados pelo número do slot.
// Slots mortos aparecem como nil. Os registros apontam para dentro da página.
func pageRecords(page []byte) ([][]byte, error) {
	count, start := slotCount(page), recordsStart(page)
	if pageHeaderSize+count*slotSize > start || start > len(page) {
		return nil, fmt.Errorf("cabeçalho da página inválido: %d slots e registros a partir do byte %d", count, start)
	}

	records := make([][]byte, count)
	for i := range records {
		offset, size := slot(page, i)
		if size == 0 {
			continue
		}
		if offset < start || offset+size > len(page) {
			return nil, fmt.Errorf("slot %d aponta para fora da área de registros", i)
		}
		records[i] = page[offset : offset+size]
	}
	return records, nil
}

// buildPage monta uma página nova do tipo kind com os registros em ordem, um
// por slot. false quer dizer que eles não cabem numa página só.
func buildPage(size int, kind byte, link uint32, records [][]byte) ([]byte, bool) {
	if pageHeaderSize+recordsSize(records) > size {
		return nil, false
	}
	page := newPage(size, kind)
	setPageLink(page, link)
	for _, rec := range records {
		addRecord(page, rec)
	}
	return page, true
}

// recordsSize é o espaço que os registros ocupam numa página, com os slots.
func recordsSize(records [][]byte) int {
	n := 0
	for _, rec := range records {
		n += len(rec) + slotSize
	}
	return n
}

// fileHeader monta a página 0 de um arquivo .mari.
func fileHeader(pageSize int) []byte {
	page := make([]byte, pageSize)
	copy(page, fileMagic)
	binary.LittleEndian.PutUint16(page[4:], fileVersion)
	binary.LittleEndian.PutUint16(page[6:], uint16(pageSize))
	return page
}

// readHeader confere a página 0 e devolve o page_size gravado nela.
func readHeader(data []byte) (int, error) {
	if len(data) < 8 || string(data[:4]) != fileMagic {
		return 0, errors.New("não é um arquivo do my_dbms (pode ser do formato antigo, em CSV)")
	}
	if v := binary.LittleEndian.Uint16(data[4:]); v != fileVersion {
		return 0, fmt.Errorf("versão %d do formato não é suportada (esta versão do my_dbms lê a %d)", v, fileVersion)
	}
	return int(binary.LittleEndian.Uint16(data[6:])), nil
}

// createFile cria um arquivo .mari novo, com a página 0 e as páginas dadas, e
// só volta depois do fsync do arquivo e do diretório. Se o arquivo já existe,
// é erro: ele nunca é sobrescrito.
func createFile(path string, pageSize int, pages ...[]byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	data := fileHeader(pageSize)
	for _, p := range pages {
		data = append(data, p...)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// writeSynced grava data em path e só volta depois do fsync.
func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// syncDir faz o fsync do diretório, para que criar, renomear ou apagar um
// arquivo também chegue ao disco. No Windows não dá para abrir um diretório
// assim, e o NTFS já registra essas operações no journal dele.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
