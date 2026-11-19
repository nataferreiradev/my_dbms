package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"my_DBMS/internal/storage"
)

func TestHTTP(t *testing.T) {
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer((&server{db: db}).httpHandler())
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Errorf("GET /: status %d, tipo %q", res.StatusCode, res.Header.Get("Content-Type"))
	}

	steps := []struct {
		input, want, wantErr string
	}{
		{input: "CREATE TABLE p (id INT PRIMARY KEY, nome TEXT);", want: "tabela p criada"},
		{input: "INSERT INTO p VALUES (1, 'Ana');", want: "1 linha inserida"},
		{input: "SELECT nome FROM p WHERE id = 1;", want: "nome\n----\nAna\n(1 linha)"},
		{input: "DROP TABLE nada;", wantErr: "não existe"},
		{input: "  ", want: ""},
	}
	for _, s := range steps {
		res, err := http.Post(ts.URL+"/sql", "text/plain", strings.NewReader(s.input))
		if err != nil {
			t.Fatalf("%q: %v", s.input, err)
		}
		if got := res.Header.Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("%q: Access-Control-Allow-Origin %q, queria \"*\"", s.input, got)
		}
		var resp response
		err = json.NewDecoder(res.Body).Decode(&resp)
		res.Body.Close()
		if err != nil {
			t.Fatalf("%q: %v", s.input, err)
		}
		if resp.Output != s.want {
			t.Errorf("%q: saída %q, queria %q", s.input, resp.Output, s.want)
		}
		if !strings.Contains(resp.Error, s.wantErr) || (s.wantErr == "") != (resp.Error == "") {
			t.Errorf("%q: erro %q, queria algo com %q", s.input, resp.Error, s.wantErr)
		}
	}
}
