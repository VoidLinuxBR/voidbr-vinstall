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
    "unsafe"
)

const (
    // Estilos Básicos
    Reset  = "\033[0m"
    Bold   = "\033[1m"
    Dim    = "\033[2m"
    Italic = "\033[3m"
    Under  = "\033[4m"
    Rev    = "\033[7m"

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
    dynamicQuery      string
    dynamicQueryLower string
    queryMu           sync.RWMutex
    logChan           = make(chan string, 100)
    paused            bool
    pauseMu           sync.RWMutex
    printMu           sync.Mutex

    termRows int
    termCols int
    termMu   sync.RWMutex
)

type winsize struct {
    Row    uint16
    Col    uint16
    Xpixel uint16
    Ypixel uint16
}

// getTerminalSize consulta o kernel pelo tamanho atual do terminal.
// Se falhar (ex.: saída redirecionada para um arquivo), assume 24x80.
func getTerminalSize() (rows, cols int) {
    ws := &winsize{}
    retCode, _, _ := syscall.Syscall(syscall.SYS_IOCTL,
        uintptr(syscall.Stdin),
        uintptr(syscall.TIOCGWINSZ),
        uintptr(unsafe.Pointer(ws)))
    if int(retCode) == -1 || ws.Row == 0 {
        return 24, 80
    }
    return int(ws.Row), int(ws.Col)
}

// setupTerminal reserva a última linha da tela para a barra de status,
// fazendo com que a rolagem normal do terminal pare uma linha antes do fim.
func setupTerminal() {
    rows, cols := getTerminalSize()
    termMu.Lock()
    termRows, termCols = rows, cols
    termMu.Unlock()

    printMu.Lock()
    fmt.Printf("\033[1;%dr", rows-1) // define a região de rolagem (DECSTBM)
    fmt.Printf("\033[%d;1H", rows-1) // posiciona o cursor dentro da região
    printMu.Unlock()
}

// restoreTerminal remove a região de rolagem customizada e limpa a barra
// de status antes do programa encerrar, devolvendo o terminal ao normal.
func restoreTerminal() {
    termMu.RLock()
    rows := termRows
    termMu.RUnlock()

    printMu.Lock()
    fmt.Print("\033[r")                    // remove a região de rolagem
    fmt.Printf("\033[%d;1H\033[K\n", rows) // limpa a última linha
    printMu.Unlock()
}

func elevateToRoot() {
    if os.Geteuid() == 0 { return }
    args := append([]string{os.Args[0]}, os.Args[1:]...)
    cmd := exec.Command("sudo", args...)
    cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
    err := cmd.Run()
    if err != nil {
        fmt.Fprintf(os.Stderr, "%sFalha ao elevar privilégios via sudo: %v%s\n", Red, err, Reset)
        os.Exit(1)
    }
    os.Exit(0)
}

// colorize destaca a ocorrência da query dentro da linha.
// lowerLine e lowerQuery já vêm pré-calculados pelo chamador para evitar
// refazer strings.ToLower() em cima da mesma linha/query repetidamente.
func colorize(line, lowerLine, query, lowerQuery string) string {
    if query == "" { return line }
    idx := strings.Index(lowerLine, lowerQuery)
    if idx == -1 { return line }
    foundText := line[idx : idx+len(query)]
    return line[:idx] + Bold + Yellow + foundText + Reset + line[idx+len(query):]
}

// printStatus redesenha a barra de status fixa na última linha do
// terminal, em vídeo reverso (estilo vim/nano), sem afetar a posição
// do cursor onde o conteúdo de log está sendo impresso.
func printStatus(buffer string) {
    pauseMu.RLock()
    isPaused := paused
    pauseMu.RUnlock()

    termMu.RLock()
    row, cols := termRows, termCols
    termMu.RUnlock()

    var msg string
    if isPaused {
        msg = " [PAUSADO - Pressione ESPAÇO para retomar]"
    } else {
        queryMu.RLock()
        q := dynamicQuery
        queryMu.RUnlock()
        msg = fmt.Sprintf(" [SP:Pausar|FILTRO:%s] %s", q, buffer)
    }

    // preenche/trunca até a largura do terminal, para o reverso cobrir a linha toda
    if len(msg) < cols {
        msg += strings.Repeat(" ", cols-len(msg))
    } else if len(msg) > cols {
        msg = msg[:cols]
    }

    printMu.Lock()
    defer printMu.Unlock()
    fmt.Print("\0337")            // salva posição do cursor (DECSC)
    fmt.Printf("\033[%d;1H", row) // move para a última linha
    fmt.Print(Rev + msg + Reset)  // desenha em vídeo reverso
    fmt.Print("\0338")            // restaura posição do cursor (DECRC)
}

func main() {
    if len(os.Args) > 1 {
        dynamicQuery = strings.Join(os.Args[1:], " ")
        dynamicQueryLower = strings.ToLower(dynamicQuery)
    }
    elevateToRoot()

    exec.Command("stty", "-F", "/dev/tty", "cbreak", "-echo").Run()
    defer exec.Command("stty", "-F", "/dev/tty", "echo", "-cbreak").Run()

    setupTerminal()
    defer restoreTerminal()

    // Redesenha a região de rolagem e a barra quando o terminal é redimensionado.
    winch := make(chan os.Signal, 1)
    signal.Notify(winch, syscall.SIGWINCH)
    go func() {
        for range winch {
            setupTerminal()
            printStatus("")
        }
    }()

    go func() {
        for msg := range logChan {
            pauseMu.RLock()
            isPaused := paused
            pauseMu.RUnlock()
            if !isPaused {
                printMu.Lock()
                fmt.Printf("%s\n", msg)
                printMu.Unlock()
                printStatus("")
            }
        }
    }()

    printMu.Lock()
    fmt.Println("--- Monitor Iniciado (Carregando histórico...) ---")
    printMu.Unlock()
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
                dynamicQueryLower = strings.ToLower(dynamicQuery)
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
                printMu.Lock()
                fmt.Printf("Monitorando: %s\n", path)
                printMu.Unlock()
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
                        loweredLine := strings.ToLower(cleanLine)

                        queryMu.RLock()
                        q := dynamicQuery
                        qLower := dynamicQueryLower
                        queryMu.RUnlock()

                        // Aplica o filtro apenas se houver query, caso contrário imprime tudo
                        if qLower == "" || strings.Contains(loweredLine, qLower) {
                            logChan <- colorize(cleanLine, loweredLine, q, qLower)
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
