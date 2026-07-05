/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 09:10:00 -04
    Version:   0.1.8
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
	"strings"
	"sync"
	"syscall"
	"time"
)

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
	stats        = make(map[string]*Stats)
	mu           sync.Mutex
	dynamicQuery string
	queryMu      sync.RWMutex
	files        []string
)

func elevateToRoot() {
	if os.Geteuid() == 0 { return }
	args := append([]string{os.Args[0]}, os.Args[1:]...)
	cmd := exec.Command("sudo", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Run()
	os.Exit(0)
}

func shouldIgnore(path string) bool {
	ignoreList := []string{"btmp", "wtmp", "lastlog"}
	name := filepath.Base(path)
	for _, item := range ignoreList {
		if name == item { return true }
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

// showHistory lê arquivos e imprime apenas o que casa com a query
func showHistory(query string) {
	fmt.Print("\033[H\033[2J") // Limpa tela
	for _, path := range files {
		file, err := os.Open(path)
		if err != nil { continue }
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if query == "" || strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
				fmt.Println(colorize(line))
			}
		}
		file.Close()
	}
}

func main() {
	elevateToRoot()

	// Coleta arquivos
	filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !shouldIgnore(path) {
			files = append(files, path)
		}
		return nil
	})

	// Captura Ctrl+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Exit(0)
	}()

	// Filtro interativo
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			query := strings.TrimSpace(scanner.Text())
			showHistory(query)
		}
	}()

	// Exibição inicial
	showHistory("")

	// Mantém o programa rodando
	select {}
}
