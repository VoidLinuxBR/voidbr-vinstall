/*
    voidbr-vinstall
    Wrapper para o Void xbps-query e xbps-install

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   ter 03 fev 2026 22:10:15 -04
    Version:   2.1.6-20260203
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

	"github.com/fatih/color"
)

const (
	Version   = "2.1.6-20260203"
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

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

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
	if err != nil {
		fmt.Printf("%s %s %v\n", red("[X]"), white("Erro ao acessar o cache:"), err)
		return
	}

	var pkgCount int
	var totalSize int64

	fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Iniciando limpeza total do cache em /var/cache/xbps..."))

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		isPkg := strings.HasSuffix(name, ".xbps")
		isSig := strings.HasSuffix(name, ".sig2")

		if isPkg || isSig {
			info, err := file.Info()
			if err == nil {
				totalSize += info.Size()
				errRemove := os.Remove(filepath.Join(cachePath, name))
				if errRemove == nil && isPkg {
					pkgCount++
				}
			}
		}
	}

	fmt.Printf("\n%s %s\n", green("[✔]"), white("Limpeza concluída com sucesso!"))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Total de pacotes (.xbps) removidos:"), cyan(strconv.Itoa(pkgCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Espaço total liberado (incluindo assinaturas):"), green(formatBytes(totalSize)))
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		return
	}

	var flags []string
	var targets []string
	isScc := false
	isRemove := false

	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printUsage()
			return
		}
		if arg == "-v" || arg == "--version" {
			fmt.Printf("vinstall v%s\n", Version)
			return
		}
		if arg == "-Scc" {
			isScc = true
			continue
		}
		if arg == "-X" {
			isRemove = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
		} else {
			targets = append(targets, arg)
		}
	}

	if isScc {
		cleanXbpsCache()
		return
	}

	target := ""
	if len(targets) > 0 {
		target = targets[0]
	}

	fmt.Printf("%s v%s\n", white("vinstall"), Version)

	if isRemove {
		if target == "" {
			fmt.Printf("%s %s\n", red("[!]"), white("Erro: Informe o pacote para remover."))
			return
		}
		fmt.Printf("%s %s %s...\n", red("[vinstall]"), white("🗑️ Removendo:"), yellow(target))
		runBinary("xbps-remove", flags, target)
		return
	}

	if len(targets) == 0 && len(flags) > 0 {
		runBinary("xbps-install", flags, "")
		return
	}

	if target != "" {
		fmt.Printf("%s %s %s...\n", cyan("[vinstall]"), white("📦 Verificando:"), yellow(target))
	}

	if runBinary("xbps-install", flags, target) {
		fmt.Printf("\n%s %s\n", green("[✔]"), white("Operação concluída com sucesso!"))
		return
	}

	if target != "" {
		fmt.Printf("\n%s %s '%s'. %s\n", red("[!]"), white("Pacote não instalado."), yellow(target), white("Buscando alternativas..."))
		suggestions := fetchSuggestions(target)
		
		if len(suggestions) > 0 {
			displayMenu(suggestions, flags)
		} else {
			fmt.Printf("%s %s\n", red("[x]"), white("Nenhuma correspondência encontrada no repositório."))
		}
	}
}

func getTerminalWidth() int {
	cmd := exec.Command("tput", "cols")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return 80
	}
	w, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return w
}

func printUsage() {
	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s\n\n", cyan(Copyright))
	fmt.Printf("%s vinstall [flags] <pacote>\n\n", yellow("Uso:"))
	fmt.Println("Exemplos:")
	fmt.Printf("  %s %s\n", green("vinstall"), white("telegram"))
	fmt.Printf("  %s %s\n", green("vinstall"), white("-Syu"))
	fmt.Printf("  %s %s\n", green("vinstall"), white("-X telegram (Remove pacote)"))
	fmt.Printf("  %s %s %s\n", green("vinstall"), white("-Scc"), cyan("(Limpa cache de pacotes e assinaturas)"))
}

func runBinary(bin string, flags []string, pkg string) bool {
	params := []string{bin}
	params = append(params, flags...)
	if pkg != "" {
		params = append(params, pkg)
	}
	cmd := exec.Command("sudo", params...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run() == nil
}

func fetchSuggestions(query string) []Package {
	cmd := exec.Command("xbps-query", "-Rs", query)
	output, _ := cmd.Output()
	lines := strings.Split(string(output), "\n")
	var pkgs []Package

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" { continue }
		
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			status := parts[0]
			fullName := parts[1]
			
			description := ""
			idx := strings.Index(line, fullName)
			if idx != -1 {
				description = strings.TrimSpace(line[idx+len(fullName):])
			}

			pkgs = append(pkgs, Package{
				Status:      status,
				FullName:    fullName,
				Description: description,
			})
		}
	}
	return pkgs
}

func cleanVersion(fullName string) string {
	lastDash := strings.LastIndex(fullName, "-")
	if lastDash != -1 {
		return fullName[:lastDash]
	}
	return fullName
}

func displayMenu(pkgs []Package, flags []string) {
	width := getTerminalWidth()
	lineStr := white(strings.Repeat("─", width))

	maxNameLen := 0
	for _, p := range pkgs {
		if len(p.FullName) > maxNameLen {
			maxNameLen = len(p.FullName)
		}
	}
	if maxNameLen > 50 { maxNameLen = 50 }

	fmt.Printf("\n%s\n", cyan("Sugestões encontradas no repositório:"))
	fmt.Println(lineStr)
	
	for i, pkg := range pkgs {
		idx := yellow(fmt.Sprintf("[%2d]", i+1))
		statusColor := red(pkg.Status)
		if pkg.Status == "[*]" {
			statusColor = green(pkg.Status)
		}

		fmt.Printf("%s %s %s  %s\n", 
			idx, 
			statusColor, 
			white(fmt.Sprintf("%-*s", maxNameLen, pkg.FullName)), 
			green(pkg.Description))
	}
	
	fmt.Println(lineStr)
	fmt.Printf("%s ", yellow("Selecione o número para instalar ou 'q' para sair: "))
	
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	
	if input == "q" || input == "" {
		fmt.Println(white("Operação cancelada."))
		return
	}

	choice, err := strconv.Atoi(input)
	if err == nil && choice > 0 && choice <= len(pkgs) {
		selected := cleanVersion(pkgs[choice-1].FullName)
		fmt.Printf("\n%s %s %s...\n", cyan("[vinstall]"), white("Iniciando instalação de:"), green(selected))
		runBinary("xbps-install", flags, selected)
	} else {
		fmt.Printf("%s %s\n", red("[!]"), white("Opção inválida."))
	}
}
