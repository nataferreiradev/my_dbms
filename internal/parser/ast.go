package parser

// Statement é um comando já analisado. O tipo concreto diz qual:
// *CreateTable, *Insert, *Select, *Update, *Delete, *DropTable, *CreateIndex,
// *DropIndex, *Explain ou *Search.
type Statement interface {
	statement()
}

// CreateTable é CREATE TABLE nome (colunas e chaves) [WITH EMBEDDING (colunas)].
// Embedding são as colunas do índice vetorial; nil é sem índice vetorial.
type CreateTable struct {
	Name      string
	Columns   []ColumnDef
	Keys      []KeyDef
	Embedding []string
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

// Select é SELECT colunas FROM tabela [JOINs] [WHERE condição]. Columns nil
// quer dizer *, e Where nil quer dizer todas as linhas.
type Select struct {
	Table   string
	Joins   []Join
	Columns []ColumnRef
	Where   Expr
}

// Join é um JOIN depois do FROM: [INNER] JOIN, LEFT [OUTER] JOIN ou
// RIGHT [OUTER] JOIN tabela ON condição. Kind é INNER, LEFT ou RIGHT.
type Join struct {
	Kind  string
	Table string
	On    Expr
}

// Update é UPDATE tabela SET a = valor, ... [WHERE condição].
type Update struct {
	Table string
	Set   []Assignment
	Where Expr
}

// Assignment é um "coluna = valor" do SET.
type Assignment struct {
	Column string
	Value  Literal
}

// Delete é DELETE FROM tabela [WHERE condição].
type Delete struct {
	Table string
	Where Expr
}

// DropTable é DROP TABLE nome.
type DropTable struct {
	Name string
}

// CreateIndex é CREATE [VECTOR] INDEX nome ON tabela (colunas). Vector é o
// índice vetorial, para o SEARCH.
type CreateIndex struct {
	Name    string
	Table   string
	Columns []string
	Vector  bool
}

// DropIndex é DROP INDEX nome.
type DropIndex struct {
	Name string
}

// Explain é EXPLAIN seguido de um SELECT, UPDATE ou DELETE: mostra como as
// linhas seriam buscadas, sem rodar o comando.
type Explain struct {
	Statement Statement
}

// Search é SEARCH tabela FOR 'texto' [TOP n]: as n linhas (10 sem TOP) mais
// parecidas com o texto, pelo índice vetorial da tabela.
type Search struct {
	Table string
	Query string
	Top   int
}

// DefaultTop é quantas linhas o SEARCH devolve sem TOP.
const DefaultTop = 10

func (*CreateTable) statement() {}
func (*Insert) statement()      {}
func (*Select) statement()      {}
func (*Update) statement()      {}
func (*Delete) statement()      {}
func (*DropTable) statement()   {}
func (*CreateIndex) statement() {}
func (*DropIndex) statement()   {}
func (*Explain) statement()     {}
func (*Search) statement()      {}

type LiteralKind int

const (
	Number LiteralKind = iota
	String
	Bool
	Null
)

// Literal é um valor escrito no comando: 42, -1.5, 'Ana', TRUE, FALSE ou
// NULL. Text é o texto como veio, sem aspas (TRUE e FALSE em maiúsculo).
type Literal struct {
	Kind LiteralKind
	Text string
}

// Expr é uma condição do WHERE ou um operando dela. O tipo concreto diz qual:
// *Logical, *Comparison, *IsNull, ColumnRef ou Literal.
type Expr interface {
	expr()
}

// Logical é a AND b ou a OR b.
type Logical struct {
	Op          string
	Left, Right Expr
}

// Comparison é a op b, com op sendo =, <>, <, >, <= ou >=.
type Comparison struct {
	Op          string
	Left, Right Expr
}

// IsNull é a IS NULL ou, com Not, a IS NOT NULL.
type IsNull struct {
	Expr Expr
	Not  bool
}

// ColumnRef é uma coluna usada no SELECT ou numa condição: coluna ou
// tabela.coluna. Table vazio quer dizer que o nome não veio qualificado.
type ColumnRef struct {
	Table string
	Name  string
}

// String escreve a coluna como no SQL.
func (c ColumnRef) String() string {
	if c.Table == "" {
		return c.Name
	}
	return c.Table + "." + c.Name
}

func (*Logical) expr()    {}
func (*Comparison) expr() {}
func (*IsNull) expr()     {}
func (ColumnRef) expr()   {}
func (Literal) expr()     {}
