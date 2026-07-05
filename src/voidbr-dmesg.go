package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Regras de cores (compatíveis com o original)
const (
	Reset  = "\033[0m"
	Red    = "\033[1;31m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
	Green  = "\033[32m"
	Mag    = "\033[35m"
)

func colorize(line string) string {
	up := strings.ToUpper(line)
	if strings.Contains(up, "ERROR") || strings.Contains(up, "FAIL") || strings.Contains(up, "PANIC") {
		return Red + line + Reset
	}
	if strings.Contains(up, "WARN") {
		return Yellow + line + Reset
	}
	if strings.Contains(up, "INFO") {
		return Cyan + line + Reset
	}
	if strings.Contains(line, "daemon") {
		return Green + line + Reset
	}
	if strings.Contains(line, "kernel") || strings.Contains(line, "kern") {
		return Mag + line + Reset
	}
	return "\033[37m" + line + Reset
}

func main() {
	if os.Geteuid() != 0 {
		fmt.Println("Erro: Este script deve ser executado como root.")
		os.Exit(1)
	}

	// 1. Identificar arquivos (equivalente ao resolve_log do seu script)
	args := os.Args[1:]
	var files []string
	
	// Busca arquivos baseada nos argumentos ou padrão
	searchRoots := []string{"/var/log/socklog", "/var/log"}
	for _, root := range searchRoots {
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && info.Size() > 0 {
				if len(args) == 0 || strings.Contains(path, args[0]) {
					files = append(files, path)
				}
			}
			return nil
		})
	}

	// 2. Histórico (cat + sort)
	var history []string
	for _, f := range files {
		data, _ := os.ReadFile(f)
		history = append(history, strings.Split(string(data), "\n")...)
	}
	sort.Strings(history)
	for _, line := range history {
		if strings.TrimSpace(line) != "" {
			fmt.Println(colorize(line))
		}
	}

	// 3. Follow (Tail -F)
	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			file, _ := os.Open(path)
			file.Seek(0, io.SeekEnd)
			reader := bufio.NewReader(file)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				fmt.Println(colorize(strings.TrimSpace(line)))
			}
		}(f)
	}
	wg.Wait()
}
