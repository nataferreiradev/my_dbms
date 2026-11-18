package sql

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Lex transforma uma string SQL em tokens classificados.
// Keywords são normalizadas para maiúsculo, strings perdem as aspas e o resto
// mantém o texto original.
func Lex(input string) ([]Token, error) {
	runes := []rune(input)
	var tokens []Token

	for i := 0; i < len(runes); {
		r, start := runes[i], i
		switch {
		case unicode.IsSpace(r):
			i++

		case r == '"' || r == '\'':
			text, end, err := scanString(runes, start)
			if err != nil {
				return nil, fmt.Errorf("posição %d: %w", start+1, err)
			}
			tokens = append(tokens, Token{String, text, start})
			i = end

		case strings.ContainsRune(symbols, r):
			tokens = append(tokens, Token{Symbol, string(r), start})
			i++

		case strings.ContainsRune(operatorChars, r):
			op := string(r)
			if i+1 < len(runes) && operators[op+string(runes[i+1])] {
				op += string(runes[i+1])
			}
			tokens = append(tokens, Token{Operator, op, start})
			i += len(op)

		default:
			for i < len(runes) && !isBoundary(runes[i]) {
				i++
			}
			word := string(runes[start:i])
			typ, err := classify(word)
			if err != nil {
				return nil, fmt.Errorf("posição %d: %w", start+1, err)
			}
			if typ == Keyword {
				word = strings.ToUpper(word)
			}
			tokens = append(tokens, Token{typ, word, start})
		}
	}

	return tokens, nil
}

// scanString lê a string que começa na aspa runes[start] e devolve o texto e
// a posição logo depois da aspa de fechamento. Aspas simples e duplas valem
// igual. A mesma aspa repetida dentro da string vira uma só, então
// "ele disse ""oi""" é o texto: ele disse "oi".
func scanString(runes []rune, start int) (string, int, error) {
	quote := runes[start]
	var b strings.Builder
	for i := start + 1; i < len(runes); i++ {
		if runes[i] != quote {
			b.WriteRune(runes[i])
			continue
		}
		if i+1 < len(runes) && runes[i+1] == quote {
			b.WriteRune(quote)
			i++
			continue
		}
		return b.String(), i + 1, nil
	}
	return "", 0, errors.New("string sem aspas de fechamento")
}

// isBoundary diz se o caractere termina uma palavra.
func isBoundary(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(symbols+operatorChars+`"'`, r)
}

// Um nome pode vir qualificado pela tabela, como pessoas.id. Ele sai como um
// token só, e o parser separa as partes. O ponto não vira símbolo porque
// separaria também os números: 1.5 viraria [1] [.] [5].
var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)
	numberPattern     = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)
)

func classify(word string) (TokenType, error) {
	switch {
	case keywords[strings.ToUpper(word)]:
		return Keyword, nil
	case numberPattern.MatchString(word):
		return Number, nil
	case identifierPattern.MatchString(word):
		return Identifier, nil
	}
	return "", fmt.Errorf("token inválido %q", word)
}
