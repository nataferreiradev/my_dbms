# my_dbms

Um banco de dados relacional escrito do zero em Go, só com a biblioteca
padrão. É um projeto de estudo: a ideia é entender o que acontece entre
digitar um `SELECT` e ver a tabela na tela, com código pequeno que dê para
ler de cima a baixo.

> English version of the docs: [docs/en/structure.md](docs/en/structure.md)

## O que ele já faz

- `CREATE TABLE` com os tipos `INT`, `REAL`, `TEXT` e `BOOLEAN` e as
  restrições `NOT NULL`, `PRIMARY KEY` e `UNIQUE`, inclusive chaves de
  várias colunas
- `INSERT` com lista de colunas e com várias linhas de uma vez
- `SELECT` com projeção de colunas e `WHERE` com `=`, `<>`, `<`, `>`, `<=`,
  `>=`, `AND`, `OR`, parênteses e `IS [NOT] NULL`
- `[INNER] JOIN`, `LEFT JOIN` e `RIGHT JOIN` por nested loop, com colunas
  qualificadas (`pessoas.id`)
- `UPDATE`, `DELETE` e `DROP TABLE`
- índices B+tree: um para cada `PRIMARY KEY` e `UNIQUE` (é ele que confere a
  chave) e os criados com `CREATE INDEX`; `SELECT`, `UPDATE` e `DELETE`
  escolhem entre o índice e a varredura completa, e o `EXPLAIN` mostra qual
- transações: cada comando grava tudo ou nada, com um rollback journal que
  desfaz um commit interrompido por uma queda
- `NULL` com a semântica do SQL padrão (lógica de três valores)
- tabelas do sistema que podem ser consultadas: o catálogo (`_catalog`), os
  parâmetros do banco (`_settings`), o espaço que cada tabela ocupa em disco
  (`_tables`) e os índices, com a altura de cada árvore (`_indexes`)
- mensagens de erro em português, com a posição do problema no comando

## Como rodar

Precisa do Go 1.23 ou mais novo.

```
git clone https://github.com/nataferreiradev/my_dbms.git
cd my_dbms
go run ./cmd/my_dbms
```

Abre o REPL. Cada linha é um comando, e `exit` ou `quit` sai:

```
my_dbms> CREATE TABLE pessoas (id INT PRIMARY KEY, nome TEXT NOT NULL, cidade TEXT, idade INT);
tabela pessoas criada
my_dbms> INSERT INTO pessoas VALUES (1, 'Ana', 'Recife', 30), (2, 'Bruno', 'Natal', 17), (3, 'Carla', 'Olinda', 42), (4, 'Diego', NULL, 25), (5, 'Elisa', 'Recife', 19);
5 linhas inseridas
my_dbms> INSERT INTO pessoas VALUES (1, 'Fábio', 'Natal', 33);
erro: valor duplicado viola a chave pessoas_pkey: (id) = (1) já existe
my_dbms> SELECT * FROM pessoas;
id | nome  | cidade | idade
---+-------+--------+------
 1 | Ana   | Recife |    30
 2 | Bruno | Natal  |    17
 3 | Carla | Olinda |    42
 4 | Diego | NULL   |    25
 5 | Elisa | Recife |    19
(5 linhas)
my_dbms> SELECT nome, cidade, idade FROM pessoas WHERE idade >= 18 AND cidade IS NOT NULL;
nome  | cidade | idade
------+--------+------
Ana   | Recife |    30
Carla | Olinda |    42
Elisa | Recife |    19
(3 linhas)
```

## Como servidor

```
go run ./cmd/my_dbms -listen localhost:4321
```

Atende vários clientes ao mesmo tempo por TCP: um comando por linha, e uma
linha de JSON de resposta para cada um. Dá para testar com
`nc localhost 4321`. O protocolo e como a concorrência funciona estão em
[docs/estrutura.md](docs/estrutura.md#servidor).

Para usar pelo navegador, suba com `-http` no lugar de `-listen` e abra
`http://localhost:4321`.

## Com IA (MCP)

```
go build -o my_dbms.exe ./cmd/my_dbms
claude mcp add my_dbms -- ./my_dbms.exe -mcp
```

Com `-mcp`, um assistente de IA (Claude Code, Claude Desktop) enxerga o
schema e roda `SELECT` no banco. Com `-mcp-write`, também pode alterar. Veja
[docs/estrutura.md](docs/estrutura.md#por-ia-mcp).

## Mais exemplos

```sql
-- Só algumas colunas: as outras ficam NULL
INSERT INTO pessoas (id, nome) VALUES (6, 'Fábio');

-- Strings com aspas simples ou duplas; aspa repetida é uma aspa
INSERT INTO pessoas VALUES (7, 'Gabi d''Ávila', "Natal", 28);

-- AND liga antes de OR; parênteses mudam a ordem
SELECT nome FROM pessoas WHERE (cidade = 'Recife' OR cidade = 'Natal') AND idade > 18;

-- Comparar com NULL dá "desconhecido": use IS NULL
SELECT nome FROM pessoas WHERE cidade IS NULL;

UPDATE pessoas SET cidade = 'Recife' WHERE id = 4;
DELETE FROM pessoas WHERE idade < 18;

-- JOIN: LEFT mantém as pessoas sem cidade, com NULL
CREATE TABLE cidades (nome TEXT PRIMARY KEY, estado TEXT);
INSERT INTO cidades VALUES ('Recife', 'PE'), ('Natal', 'RN');
SELECT pessoas.nome, estado FROM pessoas LEFT JOIN cidades ON pessoas.cidade = cidades.nome;

-- Chave composta, REAL e BOOLEAN
CREATE TABLE matriculas (aluno INT, curso INT, nota REAL, aprovado BOOLEAN, PRIMARY KEY (aluno, curso));
INSERT INTO matriculas VALUES (1, 10, 8.5, TRUE), (2, 10, 4.75, FALSE);
SELECT aluno, nota FROM matriculas WHERE aprovado = TRUE AND nota >= 7;

-- Índice: o EXPLAIN mostra se ele vai ser usado
CREATE INDEX pessoas_cidade ON pessoas (cidade, idade);
EXPLAIN SELECT nome FROM pessoas WHERE cidade = 'Recife' AND idade >= 18;
-- busca no índice pessoas_cidade da tabela pessoas (cidade = 'Recife' AND idade >= 18)
SELECT * FROM _indexes;
DROP INDEX pessoas_cidade;

-- Como o banco descreve as próprias tabelas
SELECT * FROM _catalog;

-- Parâmetros do banco e quanto cada tabela ocupa (linhas, páginas, bytes)
SELECT * FROM _settings;
SELECT name, rows, pages FROM _tables WHERE rows > 0;

DROP TABLE matriculas;
DROP TABLE cidades;
```

Os erros dizem o que estava errado e onde:

```
my_dbms> CREATE TABLE t (id FLOAT);
erro: posição 20: esperava um tipo (INT, REAL, TEXT ou BOOLEAN), veio "FLOAT"
my_dbms> SELECT * FROM pessoas WHERE idade = 'trinta';
erro: WHERE: esperava um valor INT, veio 'trinta'
my_dbms> INSERT INTO pessoas (id, cidade) VALUES (8, 'Natal');
erro: a coluna nome não aceita NULL
```

## Como funciona

Todo comando passa pelas mesmas etapas:

```
texto ──► sql.Lex() ──► parser.Parse() ──► execute() ──► storage.DB ──► disco
          tokens com      árvore (AST)      confere o       catálogo,
          posição                           schema e        NOT NULL
                                            filtra linhas   e chaves
```

1. **Lexer** (`internal/sql`): quebra o texto em tokens, cada um com a
   posição em que começa.
2. **Parser** (`internal/parser`): um parser descendente recursivo monta a
   árvore do comando, com a precedência do SQL.
3. **Execução** (`cmd/my_dbms`): confere colunas e tipos contra o schema antes
   de ler qualquer linha, escolhe entre um índice e a varredura completa e
   avalia o `WHERE` com a lógica de três valores.
4. **Storage** (`internal/storage`): guarda cada tabela em
   `data/<tabela>.mari`, num formato binário próprio dividido em páginas
   de tamanho fixo (slotted pages, como no SQLite e no PostgreSQL). INT é
   gravado como int64, REAL como float64, BOOLEAN num byte, TEXT com o
   tamanho na frente e NULL num bitmap. O
   catálogo `data/_catalog.mari` descreve colunas, chaves e índices, no
   mesmo formato. Cada linha tem um endereço fixo (página, slot), que é o
   que os índices B+tree guardam, cada um no seu arquivo. Cada comando é uma
   transação: as páginas alteradas ficam na memória e o commit passa por um
   rollback journal, então uma queda no meio não corrompe nada.

A pasta dos dados e o tamanho da página vêm do `my_dbms.conf`
(`data_dir = data`, `page_size = 4096`).

Os detalhes de cada pacote, o formato em disco e o passo a passo para
adicionar um comando novo estão em [docs/estrutura.md](docs/estrutura.md).

## Desenvolvimento

```
go test ./...    # todos os testes
go vet ./...
gofmt -l .       # deve sair vazio
```

Os commits seguem o Conventional Commits, em inglês: veja
[docs/commits.md](docs/commits.md).

## Próximos passos

- `ORDER BY` e `LIMIT`, aproveitando a ordem do índice
- páginas de overflow para registros maiores que uma página
- um mapa de espaço livre, para o `INSERT` reaproveitar páginas antigas
- `BEGIN`/`COMMIT` com vários comandos numa transação, e depois WAL e MVCC
- alias de tabela e índice ou hash join no `JOIN`

## Licença

[MIT](LICENSE)
