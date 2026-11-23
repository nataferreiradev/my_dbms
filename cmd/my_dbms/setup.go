package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"my_DBMS/internal/config"
	"my_DBMS/internal/embed"
	"my_DBMS/internal/storage"
)

// provider é uma opção do -setup, com os valores sugeridos. Todos falam o
// formato da OpenAI, então mudam só o endereço, o modelo e a chave.
type provider struct {
	name     string
	url      string
	model    string
	needsKey bool
	note     string
}

var providers = []provider{
	{name: "Ollama (local, grátis, sem chave)", url: "http://localhost:11434/v1", model: "nomic-embed-text",
		note: "Instale o Ollama (https://ollama.com) e baixe o modelo: ollama pull nomic-embed-text"},
	{name: "OpenAI", url: "https://api.openai.com/v1", model: "text-embedding-3-small", needsKey: true},
	{name: "Voyage AI (a recomendada pela Anthropic para usar com o Claude)", url: "https://api.voyageai.com/v1", model: "voyage-3.5", needsKey: true},
	{name: "Google Gemini", url: "https://generativelanguage.googleapis.com/v1beta/openai", model: "gemini-embedding-001", needsKey: true},
	{name: "Outra API no formato da OpenAI (LM Studio, vLLM, Azure...)",
		note: "O endereço é a base da API, sem o /embeddings no fim. LM Studio: http://localhost:1234/v1"},
}

// setupWizard é o -setup: pergunta qual API de embeddings usar, testa a
// conexão e grava o endereço e o modelo no my_dbms.conf e a chave no
// arquivo de credenciais do usuário.
type setupWizard struct {
	in       *bufio.Reader
	out      io.Writer
	confPath string
	keyPath  string
	// secret lê a chave sem mostrar na tela. nil lê uma linha de in (quando
	// a entrada não é um terminal, como num pipe ou num teste).
	secret func() (string, error)
}

func (w *setupWizard) run() error {
	cfg, err := config.Load(w.confPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(w.out, "Configuração dos embeddings do my_dbms")
	fmt.Fprintln(w.out, "Os índices vetoriais (WITH EMBEDDING, CREATE VECTOR INDEX) usam uma API de embeddings para o SEARCH.")
	if cfg.EmbeddingURL == "" {
		fmt.Fprintln(w.out, "Hoje: desligados.")
	} else {
		fmt.Fprintf(w.out, "Hoje: %s, modelo %s.\n", cfg.EmbeddingURL, cfg.EmbeddingModel)
	}
	fmt.Fprintln(w.out)
	for i, p := range providers {
		fmt.Fprintf(w.out, "  %d) %s\n", i+1, p.name)
	}
	fmt.Fprintln(w.out, "  0) Desligar os embeddings")

	var choice int
	for {
		answer, err := w.ask("Escolha", "1")
		if err != nil {
			return err
		}
		choice, err = strconv.Atoi(answer)
		if err == nil && choice >= 0 && choice <= len(providers) {
			break
		}
		fmt.Fprintf(w.out, "Responda com um número de 0 a %d.\n", len(providers))
	}

	if choice == 0 {
		if err := config.Save(w.confPath, map[string]string{"embedding_url": "", "embedding_model": ""}); err != nil {
			return err
		}
		fmt.Fprintf(w.out, "Embeddings desligados em %s.\n", w.confPath)
		return nil
	}
	return w.configure(cfg, providers[choice-1])
}

func (w *setupWizard) configure(cfg config.Config, p provider) error {
	if p.note != "" {
		fmt.Fprintln(w.out, p.note)
	}
	// Rodando de novo, o que já está configurado vira o padrão.
	custom := p.url == ""
	if custom || p.url == cfg.EmbeddingURL {
		p.url, p.model = cfg.EmbeddingURL, cfg.EmbeddingModel
	}
	url, err := w.askRequired("Endereço da API", p.url)
	if err != nil {
		return err
	}
	model, err := w.askRequired("Modelo", p.model)
	if err != nil {
		return err
	}
	needsKey := p.needsKey
	if custom {
		if needsKey, err = w.confirm("A API pede chave?"); err != nil {
			return err
		}
	}

	var key string
	if needsKey {
		if key, err = w.askKey(url); err != nil {
			return err
		}
	}

	// O teste usa a chave que vai valer de verdade: a do ambiente, se houver.
	testKey := key
	if env := os.Getenv(config.KeyEnv); env != "" {
		testKey = env
	}
	fmt.Fprint(w.out, "Testando a conexão... ")
	client := &embed.Client{URL: url, Model: model, Key: testKey}
	vectors, err := client.Embed([]string{"teste de conexão do my_dbms"})
	switch {
	case err != nil:
		fmt.Fprintf(w.out, "falhou:\n  %v\n", err)
		if ok, err := w.confirm("Gravar mesmo assim?"); err != nil || !ok {
			return errors.Join(err, errors.New("nada foi gravado"))
		}
	case len(vectors[0]) > storage.MaxVectorDims(cfg.PageSize):
		fmt.Fprintf(w.out, "o modelo devolve vetores de %d dimensões, mas com page_size = %d cabem no máximo %d.\n",
			len(vectors[0]), cfg.PageSize, storage.MaxVectorDims(cfg.PageSize))
		fmt.Fprintln(w.out, "  Use outro modelo, ou um page_size maior num banco novo.")
		if ok, err := w.confirm("Gravar mesmo assim?"); err != nil || !ok {
			return errors.Join(err, errors.New("nada foi gravado"))
		}
	default:
		fmt.Fprintf(w.out, "ok: vetores de %d dimensões.\n", len(vectors[0]))
	}

	if err := config.Save(w.confPath, map[string]string{"embedding_url": url, "embedding_model": model}); err != nil {
		return err
	}
	fmt.Fprintf(w.out, "Endereço e modelo gravados em %s.\n", w.confPath)
	if key != "" {
		if err := config.SaveAPIKey(w.keyPath, url, key); err != nil {
			return fmt.Errorf("gravando a chave: %w", err)
		}
		fmt.Fprintf(w.out, "Chave gravada em %s, só para %s (fora do projeto, longe do git).\n", w.keyPath, url)
	}
	return nil
}

// askKey pede a chave da API. Enter mantém a que já está gravada para o
// mesmo endereço.
func (w *setupWizard) askKey(url string) (string, error) {
	env := os.Getenv(config.KeyEnv) != ""
	if env {
		fmt.Fprintf(w.out, "A variável %s está definida e vale mais que a chave gravada.\n", config.KeyEnv)
	}
	// Com a variável definida, APIKey devolveria ela, e não a do arquivo.
	saved := ""
	if !env {
		var err error
		if saved, err = config.APIKey(w.keyPath, url); err != nil {
			return "", err
		}
	}
	for {
		prompt := "Chave da API (não aparece na tela)"
		if saved != "" {
			prompt += "; Enter mantém a gravada"
		}
		fmt.Fprint(w.out, prompt+": ")
		var key string
		var err error
		if w.secret != nil {
			key, err = w.secret()
			fmt.Fprintln(w.out)
		} else {
			key, err = w.readLine()
		}
		if err != nil {
			return "", err
		}
		if key = strings.TrimSpace(key); key == "" {
			key = saved
		}
		if key != "" || env {
			return key, nil
		}
		fmt.Fprintln(w.out, "Este provedor precisa de uma chave.")
	}
}

// ask mostra a pergunta com o valor padrão entre colchetes. Enter fica com
// o padrão.
func (w *setupWizard) ask(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(w.out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(w.out, "%s: ", label)
	}
	answer, err := w.readLine()
	if err != nil {
		return "", err
	}
	if answer == "" {
		return def, nil
	}
	return answer, nil
}

// askRequired pergunta de novo até vir uma resposta.
func (w *setupWizard) askRequired(label, def string) (string, error) {
	for {
		answer, err := w.ask(label, def)
		if err != nil || answer != "" {
			return answer, err
		}
		fmt.Fprintln(w.out, "Esta resposta é obrigatória.")
	}
}

// confirm faz uma pergunta de sim ou não. O padrão é não.
func (w *setupWizard) confirm(question string) (bool, error) {
	answer, err := w.ask(question+" (s/N)", "")
	answer = strings.ToLower(answer)
	return answer == "s" || answer == "sim", err
}

func (w *setupWizard) readLine() (string, error) {
	line, err := w.in.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		if errors.Is(err, io.EOF) {
			return "", errors.New("a entrada acabou antes do fim da configuração")
		}
		return "", err
	}
	// O pipe do PowerShell pode mandar um BOM no início da entrada.
	return strings.TrimSpace(strings.TrimPrefix(line, "\xef\xbb\xbf")), nil
}
