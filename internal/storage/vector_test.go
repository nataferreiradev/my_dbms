package storage

import (
	"errors"
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"my_DBMS/internal/embed"
)

var docColumns = []Column{{Name: "id", Type: "INT"}, {Name: "titulo", Type: "TEXT"}, {Name: "corpo", Type: "TEXT"}}

// openDocs abre um banco com a tabela docs e o índice vetorial dela. calls
// conta quantos textos foram mandados para o embedder.
func openDocs(t *testing.T) (*DB, *int) {
	t.Helper()
	db, err := Open(t.TempDir(), testPageSize)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	fake := embed.Fake(64)
	db.Embed = func(texts []string) ([][]float32, error) {
		calls += len(texts)
		return fake(texts)
	}
	keys := []Index{{Kind: PrimaryKey, Columns: []string{"id"}}, {Kind: VectorIndex, Columns: []string{"titulo", "corpo"}}}
	if err := db.CreateTable("docs", docColumns, keys); err != nil {
		t.Fatal(err)
	}
	return db, &calls
}

// searchIDs devolve os ids achados pela busca, em ordem.
func searchIDs(t *testing.T, db *DB, query string, k int) []string {
	t.Helper()
	results, err := db.Search("docs", query, k)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i, r := range results {
		if i > 0 && r.Similarity > results[i-1].Similarity {
			t.Errorf("fora de ordem: %v", results)
		}
		ids = append(ids, r.Values[0].Text)
	}
	return ids
}

func TestVectorSearch(t *testing.T) {
	db, calls := openDocs(t)
	err := db.InsertRows("docs", [][]Value{
		texts("1", "receita de bolo", "farinha ovos e açúcar"),
		texts("2", "gato preto", "o gato preto dorme no sofá"),
		texts("3", "carro novo", "motor e rodas"),
		{Text("4"), Null, Null},
	})
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 3 {
		t.Errorf("%d textos para o embedder, queria 3 (a linha sem texto não tem vetor)", *calls)
	}

	if got := strings.Join(searchIDs(t, db, "onde o gato dorme", 2), ","); !strings.HasPrefix(got, "2,") {
		t.Errorf("busca pelo gato: %s", got)
	}
	if got := searchIDs(t, db, "qualquer coisa", 10); len(got) != 3 {
		t.Errorf("TOP maior que a tabela: %v, queria as 3 linhas com vetor", got)
	}
	if schema, _ := db.TableSchema("docs"); schema.Vector == nil || schema.Vector.Name != "docs_embedding" {
		t.Errorf("schema.Vector = %+v", schema.Vector)
	}
}

func TestVectorUpdateAndDelete(t *testing.T) {
	db, calls := openDocs(t)
	var rows [][]Value
	for i := range 20 {
		rows = append(rows, texts(itoa(i).Text, "doc "+itoa(i).Text, "nada de especial"))
	}
	rows = append(rows, texts("99", "gato", "miau"))
	if err := db.InsertRows("docs", rows); err != nil {
		t.Fatal(err)
	}
	ridOf := func(id string) RID {
		for row, err := range db.Scan("docs") {
			if err != nil {
				t.Fatal(err)
			}
			if row.Values[0].Text == id {
				return row.RID
			}
		}
		t.Fatalf("linha %s não achada", id)
		return RID{}
	}

	// Mudar só o id não muda o texto: o vetor é reaproveitado.
	*calls = 0
	if err := db.UpdateRows("docs", []RID{ridOf("99")}, [][]Value{texts("100", "gato", "miau")}); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Errorf("UPDATE sem mudar o texto chamou o embedder %d vezes", *calls)
	}

	// Um texto maior muda a linha de página (e de RID); o vetor vai junto.
	before := ridOf("100")
	long := "cachorro late " + strings.Repeat("au ", 200)
	if err := db.UpdateRows("docs", []RID{before}, [][]Value{texts("100", "cachorro", long)}); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Errorf("UPDATE do texto: %d chamadas, queria 1", *calls)
	}
	if ridOf("100") == before {
		t.Fatal("o teste queria que a linha mudasse de página")
	}
	if got := searchIDs(t, db, "cachorro late", 1); len(got) != 1 || got[0] != "100" {
		t.Errorf("depois do UPDATE: %v", got)
	}

	if err := db.DeleteRows("docs", []RID{ridOf("100")}); err != nil {
		t.Fatal(err)
	}
	for _, id := range searchIDs(t, db, "cachorro late", 50) {
		if id == "100" {
			t.Error("a linha apagada continua na busca")
		}
	}
	if got := searchIDs(t, db, "x", 50); len(got) != 20 {
		t.Errorf("%d vetores depois do DELETE, queria 20", len(got))
	}
}

func TestCreateVectorIndex(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir, testPageSize)
	if err != nil {
		t.Fatal(err)
	}
	db.Embed = embed.Fake(32)
	if err := db.CreateTable("p", userColumns, nil); err != nil {
		t.Fatal(err)
	}
	// Sem índice vetorial, a tabela não precisa de embedder.
	db.Embed = nil
	if err := db.InsertRows("p", [][]Value{texts("1", "Ana Souza"), texts("2", "Bia Lima")}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateVectorIndex("p_vec", "p", []string{"nome"}); err == nil || !strings.Contains(err.Error(), "-setup") {
		t.Errorf("sem embedder: %v", err)
	}
	if _, err := os.Stat(db.tablePath("p_vec")); !os.IsNotExist(err) {
		t.Error("o arquivo do índice ficou para trás depois do erro")
	}

	db.Embed = embed.Fake(32)
	if err := db.CreateVectorIndex("p_vec", "p", []string{"nome"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateVectorIndex("p_vec2", "p", []string{"nome"}); err == nil {
		t.Error("segundo índice vetorial na mesma tabela foi aceito")
	}

	// O catálogo guarda o índice, e as linhas antigas já têm vetor.
	db, err = Open(dir, testPageSize)
	if err != nil {
		t.Fatal(err)
	}
	db.Embed = embed.Fake(32)
	results, err := db.Search("p", "Bia", 1)
	if err != nil || len(results) != 1 || results[0].Values[1].Text != "Bia Lima" {
		t.Errorf("busca depois de reabrir: %v, %v", results, err)
	}
	var found bool
	for row, err := range db.Scan(indexesName) {
		if err != nil {
			t.Fatal(err)
		}
		if row.Values[0].Text == "p_vec" {
			found = true
			got := []Value{row.Values[1], row.Values[2], row.Values[3], row.Values[5]}
			if want := []Value{Text("p"), Text("vector"), Text("nome"), Null}; !slices.Equal(got, want) {
				t.Errorf("_indexes: %v, queria %v", got, want)
			}
		}
	}
	if !found {
		t.Error("o índice vetorial não aparece em _indexes")
	}

	if err := db.DropIndex("p_vec"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Search("p", "Bia", 1); err == nil || !strings.Contains(err.Error(), "não tem índice vetorial") {
		t.Errorf("busca sem índice: %v", err)
	}
	if _, err := os.Stat(db.tablePath("p_vec")); !os.IsNotExist(err) {
		t.Error("DROP INDEX não apagou o arquivo")
	}
}

func TestVectorErrors(t *testing.T) {
	db, _ := openDocs(t)
	row := [][]Value{texts("1", "a", "b")}

	db.Embed = nil
	if err := db.InsertRows("docs", row); err == nil || !strings.Contains(err.Error(), "-setup") {
		t.Errorf("sem embedder: %v", err)
	}

	db.Embed = func([]string) ([][]float32, error) { return nil, errors.New("API fora do ar") }
	if err := db.InsertRows("docs", row); err == nil || !strings.Contains(err.Error(), "fora do ar") {
		t.Errorf("API com erro: %v", err)
	}
	if table, err := db.ReadTable("docs"); err != nil || len(table.Rows) != 0 {
		t.Errorf("a API falhou, mas a tabela ficou com linhas: %v, %v", table, err)
	}

	db.Embed = embed.Fake(2000)
	if err := db.InsertRows("docs", row); err == nil || !strings.Contains(err.Error(), "page_size maior") {
		t.Errorf("vetor maior que a página: %v", err)
	}

	db.Embed = embed.Fake(64)
	if err := db.InsertRows("docs", row); err != nil {
		t.Fatal(err)
	}
	db.Embed = embed.Fake(32)
	if err := db.InsertRows("docs", [][]Value{texts("2", "c", "d")}); err == nil || !strings.Contains(err.Error(), "apague o índice") {
		t.Errorf("modelo trocado no INSERT: %v", err)
	}
	if _, err := db.Search("docs", "c", 1); err == nil || !strings.Contains(err.Error(), "apague o índice") {
		t.Errorf("modelo trocado na busca: %v", err)
	}
	if _, err := db.Search("docs", "c", 0); err == nil {
		t.Error("TOP 0 foi aceito")
	}

	err := db.CreateTable("dois", docColumns, []Index{{Kind: VectorIndex, Columns: []string{"titulo"}}, {Kind: VectorIndex, Columns: []string{"corpo"}}})
	if err == nil {
		t.Error("tabela com dois índices vetoriais foi aceita")
	}
	err = db.CreateTable("sem", docColumns, []Index{{Kind: VectorIndex, Columns: []string{"nada"}}})
	if err == nil {
		t.Error("índice vetorial com coluna que não existe foi aceito")
	}
}

func TestDropTableWithVector(t *testing.T) {
	db, _ := openDocs(t)
	if err := db.DropTable("docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(db.tablePath("docs_embedding")); !os.IsNotExist(err) {
		t.Error("DROP TABLE não apagou o arquivo do índice vetorial")
	}
}

func TestQuantize(t *testing.T) {
	v := []float32{0.12, -0.5, 0.33, 0.01, -0.07}
	if got := cosine(v, quantize(v)); math.Abs(got-1) > 0.001 {
		t.Errorf("cosseno do vetor com ele mesmo quantizado = %v", got)
	}
	opposite := []float32{-0.12, 0.5, -0.33, -0.01, 0.07}
	if got := cosine(opposite, quantize(v)); math.Abs(got+1) > 0.001 {
		t.Errorf("cosseno com o oposto = %v", got)
	}
	if got := cosine(v, quantize(make([]float32, 5))); got != 0 {
		t.Errorf("cosseno com o vetor nulo = %v", got)
	}
}
