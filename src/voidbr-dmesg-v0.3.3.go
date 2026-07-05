/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 09:45:00 -04
    Version:   0.3.3
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
	paused       bool
	pauseMu      sync.RWMutex
)

func elevateToRoot() {
	if os.Geteuid() == 0 { return }
	args := append([]string{os.Args[0]}, os.Args[1:]...)
	cmd := exec.Command("sudo", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Run()
	os.Exit(0)
}

func colorize(line, query string) string {
	if query == "" { return line }
	return strings.ReplaceAll(line, query, Bold+Yellow+query+Reset)
}

func monitorFile(path string) {
	file, err := os.Open(path)
	if err != nil { return }
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() { processLine(scanner.Text()) }
	for {
		fi, _ := file.Stat()
		if size := fi.Size(); size < 0 { continue }
		scanner := bufio.NewScanner(file)
		for scanner.Scan() { processLine(strings.TrimSpace(scanner.Text())) }
		time.Sleep(500 * time.Millisecond)
	}
}

func processLine(line string) {
	queryMu.RLock()
	q := dynamicQuery
	queryMu.RUnlock()
	if q == "" || strings.Contains(strings.ToLower(line), strings.ToLower(q)) {
		logChan <- colorize(line, q)
	}
}

func main() {
	if len(os.Args) > 1 { dynamicQuery = strings.Join(os.Args[1:], " ") }
	elevateToRoot()

	// Coloca o terminal em modo cru para ler teclas sem Enter
	exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1").Run()
	exec.Command("stty", "-F", "/dev/tty", "-echo").Run()
	defer exec.Command("stty", "-F", "/dev/tty", "echo", "-cbreak").Run()

	go func() {
		for msg := range logChan {
			pauseMu.RLock()
			isPaused := paused
			pauseMu.RUnlock()
			if !isPaused {
				fmt.Printf("\r\033[K%s\n", msg)
				queryMu.RLock()
				fmt.Print(Yellow + "[FILTRO: " + dynamicQuery + "] " + Reset)
				queryMu.RUnlock()
			}
		}
	}()

	fmt.Println("--- Monitor de Logs VoidBR Iniciado (Espaço pausa, Enter filtra) ---")

	go func() {
		buf := make([]byte, 1)
		for {
			os.Stdin.Read(buf)
			if buf[0] == ' ' { // Espaço pausa
				pauseMu.Lock()
				paused = !paused
				if paused {
					fmt.Print("\n" + Red + "[PAUSADO - Pressione Espaço para retomar] " + Reset)
				} else {
					fmt.Print("\r\033[K")
					queryMu.RLock()
					fmt.Print(Yellow + "[FILTRO: " + dynamicQuery + "] " + Reset)
					queryMu.RUnlock()
				}
				pauseMu.Unlock()
			} else if buf[0] == 13 || buf[0] == 10 { // Enter para novo filtro
				// Aqui você pode implementar um leitor de string se quiser
			}
		}
	}()

	filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			name := filepath.Base(path)
			isLogFile := strings.HasSuffix(name, ".log") || name == "current"
			isExcluded := strings.Contains(name, "btmp") || strings.Contains(name, "wtmp") || strings.Contains(name, "lastlog")
			if isLogFile && !isExcluded { go monitorFile(path) }
		}
		return nil
	})

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
