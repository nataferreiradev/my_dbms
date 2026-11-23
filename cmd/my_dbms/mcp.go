package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"my_DBMS/internal/parser"
	"my_DBMS/internal/storage"
)

// mcpServer atende um cliente MCP (Model Context Protocol) pelo stdin e
// stdout: é assim que um assistente de IA conversa com o banco. O protocolo
// é JSON-RPC 2.0, uma mensagem por linha.
//
// O assistente enxerga ferramentas: schema (as tabelas, colunas e chaves),
// query (roda um SELECT ou EXPLAIN) e, só com write, execute (roda qualquer
// comando). Sem write, a IA não consegue alterar nada.
type mcpServer struct {
	db    *storage.DB
	write bool
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Códigos de erro do JSON-RPC.
const (
	rpcParseError     = -32700
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

// mcpVersions são as versões do protocolo que o servidor fala, da mais nova
// para a mais antiga. Se o cliente pede uma delas, a resposta é a mesma;
// senão, vai a mais nova, e o cliente decide se continua.
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = `my_dbms é um banco relacional pequeno, de estudo. Use a ferramenta schema para ver as tabelas antes de consultar.
O SQL aceito é limitado: SELECT com WHERE (=, <>, <, >, <=, >=, IS [NOT] NULL, AND, OR), [INNER|LEFT|RIGHT] JOIN ... ON e colunas como tabela.coluna.
Ainda não há ORDER BY, LIMIT, GROUP BY, funções de agregação nem alias. Tipos: INT, REAL, TEXT, BOOLEAN. Um comando por chamada.`

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

// sqlInput é o schema dos argumentos das ferramentas que recebem um comando.
var sqlInput = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"sql": map[string]any{"type": "string", "description": "um comando SQL, como SELECT * FROM p WHERE id = 1;"},
	},
	"required": []string{"sql"},
}

func (m *mcpServer) tools() []mcpTool {
	tools := []mcpTool{
		{
			Name:        "schema",
			Description: "Mostra o catálogo do banco: cada tabela com as colunas e tipos, e as chaves e índices.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Annotations: map[string]any{"readOnlyHint": true},
		},
		{
			Name:        "query",
			Description: "Roda um SELECT ou um EXPLAIN e devolve o resultado como tabela de texto. Não altera nada.",
			InputSchema: sqlInput,
			Annotations: map[string]any{"readOnlyHint": true},
		},
	}
	if m.write {
		tools = append(tools, mcpTool{
			Name:        "execute",
			Description: "Roda qualquer comando SQL: CREATE TABLE, INSERT, UPDATE, DELETE, CREATE INDEX, DROP. Cada comando é uma transação.",
			InputSchema: sqlInput,
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true},
		})
	}
	return tools
}

// serve lê as mensagens até o fim da entrada e responde cada pedido.
// Notificações (mensagens sem id) não têm resposta.
func (m *mcpServer) serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, maxLine)
	enc := json.NewEncoder(w)
	for scanner.Scan() {
		// O pipe do PowerShell pode mandar um BOM no início da entrada.
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\xef\xbb\xbf"))
		if line == "" {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			resp := rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcParseError, Message: "JSON inválido: " + err.Error()}}
			if err := enc.Encode(resp); err != nil {
				return err
			}
			continue
		}
		if req.ID == nil {
			continue
		}

		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		resp.Result, resp.Error = m.call(req.Method, req.Params)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// call responde um pedido pelo método.
func (m *mcpServer) call(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(params, &p)
		version := mcpVersions[0]
		if slices.Contains(mcpVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "my_dbms", "version": "0.1.0"},
			"instructions":    mcpInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": m.tools()}, nil
	case "tools/call":
		var p struct {
			Name      string `json:"name"`
			Arguments struct {
				SQL string `json:"sql"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "parâmetros inválidos: " + err.Error()}
		}
		if !slices.ContainsFunc(m.tools(), func(t mcpTool) bool { return t.Name == p.Name }) {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "ferramenta desconhecida: " + p.Name}
		}
		out, err := m.runTool(p.Name, p.Arguments.SQL)
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		return toolResult(out, false), nil
	}
	return nil, &rpcError{Code: rpcMethodNotFound, Message: "método desconhecido: " + method}
}

// runTool roda uma ferramenta. O erro volta para a IA como resultado da
// ferramenta, para ela poder corrigir o comando e tentar de novo.
func (m *mcpServer) runTool(name, input string) (string, error) {
	if name == "schema" {
		return execute(m.db, "SELECT * FROM _catalog;")
	}

	stmt, err := parse(input)
	if err != nil {
		return "", err
	}
	if name == "query" {
		switch stmt.(type) {
		case *parser.Select, *parser.Explain:
		default:
			if m.write {
				return "", errors.New("a ferramenta query só roda SELECT e EXPLAIN; para alterar o banco, use execute")
			}
			return "", errors.New("a ferramenta query só roda SELECT e EXPLAIN, e este servidor não aceita alterações (suba com -mcp-write para liberar)")
		}
	}
	return run(m.db, stmt)
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}
