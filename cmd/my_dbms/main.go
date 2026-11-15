package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"my_DBMS/internal/storage"
)

const prompt = "my_dbms> "

func main() {
	db, err := storage.Open("data")
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao abrir banco:", err)
		os.Exit(1)
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
