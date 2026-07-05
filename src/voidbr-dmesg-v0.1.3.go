/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 08:48:00 -04
    Version:   0.1.3
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Estrutura para contagem de estatísticas
type Stats struct {
	Errors int
	Warns  int
}

const (
	Reset  = "\033[0m"
	Red    = "\033[1;31m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
	Green  = "\033[32m"
	Mag    = "\033[35m"
)

var (
	stats = make(map[string]*Stats)
	mu    sync.Mutex
)

// elevateToRoot verifica privilégios e eleva se necessário
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

// shouldIgnore filtra arquivos binários por padrão
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

// updateStats contabiliza erros e avisos
func updateStats(path string, line string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := stats[path]; !ok {
		stats[path] = &Stats{}
	}
	up := strings.ToUpper(line)
	if strings.Contains(up, "ERROR") || strings.Contains(up, "FAIL") || strings.Contains(up, "PANIC") {
		stats[path].Errors++
	} else if strings.Contains(up, "WARN") {
		stats[path].Warns++
	}
}

// colorize aplica formatação ANSI e estatísticas
func colorize(path string, line string) string {
	updateStats(path, line)
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

	// Captura Ctrl+C para exibir estatísticas
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\n\n\033[1;36m--- Resumo dos Logs (Estatísticas) ---\033[0m")
		for path, s := range stats {
			fmt.Printf("%s: %d Erros, %d Avisos\n", path, s.Errors, s.Warns)
		}
		os.Exit(0)
	}()

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

	// 1. Processamento de Histórico
	var history []string
	for _, f := range files {
		content, _ := os.ReadFile(f)
		lines := strings.Split(string(content), "\n")
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				history = append(history, l)
			}
		}
	}
	sort.Strings(history)
	for _, line := range history {
		fmt.Println(colorize("HIST", line))
	}

	// 2. Monitoramento Contínuo
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
				fmt.Println(colorize(path, strings.TrimSpace(line)))
			}
		}(f)
	}
	wg.Wait()
}
