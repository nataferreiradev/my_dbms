package parser

import (
	"fmt"

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
	var stmt Statement
	var err error
	switch tokens[0].Value {
	case "CREATE":
		stmt, err = p.createTable()
	case "INSERT":
		stmt, err = p.insert()
	case "SELECT":
		stmt, err = p.selectStmt()
	default:
		return nil, fmt.Errorf("comando não suportado: %s", tokens[0].Value)
	}
	if err != nil {
		return nil, err
	}
	if !p.done() {
		return nil, p.unexpected("o fim do comando")
	}
	return stmt, nil
}

type parser struct {
	tokens []sql.Token
	pos    int
}

// createTable lê
//
//	CREATE TABLE nome (coluna TIPO [PRIMARY KEY | UNIQUE], ...,
//	                   PRIMARY KEY (a, b), UNIQUE (c))
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
	return stmt, err
}

// columnDef lê "nome TIPO" seguido de PRIMARY KEY ou UNIQUE, se houver.
func (p *parser) columnDef(stmt *CreateTable) error {
	name, err := p.ident()
	if err != nil {
		return err
	}
	typ := p.peek().Value
	if !p.accept("INT") && !p.accept("TEXT") {
		return p.unexpected("um tipo (INT ou TEXT)")
	}
	stmt.Columns = append(stmt.Columns, ColumnDef{Name: name, Type: typ})

	for {
		primary, isKey, err := p.keyPrefix()
		if err != nil || !isKey {
			return err
		}
		stmt.Keys = append(stmt.Keys, KeyDef{Primary: primary, Columns: []string{name}})
	}
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

// insert lê INSERT INTO tabela VALUES (valor, ...).
func (p *parser) insert() (*Insert, error) {
	if err := p.expect("INSERT", "INTO"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expect("VALUES"); err != nil {
		return nil, err
	}

	stmt := &Insert{Table: table}
	err = p.list(func() error {
		lit, err := p.literal()
		stmt.Values = append(stmt.Values, lit)
		return err
	})
	return stmt, err
}

// selectStmt lê SELECT * FROM tabela ou SELECT a, b FROM tabela.
func (p *parser) selectStmt() (*Select, error) {
	if err := p.expect("SELECT"); err != nil {
		return nil, err
	}
	stmt := &Select{}
	if !p.accept("*") {
		for {
			name, err := p.ident()
			if err != nil {
				return nil, err
			}
			stmt.Columns = append(stmt.Columns, name)
			if !p.accept(",") {
				break
			}
		}
	}
	if err := p.expect("FROM"); err != nil {
		return nil, err
	}
	table, err := p.ident()
	stmt.Table = table
	return stmt, err
}

// literal lê um número ou uma string.
func (p *parser) literal() (Literal, error) {
	t := p.peek()
	switch t.Type {
	case sql.Number:
		p.pos++
		return Literal{Kind: Number, Text: t.Value}, nil
	case sql.String:
		p.pos++
		return Literal{Kind: String, Text: t.Value}, nil
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
	if p.done() {
		return sql.Token{}
	}
	return p.tokens[p.pos]
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

// ident consome um nome (de tabela ou coluna).
func (p *parser) ident() (string, error) {
	t := p.peek()
	if t.Type != sql.Identifier {
		return "", p.unexpected("um nome")
	}
	p.pos++
	return t.Value, nil
}

// unexpected monta o erro "esperava X, veio Y" com a posição do token atual.
func (p *parser) unexpected(want string) error {
	if p.done() {
		return fmt.Errorf("esperava %s, mas o comando acabou", want)
	}
	t := p.peek()
	return fmt.Errorf("posição %d: esperava %s, veio %q", t.Pos+1, want, t.Value)
}
