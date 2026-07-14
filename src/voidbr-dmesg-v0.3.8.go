/*
    voidbr-dmesg
    Monitor de logs para o sistema VoidBR

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   dom 05 jul 2026 11:00:00 -04
    Version:   0.3.8
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

const (
    // Estilos Básicos
    Reset  = "\033[0m"
    Bold   = "\033[1m"
    Dim    = "\033[2m"
    Italic = "\033[3m"
    Under  = "\033[4m"

    // Cores Padrão
    Black   = "\033[30m"
    Red     = "\033[31m"
    Green   = "\033[32m"
    Yellow  = "\033[33m"
    Blue    = "\033[34m"
    Magenta = "\033[35m"
    Cyan    = "\033[36m"
    White   = "\033[37m"

    // Cores em Negrito (Bright)
    BRed     = "\033[1;31m"
    BGreen   = "\033[1;32m"
    BYellow  = "\033[1;33m"
    BBlue    = "\033[1;34m"
    BMagenta = "\033[1;35m"
    BCyan    = "\033[1;36m"
    BWhite   = "\033[1;37m"
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
    lowerLine := strings.ToLower(line)
    lowerQuery := strings.ToLower(query)
    idx := strings.Index(lowerLine, lowerQuery)
    if idx == -1 { return line }
    foundText := line[idx : idx+len(query)]
    return line[:idx] + Bold + Yellow + foundText + Reset + line[idx+len(query):]
}

func printStatus(buffer string) {
    if paused {
        fmt.Printf("\r\033[K%s[PAUSADO - Pressione ESPAÇO para retomar]%s", Red, Reset)
    } else {
        fmt.Printf("\r\033[K%s[SP:Pausar%s|FILTRO:%s] %s%s", Bold + Black, BYellow, dynamicQuery, buffer, Reset)
    }
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
                fmt.Printf("%s\n", msg)
                printStatus("")
            }
        }
    }()

    fmt.Println("--- Monitor Iniciado (Carregando histórico...) ---")
    printStatus("")

    go func() {
        reader := bufio.NewReader(os.Stdin)
        var buffer string
        for {
            char, _ := reader.ReadByte()
            if char == ' ' {
                pauseMu.Lock()
                paused = !paused
                pauseMu.Unlock()
                printStatus(buffer)
            } else if char == 10 || char == 13 {
                queryMu.Lock()
                dynamicQuery = strings.TrimSpace(buffer)
                buffer = ""
                queryMu.Unlock()
                printStatus(buffer)
            } else if char == 127 {
                if len(buffer) > 0 { buffer = buffer[:len(buffer)-1] }
                printStatus(buffer)
            } else {
                buffer += string(char)
                printStatus(buffer)
            }
        }
    }()

    filepath.Walk("/var/log", func(path string, info os.FileInfo, err error) error {
        if err == nil && !info.IsDir() {
            name := filepath.Base(path)
            isLogFile := strings.HasSuffix(name, ".log") || name == "current"
            isExcluded := strings.Contains(name, "btmp") || strings.Contains(name, "wtmp") || strings.Contains(name, "lastlog")
            if isLogFile && !isExcluded {
              fmt.Printf("Monitorando: %s\n", path)
                go func(p string) {
                    file, err := os.Open(p)
                    if err != nil { return }
                    defer file.Close()

                    reader := bufio.NewReader(file)
                    
                    for {
                        line, err := reader.ReadString('\n')
                        if err != nil {
                            if err == io.EOF {
                                // Se atingiu o fim, espera um pouco antes de tentar ler novamente
                                time.Sleep(500 * time.Millisecond)
                                continue
                            }
                            break
                        }
                        
                        cleanLine := strings.TrimSpace(line)
                        queryMu.RLock()
                        q := strings.ToLower(dynamicQuery)
                        loweredLine := strings.ToLower(cleanLine)
                        queryMu.RUnlock()

                        // Aplica o filtro apenas se houver query, caso contrário imprime tudo
                        if q == "" || strings.Contains(loweredLine, q) {
                            logChan <- colorize(cleanLine, dynamicQuery)
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
