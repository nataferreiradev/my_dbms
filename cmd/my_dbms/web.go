package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"my_DBMS/web"
)

// httpHandler entrega a página web/index.html em / e roda um comando por
// POST /sql. O corpo da requisição é o comando, e a resposta é o mesmo JSON
// do protocolo TCP: o navegador não fala TCP puro.
func (s *server) httpHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(web.Index)
	})
	mux.HandleFunc("POST /sql", s.handleSQL)
	return mux
}

func (s *server) handleSQL(w http.ResponseWriter, r *http.Request) {
	// A página pode ser aberta de qualquer lugar, até direto do disco.
	w.Header().Set("Access-Control-Allow-Origin", "*")

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLine))
	if err != nil {
		http.Error(w, "comando grande demais", http.StatusRequestEntityTooLarge)
		return
	}

	var resp response
	if line := strings.TrimSpace(string(body)); line != "" {
		resp = s.execute(line)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
