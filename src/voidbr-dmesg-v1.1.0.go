package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// elevateToRoot verifica se o usuário é root e, se não, reexecuta o script
func elevateToRoot() {
	if os.Geteuid() == 0 {
		return
	}

	fmt.Println("This script must be run as root. Elevating privileges...")
	
	args := append([]string{os.Args[0]}, os.Args[1:]...)
	
	// Tenta sudo, caso contrário tenta su
	cmd := exec.Command("sudo", args...)
	if _, err := exec.LookPath("sudo"); err != nil {
		cmd = exec.Command("su", "-c", strings.Join(args, " "))
	}
	
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		fmt.Println("Unable to elevate privileges.")
		os.Exit(1)
	}
	os.Exit(0)
}

func colorize(line string) string {
	upper := strings.ToUpper(line)
	switch {
	case strings.Contains(upper, "ERROR") || strings.Contains(upper, "FAIL") || strings.Contains(upper, "PANIC"):
		return "\033[1;31m" + line + "\033[0m"
	case strings.Contains(upper, "WARN"):
		return "\033[33m" + line + "\033[0m"
	case strings.Contains(upper, "INFO"):
		return "\033[36m" + line + "\033[0m"
	case strings.Contains(line, "daemon"):
		return "\033[32m" + line + "\033[0m"
	default:
		return "\033[37m" + line + "\033[0m"
	}
}

func main() {
	elevateToRoot()

	// Busca de arquivos (equivalente ao resolve_log)
	searchDirs := []string{"/var/log/socklog", "/var/log"}
	var files []string
	query := ""
	if len(os.Args) > 1 {
		query = os.Args[1]
	}

	for _, dir := range searchDirs {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && (query == "" || strings.Contains(info.Name(), query)) {
				files = append(files, path)
			}
			return nil
		})
	}

	// Histórico: Lê, ordena e imprime
	var history []string
	for _, f := range files {
		content, _ := os.ReadFile(f)
		history = append(history, strings.Split(string(content), "\n")...)
	}
	sort.Strings(history)
	for _, line := range history {
		if strings.TrimSpace(line) != "" {
			fmt.Println(colorize(line))
		}
	}

	// Follow: Monitora arquivos usando goroutines
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
					continue // Aguarda novas linhas
				}
				fmt.Println(colorize(strings.TrimSpace(line)))
			}
		}(f)
	}
	wg.Wait()
}
