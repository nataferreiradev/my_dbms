package tokenizer_test

import (
	v1 "my_DBMS/internal/sqlParser/tokenizer/implementation/v1"
	"testing"
)

func TestTokenizer(t *testing.T) {
	input := "SELECT * FROM 1TABLE ;"
	input2 := "SELECT * FROM TABLE ;"

	var expectedlength1 = 6
	var expectedength2 = 6

	tokenizer := v1.TokenizerV1{}

	output1 := tokenizer.FindTokens(input)
	output2 := tokenizer.FindTokens(input2)

	validate := func(output []string, expectedLength int) bool {
		return len(output) != expectedLength
	}

	if validate(output1, expectedlength1) {
		t.Errorf("Unexpected amount tokens for %v found[%d] expect [%d]", input, len(output1), expectedlength1)
	}

	if validate(output2, expectedength2) {
		t.Errorf("Unexpected amount tokens for %v found[%d] expect [%d]", input2, len(output2), expectedength2)
	}
}
