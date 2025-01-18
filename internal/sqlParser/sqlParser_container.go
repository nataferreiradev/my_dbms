package sqlparser

import (
	lexicalanalyzer "my_DBMS/internal/sqlParser/lexicalAnalyzer"
	v1Lexical "my_DBMS/internal/sqlParser/lexicalAnalyzer/implementation/v1"
	"my_DBMS/internal/sqlParser/sanitizer"
	v1Sanitizer "my_DBMS/internal/sqlParser/sanitizer/implementation/v1"
	"my_DBMS/internal/sqlParser/tokenizer"
	v1Tokenizer "my_DBMS/internal/sqlParser/tokenizer/implementation/v1"
)

type SqlParserContainer struct {
	sanitizer       sanitizer.Sanitizer
	lexicalAnalyzer lexicalanalyzer.LexicalAnalyzer
	tokenizer       tokenizer.Tokenizer
}

func GetContainerV1() *SqlParserContainer {

	lexicalanalyzer := v1Lexical.LexicalAnalyzerV1{}
	sanitizer := v1Sanitizer.SanitizerV1{}
	tokenizer := v1Tokenizer.TokenizerV1{}

	return &SqlParserContainer{
		lexicalAnalyzer: &lexicalanalyzer,
		sanitizer:       &sanitizer,
		tokenizer:       &tokenizer,
	}
}
