package parser

// Statement é um comando já analisado. O tipo concreto diz qual:
// *CreateTable, *Insert, *Select, *Update, *Delete, *DropTable, *CreateIndex,
// *DropIndex ou *Explain.
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
	Columns []ColumnRef
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

// CreateIndex é CREATE INDEX nome ON tabela (colunas).
type CreateIndex struct {
	Name    string
	Table   string
	Columns []string
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

func (*CreateTable) statement() {}
func (*Insert) statement()      {}
func (*Select) statement()      {}
func (*Update) statement()      {}
func (*Delete) statement()      {}
func (*DropTable) statement()   {}
func (*CreateIndex) statement() {}
func (*DropIndex) statement()   {}
func (*Explain) statement()     {}

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
