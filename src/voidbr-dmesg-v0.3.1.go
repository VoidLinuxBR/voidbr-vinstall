/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 11:15:00 -04
    Version:   0.3.1
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
	Bold   = "\033[1m"
	Red    = "\033[1;31m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
)

var (
	dynamicQuery string
	queryMu      sync.RWMutex
	logChan      = make(chan string, 100)
)

func elevateToRoot() {
	if os.Geteuid() == 0 { return }
	args := append([]string{os.Args[0]}, os.Args[1:]...)
	cmd := exec.Command("sudo", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Run()
	os.Exit(0)
}

// colorize destaca apenas o termo buscado na linha
func colorize(line, query string) string {
	if query == "" {
		return line
	}
	// Cria a versão destacada: Negrito + Amarelo
	highlight := Bold + Yellow + query + Reset
	// Substitui a ocorrência do termo pelo destaque
	return strings.ReplaceAll(line, query, highlight)
}

func monitorFile(path string) {
	file, err := os.Open(path)
	if err != nil { return }
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		processLine(scanner.Text())
	}

	for {
		fi, _ := file.Stat()
		if size := fi.Size(); size < 0 { continue }
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			processLine(strings.TrimSpace(scanner.Text()))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func processLine(line string) {
	queryMu.RLock()
	q := dynamicQuery
	queryMu.RUnlock()

	// Verifica se a linha contém o filtro (case insensitive para a busca)
	if q == "" || strings.Contains(strings.ToLower(line), strings.ToLower(q)) {
		logChan <- colorize(line, q)
	}
}

func main() {
	if len(os.Args) > 1 {
		dynamicQuery = strings.Join(os.Args[1:], " ")
	}

	elevateToRoot()

	// Goroutine do Impressor Central (garante a ordem e as cores)
	go func() {
		for msg := range logChan {
			fmt.Printf("\r\033[K%s\n", msg)
			queryMu.RLock()
			fmt.Print(Yellow + "[FILTRO: " + dynamicQuery + "] " + Reset)
			queryMu.RUnlock()
		}
	}()

	fmt.Println("--- Monitor de Logs VoidBR Iniciado ---")

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			queryMu.Lock()
			dynamicQuery = strings.TrimSpace(scanner.Text())
			queryMu.Unlock()
			fmt.Print("\r\033[K")
			fmt.Print(Yellow + "[FILTRO: " + dynamicQuery + "] " + Reset)
		}
	}()

	filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			name := filepath.Base(path)
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
