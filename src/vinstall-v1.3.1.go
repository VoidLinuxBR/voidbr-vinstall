/*
    voidbr-vinstall
    Wrapper para o Void xbps-query e xbps-install

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   qua 04 fev 2026 02:40:12 -04
    Version:   1.3.1-20260204
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

const (
	Version   = "1.3.1-20260204"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
)

var (
	cyan   = color.New(color.Bold, color.FgCyan).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	white  = color.New(color.Bold, color.FgWhite).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	yellow = color.New(color.Bold, color.FgYellow).SprintFunc()
)

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
	cmd := exec.Command("tput", "cols")
	out, err := cmd.Output()
	if err != nil { return 80 }
	w, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return w
}

func cleanVersion(fullName string) string {
	if i := strings.LastIndex(fullName, "-"); i != -1 { return fullName[:i] }
	return fullName
}

// --- ITEM 5: BUSCAS E LISTAGENS ---

func findProvides(file string) {
	fmt.Printf("%s %s '%s'...\n", cyan("[vinstall]"), white("Procurando pacote que contém:"), yellow(file))
	cmd := exec.Command("xbps-query", "-Ro", file)
	output, _ := cmd.Output()
	if res := strings.TrimSpace(string(output)); res != "" {
		fmt.Println(green(res))
	} else {
		fmt.Printf("%s %s\n", red("[!]"), white("Nenhum pacote encontrado para este ficheiro."))
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
		fmt.Printf("%s %s '%s'...\n", cyan("[vinstall]"), white("Buscando nos instalados por:"), yellow(query))
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

// --- ITEM 3: MANUTENÇÃO ---

func checkOrphans() {
	cmd := exec.Command("xbps-query", "-O")
	output, _ := cmd.Output()
	orphans := strings.TrimSpace(string(output))
	if orphans != "" {
		fmt.Printf("\n%s %s\n%s\n", yellow("[!]"), white("Órfãos detetados:"), cyan(orphans))
		fmt.Printf("%s", white("Deseja removê-los? [s/N]: "))
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if input = strings.ToLower(strings.TrimSpace(input)); input == "s" || input == "sim" {
			runBinary("xbps-remove", []string{"-o"}, "")
		}
	}
}

func cleanXbpsCache() {
	if os.Geteuid() != 0 {
		runBinary(os.Args[0], []string{"-Scc"}, "")
		return
	}
	cachePath := "/var/cache/xbps"
	files, _ := os.ReadDir(cachePath)
	var totalSize int64
	var count int
	fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Limpando cache..."))
	for _, file := range files {
		if !file.IsDir() && (strings.HasSuffix(file.Name(), ".xbps") || strings.HasSuffix(file.Name(), ".sig2")) {
			info, _ := file.Info()
			totalSize += info.Size()
			if os.Remove(filepath.Join(cachePath, file.Name())) == nil && strings.HasSuffix(file.Name(), ".xbps") {
				count++
			}
		}
	}
	fmt.Printf("%s %s %s (%d pacotes)\n", green("[✔]"), white("Limpeza concluída!"), green(formatBytes(totalSize)), count)
	checkOrphans()
}

// --- ITEM 2: RUNIT ---

func checkAndEnableService(pkgName string) {
	servicePath := filepath.Join("/etc/sv", pkgName)
	targetPath := filepath.Join("/var/service", pkgName)
	if info, err := os.Stat(servicePath); err == nil && info.IsDir() {
		if _, err := os.Lstat(targetPath); os.IsNotExist(err) {
			fmt.Printf("\n%s %s '%s'. Ativar agora? [s/N]: ", yellow("[!]"), white("Serviço disponível para"), cyan(pkgName))
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			if input = strings.ToLower(strings.TrimSpace(input)); input == "s" || input == "sim" {
				exec.Command("sudo", "ln", "-s", servicePath, targetPath).Run()
				fmt.Printf("%s %s\n", green("[✔]"), white("Habilitado! Aguardando 5s..."))
				time.Sleep(5 * time.Second)
				runBinary("sv", []string{"status"}, pkgName)
			}
		}
	}
}

// --- ITEM 4: HISTÓRICO ---

func showHistory() {
	logPath := "/var/log/socklog/xbps/current"
	file, err := os.Open(logPath)
	if err != nil {
		if os.IsPermission(err) { runBinary(os.Args[0], []string{"--history"}, ""); return }
		fmt.Println(red("[X] Erro ao ler logs.")) ; return
	}
	defer file.Close()
	
	fmt.Printf("\n%s %s\n%s\n", cyan("[vinstall]"), white("Histórico de transações:"), white(strings.Repeat("─", getTerminalWidth())))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 25 { line = line[25:] }
		if strings.Contains(line, "installed") { fmt.Println(green(line)) } else if strings.Contains(line, "removed") { fmt.Println(red(line)) } else if strings.Contains(line, "updated") { fmt.Println(yellow(line)) } else { fmt.Println(white(line)) }
	}
}

// --- CORE ---

func runBinary(bin string, flags []string, pkg string) bool {
	params := []string{bin}
	params = append(params, flags...)
	if pkg != "" { params = append(params, pkg) }
	cmd := exec.Command("sudo", params...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Run() == nil
}

func fetchSuggestions(query string) []Package {
	cmd := exec.Command("xbps-query", "-Rs", query)
	out, _ := cmd.Output()
	var pkgs []Package
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			desc := ""
			if idx := strings.Index(line, f[1]); idx != -1 { desc = strings.TrimSpace(line[idx+len(f[1]):]) }
			pkgs = append(pkgs, Package{f[0], f[1], desc})
		}
	}
	return pkgs
}

func displayMenu(pkgs []Package, flags []string) {
	if len(pkgs) == 0 { 
		fmt.Println(red("[x] Nenhuma correspondência encontrada."))
		return 
	}
	fmt.Printf("\n%s\n%s\n", cyan("Sugestões encontradas:"), white(strings.Repeat("─", getTerminalWidth())))
	for i, p := range pkgs {
		fmt.Printf("%s %s %-30s %s\n", yellow(fmt.Sprintf("[%2d]", i+1)), p.Status, white(p.FullName), green(p.Description))
	}
	fmt.Printf("\n%s", yellow("Escolha o número ou 'q': "))
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "q" || input == "" { return }
	choice, _ := strconv.Atoi(input)
	if choice > 0 && choice <= len(pkgs) {
		name := cleanVersion(pkgs[choice-1].FullName)
		if runBinary("xbps-install", flags, name) { checkAndEnableService(name) }
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 { printUsage(); return }

	var flags []string
	var targets []string
	mode := "install"

	for _, arg := range args {
		switch arg {
		case "-h", "--help": printUsage(); return
		case "-v", "--version": fmt.Printf("v%s\n", Version); return
		case "--history": mode = "history"
		case "-Scc": mode = "clean"
		case "-X", "-x": mode = "remove"
		case "-F": mode = "find"
		case "-Li": mode = "list-installed"
		case "-Lo": mode = "list-orphans"
		case "-Qs": mode = "query-search"
		default:
			if strings.HasPrefix(arg, "-") { flags = append(flags, arg) } else { targets = append(targets, arg) }
		}
	}

	target := ""
	if len(targets) > 0 { target = targets[0] }

	switch mode {
	case "history": showHistory()
	case "clean": cleanXbpsCache()
	case "find": findProvides(target)
	case "list-installed": listLocal("installed", "")
	case "list-orphans": listLocal("orphans", "")
	case "query-search": listLocal("search", target)
	case "remove":
		if target != "" { runBinary("xbps-remove", flags, target) }
	default:
		if target != "" {
			if !runBinary("xbps-install", flags, target) {
				displayMenu(fetchSuggestions(target), flags)
			} else {
				checkAndEnableService(target)
			}
		} else if len(flags) > 0 {
			runBinary("xbps-install", flags, "")
		}
	}
}

func printUsage() {
	fmt.Printf("%s v%s\n%s\n", white("vinstall"), Version, cyan(Copyright))
	fmt.Printf("\n%s vinstall [flags] <pacote>\n", yellow("Uso:"))
	fmt.Printf("\n%s\n", white("O vinstall aceita todas as flags nativas do xbps-install."))
	
	fmt.Println("\nExemplos:")
	fmt.Printf("  %s %s\n", green("vinstall"), white("telegram"))
	fmt.Printf("  %s %s\n", green("vinstall"), white("-Syu"))
	fmt.Printf("  %s %s %s\n", green("vinstall"), white("-X / -x"), white("pacote (Remover)"))
	fmt.Printf("  %s %s %s\n", green("vinstall"), white("-F"), white("ifconfig (Procurar)"))

	fmt.Println("\nAtalhos de Consulta:")
	fmt.Printf("  %-15s %s\n", green("-Li"), "Lista todos os pacotes instalados")
	fmt.Printf("  %-15s %s\n", green("-Lo"), "Lista apenas pacotes órfãos")
	fmt.Printf("  %-15s %s\n", green("-Qs <query>"), "Busca termo nos pacotes instalados")
	
	fmt.Println("\nManutenção:")
	fmt.Printf("  %-15s %s\n", green("-Scc"), "Limpa cache e órfãos")
	fmt.Printf("  %-15s %s\n", green("--history"), "Mostra histórico de transações")
}
