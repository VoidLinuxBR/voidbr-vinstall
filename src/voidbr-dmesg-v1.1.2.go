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
	"time"
)

const (
	// Versão do script
	Version = "1.0.0"

	// Cores ANSI
	Reset  = "\033[0m"
	Red    = "\033[1;31m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
	Green  = "\033[32m"
	Mag    = "\033[35m"
)

func elevateToRoot() {
	if os.Geteuid() == 0 {
		return
	}
	fmt.Println("This script must be run as root. Elevating privileges...")
	args := append([]string{os.Args[0]}, os.Args[1:]...)
	cmd := exec.Command("sudo", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println("Unable to elevate privileges.")
		os.Exit(1)
	}
	os.Exit(0)
}

func shouldIgnore(path string) bool {
	ignoreList := []string{"btmp", "wtmp", "lastlog"}
	name := filepath.Base(path)
	for _, item := range ignoreList {
		if name == item {
			return true
		}
	}
	return false
}

func colorize(line string) string {
	up := strings.ToUpper(line)
	switch {
	case strings.Contains(up, "ERROR") || strings.Contains(up, "FAIL") || strings.Contains(up, "PANIC"):
		return Red + line + Reset
	case strings.Contains(up, "WARN"):
		return Yellow + line + Reset
	case strings.Contains(up, "INFO"):
		return Cyan + line + Reset
	case strings.Contains(line, "daemon"):
		return Green + line + Reset
	case strings.Contains(line, "kernel") || strings.Contains(line, "kern"):
		return Mag + line + Reset
	default:
		return "\033[37m" + line + Reset
	}
}

func main() {
	elevateToRoot()

	query := ""
	if len(os.Args) > 1 {
		query = os.Args[1]
	}

	searchDirs := []string{"/var/log/socklog", "/var/log"}
	var files []string

	for _, dir := range searchDirs {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			
			if query == "" && shouldIgnore(path) {
				return nil
			}

			// Busca baseada no caminho completo para encontrar subdiretórios como 'daemon'
			if query == "" || strings.Contains(path, query) {
				files = append(files, path)
			}
			return nil
		})
	}

	if len(files) == 0 {
		fmt.Printf("\033[1;33m[*]\033[0m Nenhum log encontrado para '%s'\n", query)
		return
	}

	// 1. Histórico
	var history []string
	for _, f := range files {
		content, _ := os.ReadFile(f)
		lines := strings.Split(string(content), "\n")
		history = append(history, lines...)
	}
	sort.Strings(history)
	for _, line := range history {
		if strings.TrimSpace(line) != "" {
			fmt.Println(colorize(line))
		}
	}

	// 2. Follow
	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			file, err := os.Open(path)
			if err != nil {
				return
			}
			defer file.Close()
			file.Seek(0, io.SeekEnd)
			reader := bufio.NewReader(file)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				fmt.Println(colorize(strings.TrimSpace(line)))
			}
		}(f)
	}
	wg.Wait()
}
