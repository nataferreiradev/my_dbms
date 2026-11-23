// Package embed transforma textos em vetores (embeddings) chamando uma API.
package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// Client pede embeddings a uma API no formato da OpenAI: POST <URL>/embeddings
// com {"model", "input"} e a resposta em data[i].embedding. É o formato que
// OpenAI, Voyage, Gemini, Ollama e LM Studio falam, então um cliente só serve
// para todos.
type Client struct {
	URL   string // endereço base, como https://api.openai.com/v1
	Model string
	Key   string // vazio para provedores locais, como o Ollama
	HTTP  *http.Client
}

// batchSize é quantos textos vão em cada requisição. As APIs limitam o
// tamanho do pedido, e um INSERT grande vira várias requisições.
const batchSize = 64

// Embed devolve um vetor por texto, na mesma ordem.
func (c *Client) Embed(texts []string) ([][]float32, error) {
	var vectors [][]float32
	for start := 0; start < len(texts); start += batchSize {
		batch, err := c.embedBatch(texts[start:min(start+batchSize, len(texts))])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batch...)
	}
	return vectors, nil
}

func (c *Client) embedBatch(texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": c.Model, "input": texts})
	if err != nil {
		return nil, err
	}
	url := strings.TrimSuffix(c.URL, "/") + "/embeddings"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("embedding: lendo a resposta de %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return nil, fmt.Errorf("embedding: %s respondeu %s: %s", url, resp.Status, msg)
	}

	var r struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("embedding: resposta de %s não é o JSON esperado: %w", url, err)
	}
	if len(r.Data) != len(texts) {
		return nil, fmt.Errorf("embedding: pedi %d vetores e vieram %d", len(texts), len(r.Data))
	}
	// As APIs devolvem em ordem, mas o index é o que vale.
	vectors := make([][]float32, len(texts))
	for _, d := range r.Data {
		if d.Index < 0 || d.Index >= len(texts) || vectors[d.Index] != nil || len(d.Embedding) == 0 {
			return nil, fmt.Errorf("embedding: resposta com o vetor %d inválido ou repetido", d.Index)
		}
		vectors[d.Index] = d.Embedding
	}
	return vectors, nil
}

// Fake devolve um embedder sem rede, para testes: cada palavra do texto soma
// 1 numa das dims posições do vetor, escolhida por um hash da palavra. Textos
// com palavras em comum ficam parecidos, o que basta para testar a busca.
func Fake(dims int) func([]string) ([][]float32, error) {
	return func(texts []string) ([][]float32, error) {
		vectors := make([][]float32, len(texts))
		for i, text := range texts {
			v := make([]float32, dims)
			words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsDigit(r)
			})
			for _, w := range words {
				h := fnv.New32a()
				h.Write([]byte(w))
				v[h.Sum32()%uint32(dims)]++
			}
			vectors[i] = v
		}
		return vectors, nil
	}
}
