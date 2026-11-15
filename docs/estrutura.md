# Estrutura do projeto

O my_dbms é um banco de dados relacional escrito do zero em Go, para estudo.
O objetivo é ter código pequeno e direto, que dê para ler de cima a baixo.

## Árvore

```
my_dbms/
├── cmd/my_dbms/          → executável (REPL)
│   ├── main.go           → lê linhas do stdin e imprime o resultado
│   ├── execute.go        → reconhece o comando, valida e chama o storage
│   ├── execute_test.go
│   ├── format.go         → desenha o resultado do SELECT como tabela
│   └── format_test.go
├── internal/
│   ├── config/           → parâmetros do banco
│   │   ├── config.go     → Config, Default(), Load(): lê o my_dbms.conf
│   │   └── config_test.go
│   ├── sql/              → análise léxica do SQL
│   │   ├── token.go      → Token, TokenType, keywords, operadores, símbolos
│   │   ├── lexer.go      → Lex(): string → []Token
│   │   └── lexer_test.go
│   └── storage/          → persistência em disco
│       ├── storage.go    → DB: catálogo, CreateTable, InsertRow, ReadTable
│       └── storage_test.go
├── docs/                 → documentação do projeto (português)
│   └── en/               → versão em inglês
├── my_dbms.conf          → parâmetros (data_dir etc.)
└── data/                 → criado em tempo de execução (ignorado pelo git)
```

## Configuração

Na inicialização, o `main.go` lê o `my_dbms.conf` da pasta onde o programa
roda. O formato é `chave = valor`, uma por linha, com `#` para comentários.
Se o arquivo não existir, valem os padrões de `config.Default()`.

| Parâmetro | Padrão | O que é |
|---|---|---|
| `data_dir` | `data` | Pasta dos arquivos das tabelas. Caminho relativo parte da pasta onde o programa roda. |

Para adicionar um parâmetro: um campo em `Config`, o valor padrão em
`Default()`, um `case` em `Load()` e uma linha comentada no `my_dbms.conf`.
Chave desconhecida é erro, para um erro de digitação não passar em silêncio.

## Fluxo de um comando

```
"select nome from pessoas;"
        │
        ▼
  sql.Lex()            → [SELECT] [nome] [FROM] [pessoas] [;]
        │
        ▼
  execute()            → casa SELECT ... FROM <identificador> e chama selectRows
        │
        ▼
  storage.ReadTable()  → schema do catálogo em memória + linhas de data/pessoas.marina
        │
        ▼
  selectRows()         → escolhe as colunas pedidas
        │
        ▼
  formatTable()        → monta o texto alinhado que o REPL imprime
```

## Responsabilidade de cada pacote

| Pacote | Faz | Não faz |
|---|---|---|
| `internal/config` | Lê o arquivo de parâmetros e aplica os padrões. | Não abre o banco nem conhece SQL. |
| `internal/sql` | Transforma texto em tokens classificados. Normaliza keywords para maiúsculo. | Não sabe que existe disco nem tabela. |
| `internal/storage` | Mantém o catálogo, cria tabelas, acrescenta registros e lê tabelas dentro de um diretório. | Não conhece SQL. Recebe nomes e valores já validados. |
| `cmd/my_dbms` | Liga os dois: recebe a linha, chama `Lex`, decide o comando, valida valores contra o schema, chama o storage e formata a saída. | Não implementa regra de formato de arquivo. |

`internal/` impede que código de fora do módulo importe esses pacotes, o que
nos deixa livres para mudar a API interna.

## Formato em disco

Tudo é CSV, lido e escrito com `encoding/csv`. Cada tabela é um arquivo
`<data_dir>/<nome>.marina` só com os registros:

```
1,Ana
2,"Silva, Bruno"
```

O que descreve as tabelas fica no **catálogo**, `<data_dir>/_catalog.marina`:
uma linha por objeto do banco, no formato `type,name,table_name,definition`.

```
table,pessoas,pessoas,"id INT,nome TEXT"
```

| `type` | `definition` |
|---|---|
| `table` | Colunas e tipos: `id INT,nome TEXT`. |

O `Open` carrega o catálogo inteiro para a memória, e as operações consultam
essa cópia. O schema do próprio catálogo fica fixo no código
(`catalogSchema`), por isso ele pode ser lido como uma tabela qualquer:
`SELECT * FROM _catalog;`. Nomes começando com `_` são reservados para o
sistema: não dá para criar tabela com eles nem fazer `INSERT` neles.

O `CREATE TABLE` cria primeiro o arquivo de dados e depois grava no catálogo.
Se o programa cair no meio, sobra no máximo um arquivo que o catálogo não
enxerga. Ele não é apagado nem sobrescrito: criar uma tabela com esse nome dá
erro até alguém remover o arquivo.

Os tipos aceitos são `INT` e `TEXT`. O CSV cuida de vírgulas e aspas dentro
dos valores. O único caso especial é um registro com um só campo vazio, que é
gravado como `""` para o leitor não pular a linha.

## Como adicionar um comando

1. Se o comando usa uma palavra nova, adicione em `keywords` em
   `internal/sql/token.go`.
2. Adicione um `case` em `cmd/my_dbms/execute.go` que chama uma função
   própria do comando (como `createTable`, `insert`, `selectRows`):
   ```go
   case match(tokens, "DROP", "TABLE", sql.Identifier):
       return dropTable(db, tokens[2].Value)
   ```
   Para listas entre parênteses, use `parseList`.
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
