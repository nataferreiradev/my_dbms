package parser

// Statement é um comando já analisado. O tipo concreto diz qual:
// *CreateTable, *Insert ou *Select.
type Statement interface {
	statement()
}

// CreateTable é CREATE TABLE nome (colunas e chaves).
type CreateTable struct {
	Name    string
	Columns []ColumnDef
	Keys    []KeyDef
}

type ColumnDef struct {
	Name    string
	Type    string
	NotNull bool
}

// KeyDef é uma chave primária (Primary) ou única sobre uma ou mais colunas.
type KeyDef struct {
	Primary bool
	Columns []string
}

// Insert é INSERT INTO tabela [(colunas)] VALUES (valores), (valores)...
// Columns nil quer dizer todas as colunas, na ordem da tabela.
type Insert struct {
	Table   string
	Columns []string
	Rows    [][]Literal
}

// Select é SELECT colunas FROM tabela. Columns nil quer dizer *.
type Select struct {
	Table   string
	Columns []string
}

func (*CreateTable) statement() {}
func (*Insert) statement()      {}
func (*Select) statement()      {}

type LiteralKind int

const (
	Number LiteralKind = iota
	String
	Null
)

// Literal é um valor escrito no comando: 42, -1.5, 'Ana' ou NULL. Text é o
// texto como veio, sem aspas.
type Literal struct {
	Kind LiteralKind
	Text string
}
