# Project structure

my_dbms is a relational database written from scratch in Go, as a learning
project. The goal is small, straightforward code that you can read top to
bottom.

## Tree

```
my_dbms/
├── cmd/my_dbms/          → executable (REPL)
│   ├── main.go           → reads lines from stdin and prints the result
│   ├── execute.go        → runs each kind of Statement: validates and calls storage
│   ├── execute_test.go
│   ├── where.go          → compileWhere(): WHERE condition → function that tests a row
│   ├── format.go         → renders the SELECT result as a table
│   └── format_test.go
├── internal/
│   ├── config/           → database parameters
│   │   ├── config.go     → Config, Default(), Load(): reads my_dbms.conf
│   │   └── config_test.go
│   ├── sql/              → lexical analysis of SQL
│   │   ├── token.go      → Token, TokenType, keywords, operators, symbols
│   │   ├── lexer.go      → Lex(): string → []Token
│   │   └── lexer_test.go
│   ├── parser/           → syntax analysis
│   │   ├── ast.go        → Statement and the type of each command (the AST)
│   │   ├── parser.go     → Parse(): []Token → Statement
│   │   └── parser_test.go
│   └── storage/          → on-disk persistence
│       ├── storage.go    → DB: catalog, CreateTable, InsertRows, ReadTable,
│       │                   ReplaceRows, DropTable
│       ├── storage_test.go
│       ├── page.go       → on-disk format: header, slotted pages, records
│       ├── page_test.go
│       ├── system.go     → system tables built in memory: _settings, _tables
│       └── system_test.go
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

To add a parameter: a field in `Config`, the default value in `Default()`, a
`case` in `Load()` and a commented line in `my_dbms.conf`. An unknown key is
an error, so a typo doesn't slip by silently.

## Accepted SQL

```sql
CREATE TABLE p (id INT PRIMARY KEY, nome TEXT NOT NULL, email TEXT UNIQUE);
INSERT INTO p VALUES (1, 'Ana', NULL), (2, 'Bia', 'bia@x');
INSERT INTO p (nome, id) VALUES ('Caio', 3);
SELECT * FROM p WHERE (id > 1 OR email IS NULL) AND nome <> 'Bia';
UPDATE p SET email = 'ana@x' WHERE id = 1;
DELETE FROM p WHERE email IS NULL;
DROP TABLE p;
```

- Types: `INT` and `TEXT`. Strings use single or double quotes; a doubled
  quote inside one is a quote (`'it''s'`).
- Column constraints: `NOT NULL`, `PRIMARY KEY`, `UNIQUE`. Multi-column keys:
  `PRIMARY KEY (a, b)`, `UNIQUE (a, b)`.
- `WHERE`: `=`, `<>`, `<`, `>`, `<=`, `>=`, `IS [NOT] NULL`, `AND`, `OR` and
  parentheses. Comparing with `NULL` is "unknown" and the row doesn't pass,
  as in standard SQL; use `IS NULL`.
- A multi-row `INSERT` writes all rows or none.

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
  storage.ReadTable()  → schema from the in-memory catalog + rows from data/pessoas.marina
        │
        ▼
  selectRows()         → picks the requested columns
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
| `internal/storage` | Defines the on-disk format, maintains the catalog, creates tables, appends records and reads tables inside a directory. | Know about SQL. It receives names and values that are already validated. |
| `cmd/my_dbms` | Connects everything: takes the line, calls `Lex` and `Parse`, runs the `Statement` against the schema, calls storage and formats the output. | Implement file-format or grammar rules. |

`internal/` prevents code outside the module from importing these packages,
which leaves us free to change the internal API.

## On-disk format

Each table is a file at `<data_dir>/<name>.marina` holding only its records.
What describes the tables lives in the **catalog**,
`<data_dir>/_catalog.marina`, stored in the same format. The format code is
in `internal/storage/page.go`.

### Pages

The file is split into **pages** of `page_size` bytes (4096 by default), as
in SQLite and PostgreSQL: page N starts at byte `N × page_size`. Page 0 is
the file header, and the others hold the records:

```
page 0   "MRNA" | version (2 bytes) | page_size (2 bytes) | zeros to the end

page N   ┌───────────────────────────────────────────────────────┐
         │ slot count | start of records | slot 0 | slot 1 …     │  slots grow →
         │                                                       │
         │                     free space                        │
         │                                                       │
         │                  … | record 1 | record 0              │  ← records grow from the end
         └───────────────────────────────────────────────────────┘
```

Each slot is 4 bytes: the offset and size of the record inside the page.
Slots grow from the start and records from the end, and the page is full
when the two meet. All integers are little endian; the 2-byte fields limit a
page to 32 KB.

`page_size` comes from `my_dbms.conf` and is written to page 0 of every file
when the database is created. Opening an existing database with a different
value is an error, because a file of 4 KB pages read as 8 KB pages turns into
garbage. The header also makes a file in another format (like the CSV of
older versions) get rejected with an error instead of read wrong.

A record must fit in an empty page: the maximum is `page_size − 8` bytes (the
page header and one slot). A bigger record is rejected with an error, and
nothing is written.

### Records

Each row becomes bytes guided by the column types:

```
NULL bitmap | value 0 | value 1 | …
```

- **NULL bitmap**: one bit per column (bit `i % 8` of byte `i / 8`), set when
  the value is `NULL`. A `NULL` takes no other space.
- **INT**: 8 bytes (int64).
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
numbers already normalized as text. The conversion to int64 only happens when
writing.

### Catalog

The catalog has one row per database object, with the columns `type`,
`name`, `table_name` and `definition`:

```
type        | name              | table_name | definition
table       | pessoas           | pessoas    | id INT NOT NULL,email TEXT,nome TEXT
primary_key | pessoas_pkey      | pessoas    | id
unique      | pessoas_email_key | pessoas    | email
```

| `type` | `definition` |
|---|---|
| `table` | Columns and types, with `NOT NULL` when present: `id INT NOT NULL,nome TEXT`. |
| `primary_key` | Primary key columns: `id` or `a,b`. At most one per table. |
| `unique` | Columns of a unique key. |

The `type` column is the discriminator: it says what kind of object the line
describes. A new object (index, foreign key) is a new `type` value and a
`case` in `loadCatalog`. A key always comes after its table's line, and its
name is generated: `<table>_pkey` or `<table>_<columns>_key`.

In SQL, a key goes after the column type, or as its own list item for
multi-column keys:

```sql
CREATE TABLE pessoas (id INT PRIMARY KEY, email TEXT UNIQUE, nome TEXT NOT NULL);
CREATE TABLE pares (a INT, b INT, PRIMARY KEY (a, b));
```

There is no index yet: `InsertRows` checks each key by reading the whole table
and rejects the record if another one already has the same values in the
key's columns. As in standard SQL, primary key columns become `NOT NULL`, and
`NULL` equals nothing, so a `UNIQUE` key accepts several rows with `NULL`.

`Open` loads the whole catalog into memory, and operations read that copy.
The catalog's own schema is fixed in code (`catalogSchema`), so it can be
read like any other table: `SELECT * FROM _catalog;`. Names starting with `_`
are reserved for the system: you can't create a table with one or `INSERT`
into it.

### System tables

Besides `_catalog`, two system tables don't exist on disk: `ReadTable` builds
their rows on the spot from the database state (like PostgreSQL's
`pg_settings` and `pg_tables` views). They accept `SELECT` and `WHERE`, but
can't be changed.

```
my_dbms> SELECT * FROM _settings;
name           | value | description
---------------+-------+-------------------------------------------------------------------
data_dir       | data  | pasta dos arquivos das tabelas
page_size      | 4096  | tamanho das páginas em bytes, gravado no banco quando ele é criado
format_version | 1     | versão do formato dos arquivos .marina

my_dbms> SELECT * FROM _tables;
name     | rows | pages | bytes
---------+------+-------+------
_catalog |    2 |     1 |  8192
pessoas  |    2 |     1 |  8192
```

- `_settings`: the parameters the database is running with. `my_dbms.conf`
  still exists, because `data_dir` is needed to find the database before
  opening it.
- `_tables`: one row per `.marina` file, with the row count, the data pages
  (not counting page 0) and the file size in bytes.

To add another system table: its name and schema in `system.go`, the schema
in the `tables` map in `Open` and a `case` in `ReadTable`.

### Writing

`CREATE TABLE` creates the data file first (with only page 0) and then
writes to the catalog. If the program dies in between, at worst a file is
left that the catalog doesn't see. It is never deleted or overwritten:
creating a table with that name fails until someone removes the file.

Every write builds the pages in memory and writes the whole file to
`<name>.marina.tmp`, which is then renamed over the original: if the program
dies midway, the original stays whole. `INSERT` reads the pages, fills the
last one and opens new pages when it runs out. `UPDATE` and `DELETE` read the
table, change it in memory and call `ReplaceRows`, which builds the pages
from scratch. `DROP TABLE` rewrites the catalog the same way, without the
rows for the table and its keys, then deletes the data file.

Rewriting the whole file is simple and safe, but gets expensive for large
tables. The next step is writing only the page that changed, together with a
journal that protects against crashes in the middle of a write.

## Adding a command

1. If the command uses a new word, add it to `keywords` in
   `internal/sql/token.go`.
2. In `internal/parser/ast.go`, create the command's type with the
   `statement()` method. In `parser.go`, add a `case` in `Parse` and the
   method that reads its grammar, using the helpers `expect`, `accept`,
   `ident`, `literal`, `list` and `names`.
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

- `internal/engine`: execute the AST, moving that logic out of `cmd/`.
- `internal/index`: indexes over tables.

Don't create the package ahead of time: let the code ask for it.
