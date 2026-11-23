package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"my_DBMS/internal/config"
	"my_DBMS/internal/embed"
	"my_DBMS/internal/storage"

	"golang.org/x/term"
)

const (
	prompt     = "my_dbms> "
	configFile = "my_dbms.conf"
)

func main() {
	listen := flag.String("listen", "", "em vez do REPL, atende clientes por TCP neste endereço (ex.: localhost:4321)")
	httpAddr := flag.String("http", "", "em vez do REPL, atende a página web/index.html por HTTP neste endereço (ex.: localhost:4321)")
	mcp := flag.Bool("mcp", false, "em vez do REPL, atende um assistente de IA por MCP no stdin e stdout")
	mcpWrite := flag.Bool("mcp-write", false, "com -mcp, deixa a IA alterar o banco (ferramenta execute)")
	setup := flag.Bool("setup", false, "configura a API de embeddings (para o SEARCH) e sai")
	flag.Parse()

	if *setup {
		if err := runSetup(); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao ler configuração:", err)
		os.Exit(1)
	}

	db, err := storage.Open(cfg.DataDir, cfg.PageSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao abrir banco:", err)
		os.Exit(1)
	}
	if db.Embed, err = embedder(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "erro ao configurar os embeddings:", err)
		os.Exit(1)
	}

	if *mcp {
		// O stdout é do protocolo: os avisos vão para o stderr, pelo log.
		log.Printf("my_dbms atendendo MCP no stdin (dados em %s)", cfg.DataDir)
		m := &mcpServer{db: db, write: *mcpWrite}
		if err := m.serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "erro no MCP:", err)
			os.Exit(1)
		}
		return
	}

	if *httpAddr != "" {
		log.Printf("my_dbms atendendo HTTP em %s (dados em %s)", *httpAddr, cfg.DataDir)
		s := &server{db: db}
		if err := http.ListenAndServe(*httpAddr, s.httpHandler()); err != nil {
			fmt.Fprintln(os.Stderr, "erro no servidor HTTP:", err)
			os.Exit(1)
		}
		return
	}

	if *listen != "" {
		ln, err := net.Listen("tcp", *listen)
		if err != nil {
			fmt.Fprintln(os.Stderr, "erro ao abrir a porta:", err)
			os.Exit(1)
		}
		log.Printf("my_dbms ouvindo em %s (dados em %s)", ln.Addr(), cfg.DataDir)
		s := &server{db: db}
		if err := s.serve(ln); err != nil {
			fmt.Fprintln(os.Stderr, "erro no servidor:", err)
			os.Exit(1)
		}
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print(prompt)
	for scanner.Scan() {
		// O pipe do PowerShell pode mandar um BOM no início da entrada.
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\xef\xbb\xbf"))

		switch line {
		case "":
		case "exit", "quit":
			return
		default:
			out, err := execute(db, line)
			if err != nil {
				fmt.Println("erro:", err)
			} else {
				fmt.Println(out)
			}
		}

		fmt.Print(prompt)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "erro ao ler entrada:", err)
		os.Exit(1)
	}
}

// embedder liga o banco na API de embeddings do my_dbms.conf, com a chave
// do arquivo de credenciais (ou de MY_DBMS_API_KEY). Sem API configurada,
// devolve nil, e só os comandos que precisam de embedding dão erro.
func embedder(cfg config.Config) (func([]string) ([][]float32, error), error) {
	if cfg.EmbeddingURL == "" {
		return nil, nil
	}
	// Sem pasta do usuário (um contêiner, por exemplo), só vale a variável.
	key := os.Getenv(config.KeyEnv)
	if path, err := config.CredentialsPath(); err == nil {
		if key, err = config.APIKey(path, cfg.EmbeddingURL); err != nil {
			return nil, fmt.Errorf("lendo a chave da API: %w", err)
		}
	}
	c := &embed.Client{URL: cfg.EmbeddingURL, Model: cfg.EmbeddingModel, Key: key}
	return c.Embed, nil
}

// runSetup abre o assistente do -setup no terminal. A chave é digitada sem
// aparecer na tela; num pipe, ela é lida como uma linha comum.
func runSetup() error {
	path, err := config.CredentialsPath()
	if err != nil {
		return err
	}
	w := &setupWizard{in: bufio.NewReader(os.Stdin), out: os.Stdout, confPath: configFile, keyPath: path}
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		w.secret = func() (string, error) {
			key, err := term.ReadPassword(fd)
			return string(key), err
		}
	}
	return w.run()
}
