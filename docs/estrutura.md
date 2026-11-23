# Estrutura do projeto

O my_dbms é um banco de dados relacional escrito do zero em Go, para estudo.
O objetivo é ter código pequeno e direto, que dê para ler de cima a baixo.

## Árvore

```
my_dbms/
├── cmd/my_dbms/          → executável (REPL ou servidor TCP)
│   ├── main.go           → lê linhas do stdin e imprime o resultado; com -listen ou -http, sobe o servidor
│   ├── execute.go        → roda cada tipo de Statement: valida e chama o storage
│   ├── execute_test.go
│   ├── where.go          → compileWhere(): condição do WHERE → função que testa a linha;
│   │                       scope: as colunas visíveis e de que tabela é cada uma
│   ├── join.go           → JOIN por nested loop e o texto do EXPLAIN de um JOIN
│   ├── join_test.go
│   ├── plan.go           → choosePlan(): varredura completa ou índice, e o texto do EXPLAIN
│   ├── format.go         → desenha o resultado do SELECT como tabela
│   ├── format_test.go
│   ├── server.go         → servidor TCP: uma goroutine por conexão e trava de leitura/escrita
│   ├── server_test.go
│   ├── web.go            → servidor HTTP: POST /sql, para a página web/index.html
│   ├── web_test.go
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
│       ├── storage.go    → DB: catálogo, CreateTable, InsertRows, UpdateRows,
│       │                   DeleteRows, Scan, ReadTable, DropTable
│       ├── storage_test.go
│       ├── page.go       → formato em disco: cabeçalho, páginas com slots, registros
│       ├── page_test.go
│       ├── heap.go       → RID e as operações de linha numa tabela: inserir, ler, apagar, trocar
│       ├── tx.go         → transação: páginas alteradas na memória, commit com rollback journal
│       ├── tx_test.go
│       ├── btree.go      → B+tree: inserir, apagar, buscar e percorrer as folhas
│       ├── btree_test.go
│       ├── key.go        → codificação das chaves de índice em bytes que ordenam certo
│       ├── index.go      → índices: manutenção, unicidade, CreateIndex, DropIndex, IndexScan
│       ├── index_test.go
│       ├── system.go     → tabelas do sistema montadas na memória: _settings, _tables, _indexes
│       ├── system_test.go
│       └── bench_test.go → benchmarks de INSERT com chave e de leitura da tabela
├── web/
│   ├── index.html        → página para rodar SQL pelo navegador (fala com o -http)
│   └── web.go            → põe a index.html dentro do executável (go:embed)
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
SELECT p.nome, pedidos.item FROM p LEFT JOIN pedidos ON p.id = pedidos.pessoa_id;
CREATE INDEX p_nome ON p (nome);
EXPLAIN SELECT * FROM p WHERE nome = 'Ana';
DROP INDEX p_nome;
DROP TABLE p;
```

- Tipos: `INT` (inteiro de 64 bits), `REAL` (ponto flutuante de 64 bits),
  `TEXT` e `BOOLEAN` (`TRUE` ou `FALSE`). Strings com aspas simples ou
  duplas; aspa repetida dentro dela é uma aspa (`'it''s'`).
- Uma coluna `REAL` aceita `1.5` e também `2`; uma coluna `INT` recusa
  `1.5`. Os tipos não se misturam no `WHERE`: `ativo = 1` ou `id = 1.5` são
  erro, e comparar uma coluna `INT` com uma `REAL` também.
- Restrições de coluna: `NOT NULL`, `PRIMARY KEY`, `UNIQUE`. Chaves de várias
  colunas: `PRIMARY KEY (a, b)`, `UNIQUE (a, b)`.
- `WHERE`: `=`, `<>`, `<`, `>`, `<=`, `>=`, `IS [NOT] NULL`, `AND`, `OR` e
  parênteses. Comparar com `NULL` dá "desconhecido" e a linha não passa, como
  no SQL padrão; use `IS NULL`.
- Colunas podem vir qualificadas pela tabela (`p.id`) no `SELECT` e nas
  condições. `[INNER] JOIN`, `LEFT [OUTER] JOIN` e `RIGHT [OUTER] JOIN`
  juntam tabelas no `SELECT`. Veja [JOIN](#join).
- `INSERT`, `UPDATE` e `DELETE` gravam todas as linhas ou nenhuma: cada
  comando é uma transação.
- `CREATE INDEX` cria um índice B+tree (não único) e `EXPLAIN` mostra se um
  `SELECT`, `UPDATE` ou `DELETE` vai usar um índice ou ler a tabela inteira.
  Veja [Índices](#índices).

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

### Pela web

O navegador não fala TCP puro, então com `-http` o executável atende por
HTTP em vez de TCP: cada `POST /sql` leva um comando no corpo e recebe o
mesmo JSON de cima. A resposta tem `Access-Control-Allow-Origin: *`, para a
página funcionar aberta de qualquer lugar.

```
go run ./cmd/my_dbms -http localhost:4321
```

A página é `web/index.html`, um arquivo só, sem dependências. Ela vai dentro
do executável (`go:embed`, no pacote `web`) e o servidor a entrega em `/`:
basta abrir `http://localhost:4321` no navegador e rodar SQL, um comando por
linha. Aberta direto do disco, ela pede o endereço do servidor. Um `SELECT`
vira uma tabela HTML montada com `columns` e `rows`.

Cada conexão roda na sua goroutine, e todas usam o mesmo `storage.DB`. Um
`sync.RWMutex` no `server` controla o acesso: vários `SELECT` rodam juntos,
mas um comando que escreve roda sozinho. A trava é por comando, e não por
chamada ao storage, porque `UPDATE` e `DELETE` primeiro leem as linhas e
depois gravam: outro comando no meio mudaria o que foi lido. E o storage
aceita várias transações de leitura ao mesmo tempo, mas só uma de escrita
(veja [Transações](#transações-e-journal)). Travas por tabela, ou por linha,
ficam para quando houver MVCC.

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
  compileWhere()       → confere colunas e tipos e monta a função que testa a linha
        │
        ▼
  choosePlan()         → varredura completa (db.Scan) ou índice (db.IndexScan)
        │
        ▼
  storage              → lê as páginas de data/pessoas.mari (e do índice) numa transação
        │
        ▼
  selectRows()         → aplica o WHERE e escolhe as colunas pedidas
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
| `internal/storage` | Define o formato em disco, mantém o catálogo, guarda e lê linhas por endereço (RID), mantém os índices B+tree e roda cada operação numa transação com journal. | Não conhece SQL. Recebe nomes e valores já validados. |
| `cmd/my_dbms` | Liga tudo: recebe a linha, chama `Lex` e `Parse`, roda o `Statement` contra o schema, escolhe o plano (índice ou varredura), chama o storage e formata a saída. Também atende clientes por TCP (`server.go`). | Não implementa regra de formato de arquivo nem de gramática. |

`internal/` impede que código de fora do módulo importe esses pacotes, o que
nos deixa livres para mudar a API interna.

## Formato em disco

Cada tabela é um arquivo `<data_dir>/<nome>.mari` só com os registros, e
cada índice é outro arquivo `<data_dir>/<índice>.mari`, com a B+tree. Por
isso tabelas e índices dividem os nomes, como no PostgreSQL. O que descreve
tudo fica no **catálogo**, `<data_dir>/_catalog.mari`, gravado no mesmo
formato. O código do formato está em `internal/storage/page.go`.

### Páginas

O arquivo é dividido em **páginas** de `page_size` bytes (4096 por padrão),
como no SQLite e no PostgreSQL: a página N começa no byte `N × page_size`. A
página 0 é o cabeçalho do arquivo, e as outras guardam registros:

```
página 0   "MRNA" | versão (2 bytes) | page_size (2 bytes) | zeros até o fim

página N   ┌──────────────────────────────────────────────────────────────┐
           │ tipo | – | nº de slots | início dos regs. | link | slot 0 …  │  os slots crescem →
           │                                                              │
           │                        espaço livre                          │
           │                                                              │
           │                      … | registro 1 | registro 0             │  ← os registros crescem do fim
           └──────────────────────────────────────────────────────────────┘
```

O cabeçalho da página tem 10 bytes: o **tipo** (1 byte), um byte livre, o
número de slots e o início dos registros (2 bytes cada) e o **link** (4
bytes). O tipo diz o que a página guarda:

| Tipo | Página | Link |
|---|---|---|
| 1 | registros de uma tabela (heap) | não usa |
| 2 | folha da B+tree | a próxima folha |
| 3 | nó interno da B+tree | a filha mais à direita |

Cada slot tem 4 bytes: o offset e o tamanho do registro dentro da página. Os
slots crescem do começo e os registros do fim, e a página está cheia quando
os dois se encontram. Todos os inteiros do cabeçalho são little endian; os
campos de 2 bytes limitam a página a 32 KB.

O `page_size` vem do `my_dbms.conf` e é gravado na página 0 de cada arquivo
quando o banco é criado. Abrir um banco existente com outro valor é erro,
porque um arquivo de páginas de 4 KB lido como se fossem de 8 KB vira lixo. O
cabeçalho também faz um arquivo de outro formato (como o CSV das versões
antigas, ou a versão 1 do formato, de antes dos índices) ser recusado com
erro, em vez de lido errado.

Um registro precisa caber numa página vazia: o máximo é `page_size − 14`
bytes (o cabeçalho da página e um slot). Um registro maior é recusado com
erro, e nada é gravado.

### Endereço das linhas (RID)

Cada linha tem um endereço, o **RID** (row id): a página e o slot dela, como
o `ctid` do PostgreSQL. É ele que o índice guarda para achar a linha, então
ele não pode mudar à toa (`internal/storage/heap.go`):

- `DELETE` não remonta a página: só marca o slot como **morto** (tamanho 0).
  Os bytes do registro viram um buraco.
- `UPDATE` regrava o registro no mesmo slot. Se o novo for maior e não couber
  nem juntando os buracos, a linha muda de página e ganha outro RID (o índice
  é atualizado junto).
- Quando o espaço livre está espalhado em buracos, a página é **compactada**:
  os registros são juntados no fim, e os slots continuam com os mesmos
  números.
- Um `INSERT` reaproveita um slot morto da página, se houver. Ele tenta só a
  última página da tabela; o espaço livre das anteriores é aproveitado pelos
  `UPDATE`. Um mapa de espaço livre fica para depois.

### Registros

Cada linha vira bytes guiada pelos tipos das colunas:

```
bitmap de NULL | valor 0 | valor 1 | …
```

- **Bitmap de NULL**: um bit por coluna (o bit `i % 8` do byte `i / 8`),
  ligado quando o valor é `NULL`. Um `NULL` não ocupa mais nada.
- **INT**: 8 bytes (int64).
- **REAL**: 8 bytes (float64, IEEE 754). `NaN`, infinito e `-0` nunca são
  gravados: um registro com eles é tratado como corrompido.
- **BOOLEAN**: 1 byte, `0` (`FALSE`) ou `1` (`TRUE`). Outro valor é
  corrupção.
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
`NULL`, com os valores já normalizados como texto: `007` vira `7`, `1.50`
vira `1.5` (`storage.FormatReal`, sem expoente), `-0.0` vira `0` e `true`
vira `TRUE`. A conversão para int64, float64 ou byte acontece só na hora de
gravar (e de montar a chave de um índice).

### Catálogo

O catálogo tem uma linha por objeto do banco, com as colunas `type`, `name`,
`table_name` e `definition`:

```
type        | name              | table_name | definition
table       | pessoas           | pessoas    | id INT NOT NULL,email TEXT,nome TEXT
primary_key | pessoas_pkey      | pessoas    | id
unique      | pessoas_email_key | pessoas    | email
index       | pessoas_nome      | pessoas    | nome
```

| `type` | `definition` |
|---|---|
| `table` | Colunas e tipos, com `NOT NULL` quando houver: `id INT NOT NULL,nome TEXT`. |
| `primary_key` | Colunas da chave primária: `id` ou `a,b`. No máximo uma por tabela. |
| `unique` | Colunas de uma chave única. |
| `index` | Colunas de um índice criado com `CREATE INDEX`. |

A coluna `type` é o discriminador: diz que tipo de objeto a linha descreve.
Um objeto novo (chave estrangeira, view) é um valor novo de `type` e um
`case` em `loadCatalog`. Uma chave ou índice sempre vem depois da linha da
tabela dele. O nome das chaves é gerado: `<tabela>_pkey` ou
`<tabela>_<colunas>_key`. Toda chave tem um índice com o mesmo nome, no
arquivo `<nome>.mari`.

No SQL, a chave vem depois do tipo da coluna ou como item próprio da lista,
para chaves de várias colunas:

```sql
CREATE TABLE pessoas (id INT PRIMARY KEY, email TEXT UNIQUE, nome TEXT NOT NULL);
CREATE TABLE pares (a INT, b INT, PRIMARY KEY (a, b));
```

Quem confere a chave é o índice dela: antes de gravar uma linha, o storage
busca os valores da chave na B+tree e recusa a linha se já existe outra com
eles. Como no SQL padrão, colunas da chave primária viram `NOT NULL`, e
`NULL` não é igual a nada, então uma chave `UNIQUE` aceita várias linhas com
`NULL`.

O `Open` carrega o catálogo inteiro para a memória, e as operações consultam
essa cópia. O schema do próprio catálogo fica fixo no código
(`catalogSchema`), por isso ele pode ser lido como uma tabela qualquer:
`SELECT * FROM _catalog;`. Nomes começando com `_` são reservados para o
sistema: não dá para criar tabela com eles nem fazer `INSERT` neles.

### Tabelas do sistema

Além do `_catalog`, três tabelas do sistema não existem em disco: o storage
monta as linhas na hora, a partir do estado do banco (como as views
`pg_settings`, `pg_tables` e `pg_indexes` do PostgreSQL). Elas aceitam
`SELECT` e `WHERE`, mas não podem ser alteradas.

```
my_dbms> SELECT * FROM _settings;
name           | value | description
---------------+-------+-------------------------------------------------------------------
data_dir       | data  | pasta dos arquivos das tabelas
page_size      | 4096  | tamanho das páginas em bytes, gravado no banco quando ele é criado
format_version | 2     | versão do formato dos arquivos .mari

my_dbms> SELECT * FROM _tables;
name     | rows | pages | bytes
---------+------+-------+------
_catalog |    3 |     1 |  8192
p        |    3 |     1 |  8192

my_dbms> SELECT * FROM _indexes;
name    | table_name | type        | columns | pages | height
--------+------------+-------------+---------+-------+-------
p_pkey  | p          | primary_key | id      |     1 |      1
p_idade | p          | index       | idade   |     1 |      1
```

- `_settings`: os parâmetros com que o banco está rodando. O `my_dbms.conf`
  continua existindo, porque o `data_dir` é necessário para achar o banco
  antes de abri-lo.
- `_tables`: uma linha por tabela em disco, com o número de linhas, as
  páginas de dados (sem a página 0) e o tamanho do arquivo em bytes.
- `_indexes`: uma linha por índice, com as páginas e a **altura** da B+tree
  (1 quando a raiz ainda é a única folha).

Para criar outra tabela do sistema: o nome e o schema em `system.go`, o
schema no mapa `tables` do `Open`, o nome em `isVirtual` e um `case` em
`systemRows`.

### Transações e journal

Toda operação do storage roda numa **transação** (`internal/storage/tx.go`).
A transação abre os arquivos que precisa, lê as páginas pelo número (`ReadAt`
no byte `N × page_size`) e guarda na memória as páginas que alterou, sem
tocar no disco. No fim:

- **Deu erro** (chave repetida na sétima linha de um `INSERT`, registro
  grande demais): a transação só fecha os arquivos. Nada foi gravado, então
  não há o que desfazer.
- **Deu certo**: o commit grava as páginas com um **rollback journal**, como
  o SQLite:
  1. copia a versão original de cada página que vai mudar para o arquivo
     `<data_dir>/_journal`, com o tamanho original de cada arquivo e um
     CRC32 no fim, e faz o `fsync`;
  2. grava as páginas novas nos arquivos (tabela, índices, catálogo) e faz o
     `fsync` de cada um;
  3. apaga o `_journal`. Esse é o momento do commit.

Se o programa cair no passo 2, os arquivos ficam com parte das páginas
novas. O próximo `Open` acha o `_journal`, devolve as páginas originais e
corta os arquivos no tamanho original. Se cair no passo 1, o CRC32 não bate:
os arquivos ainda não foram tocados, e o `_journal` incompleto é só apagado.
Como uma transação cobre todos os arquivos que um comando mexe, a linha e as
entradas dela nos índices sempre mudam juntas.

Os arquivos ficam abertos só durante a transação. Várias transações de
leitura podem rodar ao mesmo tempo, mas só uma de escrita, porque cada uma
guarda as próprias páginas alteradas; quem garante isso é quem chama (o
REPL tem um comando por vez, e o servidor usa uma trava).

O `CREATE TABLE` e o `CREATE INDEX` criam primeiro os arquivos novos (com
`fsync`) e depois gravam no catálogo, numa transação. Se o programa cair no
meio, sobra no máximo um arquivo que o catálogo não enxerga. Ele não é
apagado nem sobrescrito: criar um objeto com esse nome dá erro até alguém
remover o arquivo. O `DROP TABLE` e o `DROP INDEX` fazem o contrário: tiram
as linhas do catálogo numa transação e depois apagam os arquivos.

## Índices

Cada índice é uma **B+tree** num arquivo próprio
(`internal/storage/btree.go`), com as páginas no mesmo formato com slots das
tabelas:

```
                  página 1: raiz (nó interno)
          ┌───────────────────────────────────────────┐
          │ (k1, pág 2) | (k2, pág 3) | link = pág 4  │
          └───────────────────────────────────────────┘
               /                |                \
          < k1 /       >= k1 e < k2 |                 \ >= k2
  ┌─────────────┐      ┌─────────────┐      ┌─────────────┐
  │ folha pág 2 │ ───→ │ folha pág 3 │ ───→ │ folha pág 4 │ ───→ fim
  └─────────────┘      └─────────────┘      └─────────────┘
```

- **Folhas**: as entradas do índice em ordem, uma por slot. Cada entrada é a
  chave codificada seguida do RID da linha (6 bytes). O link aponta para a
  próxima folha, então percorrer um intervalo é andar pelas folhas.
- **Nós internos**: registros `chave | página filha`. A filha de cada
  registro tem as entradas menores que a chave dele; o link aponta para a
  filha com as entradas maiores ou iguais à última chave.
- A **raiz é sempre a página 1**. Quando ela enche, o conteúdo desce para
  duas páginas novas e ela vira um nó interno com uma chave só. Assim a
  altura cresce pela raiz, e ninguém precisa guardar onde a raiz está.
- Quando uma página enche, ela é dividida no meio (contando bytes) e a chave
  que separa as metades sobe para o pai.
- Apagar nunca junta páginas: uma folha pode até ficar vazia. O PostgreSQL
  também não junta no `DELETE` comum.
- Uma entrada pode ter no máximo um quarto da página (um pouco menos de 250
  bytes com páginas de 1 KB, quase 1 KB com as de 4 KB), para dividir uma
  página cheia sempre dar duas metades que cabem. Um valor maior numa coluna
  indexada é recusado com erro.

Como o RID vai no fim, toda entrada é única, mesmo num índice que aceita
valores repetidos, e apagar a entrada de uma linha acha exatamente ela.

### Codificação das chaves

A B+tree compara entradas com `bytes.Compare`, sem saber os tipos das
colunas. Para isso, a chave é codificada de um jeito em que a ordem dos bytes
é a ordem dos valores (`internal/storage/key.go`):

| Valor | Bytes |
|---|---|
| `NULL` | `00` (vem antes de tudo) |
| `INT` | `01` + 8 bytes big endian com o bit de sinal invertido |
| `REAL` | `01` + os 8 bytes do float64 em big endian: positivo com o bit de sinal ligado, negativo com todos os bits invertidos |
| `BOOLEAN` | `01` + `00` (`FALSE`) ou `01` (`TRUE`) |
| `TEXT` | `01` + os bytes do texto, com cada `00` virando `00 FF`, + `00 01` |

Inverter o bit de sinal põe os negativos antes dos positivos. No `REAL` é
preciso mais um passo: no IEEE 754, os bits de um negativo crescem com o
módulo (`-2` tem bits maiores que `-1`), então os negativos têm todos os
bits invertidos, o que também desliga o bit de sinal. O terminador
`00 01` do texto é menor que qualquer continuação, então `'a'` vem antes de
`'ab'`, e numa chave de várias colunas `('a', 'bc')` não se confunde com
`('ab', 'c')`. Prefixar o tamanho, como no registro, não serviria: `'b'`
(tamanho 1) viria antes de `'aa'` (tamanho 2).

### Escolha do plano

O `choosePlan` (`cmd/my_dbms/plan.go`) decide como um `SELECT`, `UPDATE` ou
`DELETE` lê as linhas (um `SELECT` com JOIN não passa por ele: veja
[JOIN](#join)). Ele olha só as comparações `coluna op valor` ligadas
por `AND` no nível de cima do `WHERE` (com `OR`, uma linha pode passar pelo
lado que o índice não cobre):

- um índice `(a, b, c)` serve quando o `WHERE` fixa as primeiras colunas com
  `=` e, opcionalmente, limita a coluna seguinte com `<`, `<=`, `>` ou `>=`;
- ganha o índice que fixa mais colunas (um intervalo vale menos que um `=`),
  e o empate fica com o primeiro da tabela, que é a chave primária;
- se nenhum serve, é varredura completa.

O índice é só um filtro grosso: o `WHERE` inteiro continua sendo aplicado em
cada linha que ele devolve. Sem `ORDER BY`, as linhas lidas pelo índice saem
na ordem da chave, e as da varredura, na ordem do arquivo.

```
my_dbms> EXPLAIN SELECT * FROM p WHERE idade >= 30 AND idade < 40 AND nome <> 'x';
busca no índice p_idade da tabela p (idade >= 30 AND idade < 40)
my_dbms> EXPLAIN SELECT * FROM p WHERE idade = 30 OR id = 1;
varredura completa na tabela p
```

## JOIN

```sql
SELECT pessoas.nome, pedidos.item
FROM pessoas
LEFT JOIN pedidos ON pessoas.id = pedidos.pessoa_id
WHERE pedidos.id IS NULL;   -- as pessoas sem pedido
```

- `[INNER] JOIN`, `LEFT [OUTER] JOIN` e `RIGHT [OUTER] JOIN`, cada um com o
  seu `ON`. Vários JOINs são juntados da esquerda para a direita:
  `a JOIN b ON … LEFT JOIN c ON …` é `(a JOIN b) LEFT JOIN c`.
- Uma coluna pode vir como `tabela.coluna` no `SELECT`, no `ON` e no `WHERE`
  (com ou sem JOIN). Sem a tabela, o nome precisa existir numa tabela só; se
  estiver em duas, é erro de coluna ambígua. Quem resolve os nomes é o
  `scope` (`cmd/my_dbms/where.go`): as colunas de todas as tabelas, uma
  depois da outra, e a tabela de cada uma.
- O `ON` enxerga só as tabelas que vieram antes dele; o `WHERE` e as colunas
  do `SELECT` enxergam todas.
- O `ON` decide quem casa, e o `WHERE` filtra depois. Num `LEFT JOIN`, uma
  condição sobre a tabela da direita no `ON` mantém as linhas da esquerda sem
  par (com `NULL`); a mesma condição no `WHERE` tira essas linhas. Como no
  `WHERE`, só casa o que dá verdadeiro: `NULL = NULL` não casa.
- `LEFT` mantém as linhas da esquerda sem par, com `NULL` nas colunas da
  direita. `RIGHT` faz o contrário, e as linhas da direita sem par vêm no
  fim, depois dos pares.
- `SELECT *` traz as colunas na ordem do `FROM` e dos JOINs. No cabeçalho,
  uma coluna ganha a tabela na frente (`pessoas.id`) só quando outra coluna
  do resultado, de outra tabela, tem o mesmo nome. O servidor manda esse
  mesmo nome em `columns`.
- Ainda não há alias, então uma tabela não pode aparecer duas vezes no mesmo
  `FROM`.
- `JOIN`, `INNER`, `LEFT`, `RIGHT` e `OUTER` são keywords: não servem mais
  como nome de tabela ou coluna.

O JOIN roda como **nested loop** (`cmd/my_dbms/join.go`). Cada tabela é lida
inteira para a memória uma vez (`db.ReadTable`) e, para cada linha juntada
até ali, o loop percorre todas as linhas da próxima tabela e testa o `ON` no
par. Ler tudo antes deixa só uma transação de leitura aberta por vez e
permite percorrer o lado de dentro de novo para cada linha de fora. O custo
é `n × m` comparações e a memória das tabelas inteiras; usar índice no JOIN
(index nested loop) ou hash join fica para depois. O `EXPLAIN` mostra isso:

```
my_dbms> EXPLAIN SELECT * FROM pessoas LEFT JOIN pedidos ON pessoas.id = pessoa_id;
varredura completa na tabela pessoas
LEFT JOIN por nested loop: varredura completa na tabela pedidos
```

## Como adicionar um comando

1. Se o comando usa uma palavra nova, adicione em `keywords` em
   `internal/sql/token.go`.
2. Em `internal/parser/ast.go`, crie o tipo do comando com o método
   `statement()`. Em `parser.go`, adicione um `case` em `statement` e o
   método que lê a gramática dele, usando os ajudantes `expect`, `accept`, `ident`,
   `columnRef`, `literal`, `list` e `names`.
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

- `internal/engine`: executar a AST e escolher o plano, tirando essa lógica
  do `cmd/`. Fica para quando houver mais de um jeito de juntar linhas (JOIN)
  ou de ordenar (ORDER BY).
- `internal/index`: hoje a B+tree mora em `internal/storage`, porque usa as
  mesmas páginas e a mesma transação das tabelas. Se ela crescer (outras
  estruturas, como hash), pode ganhar pacote próprio.

Não crie o pacote antes da hora: deixe o código pedir.
