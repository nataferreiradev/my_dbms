package storage

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
	"strings"
)

// VectorIndex é o índice vetorial: guarda um embedding de cada linha, feito
// do texto das colunas dele, para a busca por similaridade (Search). Cada
// tabela tem no máximo um, em Schema.Vector.
const VectorIndex IndexKind = "vector"

// O arquivo do índice vetorial é um heap como o das tabelas, com um registro
// por linha da tabela:
//
//	RID da linha (6 bytes) | uma dimensão por byte (int8)
//
// Os vetores são guardados em int8: cada dimensão é dividida pela maior em
// módulo e multiplicada por 127. A busca usa o cosseno, que não depende do
// tamanho do vetor, só da direção, e a direção quase não muda. Assim um
// vetor de 1536 dimensões ocupa 1542 bytes e cabe numa página de 4 KB; em
// float32 seriam 6 KB.
//
// Não há nada que leve do RID ao registro do vetor: apagar ou trocar
// vetores varre o arquivo, uma vez por comando. A busca também varre tudo
// (força bruta), então isso não muda a ordem do custo.

// embedText é o texto da linha que vira embedding: "coluna: valor", uma por
// linha, sem as colunas NULL. Sem nenhum valor, é "" e a linha fica sem
// vetor.
func embedText(schema Schema, idx Index, row []Value) string {
	var parts []string
	for _, c := range idx.Columns {
		if v := row[schema.ColumnIndex(c)]; !v.Null {
			parts = append(parts, c+": "+v.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// embed chama a API de embeddings para os textos.
func (db *DB) embed(texts []string) ([][]float32, error) {
	if db.Embed == nil {
		return nil, errors.New("nenhuma API de embeddings configurada: rode my_dbms -setup")
	}
	vectors, err := db.Embed(texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("embedding: pedi %d vetores e vieram %d", len(texts), len(vectors))
	}
	return vectors, nil
}

// embedRows calcula o vetor de cada linha, já quantizado, antes de a
// transação abrir: a API pode demorar, e nada fica travado esperando por
// ela. Uma linha sem texto fica com nil. Só os índices de rows em which são
// calculados; which nil é todas.
func (db *DB) embedRows(schema Schema, rows [][]Value, which []int) ([][]byte, error) {
	if which == nil {
		for i := range rows {
			which = append(which, i)
		}
	}
	var texts []string
	var positions []int
	for _, i := range which {
		if text := embedText(schema, *schema.Vector, rows[i]); text != "" {
			texts = append(texts, text)
			positions = append(positions, i)
		}
	}
	quantized := make([][]byte, len(rows))
	if len(texts) == 0 {
		return quantized, nil
	}
	vectors, err := db.embed(texts)
	if err != nil {
		return nil, err
	}

	dims, err := db.vectorDims(schema.Vector.Name)
	if err != nil {
		return nil, err
	}
	for j, v := range vectors {
		if dims == 0 {
			dims = len(v)
		}
		if err := db.checkDims(schema.Vector.Name, dims, len(v)); err != nil {
			return nil, err
		}
		quantized[positions[j]] = quantize(v)
	}
	return quantized, nil
}

// checkDims confere se um vetor de got dimensões cabe no índice, que tem
// vetores de want dimensões (0 se está vazio).
func (db *DB) checkDims(index string, want, got int) error {
	if max := maxRecordSize(db.pageSize) - ridSize; got > max {
		return fmt.Errorf("o modelo devolveu vetores de %d dimensões, mas uma página de %d bytes guarda no máximo %d: use um page_size maior ou um modelo menor",
			got, db.pageSize, max)
	}
	if want != 0 && got != want {
		return fmt.Errorf("o índice %s tem vetores de %d dimensões e o modelo devolveu %d: se o modelo mudou, apague o índice e crie de novo",
			index, want, got)
	}
	return nil
}

// vectorDims devolve quantas dimensões têm os vetores do índice, olhando o
// primeiro registro. Índice vazio dá 0.
func (db *DB) vectorDims(index string) (int, error) {
	t := db.begin()
	defer t.close()
	dims := 0
	err := t.scanRecords(index, func(_ RID, rec []byte) (bool, error) {
		dims = len(rec) - ridSize
		return false, nil
	})
	return dims, err
}

// quantize guarda o vetor em int8, mantendo a direção.
func quantize(v []float32) []byte {
	var biggest float64
	for _, x := range v {
		biggest = max(biggest, math.Abs(float64(x)))
	}
	q := make([]byte, len(v))
	if biggest == 0 {
		return q
	}
	for i, x := range v {
		q[i] = byte(int8(math.Round(float64(x) / biggest * 127)))
	}
	return q
}

// cosine é a similaridade entre a pergunta e um vetor guardado: 1 quando
// apontam para o mesmo lado, 0 quando não têm nada a ver. Vetor nulo dá 0.
func cosine(query []float32, stored []byte) float64 {
	var dot, nq, ns float64
	for i, x := range query {
		y := float64(int8(stored[i]))
		dot += float64(x) * y
		nq += float64(x) * float64(x)
		ns += y * y
	}
	if nq == 0 || ns == 0 {
		return 0
	}
	return dot / math.Sqrt(nq*ns)
}

// addVector grava o vetor da linha rid. vector nil (linha sem texto) não
// grava nada.
func (t *tx) addVector(index string, rid RID, vector []byte) error {
	if vector == nil {
		return nil
	}
	_, err := t.insertRecord(index, append(appendRID(nil, rid), vector...))
	return err
}

// removeVectors apaga os vetores das linhas em rids e devolve o que estava
// guardado, para um UPDATE que não mudou o texto reaproveitar.
func (t *tx) removeVectors(index string, rids []RID) (map[RID][]byte, error) {
	wanted := map[RID]bool{}
	for _, rid := range rids {
		wanted[rid] = true
	}
	old := map[RID][]byte{}
	var drop []RID
	err := t.scanRecords(index, func(at RID, rec []byte) (bool, error) {
		if rid := entryRID(rec[:ridSize]); wanted[rid] {
			old[rid] = slices.Clone(rec[ridSize:])
			drop = append(drop, at)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	for _, at := range drop {
		if err := t.deleteRecord(index, at); err != nil {
			return nil, err
		}
	}
	return old, nil
}

// checkIndexColumns confere as colunas de um índice novo.
func checkIndexColumns(schema Schema, table string, columns []string) error {
	if len(columns) == 0 {
		return errors.New("índice sem colunas")
	}
	for i, c := range columns {
		if schema.ColumnIndex(c) < 0 {
			return fmt.Errorf("coluna %q não existe na tabela %s", c, table)
		}
		if slices.Contains(columns[:i], c) {
			return fmt.Errorf("coluna %q repetida no índice", c)
		}
	}
	return nil
}

// CreateVectorIndex cria o índice vetorial da tabela sobre as colunas e
// calcula o embedding das linhas que ela já tem.
func (db *DB) CreateVectorIndex(name, table string, columns []string) error {
	if isSystem(name) || isSystem(table) {
		return errors.New("nomes começando com _ são do sistema: não dá para criar índice com eles nem neles")
	}
	if db.nameInUse(name) {
		return fmt.Errorf("já existe uma tabela ou índice chamado %q", name)
	}
	schema, err := db.TableSchema(table)
	if err != nil {
		return err
	}
	if schema.Vector != nil {
		return fmt.Errorf("a tabela %s já tem o índice vetorial %s", table, schema.Vector.Name)
	}
	if err := checkIndexColumns(schema, table, columns); err != nil {
		return err
	}
	schema.Vector = &Index{Name: name, Kind: VectorIndex, Columns: slices.Clone(columns)}

	err = createFile(db.tablePath(name), db.pageSize)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("o arquivo %s já existe, mas não está no catálogo", db.tablePath(name))
	}
	if err != nil {
		return err
	}
	existing, err := db.ReadTable(table)
	var vectors [][]byte
	if err == nil {
		vectors, err = db.embedRows(schema, existing.Rows, nil)
	}
	if err != nil {
		os.Remove(db.tablePath(name))
		return err
	}

	t := db.begin()
	defer t.close()
	for i, rid := range existing.RIDs {
		if err = t.addVector(name, rid, vectors[i]); err != nil {
			break
		}
	}
	if err == nil {
		err = t.appendCatalog(catalogRow(string(VectorIndex), name, table, strings.Join(columns, ",")))
	}
	if err == nil {
		err = t.commit()
	}
	if err != nil {
		t.close()
		os.Remove(db.tablePath(name))
		return err
	}
	db.tables[table] = schema
	return nil
}

// SearchResult é uma linha achada pela busca, com a similaridade dela.
type SearchResult struct {
	Row
	Similarity float64
}

// Search devolve as k linhas da tabela cujo texto mais se parece com query,
// da mais parecida para a menos. Linhas sem vetor (sem texto) não aparecem.
func (db *DB) Search(table, query string, k int) ([]SearchResult, error) {
	schema, err := db.TableSchema(table)
	if err != nil {
		return nil, err
	}
	if schema.Vector == nil {
		return nil, fmt.Errorf("a tabela %s não tem índice vetorial: crie com CREATE VECTOR INDEX ou WITH EMBEDDING", table)
	}
	if k <= 0 {
		return nil, fmt.Errorf("TOP precisa ser maior que zero, veio %d", k)
	}
	vectors, err := db.embed([]string{query})
	if err != nil {
		return nil, err
	}
	q := vectors[0]

	t := db.begin()
	defer t.close()
	var hits []SearchResult
	err = t.scanRecords(schema.Vector.Name, func(_ RID, rec []byte) (bool, error) {
		stored := rec[ridSize:]
		if err := db.checkDims(schema.Vector.Name, len(stored), len(q)); err != nil {
			return false, err
		}
		hits = append(hits, SearchResult{Row: Row{RID: entryRID(rec[:ridSize])}, Similarity: cosine(q, stored)})
		return true, nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(hits, func(a, b SearchResult) int { return cmp.Compare(b.Similarity, a.Similarity) })
	hits = hits[:min(k, len(hits))]
	for i := range hits {
		if hits[i].Values, err = t.row(table, schema, hits[i].RID); err != nil {
			return nil, fmt.Errorf("índice %s aponta para %s: %w", schema.Vector.Name, hits[i].RID, err)
		}
	}
	return hits, nil
}
