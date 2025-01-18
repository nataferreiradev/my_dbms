package lexicalanalyzer_test

import (
	lexicalanalyzer "my_DBMS/internal/sqlParser/lexicalAnalyzer"
	v1 "my_DBMS/internal/sqlParser/lexicalAnalyzer/implementation/v1"
	"testing"
)

func TestLexicalAnalyzerValidInput(t *testing.T) {
	expectSizeTokens := 5
	expectTokenSelect := lexicalanalyzer.NewToken(lexicalanalyzer.KEYWORD, "SELECT", 0)
	expectTokenFrom := lexicalanalyzer.NewToken(lexicalanalyzer.KEYWORD, "FROM", 2)
	expectTokenTable := lexicalanalyzer.NewToken(lexicalanalyzer.IDENTIFIER, "TABLE", 3)

	analyzer := v1.LexicalAnalyzerV1{}

	input := []string{"SELECT", "*", "FROM", "TABLE", ";"}

	tokens, tokenError := analyzer.MakeTokens(input)

	if tokenError != nil {
		t.Errorf("invalid token %v in position %d", tokenError.Message, tokenError.Position)
	}

	if len(tokens) != expectSizeTokens {
		t.Errorf("Expected token slice size %d, got %d", expectSizeTokens, len(tokens))
	}

	tokenFound := func(token lexicalanalyzer.Token) bool {
		for _, t := range tokens {
			if t == token {
				return true
			}
		}
		return false
	}

	if !tokenFound(expectTokenSelect) {
		t.Errorf("Token %v not found in token list", expectTokenSelect)
	}
	if !tokenFound(expectTokenFrom) {
		t.Errorf("Token %v not found in token list", expectTokenFrom)
	}
	if !tokenFound(expectTokenTable) {
		t.Errorf("Token %v not found in token list", expectTokenTable)
	}
}
