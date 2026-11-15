# Estrutura do projeto

O my_dbms é um banco de dados relacional escrito do zero em Go, para estudo.
O objetivo é ter código pequeno e direto, que dê para ler de cima a baixo.

## Árvore

```
my_dbms/
├── cmd/my_dbms/          → executável (REPL)
│   ├── main.go           → lê linhas do stdin e imprime o resultado
│   ├── execute.go        → roda cada tipo de Statement: valida e chama o storage
│   ├── execute_test.go
│   ├── where.go          → compileWhere(): condição do WHERE → função que testa a linha
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
│   ├── parser/           → análise sintática
│   │   ├── ast.go        → Statement e os tipos de cada comando (a AST)
│   │   ├── parser.go     → Parse(): []Token → Statement
│   │   └── parser_test.go
│   └── storage/          → persistência em disco
│       ├── storage.go    → DB: catálogo, CreateTable, InsertRows, ReadTable
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
  parser.Parse()       → &Select{Table: "pessoas", Columns: ["nome"]}
        │
        ▼
  execute()            → olha o tipo do Statement e chama selectRows
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
| `internal/sql` | Transforma texto em tokens classificados. Normaliza keywords para maiúsculo e tira as aspas das strings. | Não sabe que existe disco nem tabela. |
| `internal/parser` | Confere a gramática e transforma os tokens em uma AST (`Statement`). Erros de sintaxe saem daqui, com a posição. | Não sabe se a tabela ou a coluna existe. |
| `internal/storage` | Mantém o catálogo, cria tabelas, acrescenta registros e lê tabelas dentro de um diretório. | Não conhece SQL. Recebe nomes e valores já validados. |
| `cmd/my_dbms` | Liga tudo: recebe a linha, chama `Lex` e `Parse`, roda o `Statement` contra o schema, chama o storage e formata a saída. | Não implementa regra de formato de arquivo nem de gramática. |

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
table,pessoas,pessoas,"id INT NOT NULL,email TEXT,nome TEXT"
primary_key,pessoas_pkey,pessoas,id
unique,pessoas_email_key,pessoas,email
```

| `type` | `definition` |
|---|---|
| `table` | Colunas e tipos, com `NOT NULL` quando houver: `id INT NOT NULL,nome TEXT`. |
| `primary_key` | Colunas da chave primária: `id` ou `a,b`. No máximo uma por tabela. |
| `unique` | Colunas de uma chave única. |

A coluna `type` é o discriminador: diz que tipo de objeto a linha descreve.
Um objeto novo (índice, chave estrangeira) é um valor novo de `type` e um
`case` em `loadCatalog`. Uma chave sempre vem depois da linha da tabela dela,
e o nome é gerado: `<tabela>_pkey` ou `<tabela>_<colunas>_key`.

No SQL, a chave vem depois do tipo da coluna ou como item próprio da lista,
para chaves de várias colunas:

```sql
CREATE TABLE pessoas (id INT PRIMARY KEY, email TEXT UNIQUE, nome TEXT NOT NULL);
CREATE TABLE pares (a INT, b INT, PRIMARY KEY (a, b));
```

Ainda não há índice: o `InsertRows` confere cada chave lendo a tabela inteira
e recusa o registro se já existe outro com os mesmos valores nas colunas da
chave. Como no SQL padrão, colunas da chave primária viram `NOT NULL`, e
`NULL` não é igual a nada, então uma chave `UNIQUE` aceita várias linhas com
`NULL`.

### NULL

Na memória, cada célula é um `storage.Value`: um texto ou `NULL`. No disco,
`NULL` é gravado como `\N` (a mesma convenção do `COPY` do PostgreSQL), e um
texto que começa com `\` ganha mais uma `\` na frente, para o texto `\N`
digitado pelo usuário não virar `NULL`:

```
1,\N
2,\\N
```

A linha 1 tem `NULL`, a linha 2 tem o texto `\N`.

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
2. Em `internal/parser/ast.go`, crie o tipo do comando com o método
   `statement()`. Em `parser.go`, adicione um `case` em `Parse` e o método
   que lê a gramática dele, usando os ajudantes `expect`, `accept`, `ident`,
   `literal`, `list` e `names`.
3. Adicione um `case` em `cmd/my_dbms/execute.go` que chama uma função
   própria do comando (como `createTable`, `insert`, `selectRows`):
   ```go
   case *parser.DropTable:
       return dropTable(db, s)
   ```
4. Se precisar de disco, crie o método em `storage.DB`.
5. Escreva os testes em `parser_test.go` e `execute_test.go` (e no pacote
   que mudou).

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

- `internal/engine`: executar a AST, tirando essa lógica do `cmd/`.
- `internal/index`: índices sobre as tabelas.

Não crie o pacote antes da hora: deixe o código pedir.
