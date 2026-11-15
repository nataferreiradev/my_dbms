package main

import "testing"

func TestFormatTable(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		right   []bool
		rows    [][]string
		want    string
	}{
		{
			name:    "vazia",
			headers: []string{"id", "nome"},
			right:   []bool{true, false},
			want:    "id | nome\n---+-----\n(0 linhas)",
		},
		{
			name:    "uma linha",
			headers: []string{"id", "nome"},
			right:   []bool{true, false},
			rows:    [][]string{{"1", "Ana"}},
			want:    "id | nome\n---+-----\n 1 | Ana\n(1 linha)",
		},
		{
			name:    "célula mais larga que o cabeçalho, com acento",
			headers: []string{"nome", "id"},
			right:   []bool{false, false},
			rows:    [][]string{{"João", "1"}, {"Bernardo", "22"}},
			want: "nome     | id\n" +
				"---------+---\n" +
				"João     | 1\n" +
				"Bernardo | 22\n" +
				"(2 linhas)",
		},
		{
			name:    "números à direita, inclusive na última coluna",
			headers: []string{"nome", "idade"},
			right:   []bool{false, true},
			rows:    [][]string{{"Ana", "7"}, {"Bia", "123456"}, {"Caio", "NULL"}},
			want: "nome |  idade\n" +
				"-----+-------\n" +
				"Ana  |      7\n" +
				"Bia  | 123456\n" +
				"Caio |   NULL\n" +
				"(3 linhas)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTable(tt.headers, tt.right, tt.rows); got != tt.want {
				t.Errorf("formatTable() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}
