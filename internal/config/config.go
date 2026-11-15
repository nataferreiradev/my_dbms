package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Config guarda os parâmetros do banco.
type Config struct {
	// DataDir é a pasta onde ficam os arquivos das tabelas. Caminho
	// relativo é resolvido a partir da pasta onde o programa roda.
	DataDir string
}

func Default() Config {
	return Config{DataDir: "data"}
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
		default:
			return cfg, fmt.Errorf("%s linha %d: parâmetro desconhecido %q", path, i+1, key)
		}
	}
	return cfg, nil
}
