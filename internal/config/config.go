package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Config guarda os parâmetros do banco.
type Config struct {
	// DataDir é a pasta onde ficam os arquivos das tabelas. Caminho
	// relativo é resolvido a partir da pasta onde o programa roda.
	DataDir string
	// PageSize é o tamanho das páginas dos arquivos, em bytes. Fica gravado
	// no banco quando ele é criado; o storage confere se é válido e se bate
	// com o banco existente.
	PageSize int
	// EmbeddingURL é o endereço base de uma API de embeddings no formato da
	// OpenAI (POST <url>/embeddings). Vazio desliga os índices vetoriais. A
	// chave da API não fica aqui: veja APIKey.
	EmbeddingURL string
	// EmbeddingModel é o modelo pedido à API, como text-embedding-3-small.
	EmbeddingModel string
}

func Default() Config {
	return Config{DataDir: "data", PageSize: 4096}
}

// Load lê um arquivo no formato "chave = valor", uma por linha. Linhas
// vazias e começadas com # são ignoradas. Se o arquivo não existe, devolve
// os valores padrão.
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}

	// Editores no Windows às vezes gravam um BOM no início do arquivo.
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")

	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return cfg, fmt.Errorf("%s linha %d: esperava chave = valor", path, i+1)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)

		switch key {
		case "data_dir":
			if value == "" {
				return cfg, fmt.Errorf("%s linha %d: data_dir não pode ser vazio", path, i+1)
			}
			cfg.DataDir = value
		case "page_size":
			n, err := strconv.Atoi(value)
			if err != nil {
				return cfg, fmt.Errorf("%s linha %d: page_size precisa ser um número, veio %q", path, i+1, value)
			}
			cfg.PageSize = n
		case "embedding_url":
			cfg.EmbeddingURL = value
		case "embedding_model":
			cfg.EmbeddingModel = value
		default:
			// O my_dbms.conf vai para o git: uma chave aqui vazaria.
			if strings.Contains(strings.ToLower(key), "key") {
				return cfg, fmt.Errorf("%s linha %d: a chave da API não fica no my_dbms.conf; rode my_dbms -setup, que a guarda no arquivo de credenciais do usuário", path, i+1)
			}
			return cfg, fmt.Errorf("%s linha %d: parâmetro desconhecido %q", path, i+1, key)
		}
	}
	if cfg.EmbeddingURL != "" && cfg.EmbeddingModel == "" {
		return cfg, fmt.Errorf("%s: embedding_url sem embedding_model", path)
	}
	return cfg, nil
}

// Save grava os valores no arquivo de configuração, mexendo só nas linhas
// dessas chaves: a linha "chave = ..." (mesmo comentada) é trocada, e a
// chave que não aparece vai para o fim. Os comentários ficam como estão.
func Save(path string, values map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	text := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	lines := strings.Split(strings.TrimRight(text, "\r\n"), "\n")
	if text == "" {
		lines = nil
	}

	done := map[string]bool{}
	for i, line := range lines {
		key, _, ok := strings.Cut(strings.TrimLeft(strings.TrimSpace(line), "# "), "=")
		key = strings.TrimSpace(key)
		if value, wanted := values[key]; ok && wanted && !done[key] {
			lines[i] = key + " = " + value
			done[key] = true
		}
	}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if !done[key] {
			lines = append(lines, key+" = "+values[key])
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// KeyEnv é a variável de ambiente com a chave da API. Ela vale mais que o
// arquivo de credenciais, o que é prático no Docker e no CI.
const KeyEnv = "MY_DBMS_API_KEY"

// CredentialsPath é onde o -setup guarda a chave da API: fora do projeto,
// na pasta de configuração do usuário (%AppData%\my_dbms no Windows,
// ~/.config/my_dbms no Linux), como o ~/.aws/credentials.
func CredentialsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("pasta de configuração do usuário: %w", err)
	}
	return filepath.Join(dir, "my_dbms", "credentials"), nil
}

// APIKey devolve a chave da API: a de KeyEnv ou, sem ela, a do arquivo
// path. Sem nenhuma das duas, devolve "" (provedores locais não usam chave).
func APIKey(path string) (string, error) {
	if key := os.Getenv(KeyEnv); key != "" {
		return key, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// SaveAPIKey grava a chave no arquivo path, legível só pelo dono. No Linux
// e no macOS isso é a permissão 0600; no Windows, quem protege é a pasta do
// perfil do usuário, que os outros usuários não leem. Chave vazia apaga o
// arquivo.
func SaveAPIKey(path, key string) error {
	if key == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Se o arquivo já existe, o WriteFile não muda a permissão dele.
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
