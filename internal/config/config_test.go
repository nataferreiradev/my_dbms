package config

import (
	"os"
	"path/filepath"
	"runtime"
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
		{name: "embedding", content: "embedding_url = http://localhost:11434/v1\nembedding_model = nomic-embed-text",
			want: Config{DataDir: "data", PageSize: 4096, EmbeddingURL: "http://localhost:11434/v1", EmbeddingModel: "nomic-embed-text"}},
		{name: "embedding desligado", content: "embedding_url =\nembedding_model =", want: Default()},
		{name: "embedding sem modelo", content: "embedding_url = http://x/v1", wantErr: true},
		{name: "chave da API no conf", content: "embedding_api_key = sk-123", wantErr: true},
		{name: "api_key no conf", content: "API_KEY = sk-123", wantErr: true},
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

func TestSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "my_dbms.conf")
	original := "# banco\r\ndata_dir = x\r\n# embedding_model = antigo\r\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Save(path, map[string]string{"embedding_url": "http://localhost:11434/v1", "embedding_model": "nomic-embed-text"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "# banco\r\ndata_dir = x\r\nembedding_model = nomic-embed-text\nembedding_url = http://localhost:11434/v1\n"
	if string(data) != want {
		t.Errorf("arquivo:\n%q\nqueria\n%q", data, want)
	}

	// Gravar de novo troca as linhas, sem repetir.
	if err := Save(path, map[string]string{"embedding_url": "", "embedding_model": ""}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != (Config{DataDir: "x", PageSize: 4096}) {
		t.Errorf("depois de desligar: %+v, %v", got, err)
	}

	// Arquivo que não existe é criado.
	path = filepath.Join(t.TempDir(), "novo.conf")
	if err := Save(path, map[string]string{"data_dir": "y"}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "data_dir = y\n" {
		t.Errorf("arquivo novo: %q", data)
	}
}

func TestAPIKey(t *testing.T) {
	t.Setenv(KeyEnv, "")
	path := filepath.Join(t.TempDir(), "my_dbms", "credentials")

	const url = "https://api.openai.com/v1"

	if key, err := APIKey(path, url); err != nil || key != "" {
		t.Errorf("sem arquivo: %q, %v", key, err)
	}
	if err := SaveAPIKey(path, url, "sk-arquivo"); err != nil {
		t.Fatal(err)
	}
	if key, err := APIKey(path, url); err != nil || key != "sk-arquivo" {
		t.Errorf("do arquivo: %q, %v", key, err)
	}
	// A chave é do endereço em que foi gravada: não vai para outro servidor.
	if key, err := APIKey(path, "https://outro.exemplo/v1"); err != nil || key != "" {
		t.Errorf("outro endereço recebeu a chave: %q, %v", key, err)
	}
	if err := SaveAPIKey(path, url, "sk-a\nurl = x"); err == nil {
		t.Error("chave com quebra de linha foi aceita")
	}
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Errorf("permissão do arquivo: %v, %v", info.Mode(), err)
	}

	t.Setenv(KeyEnv, "sk-ambiente")
	if key, _ := APIKey(path, url); key != "sk-ambiente" {
		t.Errorf("a variável de ambiente deveria valer mais: %q", key)
	}

	if err := SaveAPIKey(path, url, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("chave vazia deveria apagar o arquivo: %v", err)
	}
}
