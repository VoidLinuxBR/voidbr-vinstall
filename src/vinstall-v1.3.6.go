/*
    voidbr-vinstall
    Wrapper para o Void xbps-query e xbps-install

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   qui 05 fev 2026 04:55:00 -04
    Version:   1.3.6-20260205
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/fatih/color"
)

const (
	Version   = "1.3.6-20260205"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
)

var (
	cyan   = color.New(color.Bold, color.FgCyan).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	white  = color.New(color.Bold, color.FgWhite).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	yellow = color.New(color.Bold, color.FgYellow).SprintFunc()
)

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

type Package struct {
	Status      string
	FullName    string
	Description string
}

// --- UTILITÁRIOS ---

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit { return fmt.Sprintf("%d B", b) }
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func getTerminalWidth() int {
	ws := &winsize{}
	retCode, _, _ := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdin),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)))

	if int(retCode) == 0 {
		return int(ws.Col)
	}
	return 80
}

func cleanVersion(fullName string) string {
	if i := strings.LastIndex(fullName, "-"); i != -1 { return fullName[:i] }
	return fullName
}

// --- CORE: EXECUÇÃO ---

func runBinary(bin string, flags []string, pkgs []string) bool {
	params := []string{bin}
	params = append(params, flags...)
	params = append(params, pkgs...)

	cmd := exec.Command("sudo", params...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	
	err := cmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() && status.Signal() == syscall.SIGINT {
					fmt.Printf("\n%s %s\n", red("[!]"), white("Operação cancelada pelo usuário."))
					os.Exit(1)
				}
			}
		}
		return false
	}
	return true
}

// --- BUSCA DIRETA EM PLIST (XML LOCAL) ---

func searchInLocalPlist(file string) bool {
	dbPath := "/var/db/xbps"
	files, err := ioutil.ReadDir(dbPath)
	if err != nil { return false }

	foundLocal := false
	for _, f := range files {
		if strings.HasSuffix(f.Name(), "-files.plist") {
			content, err := ioutil.ReadFile(filepath.Join(dbPath, f.Name()))
			if err != nil { continue }

			data := string(content)
			if strings.Contains(data, file) {
				lines := strings.Split(data, "\n")
				for _, line := range lines {
					if strings.Contains(line, "<string>") && strings.Contains(line, file) {
						pkgName := f.Name()
						pkgName = strings.TrimPrefix(pkgName, ".")
						pkgName = strings.TrimSuffix(pkgName, "-files.plist")
						fmt.Println(green(pkgName + " (instalado localmente via plist)"))
						foundLocal = true
						break
					}
				}
			}
		}
	}
	return foundLocal
}

// --- BUSCAS E PROVIDES (-F e -FR) ---

func checkXlocateIndex() {
	home, _ := os.UserHomeDir()
	indexPath := filepath.Join(home, ".cache/xlocate.git/FETCH_HEAD")
	info, err := os.Stat(indexPath)
	if err == nil {
		if time.Since(info.ModTime()).Hours() > 168 {
			fmt.Printf("%s %s\n", yellow("[TIP]"), white("Índice xlocate antigo. Considere 'xlocate -S'."))
		}
	}
}

func findProvides(file string, searchRemote bool) {
	fmt.Printf("%s %s '%s'...\n", cyan("[vinstall]"), white("Procurando pacote que contém:"), yellow(file))
	totalFound := false

	// 1. Camada Local (Grep manual em Plist)
	fmt.Printf("%s %s %s\n", cyan(">>>"), cyan("grep local /var/db/xbps/.*-files.plist"), yellow(file))
	if searchInLocalPlist(file) {
		totalFound = true
	}

	// 2. Camada xbps-query Local (-o)
	fmt.Printf("%s %s %s %s\n", cyan(">>>"), cyan("xbps-query"), cyan("-o"), yellow(file))
	cmdLoc := exec.Command("xbps-query", "-o", file)
	outLoc, _ := cmdLoc.Output()
	resLoc := strings.TrimSpace(string(outLoc))
	if resLoc != "" {
		totalFound = true
		fmt.Println(green(resLoc + " (instalado)"))
	}

	// 3. Camada xlocate (se disponível)
	xPath, err := exec.LookPath("xlocate")
	if err == nil {
		checkXlocateIndex()
		fmt.Printf("%s %s %s\n", cyan(">>>"), cyan("xlocate"), yellow(file))
		cmd := exec.Command(xPath, file)
		output, _ := cmd.Output()
		res := strings.TrimSpace(string(output))
		if res != "" {
			totalFound = true
			lines := strings.Split(res, "\n")
			seen := make(map[string]bool)
			for _, line := range lines {
				if line != "" && !seen[line] {
					fmt.Println(green(line))
					seen[line] = true
				}
			}
		}
	}

	// 4. Camada xbps-query Remota (-Ro)
	if searchRemote {
		fmt.Printf("%s %s %s %s\n", cyan(">>>"), cyan("xbps-query"), cyan("-Ro"), yellow(file))
		cmd := exec.Command("xbps-query", "-Ro", file)
		output, _ := cmd.Output()
		res := strings.TrimSpace(string(output))
		if res != "" {
			totalFound = true
			fmt.Println(green(res))
		}
	}

	if !totalFound {
		fmt.Printf("%s %s\n", red("[!]"), white("Nenhum pacote encontrado. Use -FR para busca profunda."))
	}
}

func listLocal(mode string, query string) {
	var cmd *exec.Cmd
	switch mode {
	case "installed":
		fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Listando pacotes instalados:"))
		cmd = exec.Command("xbps-query", "-l")
	case "orphans":
		fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Listando pacotes órfãos:"))
		cmd = exec.Command("xbps-query", "-O")
	case "search":
		fmt.Printf("%s %s '%s'...\n", cyan("[vinstall]"), white("Buscando localmente por:"), yellow(query))
		cmd = exec.Command("xbps-query", "-l")
	}

	output, _ := cmd.Output()
	lines := strings.Split(string(output), "\n")
	count := 0
	for _, line := range lines {
		if line == "" { continue }
		if mode == "search" && !strings.Contains(strings.ToLower(line), strings.ToLower(query)) { continue }
		fmt.Println(white(line))
		count++
	}
	fmt.Printf("\n%s %s: %s\n", yellow("[!]"), white("Total:"), cyan(strconv.Itoa(count)))
}

// --- MANUTENÇÃO ---

func cleanXbpsCache() {
	cachePath := "/var/cache/xbps"
	if os.Geteuid() != 0 {
		fmt.Printf("%s %s\n", yellow("[vinstall]"), white("A limpeza do cache requer privilégios de root."))
		cmd := exec.Command("sudo", os.Args[0], "-Scc")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Run()
		return
	}

	files, err := os.ReadDir(cachePath)
	if err != nil { return }

	var pkgCount int
	var totalSize int64
	fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Iniciando limpeza do cache..."))

	for _, file := range files {
		if file.IsDir() { continue }
		name := file.Name()
		if strings.HasSuffix(name, ".xbps") || strings.HasSuffix(name, ".sig2") {
			info, err := file.Info()
			if err == nil {
				totalSize += info.Size()
				if os.Remove(filepath.Join(cachePath, name)) == nil && strings.HasSuffix(name, ".xbps") {
					pkgCount++
				}
			}
		}
	}

	fmt.Printf("\n%s %s\n", green("[✔]"), white("Limpeza concluída!"))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Removidos:"), cyan(strconv.Itoa(pkgCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Espaço livre:"), green(formatBytes(totalSize)))

	cmd := exec.Command("xbps-query", "-O")
	out, _ := cmd.Output()
	if orphans := strings.TrimSpace(string(out)); orphans != "" {
		fmt.Printf("%s %s\n%s\n", yellow("[!]"), white("Órfãos encontrados:"), cyan(orphans))
		fmt.Printf("%s ", white("Remover órfãos? [s/N]: "))
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(ans)); a == "s" || a == "sim" {
			runBinary("xbps-remove", []string{"-o"}, []string{})
		}
	}
}

// --- SERVIÇOS E HISTÓRICO ---

func checkAndEnableService(pkgName string) {
	servicePath := filepath.Join("/etc/sv", pkgName)
	targetPath := filepath.Join("/var/service", pkgName)
	if info, err := os.Stat(servicePath); err == nil && info.IsDir() {
		if _, err := os.Lstat(targetPath); os.IsNotExist(err) {
			fmt.Printf("\n%s %s '%s'. Ativar? [s/N]: ", yellow("[!]"), white("Serviço disponível para"), cyan(pkgName))
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(input)); a == "s" || a == "sim" {
				exec.Command("sudo", "ln", "-s", servicePath, targetPath).Run()
			}
		}
	}
}

func showHistory() {
	logPath := "/var/log/socklog/xbps/current"
	file, err := os.Open(logPath)
	if err != nil {
		if os.IsPermission(err) { runBinary(os.Args[0], []string{"--history"}, []string{}); return }
		return
	}
	defer file.Close()
	fmt.Printf("\n%s %s\n", cyan("[vinstall]"), white("Histórico:"))
	width := getTerminalWidth()
	fmt.Println(white(strings.Repeat("─", width)))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 25 { line = line[25:] }
		if strings.Contains(line, "installed") { fmt.Println(green(line)) } else if strings.Contains(line, "removed") { fmt.Println(red(line)) } else { fmt.Println(white(line)) }
	}
	fmt.Println(white(strings.Repeat("─", width)))
}

// --- CONSULTA E MENU ---

func fetchSuggestions(query string) []Package {
	cmd := exec.Command("xbps-query", "-Rs", query)
	out, _ := cmd.Output()
	var pkgs []Package
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			desc := ""
			if idx := strings.Index(line, f[1]); idx != -1 { 
				desc = strings.TrimSpace(line[idx+len(f[1]):]) 
			}
			pkgs = append(pkgs, Package{f[0], f[1], desc})
		}
	}
	return pkgs
}

func displaySearch(pkgs []Package, title string) {
	if len(pkgs) == 0 { return }
	width := getTerminalWidth()
	lineSeparator := white(strings.Repeat("─", width))

	maxNameLen := 0
	for _, p := range pkgs {
		if len(p.FullName) > maxNameLen { maxNameLen = len(p.FullName) }
	}
	if maxNameLen > 50 { maxNameLen = 50 }

	fmt.Printf("%s\n%s\n", cyan(title), lineSeparator)
	for i, p := range pkgs {
		idx := yellow(fmt.Sprintf("[%2d]", i+1))
		statusColor := red(p.Status)
		if p.Status == "[*]" { statusColor = green(p.Status) }
		fmt.Printf("%s %s %s  %s\n", idx, statusColor, white(fmt.Sprintf("%-*s", maxNameLen, p.FullName)), green(p.Description))
	}
	fmt.Println(lineSeparator)
}

func displayMenu(pkgs []Package, flags []string) {
	if len(pkgs) == 0 { return }
	displaySearch(pkgs, "\nSugestões encontradas no repositório:")
	fmt.Printf("%s", yellow("Selecione o número para instalar ou 'q' para sair: "))
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "q" || input == "" { return }
	choice, _ := strconv.Atoi(input)
	if choice > 0 && choice <= len(pkgs) {
		name := cleanVersion(pkgs[choice-1].FullName)
		if runBinary("xbps-install", flags, []string{name}) { 
			checkAndEnableService(name) 
		}
	}
}

// --- MAIN ---

func main() {
	args := os.Args[1:]
	if len(args) == 0 { printUsage(); return }
	var flags []string
	var targets []string
	mode := "install"
	searchRemote := false

	for _, arg := range args {
		switch arg {
		case "-h", "--help": printUsage(); return
		case "-v", "--version": fmt.Printf("%s %s\n", white("vinstall"), cyan("v"+Version)); return
		case "--history": mode = "history"
		case "-Scc": mode = "clean"
		case "-X", "-x": mode = "remove"
		case "-F": mode = "find"
		case "-FR": 
			mode = "find"
			searchRemote = true
		case "-Li": mode = "list-installed"
		case "-Lo": mode = "list-orphans"
		case "-Qs": mode = "query-search"
		case "-Ss": mode = "remote-search"
		default:
			if strings.HasPrefix(arg, "-") { 
				flags = append(flags, arg) 
			} else { 
				targets = append(targets, arg) 
			}
		}
	}

	switch mode {
	case "history": showHistory()
	case "clean": cleanXbpsCache()
	case "find": if len(targets) > 0 { findProvides(targets[0], searchRemote) }
	case "list-installed": listLocal("installed", "")
	case "list-orphans": listLocal("orphans", "")
	case "query-search": if len(targets) > 0 { listLocal("search", targets[0]) }
	case "remote-search": 
		if len(targets) > 0 { 
			displaySearch(fetchSuggestions(targets[0]), "\nResultados encontrados no repositório:") 
		}
	case "remove": if len(targets) > 0 { runBinary("xbps-remove", flags, targets) }
	default:
		if len(targets) > 0 {
			if !runBinary("xbps-install", flags, targets) {
				displayMenu(fetchSuggestions(targets[0]), flags)
			} else {
				for _, t := range targets { checkAndEnableService(t) }
			}
		} else if len(flags) > 0 {
			runBinary("xbps-install", flags, []string{})
		}
	}
}

func printUsage() {
	fmt.Printf("%s %s\n", white("vinstall"), cyan("v"+Version))
	fmt.Printf("%s\n\n", cyan(Copyright))
	fmt.Printf("%s vinstall [flags] <pacote>\n", yellow("Uso:"))
	fmt.Printf("%s\n\n", white("==> O vinstall aceita todas as flags nativas do xbps-install"))
	fmt.Println("Exemplos:")
	fmt.Printf("  %s %-15s\n", green("vinstall"), white("telegram"))
	fmt.Printf("  %s %-15s\n", green("vinstall"), white("-Syu"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-X"), white("pacote (Remover)"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-F"), white("ifconfig (Busca local)"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-FR"), white("ifconfig (Busca remota)"))
	fmt.Println("\nAtalhos de Consulta:")
	fmt.Printf("  %-20s %s\n", green("-Li"), white("Lista todos os pacotes instalados"))
	fmt.Printf("  %-20s %s\n", green("-Lo"), white("Lista apenas pacotes órfãos"))
	fmt.Printf("  %-20s %s\n", green("-Qs <query>"), white("Busca termo nos pacotes instalados"))
	fmt.Printf("  %-20s %s\n", green("-Ss <query>"), white("Busca termo nos repositórios remotos"))
	fmt.Println("\nManutenção:")
	fmt.Printf("  %-20s %s\n", green("-Scc"), white("Limpa cache e órfãos"))
	fmt.Printf("  %-20s %s\n", green("--history"), white("Mostra histórico de transações"))
	fmt.Println()
}
