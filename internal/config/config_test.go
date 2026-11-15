package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    Config
		wantErr bool
	}{
		{name: "vazio usa padrão", content: "", want: Default()},
		{name: "data_dir", content: "data_dir = meus_dados\n", want: Config{DataDir: "meus_dados", PageSize: 4096}},
		{name: "sem espaços", content: "data_dir=x", want: Config{DataDir: "x", PageSize: 4096}},
		{name: "comentários e CRLF", content: "# banco\r\n\r\ndata_dir = x\r\n", want: Config{DataDir: "x", PageSize: 4096}},
		{name: "BOM", content: "\xef\xbb\xbfdata_dir = x", want: Config{DataDir: "x", PageSize: 4096}},
		{name: "caminho com =", content: "data_dir = a=b", want: Config{DataDir: "a=b", PageSize: 4096}},
		{name: "page_size", content: "page_size = 8192\ndata_dir = x", want: Config{DataDir: "x", PageSize: 8192}},
		{name: "sem igual", content: "data_dir x", wantErr: true},
		{name: "valor vazio", content: "data_dir =", wantErr: true},
		{name: "page_size não numérico", content: "page_size = 4k", wantErr: true},
		{name: "chave desconhecida", content: "porta = 5432", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "my_dbms.conf")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			got, err := Load(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() erro = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "nao_existe.conf"))
	if err != nil {
		t.Fatalf("Load() erro = %v", err)
	}
	if got != Default() {
		t.Errorf("Load() = %+v, want padrão %+v", got, Default())
	}
}
