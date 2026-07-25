/*
   voidbr-dmesg
   Monitor de logs para o sistema VoidBR

   Site:      https://chililinux.com
   GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

   Created:   ter 03 fev 2026 13:08:22 -04
   Updated:   sex 24 jul 2026 -04
   Version:   0.6.0
   Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>

   Changelog 0.6.0:
     - Corrige corrupção de caracteres acentuados no filtro digitado
       (leitura por rune/UTF-8 em vez de byte cru).
     - Corrige loop ocupado (100% CPU) quando stdin fecha/EOF.
     - Aceita backspace tanto como DEL (127) quanto BS (8).
     - elevateToRoot() usa o caminho absoluto do binário (os.Executable).
     - Detecta se stdin/stdout são terminais reais; desliga sequências
       ANSI, barra de status e leitura de teclado quando não forem.
     - Corrige remoção de arquivos monitorados: goroutines de tailFile
       agora saem e se removem do mapa "watched" quando o arquivo some
       permanentemente, e paths de saída antecipada não vazam mais o
       registro (defer delete).
     - Exclusão de btmp/wtmp/lastlog agora por nome exato, não por
       substring (evitava falso positivo em arquivos como
       "mywtmp-service.log").
     - Destaque de severidade (panic/critical/fatal, error/fail,
       warning) colorindo a linha inteira.
     - Flags de linha de comando: --path, --exclude, --no-color,
       --no-root.
     - Persiste o último filtro usado em
       ~/.config/voidbr-dmesg/lastfilter.
*/

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ============================================================================
// Constantes e variáveis globais
// ============================================================================

const (
	// Estilos Básicos
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	Italic = "\033[3m"
	Under  = "\033[4m"
	Rev    = "\033[7m"

	// Barra de status: branco brilhante em negrito sobre fundo azul forte
	// (256 cores — mais compatível que o "bright background" do aixterm)
	StatusBar = "\033[1;97;48;5;27m"

	// Timestamp do log: ciano discreto, para não competir com o destaque da busca
	Timestamp = "\033[2;36m"

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

// maxMissingChecks é quantas vezes seguidas tailFile tenta um os.Stat
// falho (a cada ~500ms) antes de desistir de um arquivo que sumiu de
// vez, pra não deixar a goroutine presa pra sempre. scanAndWatch (que
// roda a cada 30s) vai voltar a monitorá-lo se ele reaparecer.
const maxMissingChecks = 20

var (
	flagPath    = flag.String("path", "/var/log", "diretório de logs para monitorar")
	flagExclude = flag.String("exclude", "", "padrões extras de nomes de arquivo a excluir, separados por vírgula")
	flagNoColor = flag.Bool("no-color", false, "desabilita cores mesmo em terminal")
	flagNoRoot  = flag.Bool("no-root", false, "não tenta elevar privilégios via sudo")

	dynamicQuery      string
	dynamicQueryLower string
	queryMu           sync.RWMutex

	logChan = make(chan string, 100)

	paused  bool
	pauseMu sync.RWMutex
	printMu sync.Mutex

	termRows int
	termCols int
	termMu   sync.RWMutex

	// stdoutTTY/stdinTTY são definidas em main() a partir de isTerminal().
	stdoutTTY bool
	stdinTTY  bool

	// colorEnabled controla se colorize() aplica códigos ANSI.
	colorEnabled bool

	watched   = make(map[string]bool)
	watchedMu sync.Mutex
)

// excludedExact são nomes de arquivo excluídos por igualdade exata (não
// por substring), pra evitar falsos positivos como "mywtmp-service.log"
// sendo excluído por conter "wtmp".
var excludedExact = map[string]bool{
	"btmp":    true,
	"wtmp":    true,
	"lastlog": true,
}

// timestampRe reconhece os formatos de timestamp mais comuns no início da linha:
// syslog ("Jul 24 14:32:01"), ISO 8601 ("2026-07-24T14:32:01[.123][Z|-04:00]")
// e TAI64N do runit/socklog ("@40000000...", usado nos arquivos "current").
var timestampRe = regexp.MustCompile(`^(?:[A-Za-z]{3}\s+\d{1,2}\s\d{2}:\d{2}:\d{2}|\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?|@[0-9a-fA-F]{24})`)

// Regexes de severidade, checadas nessa ordem (mais grave primeiro) contra
// a linha em minúsculas. Usam \b pra não confundir, por exemplo, "errata"
// com "err".
var (
	reCritical = regexp.MustCompile(`\b(panic|fatal|critical)\b`)
	reError    = regexp.MustCompile(`\b(error|err|fail(ed|ure)?)\b`)
	reWarning  = regexp.MustCompile(`\b(warn(ing)?)\b`)
)

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// ============================================================================
// Terminal: tamanho, região de rolagem, barra de status, detecção de TTY
// ============================================================================

// isTerminal reporta se o descritor de arquivo fd é um terminal real,
// consultando o kernel via ioctl(TCGETS). Usado para desligar sequências
// ANSI, barra de status e leitura de teclado quando a entrada/saída foi
// redirecionada para um arquivo ou pipe.
func isTerminal(fd uintptr) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return errno == 0
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
// Só deve ser chamada quando stdoutTTY é verdadeiro.
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

// printStatus redesenha a barra de status fixa na última linha do
// terminal, em vídeo reverso (estilo vim/nano), sem afetar a posição
// do cursor onde o conteúdo de log está sendo impresso. Não faz nada
// se stdoutTTY for falso (saída redirecionada).
func printStatus(buffer string) {
	if !stdoutTTY {
		return
	}

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
	fmt.Print("\0337")                 // salva posição do cursor (DECSC)
	fmt.Printf("\033[%d;1H", row)      // move para a última linha
	fmt.Print(StatusBar + msg + Reset) // desenha branco sobre azul
	fmt.Print("\0338")                 // restaura posição do cursor (DECRC)
}

// ============================================================================
// Watcher: varredura de /var/log e tail com detecção de rotação/truncamento
// ============================================================================

// scanAndWatch varre root e passa a monitorar (via tailFile) qualquer
// arquivo elegível que ainda não esteja sendo observado. extraExcludes
// são padrões adicionais (fornecidos pelo usuário via --exclude),
// verificados por substring já que são opt-in e não uma lista fixa
// propensa a colisão de nomes.
func scanAndWatch(root string, extraExcludes []string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			name := filepath.Base(path)
			isLogFile := strings.HasSuffix(name, ".log") || name == "current"
			isExcluded := excludedExact[name]
			if !isExcluded {
				for _, e := range extraExcludes {
					if strings.Contains(name, e) {
						isExcluded = true
						break
					}
				}
			}
			if isLogFile && !isExcluded {
				watchedMu.Lock()
				already := watched[path]
				if !already {
					watched[path] = true
				}
				watchedMu.Unlock()

				if !already {
					printMu.Lock()
					fmt.Printf("Monitorando: %s\n", path)
					printMu.Unlock()
					go tailFile(path)
				}
			}
		}
		return nil
	})
}

// fileID retorna dispositivo+inode do arquivo, usados para detectar rotação
// (quando o caminho passa a apontar para um arquivo fisicamente diferente).
func fileID(fi os.FileInfo) (dev, ino uint64) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev), st.Ino
	}
	return 0, 0
}

// tailFile acompanha um arquivo de log continuamente, como um "tail -F":
// detecta tanto rotação (o caminho passa a apontar para um inode
// diferente — ex.: logrotate com create/rename) quanto truncamento no
// lugar (ex.: logrotate com copytruncate), reabrindo/reposicionando
// conforme necessário em vez de ficar preso lendo um arquivo antigo.
// Sempre se remove do mapa "watched" ao sair, qualquer que seja o
// motivo, pra não vazar entradas de arquivos que não estão mais sendo
// observados de fato.
func tailFile(p string) {
	defer func() {
		watchedMu.Lock()
		delete(watched, p)
		watchedMu.Unlock()
	}()

	file, err := os.Open(p)
	if err != nil {
		return
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return
	}
	curDev, curIno := fileID(fi)

	reader := bufio.NewReader(file)
	var offset int64 // bytes já lidos com sucesso deste arquivo
	missing := 0

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Se atingiu o fim, espera um pouco antes de tentar ler novamente
				time.Sleep(500 * time.Millisecond)

				pfi, statErr := os.Stat(p)
				if statErr != nil {
					// Arquivo sumiu. Pode ter sido removido de vez ou
					// estar no meio de uma rotação — tenta por um
					// tempo limitado antes de desistir e liberar a
					// goroutine.
					missing++
					if missing >= maxMissingChecks {
						return
					}
					continue
				}
				missing = 0

				pDev, pIno := fileID(pfi)
				if pDev != curDev || pIno != curIno {
					// O caminho agora aponta para um arquivo diferente:
					// foi rotacionado (rename+create). Reabre do zero.
					if newFile, openErr := os.Open(p); openErr == nil {
						file.Close()
						file = newFile
						reader = bufio.NewReader(file)
						offset = 0
						if nfi, e2 := file.Stat(); e2 == nil {
							curDev, curIno = fileID(nfi)
						}
					}
					continue
				}

				if pfi.Size() < offset {
					// Mesmo arquivo, porém menor do que já lemos: foi
					// truncado no lugar (ex.: logrotate copytruncate).
					file.Seek(0, io.SeekStart)
					reader = bufio.NewReader(file)
					offset = 0
				}
				continue
			}
			break
		}

		offset += int64(len(line))

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
}

// ============================================================================
// Color: reconhecimento de timestamp, severidade e realce da query
// ============================================================================

// severityColor retorna a cor ANSI associada à maior severidade
// encontrada na linha (já em minúsculas), ou "" se nenhuma palavra-chave
// de severidade for encontrada.
func severityColor(lowerLine string) string {
	switch {
	case reCritical.MatchString(lowerLine):
		return BRed
	case reError.MatchString(lowerLine):
		return Red
	case reWarning.MatchString(lowerLine):
		return BYellow
	default:
		return ""
	}
}

// colorize destaca o timestamp (se reconhecido) em ciano discreto, a
// ocorrência da query em amarelo negrito (ou reverso, se a linha já tem
// cor de severidade), e colore a linha inteira conforme a severidade
// detectada (panic/critical/fatal, error/fail, warning). lowerLine e
// lowerQuery já vêm pré-calculados pelo chamador para evitar refazer
// strings.ToLower() em cima da mesma linha/query repetidamente.
//
// Se colorEnabled for falso (saída não é um terminal, ou --no-color foi
// passado), retorna a linha sem nenhum código ANSI.
func colorize(line, lowerLine, query, lowerQuery string) string {
	if !colorEnabled {
		return line
	}

	sc := severityColor(lowerLine)

	tsEnd := 0
	if loc := timestampRe.FindStringIndex(line); loc != nil {
		tsEnd = loc[1]
	}

	matchStart, matchEnd := -1, -1
	if query != "" {
		if idx := strings.Index(lowerLine, lowerQuery); idx != -1 {
			matchStart, matchEnd = idx, idx+len(query)
		}
	}

	tsStyle := Timestamp
	queryStyle := Bold + Yellow
	// resume é o estilo a que se volta depois de um Reset interno: nada
	// (string vazia) normalmente, ou a cor de severidade quando ela está
	// colorindo a linha inteira — senão o Reset de um destaque interno
	// apagaria também a cor de fundo do restante da linha.
	resume := ""
	if sc != "" {
		// Com a linha inteira já colorida pela severidade, sublinha o
		// timestamp em vez de trocar sua cor, e usa vídeo reverso pra
		// busca continuar se destacando independente da cor de fundo.
		tsStyle = sc + Under
		queryStyle = Rev + Bold
		resume = sc
	}

	var out string
	switch {
	case tsEnd == 0 && matchStart == -1:
		out = line
	case tsEnd == 0:
		out = line[:matchStart] + queryStyle + line[matchStart:matchEnd] + Reset + resume + line[matchEnd:]
	case matchStart == -1:
		out = tsStyle + line[:tsEnd] + Reset + resume + line[tsEnd:]
	case matchStart >= tsEnd:
		out = tsStyle + line[:tsEnd] + Reset + resume + line[tsEnd:matchStart] + queryStyle + line[matchStart:matchEnd] + Reset + resume + line[matchEnd:]
	default:
		// Busca encontrou algo dentro do próprio timestamp — prioriza o
		// destaque da busca para não complicar a sobreposição das cores.
		out = line[:matchStart] + queryStyle + line[matchStart:matchEnd] + Reset + resume + line[matchEnd:]
	}

	if sc != "" {
		out = sc + out + Reset
	}
	return out
}

// ============================================================================
// Config: persistência do último filtro usado entre execuções
// ============================================================================

// configFilePath retorna ~/.config/voidbr-dmesg/lastfilter.
//
// NOTA: como elevateToRoot() faz sudo, HOME normalmente já é preservado
// pelo sudo (depende de "Defaults env_keep+=HOME" ou de rodar com
// "sudo -E"); em configurações padrão sem isso, os.UserHomeDir() pode
// resolver para /root em vez do HOME do usuário original. Se isso for
// um problema no seu ambiente, resolva o HOME real a partir de
// SUDO_USER (ex.: via os/user.Lookup(os.Getenv("SUDO_USER"))) antes de
// montar o caminho.
func configFilePath() (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".config", "voidbr-dmesg", "lastfilter"), nil
}

// loadLastFilter lê o último filtro persistido, ou "" se não houver
// nenhum (primeira execução, arquivo ilegível, etc.) — falhas aqui não
// são fatais, só significam "começa sem filtro".
func loadLastFilter() string {
	path, err := configFilePath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// saveLastFilter grava o filtro atual em disco pra reaproveitar na
// próxima execução. Falhas são ignoradas silenciosamente — persistir o
// filtro é conveniência, não algo que deva travar o monitor.
func saveLastFilter(q string) {
	path, err := configFilePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(q), 0o600)
}

// ============================================================================
// main: elevação de privilégio, entrada de teclado e wiring geral
// ============================================================================

func elevateToRoot() {
	if os.Geteuid() == 0 {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		// Fallback pro que foi passado em Args[0] caso Executable() falhe
		exe = os.Args[0]
	}
	args := append([]string{exe}, os.Args[1:]...)
	cmd := exec.Command("sudo", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sFalha ao elevar privilégios via sudo: %v%s\n", Red, err, Reset)
		os.Exit(1)
	}
	os.Exit(0)
}

// inputLoop lê o teclado em modo cbreak e atualiza o filtro dinâmico.
// Usa ReadRune (não ReadByte) para não corromper caracteres acentuados,
// e encerra a si mesma (sem girar em loop ocupado) se stdin fechar.
func inputLoop() {
	reader := bufio.NewReader(os.Stdin)
	var buffer []rune

	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			if err == io.EOF {
				// stdin fechado (ex.: executado com < /dev/null): não há
				// mais entrada interativa possível, encerra a goroutine
				// em vez de girar consumindo CPU.
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		switch r {
		case ' ':
			pauseMu.Lock()
			paused = !paused
			pauseMu.Unlock()
		case '\n', '\r':
			queryMu.Lock()
			dynamicQuery = strings.TrimSpace(string(buffer))
			dynamicQueryLower = strings.ToLower(dynamicQuery)
			queryMu.Unlock()
			saveLastFilter(dynamicQuery)
			buffer = buffer[:0]
		case 127, 8: // DEL ou BS
			if len(buffer) > 0 {
				buffer = buffer[:len(buffer)-1]
			}
		default:
			buffer = append(buffer, r)
		}

		printStatus(string(buffer))
	}
}

func main() {
	flag.Parse()

	if len(flag.Args()) > 0 {
		dynamicQuery = strings.Join(flag.Args(), " ")
	} else {
		dynamicQuery = loadLastFilter()
	}
	dynamicQueryLower = strings.ToLower(dynamicQuery)

	if !*flagNoRoot {
		elevateToRoot()
	}

	stdoutTTY = isTerminal(os.Stdout.Fd())
	stdinTTY = isTerminal(os.Stdin.Fd())
	colorEnabled = stdoutTTY && !*flagNoColor

	var extraExcludes []string
	if *flagExclude != "" {
		for _, e := range strings.Split(*flagExclude, ",") {
			if e = strings.TrimSpace(e); e != "" {
				extraExcludes = append(extraExcludes, e)
			}
		}
	}

	if stdinTTY {
		exec.Command("stty", "-F", "/dev/tty", "cbreak", "-echo").Run()
		defer exec.Command("stty", "-F", "/dev/tty", "echo", "-cbreak").Run()
	}

	if stdoutTTY {
		setupTerminal()
		defer restoreTerminal()

		winch := make(chan os.Signal, 1)
		signal.Notify(winch, syscall.SIGWINCH)
		go func() {
			for range winch {
				setupTerminal()
				printStatus("")
			}
		}()
	}

	go func() {
		for msg := range logChan {
			pauseMu.RLock()
			isPaused := paused
			pauseMu.RUnlock()
			if !isPaused {
				printMu.Lock()
				fmt.Printf("%s\n", msg)
				printMu.Unlock()
				if stdoutTTY {
					printStatus("")
				}
			}
		}
	}()

	printMu.Lock()
	fmt.Println("--- Monitor Iniciado (Carregando histórico...) ---")
	printMu.Unlock()
	if stdoutTTY {
		printStatus("")
	}

	if stdinTTY {
		go inputLoop()
	} else {
		fmt.Fprintln(os.Stderr, "aviso: stdin não é um terminal — filtro interativo desabilitado")
	}

	scanAndWatch(*flagPath, extraExcludes)

	// Reescaneia periodicamente para pegar arquivos de log criados
	// depois que o monitor já estava rodando (ex.: serviço novo
	// instalado, novo dia de log de algum daemon).
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			scanAndWatch(*flagPath, extraExcludes)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
