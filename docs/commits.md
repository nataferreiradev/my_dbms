# Padrão de commits

O projeto segue o [Conventional Commits](https://www.conventionalcommits.org/pt-br/v1.0.0/).
As mensagens são escritas **em inglês**, para que qualquer pessoa consiga
acompanhar o histórico.

## Formato

```
<tipo>(<escopo>): <descrição>

<corpo opcional>

<rodapé opcional>
```

### Tipo

| Tipo | Quando usar |
|---|---|
| `feat` | Nova funcionalidade (novo comando SQL, nova operação no storage). |
| `fix` | Correção de bug. |
| `refactor` | Mudança de código que não altera comportamento. |
| `test` | Adiciona ou corrige testes, sem mudar código de produção. |
| `docs` | Só documentação. |
| `perf` | Melhoria de desempenho. |
| `build` | `go.mod`, scripts de build, dependências. |
| `ci` | Configuração de integração contínua. |
| `chore` | Manutenção que não se encaixa acima (`.gitignore`, renomear arquivos). |

### Escopo

Opcional, mas recomendado. Use o nome do pacote ou da área afetada:

| Escopo | Área |
|---|---|
| `sql` | `internal/sql` |
| `storage` | `internal/storage` |
| `repl` | `cmd/my_dbms` |
| `docs` | `docs/` |

Ao criar um pacote novo, o nome dele vira um escopo válido.

### Descrição

- Modo imperativo: `add`, `fix`, `remove` (não `added`, `fixes`).
- Começa com letra minúscula e não termina com ponto.
- Até 72 caracteres na primeira linha inteira.
- Completa a frase: *"If applied, this commit will..."*

### Corpo

Use quando o *porquê* não for óbvio. Explique o motivo e o efeito da mudança,
não repita o diff. Quebre linhas em ~72 caracteres.

### Rodapé

- `BREAKING CHANGE: <explicação>` quando a mudança quebra o formato em disco,
  a sintaxe aceita ou a API de um pacote.
- `Closes #12` / `Refs #12` para ligar a issues.

Uma mudança que quebra compatibilidade também leva `!` depois do tipo/escopo:
`feat(storage)!: store rows as length-prefixed records`.

## Exemplos

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

## Regras gerais

- **Um commit, uma mudança lógica.** Refatoração e funcionalidade nova vão em
  commits separados.
- **Todo commit compila e passa nos testes.** Antes de commitar:
  ```
  gofmt -l . ; go vet ./... ; go test ./...
  ```
- Não commite a pasta `data/` nem binários.

## Branches

```
<tipo>/<descricao-curta>
```

Exemplos: `feat/insert-into`, `fix/lexer-negative-numbers`, `docs/commits`.
