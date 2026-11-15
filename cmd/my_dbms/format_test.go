package main

import "testing"

func TestFormatTable(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		rows    [][]string
		want    string
	}{
		{
			name:    "vazia",
			headers: []string{"id", "nome"},
			want:    "id | nome\n---+-----\n(0 linhas)",
		},
		{
			name:    "uma linha",
			headers: []string{"id", "nome"},
			rows:    [][]string{{"1", "Ana"}},
			want:    "id | nome\n---+-----\n1  | Ana\n(1 linha)",
		},
		{
			name:    "célula mais larga que o cabeçalho, com acento",
			headers: []string{"nome", "id"},
			rows:    [][]string{{"João", "1"}, {"Bernardo", "22"}},
			want: "nome     | id\n" +
				"---------+---\n" +
				"João     | 1\n" +
				"Bernardo | 22\n" +
				"(2 linhas)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTable(tt.headers, tt.rows); got != tt.want {
				t.Errorf("formatTable() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}
