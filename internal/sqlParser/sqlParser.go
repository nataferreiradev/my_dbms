package sqlparser

import (
	"fmt"
)

func SqlParser(input string) {

	sqlParserContainer := GetContainerV1()

	sanitazedInput := sqlParserContainer.sanitizer.Sanitize(input)
	bruteTokens := sqlParserContainer.tokenizer.FindTokens(sanitazedInput)

	_, err := sqlParserContainer.lexicalAnalyzer.MakeTokens(bruteTokens)
	if err != nil {
		fmt.Printf("error: %v\n in position [%d]", err.Message, err.Position)
	}

}
