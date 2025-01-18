package lexicalanalyzer

const (
	KEYWORD    = "KEYWORD"
	IDENTIFIER = "IDENTIFIER"
	OPERATOR   = "OPERATOR"
	LITERAL    = "LITERAL"
	SYMBOL     = "SYMBOL"
	DELIMITER  = "DELIMITER"
)

var KeywordMap = map[string]string{
	"SELECT": KEYWORD,
	"FROM":   KEYWORD,
	"WHERE":  KEYWORD,
}

type Token struct {
	tokenType string
	position  int
	value     string
}

func NewToken(tokenType, value string, position int) Token {
	return Token{
		tokenType: tokenType,
		value:     value,
		position:  position,
	}
}

type TokenError struct {
	Position int
	Message  string
}

func NewTokenError(position int, message string) *TokenError {
	return &TokenError{Position: position, Message: message}
}

type LexicalAnalyzer interface {
	MakeTokens([]string) ([]Token, *TokenError)
}
