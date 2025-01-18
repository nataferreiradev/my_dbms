package tokenizer

import (
	"regexp"
)

type Tokenizer interface {
	FindTokens(string) []string
}

var Symbols = regexp.MustCompile(`\s+|[;,\(\)]`)

