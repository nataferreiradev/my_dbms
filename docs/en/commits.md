# Commit convention

The project follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/).
Messages are written **in English** so anyone can follow the history.

## Format

```
<type>(<scope>): <description>

<optional body>

<optional footer>
```

### Type

| Type | When to use |
|---|---|
| `feat` | New functionality (new SQL command, new storage operation). |
| `fix` | Bug fix. |
| `refactor` | Code change that doesn't alter behavior. |
| `test` | Adds or fixes tests without changing production code. |
| `docs` | Documentation only. |
| `perf` | Performance improvement. |
| `build` | `go.mod`, build scripts, dependencies. |
| `ci` | Continuous integration configuration. |
| `chore` | Maintenance that fits none of the above (`.gitignore`, renaming files). |

### Scope

Optional but recommended. Use the name of the package or area affected:

| Scope | Area |
|---|---|
| `sql` | `internal/sql` |
| `storage` | `internal/storage` |
| `repl` | `cmd/my_dbms` |
| `docs` | `docs/` |

When a new package is created, its name becomes a valid scope.

### Description

- Imperative mood: `add`, `fix`, `remove` (not `added`, `fixes`).
- Starts lowercase and has no trailing period.
- At most 72 characters for the whole first line.
- Completes the sentence: *"If applied, this commit will..."*

### Body

Use it when the *why* isn't obvious. Explain the reason and the effect of the
change; don't restate the diff. Wrap lines at ~72 characters.

### Footer

- `BREAKING CHANGE: <explanation>` when the change breaks the on-disk format,
  the accepted syntax, or a package's API.
- `Closes #12` / `Refs #12` to link issues.

A breaking change also gets a `!` after the type/scope:
`feat(storage)!: store rows as length-prefixed records`.

## Examples

```
feat(sql): add INSERT and VALUES keywords
```

```
fix(storage): stop CreateTable from truncating existing tables

os.Create truncates the file when it already exists, so running
CREATE TABLE twice wiped the table. Use O_EXCL and return an error.
```

```
refactor(sql): replace parser interfaces with plain functions
```

```
feat(storage)!: store rows as length-prefixed records

BREAKING CHANGE: .mari files created by older versions can't be read.
```

## General rules

- **One commit, one logical change.** Refactoring and new features go in
  separate commits.
- **Every commit builds and passes the tests.** Before committing:
  ```
  gofmt -l . ; go vet ./... ; go test ./...
  ```
- Don't commit the `data/` folder or binaries.

## Branches

```
<type>/<short-description>
```

Examples: `feat/insert-into`, `fix/lexer-negative-numbers`, `docs/commits`.
