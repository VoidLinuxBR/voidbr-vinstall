/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 08:50:00 -04
    Version:   0.1.5
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

func updateStats(path string, line string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := stats[path]; !ok { stats[path] = &Stats{} }
	up := strings.ToUpper(line)
	if strings.Contains(up, "ERROR") || strings.Contains(up, "FAIL") || strings.Contains(up, "PANIC") {
		stats[path].Errors++
	} else if strings.Contains(up, "WARN") {
		stats[path].Warns++
	}
}

func main() {
	elevateToRoot()

	// 1. Sinal para estatísticas
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\n\n\033[1;36m--- Resumo dos Logs ---\033[0m")
		for path, s := range stats {
			fmt.Printf("%s: %d Erros, %d Avisos\n", path, s.Errors, s.Warns)
		}
		os.Exit(0)
	}()

	// 2. Filtro de Entrada (Modo não bloqueante)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("\033[s\033[999;1H[Filtro]: ") // Move cursor para o fim
			text, _ := reader.ReadString('\n')
			queryMu.Lock()
			dynamicQuery = strings.TrimSpace(text)
			queryMu.Unlock()
			fmt.Print("\033[u\033[J") // Restaura cursor e limpa linha
		}
	}()

	// Busca de arquivos e monitoramento
//	searchDirs := []string{"/var/log/socklog", "/var/log"}
	var files []string
	filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !shouldIgnore(path) {
			files = append(files, path)
		}
		return nil
	})

	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			file, _ := os.Open(path)
			defer file.Close()
			file.Seek(0, io.SeekEnd)
			reader := bufio.NewReader(file)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				
				line = strings.TrimSpace(line)
				queryMu.RLock()
				q := dynamicQuery
				queryMu.RUnlock()
				
				if q == "" || strings.Contains(line, q) {
					updateStats(path, line)
					fmt.Println(line) // Exibe log
				}
			}
		}(f)
	}
	wg.Wait()
}
