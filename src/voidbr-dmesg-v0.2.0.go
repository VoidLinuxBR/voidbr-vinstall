/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 09:35:00 -04
    Version:   0.2.0
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

const (
	Reset  = "\033[0m"
	Red    = "\033[1;31m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
	Green  = "\033[32m"
	Mag    = "\033[35m"
)

var (
	files        []string
	dynamicQuery string
	queryMu      sync.RWMutex
	displayMu    sync.Mutex
)

func elevateToRoot() {
	if os.Geteuid() == 0 { return }
	args := append([]string{os.Args[0]}, os.Args[1:]...)
	cmd := exec.Command("sudo", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Run()
	os.Exit(0)
}

func colorize(line string) string {
	up := strings.ToUpper(line)
	switch {
	case strings.Contains(up, "ERROR") || strings.Contains(up, "FAIL") || strings.Contains(up, "PANIC"): return Red + line + Reset
	case strings.Contains(up, "WARN"): return Yellow + line + Reset
	case strings.Contains(up, "INFO"): return Cyan + line + Reset
	case strings.Contains(line, "daemon"): return Green + line + Reset
	case strings.Contains(line, "kernel") || strings.Contains(line, "kern"): return Mag + line + Reset
	default: return "\033[37m" + line + Reset
	}
}

func printFiltered(line string) {
	queryMu.RLock()
	q := strings.ToLower(dynamicQuery)
	queryMu.RUnlock()

	if q == "" || strings.Contains(strings.ToLower(line), q) {
		displayMu.Lock()
		fmt.Println(colorize(line))
		displayMu.Unlock()
	}
}

func main() {
	elevateToRoot()

	filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.Contains(path, "btmp") {
			files = append(files, path)
		}
		return nil
	})

	// 1. Processo de Busca (Input)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			displayMu.Lock()
			queryMu.Lock()
			dynamicQuery = strings.TrimSpace(scanner.Text())
			queryMu.Unlock()
			fmt.Print("\033[H\033[2J") // Limpa tela ao buscar
			displayMu.Unlock()
		}
	}()

	// 2. Processo de Tail (Monitoramento)
	for _, path := range files {
		go func(p string) {
			file, _ := os.Open(p)
			defer file.Close()
			// Pula para o final
			file.Seek(0, 2)
			reader := bufio.NewScanner(file)
			for {
				for reader.Scan() {
					printFiltered(reader.Text())
				}
			}
		}(path)
	}

	// 3. Sinal de saída
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
