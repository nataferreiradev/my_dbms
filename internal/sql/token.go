package sql

type TokenType string

const (
	Keyword    TokenType = "KEYWORD"
	Identifier TokenType = "IDENTIFIER"
	Operator   TokenType = "OPERATOR"
	Literal    TokenType = "LITERAL"
	Symbol     TokenType = "SYMBOL"
)

type Token struct {
	Type  TokenType
	Value string
	Pos   int
}

var keywords = map[string]bool{
	"SELECT": true,
	"FROM":   true,
	"WHERE":  true,
	"AND":    true,
	"OR":     true,
	"CREATE": true,
	"TABLE":  true,
	"INSERT": true,
	"INTO":   true,
	"VALUES": true,
	"INT":    true,
	"TEXT":   true,
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

const symbols = ";,()"
