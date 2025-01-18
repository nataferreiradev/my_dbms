package sanitizer

import (
	v1 "my_DBMS/internal/sqlParser/sanitizer/implementation/v1"
	"testing"
)

func TestSinitizer(t *testing.T) {
	input := "Select *   FROM tabela;"
	expect := "SELECT * FROM TABELA ;"

	sanitizer := v1.SanitizerV1{}

	result := sanitizer.Sanitize(input)

	if result != expect {
		t.Errorf("Sanitizer result is incorrect. Got '%s', expected '%s'", result, expect)
	}
}
