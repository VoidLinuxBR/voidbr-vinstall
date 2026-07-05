/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 10:05:00 -04
    Version:   0.3.5
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
	Bold   = "\033[1m"
	Yellow = "\033[33m"
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
	
	// Busca o índice de forma case-insensitive
	lowerLine := strings.ToLower(line)
	lowerQuery := strings.ToLower(query)
	idx := strings.Index(lowerLine, lowerQuery)
	
	if idx == -1 { return line }
	
	// Extrai o trecho exato que foi encontrado (mantendo o case original da linha)
	foundText := line[idx : idx+len(query)]
	
	return line[:idx] + Bold + Yellow + foundText + Reset + line[idx+len(query):]
}

func main() {
	if len(os.Args) > 1 { dynamicQuery = strings.Join(os.Args[1:], " ") }
	elevateToRoot()

	exec.Command("stty", "-F", "/dev/tty", "cbreak", "-echo").Run()
	defer exec.Command("stty", "-F", "/dev/tty", "echo", "-cbreak").Run()

	go func() {
		for msg := range logChan {
			pauseMu.RLock()
			isPaused := paused
			pauseMu.RUnlock()
			if !isPaused {
				fmt.Printf("\r\033[K%s\n", msg)
				queryMu.RLock()
				fmt.Printf(Yellow + "[FILTRO: %s] " + Reset, dynamicQuery)
				queryMu.RUnlock()
			}
		}
	}()

	fmt.Println("--- Monitor Iniciado. [ESPAÇO]: Pausa/Retoma | [Texto + Enter]: Filtro | [Enter vazio]: Zera Filtro ---")

	go func() {
		reader := bufio.NewReader(os.Stdin)
		var buffer string
		for {
			char, _ := reader.ReadByte()

			if char == ' ' {
				pauseMu.Lock()
				paused = !paused
				pauseMu.Unlock()
				fmt.Printf("\r\033[K" + Yellow + "[PAUSADO: %v | FILTRO: %s] " + Reset, paused, dynamicQuery)
			} else if char == 10 || char == 13 {
				queryMu.Lock()
				dynamicQuery = strings.TrimSpace(buffer)
				buffer = ""
				queryMu.Unlock()
				fmt.Printf("\r\033[K" + Yellow + "[FILTRO: %s] " + Reset, dynamicQuery)
			} else if char == 127 {
				if len(buffer) > 0 { buffer = buffer[:len(buffer)-1] }
				fmt.Printf("\r\033[K" + Yellow + "[FILTRO: %s] %s" + Reset, dynamicQuery, buffer)
			} else {
				buffer += string(char)
				fmt.Printf("\r\033[K" + Yellow + "[FILTRO: %s] %s" + Reset, dynamicQuery, buffer)
			}
		}
	}()

	filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			name := filepath.Base(path)
			isLogFile := strings.HasSuffix(name, ".log") || name == "current"
			isExcluded := strings.Contains(name, "btmp") || strings.Contains(name, "wtmp") || strings.Contains(name, "lastlog")
			if isLogFile && !isExcluded {
				go func(p string) {
					file, _ := os.Open(p)
					defer file.Close()
					s := bufio.NewScanner(file)
					for s.Scan() {
						line := s.Text()
						queryMu.RLock()
						q := strings.ToLower(dynamicQuery)
						loweredLine := strings.ToLower(line)
						queryMu.RUnlock()
						
						if q == "" || strings.Contains(loweredLine, q) {
							logChan <- colorize(line, dynamicQuery)
						}
					}
				}(path)
			}
		}
		return nil
	})

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
