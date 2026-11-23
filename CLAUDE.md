# CLAUDE.md

Banco de dados relacional escrito do zero em Go (módulo `my_DBMS`, Go 1.23),
projeto open source de estudo. A pessoa que mantém o projeto quer código
**simples e divertido de escrever**: evite abstração antecipada.

## Comandos

```
go run ./cmd/my_dbms     # abre o REPL (tabelas ficam no data_dir do my_dbms.conf)
go run ./cmd/my_dbms -listen localhost:4321   # servidor TCP (protocolo em docs/estrutura.md)
go run ./cmd/my_dbms -mcp                     # servidor MCP no stdin/stdout (-mcp-write libera alterações)
go test ./...            # todos os testes
go vet ./...
gofmt -l .               # deve sair vazio
```

## Documentação

- `docs/estrutura.md`: árvore, responsabilidade de cada pacote, fluxo de um
  comando e como adicionar um comando novo. Leia antes de mudar a arquitetura
  e atualize quando ela mudar.
- `docs/commits.md`: padrão de commits (Conventional Commits, em inglês).
- `docs/en/`: versão em inglês dos mesmos documentos (`structure.md`,
  `commits.md`). Ao alterar um doc, atualize as duas versões no mesmo commit.

## Regras de código

- **Sem interfaces até existir uma segunda implementação** ou um teste que
  precise de fake. Quando criar, declare no pacote consumidor.
- **Sem** pastas `implementation/v1`, containers de injeção de dependência ou
  pacotes só de interfaces. O projeto saiu dessa estrutura de propósito.
- Pacotes por domínio (`sql`, `parser`, `storage`), nomes em minúsculo, sem camelCase.
- Identificadores em inglês. Comentários e mensagens de erro/saída para o
  usuário em português.
- Erros com `error` e `fmt.Errorf("...: %w", err)`, sem tipos de erro
  customizados sem necessidade.
- Testes no mesmo pacote, table-driven quando houver vários casos. Testes de
  storage usam `t.TempDir()`.
- Comandos SQL novos: keyword em `internal/sql/token.go` + tipo na AST e
  regra em `internal/parser` + `case` em `cmd/my_dbms/execute.go` + testes.

## Commits

Siga `docs/commits.md`: `<tipo>(<escopo>): <descrição>` em inglês, imperativo,
um commit por mudança lógica, testes passando antes de commitar.

## Ambiente (Windows)

- O pipe do PowerShell 5.1 envia BOM no início do stdin; o REPL remove isso
  em `main.go`. Ao testar o REPL por pipe, não estranhe o tratamento.
- Nunca escreva o caractere BOM literal em arquivos `.go`: use o escape
  `"\xef\xbb\xbf"`, senão o compilador falha com "illegal byte order mark".
- O git converte LF→CRLF no checkout; os avisos sobre isso são esperados.
