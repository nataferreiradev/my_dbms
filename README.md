# my_dbms

Um banco de dados relacional escrito do zero em Go, só com a biblioteca
padrão. É um projeto de estudo: a ideia é entender o que acontece entre
digitar um `SELECT` e ver a tabela na tela, com código pequeno que dê para
ler de cima a baixo.

> English version of the docs: [docs/en/structure.md](docs/en/structure.md)

## O que ele já faz

- `CREATE TABLE` com os tipos `INT` e `TEXT` e as restrições `NOT NULL`,
  `PRIMARY KEY` e `UNIQUE`, inclusive chaves de várias colunas
- `INSERT` com lista de colunas e com várias linhas de uma vez (entram todas
  ou nenhuma)
- `SELECT` com projeção de colunas e `WHERE` com `=`, `<>`, `<`, `>`, `<=`,
  `>=`, `AND`, `OR`, parênteses e `IS [NOT] NULL`
- `UPDATE`, `DELETE` e `DROP TABLE`
- `NULL` com a semântica do SQL padrão (lógica de três valores)
- tabelas do sistema que podem ser consultadas: o catálogo (`_catalog`), os
  parâmetros do banco (`_settings`) e o espaço que cada tabela ocupa em disco
  (`_tables`)
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

-- Chave composta
CREATE TABLE matriculas (aluno INT, curso INT, nota INT, PRIMARY KEY (aluno, curso));

-- Como o banco descreve as próprias tabelas
SELECT * FROM _catalog;

-- Parâmetros do banco e quanto cada tabela ocupa (linhas, páginas, bytes)
SELECT * FROM _settings;
SELECT name, rows, pages FROM _tables WHERE rows > 0;

DROP TABLE matriculas;
```

Os erros dizem o que estava errado e onde:

```
my_dbms> CREATE TABLE t (id FLOAT);
erro: posição 20: esperava um tipo (INT ou TEXT), veio "FLOAT"
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
   de ler qualquer linha e avalia o `WHERE` com a lógica de três valores.
4. **Storage** (`internal/storage`): guarda cada tabela em
   `data/<tabela>.mari`, num formato binário próprio dividido em páginas
   de tamanho fixo (slotted pages, como no SQLite e no PostgreSQL). INT é
   gravado como int64, TEXT com o tamanho na frente e NULL num bitmap. O
   catálogo `data/_catalog.mari` descreve colunas e chaves, no mesmo
   formato. Toda escrita grava num arquivo temporário e renomeia por cima do
   original, para a tabela não corromper se o processo cair no meio.

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

- gravar só a página que mudou, com um journal contra quedas
- páginas de overflow para registros maiores que uma página
- índice B-tree
- WAL e transações
- um planner simples, que escolha entre índice e varredura completa

## Licença

[MIT](LICENSE)
