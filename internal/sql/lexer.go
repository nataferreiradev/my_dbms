package sql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Lex transforma uma string SQL em tokens classificados.
// Keywords são normalizadas para maiúsculo; o resto mantém o texto original.
func Lex(input string) ([]Token, error) {
	words := tokenize(input)
	tokens := make([]Token, 0, len(words))

	for i, word := range words {
		typ, err := classify(word)
		if err != nil {
			return nil, fmt.Errorf("posição %d: %w", i, err)
		}
		if typ == Keyword {
			word = strings.ToUpper(word)
		}
		tokens = append(tokens, Token{Type: typ, Value: word, Pos: i})
	}

	return tokens, nil
}

// tokenize quebra a entrada em palavras, separando símbolos como ; , ( )
// e mantendo strings entre aspas inteiras.
func tokenize(input string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for _, r := range input {
		switch {
		case r == '"':
			current.WriteRune(r)
			inQuote = !inQuote
		case inQuote:
			current.WriteRune(r)
		case unicode.IsSpace(r):
			flush()
		case strings.ContainsRune(symbols, r):
			flush()
			tokens = append(tokens, string(r))
		default:
			current.WriteRune(r)
		}
	}
	flush()

	return tokens
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func classify(word string) (TokenType, error) {
	switch {
	case keywords[strings.ToUpper(word)]:
		return Keyword, nil
	case len(word) == 1 && strings.Contains(symbols, word):
		return Symbol, nil
	case operators[word]:
		return Operator, nil
	case isLiteral(word):
		return Literal, nil
	case identifierPattern.MatchString(word):
		return Identifier, nil
	}
	return "", fmt.Errorf("token inválido %q", word)
}

func isLiteral(word string) bool {
	if _, err := strconv.Atoi(word); err == nil {
		return true
	}
	return len(word) > 1 && word[0] == '"' && word[len(word)-1] == '"'
}
