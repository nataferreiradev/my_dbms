package sql

type TokenType string

const (
	Keyword    TokenType = "KEYWORD"
	Identifier TokenType = "IDENTIFIER"
	Operator   TokenType = "OPERATOR"
	Number     TokenType = "NUMBER"
	String     TokenType = "STRING"
	Symbol     TokenType = "SYMBOL"
)

// Token é um pedaço classificado da entrada. Pos é a posição do primeiro
// caractere, contando a partir de 0. Em String, Value é o texto já sem as
// aspas.
type Token struct {
	Type  TokenType
	Value string
	Pos   int
}

var keywords = map[string]bool{
	"SELECT":  true,
	"FROM":    true,
	"WHERE":   true,
	"AND":     true,
	"OR":      true,
	"CREATE":  true,
	"TABLE":   true,
	"INSERT":  true,
	"INTO":    true,
	"VALUES":  true,
	"INT":     true,
	"TEXT":    true,
	"PRIMARY": true,
	"KEY":     true,
	"UNIQUE":  true,
	"NOT":     true,
	"NULL":    true,
}

var operators = map[string]bool{
	"*":  true,
	"=":  true,
	"<":  true,
	">":  true,
	"<=": true,
	">=": true,
	"<>": true,
}

// operatorChars são os caracteres que começam um operador. Eles separam
// palavras mesmo sem espaço: "id=1" vira [id] [=] [1].
const operatorChars = "*=<>"

const symbols = ";,()"
