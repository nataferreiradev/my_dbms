package storage

import (
	"fmt"
)

// RID (row id) é o endereço de uma linha: a página e o slot dela no arquivo
// da tabela, como o ctid do PostgreSQL. Ele não muda enquanto a linha
// existir, a não ser que um UPDATE a deixe grande demais para a página dela;
// é isso que deixa um índice apontar para a linha.
type RID struct {
	Page uint32
	Slot uint16
}

func (r RID) String() string {
	return fmt.Sprintf("(%d,%d)", r.Page, r.Slot)
}

// heapPage lê a página n de uma tabela e confere se ela é de registros.
func (t *tx) heapPage(name string, n int) ([]byte, error) {
	page, err := t.page(name, n)
	if err != nil {
		return nil, err
	}
	if pageKind(page) != pageHeap {
		return nil, fmt.Errorf("tabela %q corrompida: página %d é do tipo %d", name, n, pageKind(page))
	}
	// Confere os slots antes de alguém escrever na página guiado por eles.
	if _, err := pageRecords(page); err != nil {
		return nil, fmt.Errorf("tabela %q corrompida, página %d: %w", name, n, err)
	}
	return page, nil
}

// insertRecord grava rec na última página da tabela ou, se não couber, numa
// página nova. O espaço livre das páginas anteriores só é reaproveitado por
// UPDATE; um mapa de espaço livre fica para depois.
func (t *tx) insertRecord(name string, rec []byte) (RID, error) {
	if len(rec) > maxRecordSize(t.db.pageSize) {
		return RID{}, fmt.Errorf("registro de %d bytes não cabe numa página de %d bytes (máximo %d por registro)",
			len(rec), t.db.pageSize, maxRecordSize(t.db.pageSize))
	}
	count, err := t.pageCount(name)
	if err != nil {
		return RID{}, err
	}
	if count > 1 {
		page, err := t.heapPage(name, count-1)
		if err != nil {
			return RID{}, err
		}
		if slot, ok := addRecord(page, rec); ok {
			return RID{uint32(count - 1), uint16(slot)}, t.write(name, count-1, page)
		}
	}
	n, page, err := t.allocate(name, pageHeap)
	if err != nil {
		return RID{}, err
	}
	slot, _ := addRecord(page, rec)
	return RID{uint32(n), uint16(slot)}, nil
}

// record devolve os bytes do registro no endereço rid.
func (t *tx) record(name string, rid RID) ([]byte, error) {
	page, err := t.heapPage(name, int(rid.Page))
	if err != nil {
		return nil, err
	}
	if int(rid.Slot) >= slotCount(page) {
		return nil, fmt.Errorf("tabela %q: a linha %s não existe", name, rid)
	}
	offset, size := slot(page, int(rid.Slot))
	if size == 0 {
		return nil, fmt.Errorf("tabela %q: a linha %s foi apagada", name, rid)
	}
	if offset+size > len(page) {
		return nil, fmt.Errorf("tabela %q corrompida: a linha %s aponta para fora da página", name, rid)
	}
	return page[offset : offset+size], nil
}

// deleteRecord apaga a linha do endereço rid.
func (t *tx) deleteRecord(name string, rid RID) error {
	if _, err := t.record(name, rid); err != nil {
		return err
	}
	page, _ := t.heapPage(name, int(rid.Page))
	deleteRecord(page, int(rid.Slot))
	return t.write(name, int(rid.Page), page)
}

// updateRecord troca o registro do endereço rid por rec. Se couber na mesma
// página, o endereço continua o mesmo; senão a linha muda de página, e o
// endereço novo é devolvido.
func (t *tx) updateRecord(name string, rid RID, rec []byte) (RID, error) {
	if _, err := t.record(name, rid); err != nil {
		return RID{}, err
	}
	if len(rec) > maxRecordSize(t.db.pageSize) {
		return RID{}, fmt.Errorf("registro de %d bytes não cabe numa página de %d bytes (máximo %d por registro)",
			len(rec), t.db.pageSize, maxRecordSize(t.db.pageSize))
	}
	page, _ := t.heapPage(name, int(rid.Page))
	if updateRecord(page, int(rid.Slot), rec) {
		return rid, t.write(name, int(rid.Page), page)
	}
	deleteRecord(page, int(rid.Slot))
	if err := t.write(name, int(rid.Page), page); err != nil {
		return RID{}, err
	}
	return t.insertRecord(name, rec)
}

// scanRecords chama fn para cada registro vivo da tabela, na ordem das
// páginas e dos slots. fn devolve false para parar.
func (t *tx) scanRecords(name string, fn func(RID, []byte) (bool, error)) error {
	count, err := t.pageCount(name)
	if err != nil {
		return err
	}
	for n := 1; n < count; n++ {
		page, err := t.heapPage(name, n)
		if err != nil {
			return err
		}
		records, err := pageRecords(page)
		if err != nil {
			return fmt.Errorf("tabela %q corrompida, página %d: %w", name, n, err)
		}
		for i, rec := range records {
			if rec == nil {
				continue
			}
			more, err := fn(RID{uint32(n), uint16(i)}, rec)
			if err != nil || !more {
				return err
			}
		}
	}
	return nil
}
