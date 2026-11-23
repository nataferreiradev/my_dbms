package parser

import (
	"fmt"
	"strconv"
	"strings"

	"my_DBMS/internal/sql"
)

// Parse transforma os tokens de um comando em um Statement. O ; no fim é
// opcional. É um parser descendente recursivo: cada regra da gramática é um
// método que consome os tokens dela.
func Parse(tokens []sql.Token) (Statement, error) {
	if n := len(tokens); n > 0 && tokens[n-1].Type == sql.Symbol && tokens[n-1].Value == ";" {
		tokens = tokens[:n-1]
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("comando vazio")
	}

	p := &parser{tokens: tokens}
	stmt, err := p.statement()
	if err != nil {
		return nil, err
	}
	if !p.done() {
		return nil, p.unexpected("o fim do comando")
	}
	return stmt, nil
}

// statement escolhe a regra pelo primeiro token (e o segundo, quando o
// primeiro não basta).
func (p *parser) statement() (Statement, error) {
	switch p.peek().Value {
	case "CREATE":
		if p.peekAt(1).Value == "INDEX" || p.peekAt(1).Value == "VECTOR" {
			return p.createIndex()
		}
		return p.createTable()
	case "SEARCH":
		return p.search()
	case "INSERT":
		return p.insert()
	case "SELECT":
		return p.selectStmt()
	case "UPDATE":
		return p.update()
	case "DELETE":
		return p.delete()
	case "DROP":
		if p.peekAt(1).Value == "INDEX" {
			return p.dropIndex()
		}
		return p.dropTable()
	case "EXPLAIN":
		return p.explain()
	}
	return nil, fmt.Errorf("comando não suportado: %s", p.peek().Value)
}

type parser struct {
	tokens []sql.Token
	pos    int
}

// createTable lê
//
//	CREATE TABLE nome (coluna TIPO [NOT NULL] [PRIMARY KEY | UNIQUE], ...,
//	                   PRIMARY KEY (a, b), UNIQUE (c))
//	                  [WITH EMBEDDING (a, b)]
func (p *parser) createTable() (*CreateTable, error) {
	if err := p.expect("CREATE", "TABLE"); err != nil {
		return nil, err
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}

	stmt := &CreateTable{Name: name}
	err = p.list(func() error {
		primary, isKey, err := p.keyPrefix()
		if err != nil {
			return err
		}
		if isKey {
			cols, err := p.names()
			stmt.Keys = append(stmt.Keys, KeyDef{Primary: primary, Columns: cols})
			return err
		}
		return p.columnDef(stmt)
	})
	if err != nil {
		return nil, err
	}
	if p.accept("WITH") {
		if err := p.expect("EMBEDDING"); err != nil {
			return nil, err
		}
		if stmt.Embedding, err = p.names(); err != nil {
			return nil, err
		}
	}
	return stmt, nil
}

// columnDef lê "nome TIPO" seguido de NOT NULL, PRIMARY KEY ou UNIQUE, em
// qualquer ordem.
func (p *parser) columnDef(stmt *CreateTable) error {
	name, err := p.ident()
	if err != nil {
		return err
	}
	typ := p.peek().Value
	if !p.accept("INT") && !p.accept("REAL") && !p.accept("TEXT") && !p.accept("BOOLEAN") {
		return p.unexpected("um tipo (INT, REAL, TEXT ou BOOLEAN)")
	}
	col := ColumnDef{Name: name, Type: typ}

	for {
		if p.accept("NOT") {
			if err := p.expect("NULL"); err != nil {
				return err
			}
			col.NotNull = true
			continue
		}
		primary, isKey, err := p.keyPrefix()
		if err != nil {
			return err
		}
		if !isKey {
			break
		}
		stmt.Keys = append(stmt.Keys, KeyDef{Primary: primary, Columns: []string{name}})
	}
	stmt.Columns = append(stmt.Columns, col)
	return nil
}

// keyPrefix consome PRIMARY KEY ou UNIQUE, se vier um deles.
func (p *parser) keyPrefix() (primary, isKey bool, err error) {
	switch {
	case p.accept("PRIMARY"):
		return true, true, p.expect("KEY")
	case p.accept("UNIQUE"):
		return false, true, nil
	}
	return false, false, nil
}

// insert lê INSERT INTO tabela [(a, b)] VALUES (valor, ...), (valor, ...).
func (p *parser) insert() (*Insert, error) {
	if err := p.expect("INSERT", "INTO"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	stmt := &Insert{Table: table}
	if p.peek().Value == "(" {
		if stmt.Columns, err = p.names(); err != nil {
			return nil, err
		}
	}
	if err := p.expect("VALUES"); err != nil {
		return nil, err
	}

	for {
		var row []Literal
		err := p.list(func() error {
			lit, err := p.literal()
			row = append(row, lit)
			return err
		})
		if err != nil {
			return nil, err
		}
		stmt.Rows = append(stmt.Rows, row)
		if !p.accept(",") {
			return stmt, nil
		}
	}
}

// selectStmt lê SELECT * FROM tabela ou SELECT a, t.b FROM tabela, com JOINs
// e WHERE opcionais.
func (p *parser) selectStmt() (*Select, error) {
	if err := p.expect("SELECT"); err != nil {
		return nil, err
	}
	stmt := &Select{}
	if !p.accept("*") {
		for {
			col, err := p.columnRef()
			if err != nil {
				return nil, err
			}
			stmt.Columns = append(stmt.Columns, col)
			if !p.accept(",") {
				break
			}
		}
	}
	if err := p.expect("FROM"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	stmt.Table = table
	if stmt.Joins, err = p.joins(); err != nil {
		return nil, err
	}
	stmt.Where, err = p.where()
	return stmt, err
}

// joins lê os JOINs que vierem depois do FROM, cada um com o seu ON:
//
//	[INNER] JOIN t ON condição
//	LEFT [OUTER] JOIN t ON condição
//	RIGHT [OUTER] JOIN t ON condição
func (p *parser) joins() ([]Join, error) {
	var joins []Join
	for {
		kind := "INNER"
		switch {
		case p.accept("LEFT"):
			kind = "LEFT"
			p.accept("OUTER")
		case p.accept("RIGHT"):
			kind = "RIGHT"
			p.accept("OUTER")
		case p.accept("INNER"):
		case p.peek().Value != "JOIN":
			return joins, nil
		}
		if err := p.expect("JOIN"); err != nil {
			return nil, err
		}
		table, err := p.ident()
		if err != nil {
			return nil, err
		}
		if err := p.expect("ON"); err != nil {
			return nil, err
		}
		on, err := p.or()
		if err != nil {
			return nil, err
		}
		joins = append(joins, Join{Kind: kind, Table: table, On: on})
	}
}

// update lê UPDATE tabela SET a = valor, b = valor [WHERE condição].
func (p *parser) update() (*Update, error) {
	if err := p.expect("UPDATE"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expect("SET"); err != nil {
		return nil, err
	}

	stmt := &Update{Table: table}
	for {
		col, err := p.ident()
		if err != nil {
			return nil, err
		}
		if err := p.expect("="); err != nil {
			return nil, err
		}
		value, err := p.literal()
		if err != nil {
			return nil, err
		}
		stmt.Set = append(stmt.Set, Assignment{Column: col, Value: value})
		if !p.accept(",") {
			break
		}
	}
	stmt.Where, err = p.where()
	return stmt, err
}

// delete lê DELETE FROM tabela [WHERE condição].
func (p *parser) delete() (*Delete, error) {
	if err := p.expect("DELETE", "FROM"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	stmt := &Delete{Table: table}
	stmt.Where, err = p.where()
	return stmt, err
}

// dropTable lê DROP TABLE nome.
func (p *parser) dropTable() (*DropTable, error) {
	if err := p.expect("DROP", "TABLE"); err != nil {
		return nil, err
	}
	name, err := p.ident()
	return &DropTable{Name: name}, err
}

// createIndex lê CREATE [VECTOR] INDEX nome ON tabela (a, b).
func (p *parser) createIndex() (*CreateIndex, error) {
	if err := p.expect("CREATE"); err != nil {
		return nil, err
	}
	vector := p.accept("VECTOR")
	if err := p.expect("INDEX"); err != nil {
		return nil, err
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expect("ON"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	columns, err := p.names()
	return &CreateIndex{Name: name, Table: table, Columns: columns, Vector: vector}, err
}

// search lê SEARCH tabela FOR 'texto' [TOP n].
func (p *parser) search() (*Search, error) {
	if err := p.expect("SEARCH"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expect("FOR"); err != nil {
		return nil, err
	}
	query := p.peek()
	if query.Type != sql.String {
		return nil, p.unexpected("o texto da busca, entre aspas")
	}
	p.pos++
	stmt := &Search{Table: table, Query: query.Value, Top: DefaultTop}
	if p.accept("TOP") {
		n, err := strconv.Atoi(p.peek().Value)
		if p.peek().Type != sql.Number || err != nil || n <= 0 {
			return nil, p.unexpected("um número inteiro maior que zero")
		}
		p.pos++
		stmt.Top = n
	}
	return stmt, nil
}

// dropIndex lê DROP INDEX nome.
func (p *parser) dropIndex() (*DropIndex, error) {
	if err := p.expect("DROP", "INDEX"); err != nil {
		return nil, err
	}
	name, err := p.ident()
	return &DropIndex{Name: name}, err
}

// explain lê EXPLAIN seguido de SELECT, UPDATE ou DELETE.
func (p *parser) explain() (*Explain, error) {
	if err := p.expect("EXPLAIN"); err != nil {
		return nil, err
	}
	var stmt Statement
	var err error
	switch p.peek().Value {
	case "SELECT":
		stmt, err = p.selectStmt()
	case "UPDATE":
		stmt, err = p.update()
	case "DELETE":
		stmt, err = p.delete()
	default:
		return nil, p.unexpected("SELECT, UPDATE ou DELETE")
	}
	return &Explain{Statement: stmt}, err
}

// where lê "WHERE condição", se vier. A precedência é a do SQL: OR liga mais
// fraco que AND, que liga mais fraco que as comparações, então
// "a = 1 OR b = 2 AND c = 3" é "a = 1 OR (b = 2 AND c = 3)".
func (p *parser) where() (Expr, error) {
	if !p.accept("WHERE") {
		return nil, nil
	}
	return p.or()
}

func (p *parser) or() (Expr, error) {
	return p.logical("OR", p.and)
}

func (p *parser) and() (Expr, error) {
	return p.logical("AND", p.comparison)
}

// logical lê "x op x op x ...", em que cada x é lido por operand, e junta
// tudo da esquerda para a direita.
func (p *parser) logical(op string, operand func() (Expr, error)) (Expr, error) {
	left, err := operand()
	if err != nil {
		return nil, err
	}
	for p.accept(op) {
		right, err := operand()
		if err != nil {
			return nil, err
		}
		left = &Logical{Op: op, Left: left, Right: right}
	}
	return left, nil
}

// comparison lê "a op b", "a IS [NOT] NULL" ou uma condição entre parênteses.
func (p *parser) comparison() (Expr, error) {
	if p.accept("(") {
		e, err := p.or()
		if err != nil {
			return nil, err
		}
		return e, p.expect(")")
	}

	left, err := p.operand()
	if err != nil {
		return nil, err
	}
	if p.accept("IS") {
		not := p.accept("NOT")
		return &IsNull{Expr: left, Not: not}, p.expect("NULL")
	}

	op := p.peek()
	if op.Type != sql.Operator || op.Value == "*" {
		return nil, p.unexpected("um operador de comparação (=, <>, <, >, <=, >=)")
	}
	p.pos++
	right, err := p.operand()
	if err != nil {
		return nil, err
	}
	return &Comparison{Op: op.Value, Left: left, Right: right}, nil
}

// operand lê um nome de coluna ou um valor.
func (p *parser) operand() (Expr, error) {
	if p.peek().Type == sql.Identifier {
		return p.columnRef()
	}
	lit, err := p.literal()
	if err != nil {
		return nil, p.unexpected("uma coluna ou um valor")
	}
	return lit, nil
}

// literal lê um número, uma string, TRUE, FALSE ou NULL.
func (p *parser) literal() (Literal, error) {
	t := p.peek()
	switch {
	case t.Type == sql.Number:
		p.pos++
		return Literal{Kind: Number, Text: t.Value}, nil
	case t.Type == sql.String:
		p.pos++
		return Literal{Kind: String, Text: t.Value}, nil
	case p.accept("TRUE"):
		return Literal{Kind: Bool, Text: "TRUE"}, nil
	case p.accept("FALSE"):
		return Literal{Kind: Bool, Text: "FALSE"}, nil
	case p.accept("NULL"):
		return Literal{Kind: Null, Text: "NULL"}, nil
	}
	return Literal{}, p.unexpected("um valor")
}

// list lê "(item, item, ...)", chamando item para cada elemento.
func (p *parser) list(item func() error) error {
	if err := p.expect("("); err != nil {
		return err
	}
	for {
		if err := item(); err != nil {
			return err
		}
		if p.accept(")") {
			return nil
		}
		if !p.accept(",") {
			return p.unexpected(`"," ou ")"`)
		}
	}
}

// names lê "(a, b, ...)" em que cada item é um nome.
func (p *parser) names() ([]string, error) {
	var names []string
	err := p.list(func() error {
		name, err := p.ident()
		names = append(names, name)
		return err
	})
	return names, err
}

func (p *parser) done() bool {
	return p.pos >= len(p.tokens)
}

// peek devolve o token atual sem consumir. No fim, um token vazio.
func (p *parser) peek() sql.Token {
	return p.peekAt(0)
}

// peekAt devolve o token k posições depois do atual, sem consumir.
func (p *parser) peekAt(k int) sql.Token {
	if p.pos+k >= len(p.tokens) {
		return sql.Token{}
	}
	return p.tokens[p.pos+k]
}

// accept consome o token atual se ele for a keyword, o símbolo ou o operador
// value.
func (p *parser) accept(value string) bool {
	t := p.peek()
	if p.done() || t.Value != value || t.Type == sql.String || t.Type == sql.Identifier {
		return false
	}
	p.pos++
	return true
}

// expect consome os valores em sequência, ou dá erro no primeiro que faltar.
func (p *parser) expect(values ...string) error {
	for _, v := range values {
		if !p.accept(v) {
			return p.unexpected(fmt.Sprintf("%q", v))
		}
	}
	return nil
}

// ident consome um nome simples (de tabela, coluna ou índice), sem ponto.
func (p *parser) ident() (string, error) {
	t := p.peek()
	if t.Type != sql.Identifier || strings.Contains(t.Value, ".") {
		return "", p.unexpected("um nome")
	}
	p.pos++
	return t.Value, nil
}

// columnRef consome uma coluna: nome ou tabela.nome. O lexer já garantiu
// que há no máximo um ponto, com um nome de cada lado.
func (p *parser) columnRef() (ColumnRef, error) {
	t := p.peek()
	if t.Type != sql.Identifier {
		return ColumnRef{}, p.unexpected("um nome")
	}
	p.pos++
	if table, name, ok := strings.Cut(t.Value, "."); ok {
		return ColumnRef{Table: table, Name: name}, nil
	}
	return ColumnRef{Name: t.Value}, nil
}

// unexpected monta o erro "esperava X, veio Y" com a posição do token atual.
func (p *parser) unexpected(want string) error {
	if p.done() {
		return fmt.Errorf("esperava %s, mas o comando acabou", want)
	}
	t := p.peek()
	return fmt.Errorf("posição %d: esperava %s, veio %q", t.Pos+1, want, t.Value)
}
