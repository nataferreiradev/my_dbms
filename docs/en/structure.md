# Project structure

my_dbms is a relational database written from scratch in Go, as a learning
project. The goal is small, straightforward code that you can read top to
bottom.

## Tree

```
my_dbms/
├── cmd/my_dbms/          → executable (REPL)
│   ├── main.go           → reads lines from stdin and prints the result
│   ├── execute.go        → recognizes the command, validates and calls storage
│   ├── execute_test.go
│   ├── format.go         → renders the SELECT result as a table
│   └── format_test.go
├── internal/
│   ├── sql/              → lexical analysis of SQL
│   │   ├── token.go      → Token, TokenType, keywords, operators, symbols
│   │   ├── lexer.go      → Lex(): string → []Token
│   │   └── lexer_test.go
│   └── storage/          → on-disk persistence
│       ├── storage.go    → DB: CreateTable, InsertRow, ReadTable
│       └── storage_test.go
├── docs/                 → project documentation (Portuguese)
│   └── en/               → English version
└── data/                 → created at runtime (ignored by git)
```

## How a command flows

```
"select nome from pessoas;"
        │
        ▼
  sql.Lex()            → [SELECT] [nome] [FROM] [pessoas] [;]
        │
        ▼
  execute()            → matches SELECT ... FROM <identifier> and calls selectRows
        │
        ▼
  storage.ReadTable()  → reads data/pessoas.marina (schema + rows)
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
| `internal/sql` | Turns text into classified tokens. Normalizes keywords to uppercase. | Know that disks or tables exist. |
| `internal/storage` | Creates tables, appends records and reads tables inside a directory. | Know about SQL. It receives names and values that are already validated. |
| `cmd/my_dbms` | Connects the two: takes the line, calls `Lex`, picks the command, validates values against the schema, calls storage and formats the output. | Implement file-format rules. |

`internal/` prevents code outside the module from importing these packages,
which leaves us free to change the internal API.

## On-disk format

Each table is a CSV file at `data/<name>.marina`, read and written with
`encoding/csv`. The first line holds the schema and the rest are records:

```
id INT,nome TEXT
1,Ana
2,"Silva, Bruno"
```

The accepted types are `INT` and `TEXT`. CSV takes care of commas and quotes
inside values. The only special case is a record with a single empty field,
which is written as `""` so the reader doesn't skip the line.

## Adding a command

1. If the command uses a new word, add it to `keywords` in
   `internal/sql/token.go`.
2. Add a `case` in `cmd/my_dbms/execute.go` that calls the command's own
   function (like `createTable`, `insert`, `selectRows`):
   ```go
   case match(tokens, "DROP", "TABLE", sql.Identifier):
       return dropTable(db, tokens[2].Value)
   ```
   For lists in parentheses, use `parseList`.
3. If it needs the disk, add the method to `storage.DB`.
4. Write the test in `execute_test.go` (and in the package that changed).

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

- `internal/parser`: turn tokens into a tree (AST), once the patterns in
  `execute.go` get too complicated.
- `internal/engine`: execute the AST, moving that logic out of `cmd/`.
- `internal/index`: indexes over tables.

Don't create the package ahead of time: let the code ask for it.
