/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 09:20:00 -04
    Version:   0.2.9
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
	"time"
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
	dynamicQuery string
	queryMu      sync.RWMutex
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
	// Regra para "magic" em Cyan
	if strings.Contains(strings.ToLower(line), "magic") {
		return Cyan + line + Reset
	}
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

func printPrompt() {
	queryMu.RLock()
	q := dynamicQuery
	queryMu.RUnlock()
	fmt.Print(Yellow + "[FILTRO: " + q + "] " + Reset)
}

func monitorFile(path string) {
	file, err := os.Open(path)
	if err != nil { return }
	defer file.Close()

	// 1. Lê o histórico
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		checkAndPrint(scanner.Text())
	}

	// 2. Modo Tail
	for {
		fi, _ := file.Stat()
		if size := fi.Size(); size < 0 { continue }
		
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			checkAndPrint(strings.TrimSpace(scanner.Text()))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func checkAndPrint(line string) {
	queryMu.RLock()
	q := strings.ToLower(dynamicQuery)
	queryMu.RUnlock()

	if q == "" || strings.Contains(strings.ToLower(line), q) {
		fmt.Printf("\r\033[K%s\n", colorize(line))
		printPrompt()
	}
}

func main() {
	if len(os.Args) > 1 {
		dynamicQuery = strings.Join(os.Args[1:], " ")
	}

	elevateToRoot()

	if dynamicQuery != "" {
		fmt.Print("\033[H\033[2J")
	}

	fmt.Println("--- Monitor de Logs VoidBR Iniciado ---")
	printPrompt()
	fmt.Println()

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			queryMu.Lock()
			dynamicQuery = strings.TrimSpace(scanner.Text())
			queryMu.Unlock()
			fmt.Print("\r\033[K")
			printPrompt()
		}
	}()

	filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			name := filepath.Base(path)
			// Só monitora arquivos terminados em .log ou o arquivo "current"
			isLogFile := strings.HasSuffix(name, ".log") || name == "current"
			isExcluded := strings.Contains(name, "btmp") || strings.Contains(name, "wtmp") || strings.Contains(name, "lastlog")

			if isLogFile && !isExcluded {
				go monitorFile(path)
			}
		}
		return nil
	})

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
