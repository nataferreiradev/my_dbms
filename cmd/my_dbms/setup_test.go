package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"my_DBMS/internal/config"
)

// fakeEmbeddings é uma API de embeddings que só aceita a chave sk-certa e
// devolve vetores de *dims dimensões.
func fakeEmbeddings(t *testing.T, dims *int) string {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-certa" {
			http.Error(w, `{"error":"chave inválida"}`, http.StatusUnauthorized)
			return
		}
		var req struct{ Input []string }
		json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range data {
			data[i] = map[string]any{"index": i, "embedding": make([]float32, *dims)}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

// runWizard roda o -setup com as respostas, uma por linha, e devolve a
// saída. A opção 5 é "Outra API", que pergunta tudo.
func runWizard(t *testing.T, dir string, answers ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	w := &setupWizard{
		in:       bufio.NewReader(strings.NewReader(strings.Join(answers, "\n") + "\n")),
		out:      &out,
		confPath: filepath.Join(dir, "my_dbms.conf"),
		keyPath:  filepath.Join(dir, "credenciais", "credentials"),
	}
	err := w.run()
	return out.String(), err
}

func TestSetup(t *testing.T) {
	t.Setenv(config.KeyEnv, "")
	dims := 3
	url := fakeEmbeddings(t, &dims)
	dir := t.TempDir()

	// Chave errada: o teste falha e, sem confirmar, nada é gravado.
	out, err := runWizard(t, dir, "5", url, "m", "s", "sk-errada", "n")
	if err == nil || !strings.Contains(out, "401") {
		t.Fatalf("chave errada: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "my_dbms.conf")); !os.IsNotExist(err) {
		t.Error("o conf foi gravado mesmo com o teste falhando")
	}

	// Opção inválida pergunta de novo; a chave certa passa no teste.
	out, err = runWizard(t, dir, "9", "5", url, "m", "s", "sk-certa")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Responda com um número") || !strings.Contains(out, "ok: vetores de 3 dimensões") {
		t.Errorf("saída:\n%s", out)
	}
	cfg, err := config.Load(filepath.Join(dir, "my_dbms.conf"))
	if err != nil || cfg.EmbeddingURL != url || cfg.EmbeddingModel != "m" {
		t.Errorf("conf: %+v, %v", cfg, err)
	}
	conf, _ := os.ReadFile(filepath.Join(dir, "my_dbms.conf"))
	if strings.Contains(string(conf), "sk-certa") {
		t.Error("a chave foi parar no my_dbms.conf")
	}
	if key, _ := config.APIKey(filepath.Join(dir, "credenciais", "credentials"), url); key != "sk-certa" {
		t.Errorf("chave gravada: %q", key)
	}

	// Enter na chave mantém a gravada (e o teste com ela passa).
	out, err = runWizard(t, dir, "5", "", "", "s", "")
	if err != nil || !strings.Contains(out, "Enter mantém a gravada") || !strings.Contains(out, "ok:") {
		t.Errorf("mantendo a chave: %v\n%s", err, out)
	}

	// Vetores que não cabem na página: avisa e grava só se confirmar.
	dims = 5000
	out, err = runWizard(t, dir, "5", "", "outro-modelo", "s", "", "s")
	if err != nil || !strings.Contains(out, "page_size maior") {
		t.Errorf("vetor grande: %v\n%s", err, out)
	}

	// 0 desliga.
	if out, err := runWizard(t, dir, "0"); err != nil || !strings.Contains(out, "desligados") {
		t.Errorf("desligar: %v\n%s", err, out)
	}
	if cfg, _ := config.Load(filepath.Join(dir, "my_dbms.conf")); cfg.EmbeddingURL != "" {
		t.Errorf("depois de desligar: %+v", cfg)
	}

	// A entrada acabando no meio é erro, sem travar.
	if _, err := runWizard(t, dir, "5"); err == nil || !strings.Contains(err.Error(), "entrada acabou") {
		t.Errorf("entrada curta: %v", err)
	}
}

// Um provedor sem chave (o Ollama) não pede chave e grava só o conf.
func TestSetupWithoutKey(t *testing.T) {
	t.Setenv(config.KeyEnv, "sk-certa") // o fake pede chave; a do ambiente vale
	dims := 768
	url := fakeEmbeddings(t, &dims)
	dir := t.TempDir()

	out, err := runWizard(t, dir, "1", url, "")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "Chave da API") || !strings.Contains(out, "ollama pull") {
		t.Errorf("saída:\n%s", out)
	}
	if cfg, _ := config.Load(filepath.Join(dir, "my_dbms.conf")); cfg.EmbeddingModel != "nomic-embed-text" {
		t.Errorf("modelo padrão: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, "credenciais", "credentials")); !os.IsNotExist(err) {
		t.Error("gravou um arquivo de chave para um provedor sem chave")
	}
}
