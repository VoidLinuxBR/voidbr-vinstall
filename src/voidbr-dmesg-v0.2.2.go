/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 09:55:00 -04
    Version:   0.2.2
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

func monitorFile(path string) {
	file, err := os.Open(path)
	if err != nil { return }
	defer file.Close()
	file.Seek(0, io.SeekEnd)
	reader := bufio.NewReader(file)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		line = strings.TrimSpace(line)
		
		queryMu.RLock()
		q := strings.ToLower(dynamicQuery)
		queryMu.RUnlock()

		if q == "" || strings.Contains(strings.ToLower(line), q) {
			// Move para o início da linha, limpa a linha e imprime
			fmt.Printf("\r\033[K%s\n", line)
			fmt.Print("\033[1;32m[FILTRO: " + dynamicQuery + "] \033[0m")
		}
	}
}

func main() {
	elevateToRoot()

	fmt.Println("--- Monitor de Logs VoidBR Iniciado ---")
	fmt.Println("Digite um termo para filtrar ou deixe vazio para ver tudo.")
	fmt.Print("\033[1;32m[FILTRO: ] \033[0m")

	// Captura filtro
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			queryMu.Lock()
			dynamicQuery = strings.TrimSpace(scanner.Text())
			queryMu.Unlock()
			// Limpa a linha atual antes de exibir o prompt novamente
			fmt.Print("\033[1;32m[FILTRO: " + dynamicQuery + "] \033[0m")
		}
	}()

	filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.Contains(path, "btmp") {
			go monitorFile(path)
		}
		return nil
	})

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
