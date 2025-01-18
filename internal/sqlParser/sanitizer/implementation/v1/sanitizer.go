package v1

import "strings"

type SanitizerV1 struct {
}

func (s SanitizerV1) Sanitize(input string) string {
	result := strings.ToUpper(input)
	result = strings.Join(strings.Fields(result), " ")
	result = strings.ReplaceAll(result, ";", " ;")

	return result
}
