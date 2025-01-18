package v1

import (
	"errors"
	lexicalanalyzer "my_DBMS/internal/sqlParser/lexicalAnalyzer"
	"regexp"
	"strconv"
)

type LexicalAnalyzerV1 struct{}

func (lexicalAnalyzer *LexicalAnalyzerV1) MakeTokens(tokens []string) ([]lexicalanalyzer.Token, *lexicalanalyzer.TokenError) {
	var validTokens = []lexicalanalyzer.Token{}

	for i, token := range tokens {
		tokenType, err := getTokenType(token)
		if err != nil {
			return nil, lexicalanalyzer.NewTokenError(i, err.Error())
		}
		validTokens = append(validTokens, lexicalanalyzer.NewToken(tokenType, token, i))
	}

	return validTokens, nil
}

var symbols = regexp.MustCompile(`\s+|[;,\(\)]`)
var startNumber = regexp.MustCompile(`^\d`)
var invalidIndentifierSymbols = regexp.MustCompile(`[@\-\\\/]`)

func getTokenType(value string) (string, error) {
	if !isValidToken(value) {
		return "", errors.New("Invalid Token")
	}
	if tokenType, exists := lexicalanalyzer.KeywordMap[value]; exists {
		return tokenType, nil
	}
	if isLiteral(value) {
		return lexicalanalyzer.LITERAL, nil
	}
	return lexicalanalyzer.IDENTIFIER, nil
}

func isValidToken(value string) bool {
	return isSymbol(value) || isKeyWord(value) || isLiteral(value) || isIndentifier(value)
}

func isLiteral(value string) bool {
	if _, err := strconv.Atoi(value); err == nil {
		return true
	}
	if len(value) > 1 && value[0] == '"' && value[len(value)-1] == '"' {
		return true
	}
	return false
}

func isSymbol(value string) bool {
	return symbols.MatchString(value)
}

func isKeyWord(value string) bool {
	_, exist := lexicalanalyzer.KeywordMap[value]
	return exist
}

func isIndentifier(value string) bool {
	startWithNumber := startNumber.MatchString(value)
	containsInvalidSymbols := invalidIndentifierSymbols.MatchString(value)
	return !startWithNumber && !containsInvalidSymbols
}
