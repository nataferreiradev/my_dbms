package v1

import "my_DBMS/internal/sqlParser/tokenizer"

type TokenizerV1 struct{}

func (t TokenizerV1) FindTokens(input string) []string {
	var tokens = []string{}
	splitedInput := tokenizer.Symbols.Split(input, -1)

	for _, entry := range splitedInput {
		tokens = append(tokens, entry)
	}

	return tokens
}
