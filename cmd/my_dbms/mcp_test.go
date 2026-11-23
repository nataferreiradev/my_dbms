package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"my_DBMS/internal/storage"
)

// mcpSession abre um banco vazio e roda as mensagens nele.
func mcpSession(t *testing.T, write bool, messages ...string) []rpcResponse {
	t.Helper()
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	return mcpRun(t, db, write, messages...)
}

// mcpRun manda as mensagens, uma por linha, e devolve as respostas, com o
// Result ainda em JSON (json.RawMessage).
func mcpRun(t *testing.T, db *storage.DB, write bool, messages ...string) []rpcResponse {
	t.Helper()
	var out bytes.Buffer
	m := &mcpServer{db: db, write: write}
	if err := m.serve(strings.NewReader(strings.Join(messages, "\n")), &out); err != nil {
		t.Fatal(err)
	}

	var resps []rpcResponse
	dec := json.NewDecoder(&out)
	for dec.More() {
		var r struct {
			rpcResponse
			Result json.RawMessage `json:"result"`
		}
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		r.rpcResponse.Result = r.Result
		resps = append(resps, r.rpcResponse)
	}
	return resps
}

// toolCall monta a mensagem que chama uma ferramenta.
func toolCall(id int, name, sql string) string {
	msg, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": map[string]any{"sql": sql}},
	})
	return string(msg)
}

// toolText tira o texto e o isError do resultado de uma ferramenta.
func toolText(t *testing.T, r rpcResponse) (string, bool) {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("id %s: erro de protocolo %+v", r.ID, r.Error)
	}
	var res struct {
		Content []struct{ Text string } `json:"content"`
		IsError bool                    `json:"isError"`
	}
	if err := json.Unmarshal(r.Result.(json.RawMessage), &res); err != nil || len(res.Content) != 1 {
		t.Fatalf("id %s: resultado %s", r.ID, r.Result)
	}
	return res.Content[0].Text, res.IsError
}

func TestMCPHandshake(t *testing.T) {
	resps := mcpSession(t, false,
		"\xef\xbb\xbf"+`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"teste","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`,
		`isso não é json`,
	)
	if len(resps) != 6 {
		t.Fatalf("%d respostas, queria 6 (notificação não tem resposta)", len(resps))
	}

	var init struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
	}
	json.Unmarshal(resps[0].Result.(json.RawMessage), &init)
	if init.ProtocolVersion != "2025-06-18" || init.Capabilities["tools"] == nil {
		t.Errorf("initialize: %s", resps[0].Result)
	}
	json.Unmarshal(resps[1].Result.(json.RawMessage), &init)
	if init.ProtocolVersion != mcpVersions[0] {
		t.Errorf("versão desconhecida: respondeu %q, queria %q", init.ProtocolVersion, mcpVersions[0])
	}

	if string(resps[2].ID) != "3" || string(resps[2].Result.(json.RawMessage)) != "{}" {
		t.Errorf("ping: id %s, resultado %s", resps[2].ID, resps[2].Result)
	}

	var list struct{ Tools []mcpTool }
	json.Unmarshal(resps[3].Result.(json.RawMessage), &list)
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "schema,query" {
		t.Errorf("tools/list sem -mcp-write: %v", names)
	}

	if e := resps[4].Error; e == nil || e.Code != rpcMethodNotFound {
		t.Errorf("método desconhecido: %+v", e)
	}
	if e := resps[5].Error; e == nil || e.Code != rpcParseError || string(resps[5].ID) != "null" {
		t.Errorf("JSON inválido: id %s, erro %+v", resps[5].ID, e)
	}
}

func TestMCPTools(t *testing.T) {
	steps := []struct {
		tool, sql   string
		write       bool
		want        string // o texto tem que conter isto
		wantIsError bool
	}{
		{tool: "query", sql: "CREATE TABLE p (id INT PRIMARY KEY, nome TEXT);", want: "-mcp-write", wantIsError: true},
		{tool: "execute", sql: "CREATE TABLE p (id INT PRIMARY KEY, nome TEXT);", write: true, want: "tabela p criada"},
		{tool: "execute", sql: "INSERT INTO p VALUES (1, 'Ana'), (2, 'Bia')", write: true, want: "2 linhas inseridas"},
		{tool: "query", sql: "INSERT INTO p VALUES (3, 'Caio');", write: true, want: "use execute", wantIsError: true},
		{tool: "query", sql: "SELECT nome FROM p WHERE id = 2;", want: "Bia\n(1 linha)"},
		{tool: "query", sql: "EXPLAIN SELECT * FROM p WHERE id = 2;", want: "busca no índice p_pkey"},
		{tool: "query", sql: "SELECT * FROM nada;", want: "não existe", wantIsError: true},
		{tool: "query", sql: "SELEC * FROM p;", want: "não suportado", wantIsError: true},
		{tool: "query", sql: "SEARCH p FOR 'Ana';", want: "não tem índice vetorial", wantIsError: true},
		{tool: "schema", want: "p_pkey"},
	}

	// Todos os passos rodam no mesmo banco: os de leitura dependem dos de
	// escrita. Cada um vira uma sessão com o write dele.
	db, err := storage.Open(t.TempDir(), 4096)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range steps {
		text, isError := toolText(t, mcpRun(t, db, s.write, toolCall(i, s.tool, s.sql))[0])
		if !strings.Contains(text, s.want) || isError != s.wantIsError {
			t.Errorf("%s %q: %q (isError %v), queria algo com %q (isError %v)", s.tool, s.sql, text, isError, s.want, s.wantIsError)
		}
	}
}

// Sem -mcp-write, execute não existe nem pode ser chamada pelo nome.
func TestMCPExecuteNeedsWrite(t *testing.T) {
	resps := mcpSession(t, false, toolCall(1, "execute", "DROP TABLE p;"))
	if e := resps[0].Error; e == nil || e.Code != rpcInvalidParams {
		t.Errorf("execute sem write: %+v", e)
	}
}
