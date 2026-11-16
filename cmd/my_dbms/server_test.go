package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"my_DBMS/internal/storage"
)

// startServer sobe um servidor numa porta livre e devolve o endereço.
func startServer(t *testing.T) string {
	t.Helper()
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &server{db: db}
	go s.serve(ln)
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

type client struct {
	conn net.Conn
	r    *bufio.Reader
}

func dial(t *testing.T, addr string) *client {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &client{conn: conn, r: bufio.NewReader(conn)}
}

func (c *client) send(line string) (response, error) {
	if _, err := fmt.Fprintln(c.conn, line); err != nil {
		return response{}, err
	}
	data, err := c.r.ReadBytes('\n')
	if err != nil {
		return response{}, err
	}
	var resp response
	err = json.Unmarshal(data, &resp)
	return resp, err
}

func TestServer(t *testing.T) {
	c := dial(t, startServer(t))

	steps := []struct {
		input, want, wantErr string
	}{
		{input: "CREATE TABLE p (id INT PRIMARY KEY, nome TEXT);", want: "tabela p criada"},
		{input: "INSERT INTO p VALUES (1, 'Ana'), (2, 'Bia');", want: "2 linhas inseridas"},
		{input: "SELECT nome FROM p WHERE id = 2;", want: "nome\n----\nBia\n(1 linha)"},
		{input: "INSERT INTO p VALUES (1, 'Caio');", wantErr: "valor duplicado"},
		{input: "DESTROY p;", wantErr: "não suportado"},
		{input: "   ", want: ""},
	}
	for _, s := range steps {
		resp, err := c.send(s.input)
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

	// SELECT traz as colunas e as linhas, com NULL como null.
	c.send("INSERT INTO p VALUES (3, NULL);")
	resp, err := c.send("SELECT * FROM p WHERE id > 1;")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(map[string]any{"columns": resp.Columns, "rows": resp.Rows})
	want := `{"columns":[{"name":"id","type":"INT"},{"name":"nome","type":"TEXT"}],"rows":[["2","Bia"],["3",null]]}`
	if string(got) != want {
		t.Errorf("SELECT:\n got %s\nwant %s", got, want)
	}

	// exit fecha a conexão.
	fmt.Fprintln(c.conn, "exit")
	if _, err := c.r.ReadByte(); err == nil {
		t.Error("a conexão continuou aberta depois do exit")
	}
}

// Vários clientes inserindo ao mesmo tempo não podem perder linhas. Sem a
// trava, dois INSERT leem a mesma versão da tabela e um apaga o outro.
func TestServerConcurrentClients(t *testing.T) {
	addr := startServer(t)
	if resp, err := dial(t, addr).send("CREATE TABLE t (id INT PRIMARY KEY, quem TEXT);"); err != nil || resp.Error != "" {
		t.Fatal(err, resp.Error)
	}

	const clients, inserts = 10, 20
	var wg sync.WaitGroup
	for i := range clients {
		c := dial(t, addr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range inserts {
				id := i*inserts + j
				resp, err := c.send(fmt.Sprintf("INSERT INTO t VALUES (%d, 'cliente %d');", id, i))
				if err != nil || resp.Error != "" {
					t.Errorf("cliente %d: %v %s", i, err, resp.Error)
					return
				}
				if _, err := c.send("SELECT * FROM t WHERE id = 0;"); err != nil {
					t.Errorf("cliente %d: %v", i, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	resp, err := dial(t, addr).send("SELECT rows FROM _tables WHERE name = 't';")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("rows\n----\n %d\n(1 linha)", clients*inserts)
	if resp.Output != want {
		t.Errorf("depois dos INSERT: %q, queria %q", resp.Output, want)
	}
}
