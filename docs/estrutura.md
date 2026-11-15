# Estrutura do projeto

O my_dbms é um banco de dados relacional escrito do zero em Go, para estudo.
O objetivo é ter código pequeno e direto, que dê para ler de cima a baixo.

## Árvore

```
my_dbms/
├── cmd/my_dbms/          → executável (REPL)
│   ├── main.go           → lê linhas do stdin e imprime o resultado
│   ├── execute.go        → reconhece o comando e chama o storage
│   └── execute_test.go
├── internal/
│   ├── sql/              → análise léxica do SQL
│   │   ├── token.go      → Token, TokenType, keywords, operadores, símbolos
│   │   ├── lexer.go      → Lex(): string → []Token
│   │   └── lexer_test.go
│   └── storage/          → persistência em disco
│       ├── storage.go    → DB: CreateTable, ReadTable
│       └── storage_test.go
├── docs/                 → documentação do projeto
└── data/                 → criado em tempo de execução (ignorado pelo git)
```

## Fluxo de um comando

```
"select * from pessoas;"
        │
        ▼
  sql.Lex()            → [SELECT] [*] [FROM] [pessoas] [;]
        │
        ▼
  execute()            → casa o padrão SELECT * FROM <identificador>
        │
        ▼
  storage.ReadTable()  → lê data/pessoas.marina
        │
        ▼
  REPL imprime o resultado
```

## Responsabilidade de cada pacote

| Pacote | Faz | Não faz |
|---|---|---|
| `internal/sql` | Transforma texto em tokens classificados. Normaliza keywords para maiúsculo. | Não sabe que existe disco nem tabela. |
| `internal/storage` | Cria e lê arquivos de tabela dentro de um diretório. | Não conhece SQL. Recebe nomes já validados. |
| `cmd/my_dbms` | Liga os dois: recebe a linha, chama `Lex`, decide o comando, chama o storage. | Não implementa regra de parsing nem de arquivo. |

`internal/` impede que código de fora do módulo importe esses pacotes, o que
nos deixa livres para mudar a API interna.

## Formato em disco

Cada tabela é um arquivo `data/<nome>.marina`. Hoje ele é criado vazio; o
formato das linhas ainda vai ser definido junto com o `INSERT`.

## Como adicionar um comando

1. Se o comando usa uma palavra nova, adicione em `keywords` em
   `internal/sql/token.go`.
2. Adicione um `case` em `cmd/my_dbms/execute.go`:
   ```go
   case match(tokens, "DROP", "TABLE", sql.Identifier):
   ```
3. Se precisar de disco, crie o método em `storage.DB`.
4. Escreva o teste em `execute_test.go` (e no pacote que mudou).

## Princípios

- **Interface só quando precisar.** Com uma implementação só, use struct ou
  função. Crie a interface quando houver uma segunda implementação ou um teste
  que precise de um fake, e declare-a no pacote que a *usa*, com só os métodos
  que ele chama.
- **Pacote por domínio, não por camada.** Nada de `implementation/v1`,
  `interfaces/` ou `container`.
- **Funções antes de objetos.** Se não há estado, é função.
- **Erros comuns do Go.** Retorne `error` e use `fmt.Errorf("contexto: %w", err)`.
- **Testes no mesmo pacote**, em formato de tabela quando houver vários casos.

## Quando criar um pacote novo

Quando surgir uma responsabilidade que não cabe nas de cima. Candidatos
prováveis conforme o projeto crescer:

- `internal/parser`: transformar tokens em uma árvore (AST), quando os
  padrões em `execute.go` ficarem complicados demais.
- `internal/engine`: executar a AST, tirando essa lógica do `cmd/`.
- `internal/index`: índices sobre as tabelas.

Não crie o pacote antes da hora: deixe o código pedir.
