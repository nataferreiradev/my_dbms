package parser

// Statement é um comando já analisado. O tipo concreto diz qual:
// *CreateTable, *Insert, *Select, *Update, *Delete ou *DropTable.
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

// Select é SELECT colunas FROM tabela [WHERE condição]. Columns nil quer
// dizer *, e Where nil quer dizer todas as linhas.
type Select struct {
	Table   string
	Columns []string
	Where   Expr
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

func (*CreateTable) statement() {}
func (*Insert) statement()      {}
func (*Select) statement()      {}
func (*Update) statement()      {}
func (*Delete) statement()      {}
func (*DropTable) statement()   {}

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

// ColumnRef é o nome de uma coluna usado numa condição.
type ColumnRef struct {
	Name string
}

func (*Logical) expr()    {}
func (*Comparison) expr() {}
func (*IsNull) expr()     {}
func (ColumnRef) expr()   {}
func (Literal) expr()     {}
