# Estrutura do projeto

O my_dbms é um banco de dados relacional escrito do zero em Go, para estudo.
O objetivo é ter código pequeno e direto, que dê para ler de cima a baixo.

## Árvore

```
my_dbms/
├── cmd/my_dbms/          → executável (REPL ou servidor TCP)
│   ├── main.go           → lê linhas do stdin e imprime o resultado; com -listen, sobe o servidor
│   ├── execute.go        → roda cada tipo de Statement: valida e chama o storage
│   ├── execute_test.go
│   ├── where.go          → compileWhere(): condição do WHERE → função que testa a linha
│   ├── format.go         → desenha o resultado do SELECT como tabela
│   ├── format_test.go
│   ├── server.go         → servidor TCP: uma goroutine por conexão e trava de leitura/escrita
│   ├── server_test.go
│   └── bench_test.go     → benchmark do SELECT por chave
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
│       ├── storage.go    → DB: catálogo, CreateTable, InsertRows, ReadTable,
│       │                   ReplaceRows, DropTable
│       ├── storage_test.go
│       ├── page.go       → formato em disco: cabeçalho, páginas com slots, registros
│       ├── page_test.go
│       ├── system.go     → tabelas do sistema montadas na memória: _settings, _tables
│       ├── system_test.go
│       └── bench_test.go → benchmarks de INSERT com chave e de leitura da tabela
├── .github/workflows/
│   └── ci.yml            → GitHub Actions: gofmt, go vet e testes no Linux e no Windows
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
| `page_size` | `4096` | Tamanho das páginas dos arquivos, em bytes: potência de 2 entre 1024 e 32768. Fica gravado no banco quando ele é criado; abrir um banco existente com outro valor é erro. |

Para adicionar um parâmetro: um campo em `Config`, o valor padrão em
`Default()`, um `case` em `Load()` e uma linha comentada no `my_dbms.conf`.
Chave desconhecida é erro, para um erro de digitação não passar em silêncio.

## SQL aceito

```sql
CREATE TABLE p (id INT PRIMARY KEY, nome TEXT NOT NULL, email TEXT UNIQUE);
INSERT INTO p VALUES (1, 'Ana', NULL), (2, 'Bia', 'bia@x');
INSERT INTO p (nome, id) VALUES ('Caio', 3);
SELECT * FROM p WHERE (id > 1 OR email IS NULL) AND nome <> 'Bia';
UPDATE p SET email = 'ana@x' WHERE id = 1;
DELETE FROM p WHERE email IS NULL;
DROP TABLE p;
```

- Tipos: `INT` e `TEXT`. Strings com aspas simples ou duplas; aspa repetida
  dentro dela é uma aspa (`'it''s'`).
- Restrições de coluna: `NOT NULL`, `PRIMARY KEY`, `UNIQUE`. Chaves de várias
  colunas: `PRIMARY KEY (a, b)`, `UNIQUE (a, b)`.
- `WHERE`: `=`, `<>`, `<`, `>`, `<=`, `>=`, `IS [NOT] NULL`, `AND`, `OR` e
  parênteses. Comparar com `NULL` dá "desconhecido" e a linha não passa, como
  no SQL padrão; use `IS NULL`.
- `INSERT` com várias linhas grava todas ou nenhuma.

## Servidor

Com `-listen`, o executável não abre o REPL: atende clientes por TCP.

```
go run ./cmd/my_dbms -listen localhost:4321
```

O protocolo é de texto, para dar para testar com `nc localhost 4321`: o
cliente manda um comando por linha e recebe uma linha de JSON para cada um.

```
→ SELECT nome FROM p WHERE id = 1;
← {"output":"nome\n----\nAna\n(1 linha)"}
→ DROP TABLE nada;
← {"error":"tabela \"nada\" não existe"}
```

Um `SELECT` também traz `columns` e `rows`, para o cliente montar a tabela do
jeito dele (a interface web desenha em HTML). Os valores vêm como texto e
`NULL` vem como `null`:

```
→ SELECT * FROM p WHERE id > 1;
← {"output":"…","columns":[{"name":"id","type":"INT"},{"name":"nome","type":"TEXT"}],"rows":[["2","Bia"],["3",null]]}
```

Linha vazia recebe `{}`, e `exit` ou `quit` fecham a conexão.

Cada conexão roda na sua goroutine, e todas usam o mesmo `storage.DB`. Um
`sync.RWMutex` no `server` controla o acesso: vários `SELECT` rodam juntos,
mas um comando que escreve roda sozinho. A trava é por comando, e não por
chamada ao storage, porque `INSERT`, `UPDATE` e `DELETE` leem a tabela e
gravam a versão nova: dois deles ao mesmo tempo partiriam da mesma versão, e
um apagaria o que o outro fez. Quando o storage gravar só as páginas que
mudaram e tiver transações, essa trava única pode virar travas por tabela.

O servidor e o REPL não devem abrir a mesma `data_dir` ao mesmo tempo: são
dois processos, e a trava só vale dentro de um.

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
  storage.ReadTable()  → schema do catálogo em memória + linhas de data/pessoas.mari
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
| `internal/storage` | Define o formato em disco, mantém o catálogo, cria tabelas, acrescenta registros e lê tabelas dentro de um diretório. | Não conhece SQL. Recebe nomes e valores já validados. |
| `cmd/my_dbms` | Liga tudo: recebe a linha, chama `Lex` e `Parse`, roda o `Statement` contra o schema, chama o storage e formata a saída. Também atende clientes por TCP (`server.go`). | Não implementa regra de formato de arquivo nem de gramática. |

`internal/` impede que código de fora do módulo importe esses pacotes, o que
nos deixa livres para mudar a API interna.

## Formato em disco

Cada tabela é um arquivo `<data_dir>/<nome>.mari` só com os registros. O
que descreve as tabelas fica no **catálogo**, `<data_dir>/_catalog.mari`,
gravado no mesmo formato. O código do formato está em
`internal/storage/page.go`.

### Páginas

O arquivo é dividido em **páginas** de `page_size` bytes (4096 por padrão),
como no SQLite e no PostgreSQL: a página N começa no byte `N × page_size`. A
página 0 é o cabeçalho do arquivo, e as outras guardam os registros:

```
página 0   "MRNA" | versão (2 bytes) | page_size (2 bytes) | zeros até o fim

página N   ┌───────────────────────────────────────────────────────┐
           │ nº de slots | início dos registros | slot 0 | slot 1 …│  os slots crescem →
           │                                                       │
           │                    espaço livre                       │
           │                                                       │
           │                  … | registro 1 | registro 0          │  ← os registros crescem do fim
           └───────────────────────────────────────────────────────┘
```

Cada slot tem 4 bytes: o offset e o tamanho do registro dentro da página. Os
slots crescem do começo e os registros do fim, e a página está cheia quando
os dois se encontram. Todos os inteiros são little endian; os campos de 2
bytes limitam a página a 32 KB.

O `page_size` vem do `my_dbms.conf` e é gravado na página 0 de cada arquivo
quando o banco é criado. Abrir um banco existente com outro valor é erro,
porque um arquivo de páginas de 4 KB lido como se fossem de 8 KB vira lixo. O
cabeçalho também faz um arquivo de outro formato (como o CSV das versões
antigas) ser recusado com erro, em vez de lido errado.

Um registro precisa caber numa página vazia: o máximo é `page_size − 8`
bytes (o cabeçalho da página e um slot). Um registro maior é recusado com
erro, e nada é gravado.

### Registros

Cada linha vira bytes guiada pelos tipos das colunas:

```
bitmap de NULL | valor 0 | valor 1 | …
```

- **Bitmap de NULL**: um bit por coluna (o bit `i % 8` do byte `i / 8`),
  ligado quando o valor é `NULL`. Um `NULL` não ocupa mais nada.
- **INT**: 8 bytes (int64).
- **TEXT**: o tamanho em uvarint (1 byte até 127, 2 bytes até 16383…) e
  depois os bytes do texto.

Como o tamanho vem antes do dado, não existe escape: o texto pode ter
vírgula, aspas ou `\N`. O tamanho do registro inteiro fica no slot. Exemplo,
`(1, 'Ana', NULL)` em `pessoas (id INT, nome TEXT, email TEXT)`:

```
04                          bitmap 0000_0100: email é NULL
01 00 00 00 00 00 00 00     id = 1
03 41 6E 61                 nome = "Ana"
```

Na memória, cada célula continua sendo um `storage.Value`: um texto ou
`NULL`, com os números já normalizados como texto. A conversão para int64
acontece só na hora de gravar.

### Catálogo

O catálogo tem uma linha por objeto do banco, com as colunas `type`, `name`,
`table_name` e `definition`:

```
type        | name              | table_name | definition
table       | pessoas           | pessoas    | id INT NOT NULL,email TEXT,nome TEXT
primary_key | pessoas_pkey      | pessoas    | id
unique      | pessoas_email_key | pessoas    | email
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

O `Open` carrega o catálogo inteiro para a memória, e as operações consultam
essa cópia. O schema do próprio catálogo fica fixo no código
(`catalogSchema`), por isso ele pode ser lido como uma tabela qualquer:
`SELECT * FROM _catalog;`. Nomes começando com `_` são reservados para o
sistema: não dá para criar tabela com eles nem fazer `INSERT` neles.

### Tabelas do sistema

Além do `_catalog`, duas tabelas do sistema não existem em disco: o
`ReadTable` monta as linhas na hora, a partir do estado do banco (como as
views `pg_settings` e `pg_tables` do PostgreSQL). Elas aceitam `SELECT` e
`WHERE`, mas não podem ser alteradas.

```
my_dbms> SELECT * FROM _settings;
name           | value | description
---------------+-------+-------------------------------------------------------------------
data_dir       | data  | pasta dos arquivos das tabelas
page_size      | 4096  | tamanho das páginas em bytes, gravado no banco quando ele é criado
format_version | 1     | versão do formato dos arquivos .mari

my_dbms> SELECT * FROM _tables;
name     | rows | pages | bytes
---------+------+-------+------
_catalog |    2 |     1 |  8192
pessoas  |    2 |     1 |  8192
```

- `_settings`: os parâmetros com que o banco está rodando. O `my_dbms.conf`
  continua existindo, porque o `data_dir` é necessário para achar o banco
  antes de abri-lo.
- `_tables`: uma linha por arquivo `.mari`, com o número de linhas, as
  páginas de dados (sem a página 0) e o tamanho do arquivo em bytes.

Para criar outra tabela do sistema: o nome e o schema em `system.go`, o
schema no mapa `tables` do `Open` e um `case` no `ReadTable`.

### Escrita

O `CREATE TABLE` cria primeiro o arquivo de dados (só com a página 0) e
depois grava no catálogo. Se o programa cair no meio, sobra no máximo um
arquivo que o catálogo não enxerga. Ele não é apagado nem sobrescrito: criar
uma tabela com esse nome dá erro até alguém remover o arquivo.

Toda escrita monta as páginas na memória e grava o arquivo inteiro em
`<nome>.mari.tmp`, que depois é renomeado por cima do original: se o
programa cair no meio, o original continua inteiro. Antes do rename vem um
`fsync` do `.tmp`, e depois um `fsync` do diretório (fora do Windows): sem
isso, uma queda de energia pode deixar o arquivo novo vazio. O `INSERT` lê as
páginas, enche a última e abre páginas novas quando ela acaba. `UPDATE` e
`DELETE` leem a tabela, mudam na memória e chamam `ReplaceRows`, que monta as
páginas do zero. O `DROP TABLE` regrava o catálogo do mesmo jeito, sem as
linhas da tabela e das chaves dela, e depois apaga o arquivo de dados.

Regravar o arquivo inteiro é simples e seguro, mas fica caro em tabelas
grandes. O próximo passo é gravar só a página que mudou, junto com um journal
que proteja contra quedas no meio da escrita.

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
