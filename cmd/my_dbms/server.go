package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"net"
	"strings"
	"sync"

	"my_DBMS/internal/parser"
	"my_DBMS/internal/storage"
)

// maxLine é o tamanho máximo de um comando recebido pela rede.
const maxLine = 1 << 20

// server atende clientes por TCP. Cada conexão roda na sua goroutine, e
// todas usam o mesmo banco.
//
// O protocolo é de texto: o cliente manda um comando por linha e recebe uma
// linha de JSON para cada um, {"output": "..."} ou {"error": "..."}. Um
// SELECT também traz "columns" e "rows", para o cliente montar a tabela do
// jeito dele; NULL vem como null. "exit" ou "quit" fecham a conexão. Dá
// para testar com nc:
//
//	nc localhost 4321
type server struct {
	db *storage.DB

	// mu deixa vários SELECT rodarem juntos, mas um comando que escreve roda
	// sozinho. O storage ainda lê e grava a tabela inteira de uma vez: dois
	// UPDATE ao mesmo tempo leriam a mesma versão e um apagaria o outro.
	mu sync.RWMutex
}

type response struct {
	Output  string      `json:"output,omitempty"`
	Error   string      `json:"error,omitempty"`
	Columns []column    `json:"columns,omitempty"`
	Rows    [][]*string `json:"rows,omitempty"`
}

type column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// execute roda um comando com a trava certa para o tipo dele. Um SELECT
// roda com a trava de leitura e devolve também as colunas e as linhas.
func (s *server) execute(input string) response {
	stmt, err := parse(input)
	if err != nil {
		return response{Error: err.Error()}
	}

	if sel, ok := stmt.(*parser.Select); ok {
		s.mu.RLock()
		r, err := querySelect(s.db, sel)
		s.mu.RUnlock()
		if err != nil {
			return response{Error: err.Error()}
		}
		return selectResponse(r)
	}

	s.mu.Lock()
	out, err := run(s.db, stmt)
	s.mu.Unlock()
	if err != nil {
		return response{Error: err.Error()}
	}
	return response{Output: out}
}

func selectResponse(r *selectResult) response {
	resp := response{Output: r.format()}
	for _, c := range r.Columns {
		resp.Columns = append(resp.Columns, column{Name: c.Name, Type: c.Type})
	}
	for _, row := range r.Rows {
		cells := make([]*string, len(row))
		for i, v := range row {
			if !v.Null {
				cells[i] = &v.Text
			}
		}
		resp.Rows = append(resp.Rows, cells)
	}
	return resp
}

// serve aceita conexões até o listener ser fechado.
func (s *server) serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

// handle lê os comandos de uma conexão, um por linha, e responde cada um.
func (s *server) handle(conn net.Conn) {
	defer conn.Close()
	addr := conn.RemoteAddr()
	log.Printf("%s conectou", addr)
	defer log.Printf("%s desconectou", addr)

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(nil, maxLine)
	enc := json.NewEncoder(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		var resp response
		switch line {
		case "":
			// Linha vazia também tem resposta, para o cliente não ficar esperando.
		case "exit", "quit":
			return
		default:
			resp = s.execute(line)
		}
		if err := enc.Encode(resp); err != nil {
			log.Printf("%s: erro ao responder: %v", addr, err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("%s: erro ao ler: %v", addr, err)
	}
}
