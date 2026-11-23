package embed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAPI responde como a API da OpenAI: o vetor de cada texto é
// [tamanho do texto, posição no pedido], em ordem invertida, para conferir
// que o cliente usa o index.
func fakeAPI(t *testing.T, requests *int) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer sk-teste" {
			http.Error(w, `{"error":"sem autorização"}`, http.StatusUnauthorized)
			return
		}
		var req struct {
			Model string
			Input []string
		}
		json.NewDecoder(r.Body).Decode(&req)
		var data []map[string]any
		for i := len(req.Input) - 1; i >= 0; i-- {
			data = append(data, map[string]any{"index": i, "embedding": []float32{float32(len(req.Input[i])), float32(i)}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data, "model": req.Model})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestClientEmbed(t *testing.T) {
	requests := 0
	ts := fakeAPI(t, &requests)
	c := &Client{URL: ts.URL + "/v1/", Model: "m", Key: "sk-teste"}

	texts := make([]string, batchSize+3)
	for i := range texts {
		texts[i] = strings.Repeat("a", i)
	}
	vectors, err := c.Embed(texts)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Errorf("%d requisições, queria 2 (lotes de %d)", requests, batchSize)
	}
	for i, v := range vectors {
		if v[0] != float32(i) || v[1] != float32(i%batchSize) {
			t.Fatalf("vetor %d = %v", i, v)
		}
	}
}

func TestClientErrors(t *testing.T) {
	requests := 0
	ts := fakeAPI(t, &requests)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`)
	}))
	t.Cleanup(broken.Close)

	tests := []struct {
		name    string
		client  *Client
		wantErr string
	}{
		{"chave errada", &Client{URL: ts.URL + "/v1", Key: "sk-outra"}, "401"},
		{"vetor repetido", &Client{URL: broken.URL}, "pedi 1 vetores e vieram 2"},
		{"servidor fora do ar", &Client{URL: "http://127.0.0.1:1"}, "embedding"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.client.Embed([]string{"oi"})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("erro %v, queria algo com %q", err, tt.wantErr)
			}
		})
	}
}

func TestFake(t *testing.T) {
	v, _ := Fake(64)([]string{"Gato preto", "gato, PRETO!", "carro azul"})
	if fmt.Sprint(v[0]) != fmt.Sprint(v[1]) {
		t.Error("caixa e pontuação não deveriam mudar o vetor")
	}
	if fmt.Sprint(v[0]) == fmt.Sprint(v[2]) {
		t.Error("textos diferentes deram o mesmo vetor")
	}
}
