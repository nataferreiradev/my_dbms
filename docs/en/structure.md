# Project structure

my_dbms is a relational database written from scratch in Go, as a learning
project. The goal is small, straightforward code that you can read top to
bottom.

## Tree

```
my_dbms/
├── cmd/my_dbms/          → executable (REPL, TCP/HTTP server or MCP)
│   ├── main.go           → reads lines from stdin and prints the result; with -listen, -http or -mcp, starts the server
│   ├── execute.go        → runs each kind of Statement: validates and calls storage
│   ├── execute_test.go
│   ├── where.go          → compileWhere(): WHERE condition → function that tests a row;
│   │                       scope: the visible columns and the table of each one
│   ├── join.go           → nested loop JOIN and the EXPLAIN text of a JOIN
│   ├── join_test.go
│   ├── plan.go           → choosePlan(): full scan or index, and the EXPLAIN text
│   ├── format.go         → renders the SELECT result as a table
│   ├── format_test.go
│   ├── server.go         → TCP server: one goroutine per connection and a read/write lock
│   ├── server_test.go
│   ├── web.go            → HTTP server: POST /sql, for the web/index.html page
│   ├── web_test.go
│   ├── mcp.go            → MCP server on stdin/stdout, for AI assistants
│   ├── mcp_test.go
│   ├── setup.go          → the -setup wizard: picks the embeddings API and stores the key
│   ├── setup_test.go
│   ├── search_test.go    → tests of WITH EMBEDDING, CREATE VECTOR INDEX and SEARCH
│   └── bench_test.go     → benchmark of SELECT by key
├── internal/
│   ├── config/           → database parameters
│   │   ├── config.go     → Config, Default(), Load(): reads my_dbms.conf; Save() writes it;
│   │   │                   APIKey(), SaveAPIKey(): the user's credentials file
│   │   └── config_test.go
│   ├── embed/            → embeddings: text → vector
│   │   ├── embed.go      → Client: calls an OpenAI-style API; Fake() for tests
│   │   └── embed_test.go
│   ├── sql/              → lexical analysis of SQL
│   │   ├── token.go      → Token, TokenType, keywords, operators, symbols
│   │   ├── lexer.go      → Lex(): string → []Token
│   │   └── lexer_test.go
│   ├── parser/           → syntax analysis
│   │   ├── ast.go        → Statement and the type of each command (the AST)
│   │   ├── parser.go     → Parse(): []Token → Statement
│   │   └── parser_test.go
│   └── storage/          → on-disk persistence
│       ├── storage.go    → DB: catalog, CreateTable, InsertRows, UpdateRows,
│       │                   DeleteRows, Scan, ReadTable, DropTable
│       ├── storage_test.go
│       ├── page.go       → on-disk format: header, slotted pages, records
│       ├── page_test.go
│       ├── heap.go       → RID and row operations on a table: insert, read, delete, replace
│       ├── tx.go         → transaction: changed pages in memory, commit with a rollback journal
│       ├── tx_test.go
│       ├── btree.go      → B+tree: insert, delete, seek and walk the leaves
│       ├── btree_test.go
│       ├── key.go        → encoding of index keys into bytes that sort correctly
│       ├── index.go      → indexes: maintenance, uniqueness, CreateIndex, DropIndex, IndexScan
│       ├── index_test.go
│       ├── vector.go     → vector index: int8 vectors, maintenance, CreateVectorIndex, Search
│       ├── vector_test.go
│       ├── system.go     → system tables built in memory: _settings, _tables, _indexes
│       ├── system_test.go
│       └── bench_test.go → benchmarks of INSERT with a key and of reading a table
├── web/
│   ├── index.html        → page to run SQL from the browser (talks to -http)
│   └── web.go            → builds index.html into the executable (go:embed)
├── .github/workflows/
│   └── ci.yml            → GitHub Actions: gofmt, go vet and tests on Linux and Windows
├── docs/                 → project documentation (Portuguese)
│   └── en/               → English version
├── my_dbms.conf          → parameters (data_dir etc.)
└── data/                 → created at runtime (ignored by git)
```

## Configuration

On startup, `main.go` reads `my_dbms.conf` from the folder the program runs
in. The format is `key = value`, one per line, with `#` for comments. If the
file doesn't exist, the defaults from `config.Default()` apply.

| Parameter | Default | What it is |
|---|---|---|
| `data_dir` | `data` | Folder for the table files. A relative path starts from the folder the program runs in. |
| `page_size` | `4096` | Size of the file pages, in bytes: a power of 2 between 1024 and 32768. It is stored in the database when the database is created; opening an existing database with a different value is an error. |
| `embedding_url` | empty | Base address of an OpenAI-style embeddings API, like `https://api.openai.com/v1`. Empty turns vector indexes off. See [Vector index](#vector-index-and-search). |
| `embedding_model` | empty | Model requested from the API, like `text-embedding-3-small`. Required with `embedding_url`. |

To add a parameter: a field in `Config`, the default value in `Default()`, a
`case` in `Load()` and a commented line in `my_dbms.conf`. An unknown key is
an error, so a typo doesn't slip by silently.

`my_dbms.conf` is committed to git, so **the API key never goes in it**: a
parameter with `key` in its name is rejected. The key lives in the user's
credentials file (`config.CredentialsPath()`: `%AppData%\my_dbms\credentials`
on Windows, `~/.config/my_dbms/credentials` on Linux), outside the project,
with `0600` permissions (on Windows, the profile folder protects it, since
other users can't read it). The file stores the key together with the API
address, and the key is only used with that address: changing
`embedding_url` by hand doesn't send the key to another server. The
`MY_DBMS_API_KEY` variable takes precedence over the file, which is handy in
Docker and CI.

The `-setup` wizard fills all of this in:

```
go run ./cmd/my_dbms -setup
```

It lists the providers (local Ollama with no key, OpenAI, Voyage, Gemini or
any other OpenAI-style API), asks for the address, model and key (hidden
while typing), **tests the connection** by requesting an embedding and checks
that the vectors fit in a page. It only saves if the test passes, or if the
user confirms. Run again, the current settings become the defaults. Hidden
typing uses `golang.org/x/term`, the project's only dependency.

## Accepted SQL

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
CREATE TABLE docs (id INT PRIMARY KEY, titulo TEXT, corpo TEXT) WITH EMBEDDING (titulo, corpo);
CREATE VECTOR INDEX p_vec ON p (nome);
SEARCH docs FOR 'gatos com medo de água' TOP 5;
```

- Types: `INT` (64-bit integer), `REAL` (64-bit floating point), `TEXT` and
  `BOOLEAN` (`TRUE` or `FALSE`). Strings use single or double quotes; a
  doubled quote inside one is a quote (`'it''s'`).
- A `REAL` column accepts `1.5` and also `2`; an `INT` column rejects `1.5`.
  Types don't mix in `WHERE`: `ativo = 1` or `id = 1.5` are errors, and so is
  comparing an `INT` column with a `REAL` one.
- Column constraints: `NOT NULL`, `PRIMARY KEY`, `UNIQUE`. Multi-column keys:
  `PRIMARY KEY (a, b)`, `UNIQUE (a, b)`.
- `WHERE`: `=`, `<>`, `<`, `>`, `<=`, `>=`, `IS [NOT] NULL`, `AND`, `OR` and
  parentheses. Comparing with `NULL` is "unknown" and the row doesn't pass,
  as in standard SQL; use `IS NULL`.
- Columns can be qualified by their table (`p.id`) in `SELECT` and in
  conditions. `[INNER] JOIN`, `LEFT [OUTER] JOIN` and `RIGHT [OUTER] JOIN`
  combine tables in a `SELECT`. See [JOIN](#join).
- `INSERT`, `UPDATE` and `DELETE` write all rows or none: each command is a
  transaction.
- `CREATE INDEX` creates a (non-unique) B+tree index, and `EXPLAIN` shows
  whether a `SELECT`, `UPDATE` or `DELETE` will use an index or read the
  whole table. See [Indexes](#indexes).
- `WITH EMBEDDING` and `CREATE VECTOR INDEX` create the vector index, and
  `SEARCH` searches by meaning. See [Vector index](#vector-index-and-search).

## Server

With `-listen`, the executable doesn't open the REPL: it serves clients over
TCP.

```
go run ./cmd/my_dbms -listen localhost:4321
```

The protocol is plain text, so you can try it with `nc localhost 4321`: the
client sends one command per line and gets one line of JSON back for each.

```
→ SELECT nome FROM p WHERE id = 1;
← {"output":"nome\n----\nAna\n(1 linha)"}
→ DROP TABLE nada;
← {"error":"tabela \"nada\" não existe"}
```

A `SELECT` also returns `columns` and `rows`, so the client can render the
table its own way (the web UI draws it in HTML). Values come as text and
`NULL` comes as `null`:

```
→ SELECT * FROM p WHERE id > 1;
← {"output":"…","columns":[{"name":"id","type":"INT"},{"name":"nome","type":"TEXT"}],"rows":[["2","Bia"],["3",null]]}
```

An empty line gets `{}`, and `exit` or `quit` close the connection.

### From the web

Browsers don't speak raw TCP, so with `-http` the executable serves HTTP
instead of TCP: each `POST /sql` carries one command in the body and gets the
same JSON as above. The response has `Access-Control-Allow-Origin: *`, so
the page works when opened from anywhere.

```
go run ./cmd/my_dbms -http localhost:4321
```

The page is `web/index.html`, a single file with no dependencies. It is built
into the executable (`go:embed`, in the `web` package) and the server serves
it at `/`: open `http://localhost:4321` in a browser and run SQL, one command
per line. Opened straight from disk, it asks for the server address. A
`SELECT` becomes an HTML table built from `columns` and `rows`.

Each connection runs in its own goroutine, and all of them share the same
`storage.DB`. A `sync.RWMutex` in `server` guards access: many `SELECT`s run
together, but a command that writes runs alone. The lock is per command, not
per storage call, because `UPDATE` and `DELETE` first read the rows and then
write: another command in between would change what was read. And storage
accepts many read transactions at once, but only one write transaction (see
[Transactions](#transactions-and-the-journal)). Per-table or per-row locks
can come with MVCC.

The server and the REPL shouldn't open the same `data_dir` at the same time:
they are two processes, and the lock only works inside one.

### From AI (MCP)

With `-mcp`, the executable speaks [MCP](https://modelcontextprotocol.io)
(Model Context Protocol) on stdin and stdout, so an AI assistant can query
the database. The client (Claude Code, Claude Desktop, an editor) is the one
that starts the process. In Claude Code, from the project folder:

```
go build -o my_dbms.exe ./cmd/my_dbms
claude mcp add my_dbms -- ./my_dbms.exe -mcp
```

The protocol is JSON-RPC 2.0, one message per line (`cmd/my_dbms/mcp.go`).
The assistant sees these tools:

| Tool | Does |
|---|---|
| `schema` | Shows `_catalog`: the tables, columns and keys. |
| `query` | Runs a `SELECT`, a `SEARCH` or an `EXPLAIN`. Any other command is refused. |
| `execute` | Runs any command. Only exists with `-mcp-write`. |

Without `-mcp-write`, the AI can't change anything. A command's error comes
back as the tool result (`isError`), not as a protocol error, so the AI can
read the message and fix the SQL. On initialization, the server also sends
short instructions saying what the SQL accepts and what is still missing
(like `ORDER BY`), so the AI doesn't waste time trying.

`my_dbms.conf` and a relative `data_dir` are resolved from the folder where
the client starts the process. Since stdout belongs to the protocol, log
messages go to stderr. The client sends one request at a time, so there is
no lock; and, as with the server, MCP shouldn't open the same `data_dir` as
another process.

## How a command flows

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
  execute()            → checks the Statement type and calls selectRows
        │
        ▼
  compileWhere()       → checks columns and types and builds the function that tests a row
        │
        ▼
  choosePlan()         → full scan (db.Scan) or index (db.IndexScan)
        │
        ▼
  storage              → reads the pages of data/pessoas.mari (and of the index) in a transaction
        │
        ▼
  selectRows()         → applies the WHERE and picks the requested columns
        │
        ▼
  formatTable()        → builds the aligned text the REPL prints
```

## What each package owns

| Package | Does | Does not |
|---|---|---|
| `internal/sql` | Turns text into classified tokens. Normalizes keywords to uppercase and strips quotes from strings. | Know that disks or tables exist. |
| `internal/parser` | Checks the grammar and turns tokens into an AST (`Statement`). Syntax errors come from here, with the position. | Know whether the table or column exists. |
| `internal/config` | Reads the parameter file and applies defaults. | Open the database or know about SQL. |
| `internal/storage` | Defines the on-disk format, maintains the catalog, stores and reads rows by address (RID), maintains the B+tree and vector indexes and runs every operation in a transaction with a journal. | Know about SQL or HTTP: it receives names and values that are already validated, and embeddings come from the `DB.Embed` function. |
| `internal/embed` | Turns texts into vectors by calling an OpenAI-style embeddings API, in batches. | Know about tables or disks. |
| `cmd/my_dbms` | Connects everything: takes the line, calls `Lex` and `Parse`, runs the `Statement` against the schema, picks the plan (index or scan), calls storage and formats the output. Also serves clients over TCP (`server.go`). | Implement file-format or grammar rules. |

`internal/` prevents code outside the module from importing these packages,
which leaves us free to change the internal API.

## On-disk format

Each table is a file at `<data_dir>/<name>.mari` holding only its records,
and each index is another file, `<data_dir>/<index>.mari`, with the B+tree.
That's why tables and indexes share names, as in PostgreSQL. What describes
everything lives in the **catalog**, `<data_dir>/_catalog.mari`, stored in
the same format. The format code is in `internal/storage/page.go`.

### Pages

The file is split into **pages** of `page_size` bytes (4096 by default), as
in SQLite and PostgreSQL: page N starts at byte `N × page_size`. Page 0 is
the file header, and the others hold records:

```
page 0   "MRNA" | version (2 bytes) | page_size (2 bytes) | zeros to the end

page N   ┌──────────────────────────────────────────────────────────────┐
         │ type | – | slot count | start of records | link | slot 0 …   │  slots grow →
         │                                                              │
         │                         free space                           │
         │                                                              │
         │                    … | record 1 | record 0                   │  ← records grow from the end
         └──────────────────────────────────────────────────────────────┘
```

The page header is 10 bytes: the **type** (1 byte), a free byte, the slot
count and the start of records (2 bytes each) and the **link** (4 bytes).
The type says what the page holds:

| Type | Page | Link |
|---|---|---|
| 1 | records of a table (heap) | unused |
| 2 | B+tree leaf | the next leaf |
| 3 | B+tree internal node | the rightmost child |

Each slot is 4 bytes: the offset and size of the record inside the page.
Slots grow from the start and records from the end, and the page is full
when the two meet. All header integers are little endian; the 2-byte fields
limit a page to 32 KB.

`page_size` comes from `my_dbms.conf` and is written to page 0 of every file
when the database is created. Opening an existing database with a different
value is an error, because a file of 4 KB pages read as 8 KB pages turns into
garbage. The header also makes a file in another format (like the CSV of
older versions, or format version 1, from before indexes) get rejected with
an error instead of read wrong.

A record must fit in an empty page: the maximum is `page_size − 14` bytes
(the page header and one slot). A bigger record is rejected with an error,
and nothing is written.

### Row addresses (RID)

Each row has an address, the **RID** (row id): its page and slot, like
PostgreSQL's `ctid`. It's what an index stores to find the row, so it can't
change for no reason (`internal/storage/heap.go`):

- `DELETE` doesn't rebuild the page: it only marks the slot **dead** (size
  0). The record's bytes become a hole.
- `UPDATE` rewrites the record in the same slot. If the new one is bigger and
  doesn't fit even after merging the holes, the row moves to another page and
  gets a new RID (the indexes are updated along with it).
- When free space is scattered in holes, the page is **compacted**: records
  are packed at the end, and slots keep their numbers.
- An `INSERT` reuses a dead slot in the page, if there is one. It only tries
  the table's last page; free space in earlier pages is used by `UPDATE`s. A
  free space map is left for later.

### Records

Each row becomes bytes guided by the column types:

```
NULL bitmap | value 0 | value 1 | …
```

- **NULL bitmap**: one bit per column (bit `i % 8` of byte `i / 8`), set when
  the value is `NULL`. A `NULL` takes no other space.
- **INT**: 8 bytes (int64).
- **REAL**: 8 bytes (float64, IEEE 754). `NaN`, infinity and `-0` are never
  written: a record holding them is treated as corrupt.
- **BOOLEAN**: 1 byte, `0` (`FALSE`) or `1` (`TRUE`). Any other value is
  corruption.
- **TEXT**: the length as a uvarint (1 byte up to 127, 2 bytes up to
  16383…) followed by the text bytes.

Since the length comes before the data, there is no escaping: the text may
contain commas, quotes or `\N`. The size of the whole record lives in the
slot. Example, `(1, 'Ana', NULL)` in `pessoas (id INT, nome TEXT, email
TEXT)`:

```
04                          bitmap 0000_0100: email is NULL
01 00 00 00 00 00 00 00     id = 1
03 41 6E 61                 nome = "Ana"
```

In memory, each cell is still a `storage.Value`: a text or `NULL`, with
values already normalized as text: `007` becomes `7`, `1.50` becomes `1.5`
(`storage.FormatReal`, no exponent), `-0.0` becomes `0` and `true` becomes
`TRUE`. The conversion to int64, float64 or a byte only happens when writing
(and when building an index key).

### Catalog

The catalog has one row per database object, with the columns `type`,
`name`, `table_name` and `definition`:

```
type        | name              | table_name | definition
table       | pessoas           | pessoas    | id INT NOT NULL,email TEXT,nome TEXT
primary_key | pessoas_pkey      | pessoas    | id
unique      | pessoas_email_key | pessoas    | email
index       | pessoas_nome      | pessoas    | nome
```

| `type` | `definition` |
|---|---|
| `table` | Columns and types, with `NOT NULL` when present: `id INT NOT NULL,nome TEXT`. |
| `primary_key` | Primary key columns: `id` or `a,b`. At most one per table. |
| `unique` | Columns of a unique key. |
| `index` | Columns of an index created with `CREATE INDEX`. |
| `vector` | Columns of the vector index (`WITH EMBEDDING` or `CREATE VECTOR INDEX`). At most one per table. |

The `type` column is the discriminator: it says what kind of object the line
describes. A new object (foreign key, view) is a new `type` value and a
`case` in `loadCatalog`. A key or index always comes after its table's line.
Key names are generated: `<table>_pkey` or `<table>_<columns>_key`. Every key
has an index with the same name, in the file `<name>.mari`.

In SQL, a key goes after the column type, or as its own list item for
multi-column keys:

```sql
CREATE TABLE pessoas (id INT PRIMARY KEY, email TEXT UNIQUE, nome TEXT NOT NULL);
CREATE TABLE pares (a INT, b INT, PRIMARY KEY (a, b));
```

The key's index is what enforces it: before writing a row, storage looks the
key's values up in the B+tree and rejects the row if another one already has
them. As in standard SQL, primary key columns become `NOT NULL`, and
`NULL` equals nothing, so a `UNIQUE` key accepts several rows with `NULL`.

`Open` loads the whole catalog into memory, and operations read that copy.
The catalog's own schema is fixed in code (`catalogSchema`), so it can be
read like any other table: `SELECT * FROM _catalog;`. Names starting with `_`
are reserved for the system: you can't create a table with one or `INSERT`
into it.

### System tables

Besides `_catalog`, three system tables don't exist on disk: storage builds
their rows on the spot from the database state (like PostgreSQL's
`pg_settings`, `pg_tables` and `pg_indexes` views). They accept `SELECT` and
`WHERE`, but can't be changed.

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

- `_settings`: the parameters the database is running with. `my_dbms.conf`
  still exists, because `data_dir` is needed to find the database before
  opening it.
- `_tables`: one row per table on disk, with the row count, the data pages
  (not counting page 0) and the file size in bytes.
- `_indexes`: one row per index, with its pages and the B+tree **height** (1
  while the root is still the only leaf). The vector index isn't a tree: its
  height is `NULL`.

To add another system table: its name and schema in `system.go`, the schema
in the `tables` map in `Open`, the name in `isVirtual` and a `case` in
`systemRows`.

### Transactions and the journal

Every storage operation runs in a **transaction** (`internal/storage/tx.go`).
The transaction opens the files it needs, reads pages by number (`ReadAt` at
byte `N × page_size`) and keeps the pages it changed in memory, without
touching the disk. At the end:

- **It failed** (a duplicate key on the seventh row of an `INSERT`, a record
  too big): the transaction just closes the files. Nothing was written, so
  there is nothing to undo.
- **It worked**: the commit writes the pages with a **rollback journal**, like
  SQLite:
  1. copy the original version of every page about to change to the file
     `<data_dir>/_journal`, with each file's original size and a CRC32 at the
     end, and `fsync` it;
  2. write the new pages to the files (table, indexes, catalog) and `fsync`
     each one;
  3. delete `_journal`. That's the moment of commit.

If the program dies in step 2, the files hold part of the new pages. The next
`Open` finds `_journal`, puts the original pages back and truncates the files
to their original size. If it dies in step 1, the CRC32 doesn't match: the
files haven't been touched yet, and the incomplete `_journal` is just
deleted. Since one transaction covers every file a command touches, a row and
its index entries always change together.

Files stay open only during the transaction. Many read transactions can run
at once, but only one write transaction, because each one keeps its own
changed pages; the caller guarantees that (the REPL runs one command at a
time, and the server uses a lock).

`CREATE TABLE` and `CREATE INDEX` first create the new files (with `fsync`)
and then write to the catalog, in a transaction. If the program dies in
between, at worst a file is left that the catalog doesn't see. It is never
deleted or overwritten: creating an object with that name fails until
someone removes the file. `DROP TABLE` and `DROP INDEX` do the opposite: they
remove the catalog rows in a transaction and then delete the files.

## Indexes

Each index is a **B+tree** in its own file (`internal/storage/btree.go`),
with pages in the same slotted format as tables:

```
                  page 1: root (internal node)
          ┌─────────────────────────────────────────────┐
          │ (k1, page 2) | (k2, page 3) | link = page 4 │
          └─────────────────────────────────────────────┘
               /                |                \
          < k1 /      >= k1 and < k2 |                \ >= k2
  ┌─────────────┐      ┌─────────────┐      ┌─────────────┐
  │ leaf page 2 │ ───→ │ leaf page 3 │ ───→ │ leaf page 4 │ ───→ end
  └─────────────┘      └─────────────┘      └─────────────┘
```

- **Leaves**: the index entries in order, one per slot. Each entry is the
  encoded key followed by the row's RID (6 bytes). The link points to the
  next leaf, so walking a range means walking the leaves.
- **Internal nodes**: records `key | child page`. Each record's child holds
  the entries smaller than its key; the link points to the child with the
  entries greater than or equal to the last key.
- The **root is always page 1**. When it fills up, its content moves down to
  two new pages and it becomes an internal node with a single key. So the
  tree grows at the root, and nobody needs to store where the root is.
- When a page fills up, it splits in the middle (counting bytes) and the key
  separating the halves goes up to the parent.
- Deleting never merges pages: a leaf may even become empty. PostgreSQL
  doesn't merge on a regular `DELETE` either.
- An entry can be at most a quarter of a page (a bit under 250 bytes with 1
  KB pages, almost 1 KB with 4 KB pages), so that splitting a full page
  always gives two halves that fit. A bigger value in an indexed column is
  rejected with an error.

Since the RID goes at the end, every entry is unique, even in an index that
allows repeated values, and deleting a row's entry finds exactly it.

### Key encoding

The B+tree compares entries with `bytes.Compare`, without knowing the column
types. For that, keys are encoded so that byte order is value order
(`internal/storage/key.go`):

| Value | Bytes |
|---|---|
| `NULL` | `00` (sorts before everything) |
| `INT` | `01` + 8 bytes big endian with the sign bit flipped |
| `REAL` | `01` + the 8 float64 bytes in big endian: positives with the sign bit set, negatives with every bit flipped |
| `BOOLEAN` | `01` + `00` (`FALSE`) or `01` (`TRUE`) |
| `TEXT` | `01` + the text bytes, with each `00` becoming `00 FF`, + `00 01` |

Flipping the sign bit puts negatives before positives. `REAL` needs one more
step: in IEEE 754, a negative's bits grow with its magnitude (`-2` has larger
bits than `-1`), so negatives get every bit flipped, which also clears the
sign bit. The text terminator
`00 01` is smaller than any continuation, so `'a'` sorts before `'ab'`, and in
a multi-column key `('a', 'bc')` isn't confused with `('ab', 'c')`. A length
prefix, as in records, wouldn't work: `'b'` (length 1) would sort before
`'aa'` (length 2).

### Choosing the plan

`choosePlan` (`cmd/my_dbms/plan.go`) decides how a `SELECT`, `UPDATE` or
`DELETE` reads its rows (a `SELECT` with a JOIN doesn't go through it: see
[JOIN](#join)). It only looks at `column op value` comparisons joined
by `AND` at the top level of the `WHERE` (with `OR`, a row may pass through
the side the index doesn't cover):

- an index `(a, b, c)` fits when the `WHERE` fixes its first columns with `=`
  and, optionally, bounds the next column with `<`, `<=`, `>` or `>=`;
- the index that fixes the most columns wins (a range counts less than an
  `=`), and ties go to the table's first index, which is the primary key;
- if none fits, it's a full scan.

The index is only a coarse filter: the whole `WHERE` is still applied to
every row it returns. Without `ORDER BY`, rows read through an index come out
in key order, and rows from a scan in file order.

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
WHERE pedidos.id IS NULL;   -- people without orders
```

- `[INNER] JOIN`, `LEFT [OUTER] JOIN` and `RIGHT [OUTER] JOIN`, each with its
  own `ON`. Several JOINs are combined left to right:
  `a JOIN b ON … LEFT JOIN c ON …` is `(a JOIN b) LEFT JOIN c`.
- A column can be written as `table.column` in `SELECT`, `ON` and `WHERE`
  (with or without a JOIN). Without the table, the name must exist in only
  one table; if it is in two, it's an ambiguous column error. Names are
  resolved by the `scope` (`cmd/my_dbms/where.go`): the columns of every
  table, one after the other, and the table of each one.
- `ON` only sees the tables that came before it; `WHERE` and the `SELECT`
  columns see all of them.
- `ON` decides what matches, and `WHERE` filters afterwards. In a
  `LEFT JOIN`, a condition on the right table placed in `ON` keeps the left
  rows without a match (with `NULL`); the same condition in `WHERE` removes
  those rows. As in `WHERE`, only true matches: `NULL = NULL` doesn't.
- `LEFT` keeps left rows without a match, with `NULL` in the right columns.
  `RIGHT` does the opposite, and unmatched right rows come last, after the
  pairs.
- `SELECT *` returns the columns in `FROM` and JOIN order. In the header, a
  column is prefixed with its table (`pessoas.id`) only when another result
  column, from another table, has the same name. The server sends that same
  name in `columns`.
- There are no aliases yet, so a table can't appear twice in the same
  `FROM`.
- `JOIN`, `INNER`, `LEFT`, `RIGHT` and `OUTER` are keywords: they can no
  longer be used as table or column names.

A JOIN runs as a **nested loop** (`cmd/my_dbms/join.go`). Each table is read
fully into memory once (`db.ReadTable`) and, for each row joined so far, the
loop goes through every row of the next table and tests `ON` on the pair.
Reading everything first keeps a single read transaction open at a time and
lets the inner side be walked again for each outer row. The cost is `n × m`
comparisons and the memory of the whole tables; using an index for a JOIN
(index nested loop) or a hash join is left for later. `EXPLAIN` shows it:

```
my_dbms> EXPLAIN SELECT * FROM pessoas LEFT JOIN pedidos ON pessoas.id = pessoa_id;
varredura completa na tabela pessoas
LEFT JOIN por nested loop: varredura completa na tabela pedidos
```

## Vector index and SEARCH

The vector index stores an **embedding** of each row (a vector that
represents the meaning of the text) for searching by meaning:

```sql
CREATE TABLE docs (id INT PRIMARY KEY, titulo TEXT, corpo TEXT) WITH EMBEDDING (titulo, corpo);
INSERT INTO docs VALUES (1, 'Gato e banho', 'meu gato foge quando vê a banheira');
SEARCH docs FOR 'gatos com medo de água' TOP 5;
```

```
similarity | id | titulo       | corpo
-----------+----+--------------+-----------------------------------
     0.812 |  1 | Gato e banho | meu gato foge quando vê a banheira
(1 linha)
```

(The exact similarity depends on the embeddings model.)

- `WITH EMBEDDING (columns)` at the end of `CREATE TABLE` creates the index
  `<table>_embedding`; `CREATE VECTOR INDEX name ON table (columns)` creates
  one on an existing table and computes the vectors of the rows it already
  has. Each table has at most one vector index, removed with `DROP INDEX`.
- After that, nobody touches vectors: `INSERT`, `UPDATE` and `DELETE`
  maintain the index by themselves, in the same transaction as the row.
- The text of each row is `column: value`, one column per line, skipping
  `NULL`s. A row whose index columns are all `NULL` has no vector and doesn't
  show up in `SEARCH`.
- `SEARCH table FOR 'text' [TOP n]` returns the `n` most similar rows (10
  without `TOP`), most similar first, with a `similarity` column in front:
  the cosine between the vectors, which goes up to 1. `SEARCH` has no
  `WHERE` yet. Over the server it runs under the read lock, and over MCP in
  the `query` tool.
- `VECTOR`, `WITH`, `EMBEDDING`, `SEARCH`, `FOR` and `TOP` became keywords
  and can no longer be used as table or column names.

### Where vectors come from

Storage doesn't speak HTTP: vectors are computed by the `DB.Embed` function,
which `main.go` wires to an `embed.Client` with the address, model and key
from [`-setup`](#configuration). The `Client` speaks the OpenAI format
(`POST <url>/embeddings`), which OpenAI, Voyage, Gemini, Ollama and LM Studio
understand, and sends texts in batches of 64. In tests, `embed.Fake` builds
vectors without a network: each word adds 1 at a position picked by a hash,
so texts that share words come out similar.

Computing is **synchronous**: an `INSERT` only finishes after the API
returns the vectors, and if it fails, nothing is written. Vectors are
requested **before** the transaction opens, so no changed page sits in
memory waiting on the network (on the server, the command still holds the
write lock, like any write). An `UPDATE` that doesn't change the text of the
index columns reuses the stored vector without calling the API. With no API
configured, creating the table works, but an `INSERT` into it fails asking
for `-setup`.

### Format

The index is a `<name>.mari` file with heap pages, like a table, and one
record per row:

```
row RID (6 bytes) | one dimension per byte (int8)
```

Vectors are stored as **int8**: each dimension is divided by the largest in
absolute value and multiplied by 127. Cosine only compares the direction of
the vectors, not their length, and the direction barely changes. As float32,
a 1536-dimension vector (`text-embedding-3-small`) would take 6 KB and not
fit a 4 KB page; as int8 it takes 1542 bytes. The maximum is
`page_size − 20` dimensions (4076 with 4 KB pages); a larger vector is
rejected with an error, and `-setup` warns ahead of time. If the model
changes and the dimensions don't match the index, `INSERT` and `SEARCH`
fail asking to recreate the index.

### Search

`SEARCH` is **brute force**: it computes the query vector, reads the whole
index file, computes the cosine with each vector and returns the `k`
largest. Up to tens of thousands of rows this is fast; an approximate index
(like HNSW) can come when the code asks for it.

Nothing leads from a RID to its vector record, so deleting or replacing
vectors (`DELETE`, `UPDATE`) also scans the file, once per command. Since
search is already linear, this doesn't change the order of the cost.

## Adding a command

1. If the command uses a new word, add it to `keywords` in
   `internal/sql/token.go`.
2. In `internal/parser/ast.go`, create the command's type with the
   `statement()` method. In `parser.go`, add a `case` in `statement` and the
   method that reads its grammar, using the helpers `expect`, `accept`,
   `ident`, `columnRef`, `literal`, `list` and `names`.
3. Add a `case` in `cmd/my_dbms/execute.go` that calls the command's own
   function (like `createTable`, `insert`, `selectRows`):
   ```go
   case *parser.DropTable:
       return dropTable(db, s)
   ```
4. If it needs the disk, add the method to `storage.DB`.
5. Write the tests in `parser_test.go` and `execute_test.go` (and in the
   package that changed).

## Principles

- **Interfaces only when needed.** With a single implementation, use a struct
  or a function. Create the interface when there is a second implementation or
  a test that needs a fake, and declare it in the package that *uses* it, with
  only the methods that package calls.
- **Packages by domain, not by layer.** No `implementation/v1`, `interfaces/`
  or `container`.
- **Functions before objects.** If there is no state, it is a function.
- **Plain Go errors.** Return `error` and use `fmt.Errorf("context: %w", err)`.
- **Tests in the same package**, table-driven when there are several cases.

## When to create a new package

When a responsibility shows up that doesn't fit the ones above. Likely
candidates as the project grows:

- `internal/engine`: execute the AST and pick the plan, moving that logic
  out of `cmd/`. It can wait until there's more than one way to join rows
  (JOIN) or to sort them (ORDER BY).
- `internal/index`: today the B+tree lives in `internal/storage`, because it
  uses the same pages and the same transaction as tables. If it grows (other
  structures, like hash), it can get its own package.

Don't create the package ahead of time: let the code ask for it.
