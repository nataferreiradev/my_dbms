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
	"my_DBMS/internal/storage"
)

const (
	prompt     = "my_dbms> "
	configFile = "my_dbms.conf"
)

func main() {
	listen := flag.String("listen", "", "em vez do REPL, atende clientes por TCP neste endereço (ex.: localhost:4321)")
	httpAddr := flag.String("http", "", "em vez do REPL, atende a página web/index.html por HTTP neste endereço (ex.: localhost:4321)")
	flag.Parse()

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
