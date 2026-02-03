/*
    voidbr-vinstall
    Wrapper para o Void xbps-query e xbps-install

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   ter 03 fev 2026 16:15:40 -04
    Version:   1.2.2-20260203
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/fatih/color"
)

const (
	Version   = "1.2.2-20260203"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
)

var (
	cyan    = color.New(color.Bold, color.FgCyan).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	white   = color.New(color.Bold, color.FgWhite).SprintFunc()
	red     = color.New(color.FgRed).SprintFunc()
	yellow  = color.New(color.Bold, color.FgYellow).SprintFunc()
)

type Package struct {
	Status      string
	FullName    string
	Description string
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		return
	}

	var flags []string
	var targets []string

	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printUsage()
			return
		}
		if arg == "-v" || arg == "--version" {
			fmt.Printf("vinstall v%s\n", Version)
			return
		}
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
		} else {
			targets = append(targets, arg)
		}
	}

	if len(targets) == 0 && len(flags) > 0 {
		runXbps(flags, "")
		return
	}

	target := ""
	if len(targets) > 0 {
		target = targets[0]
	}

	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	if target != "" {
		fmt.Printf("%s %s %s...\n", cyan("[vinstall]"), white("📦 Verificando:"), yellow(target))
	}

	if runXbps(flags, target) {
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
	fmt.Printf("  %s %s\n", green("vinstall"), white("-f yasm"))
}

func runXbps(flags []string, pkg string) bool {
	params := []string{"xbps-install"}
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
		if len(line) < 5 { continue }
		
		// O xbps-query -Rs tem um formato rígido:
		// [status] [nome-versao] [descricao]
		// Ex: [-] yasm-1.3.0_2 Assembler supporting x86 and x86-64
		
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			status := parts[0]
			fullName := parts[1]
			// A descrição começa após o nome do pacote. 
			// Usamos Index para pegar o resto da linha original fielmente.
			descIdx := strings.Index(line, fullName) + len(fullName)
			description := ""
			if descIdx < len(line) {
				description = strings.TrimSpace(line[descIdx:])
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

	// Cálculo da coluna para manter a fidelidade visual
	maxNameLen := 0
	for _, p := range pkgs {
		if len(p.FullName) > maxNameLen {
			maxNameLen = len(p.FullName)
		}
	}
	if maxNameLen > 45 { maxNameLen = 45 }

	fmt.Printf("\n%s\n", cyan("Sugestões encontradas no repositório:"))
	fmt.Println(lineStr)
	
	count := 0
	for i, pkg := range pkgs {
		if i >= 20 { break } // Aumentado para 20 sugestões
		
		idx := yellow(fmt.Sprintf("[%2d]", i+1))
		statusColor := red(pkg.Status)
		if pkg.Status == "[*]" {
			statusColor = green(pkg.Status)
		}

		// Mostra exatamente o nome do pacote e a descrição alinhada
		fmt.Printf("%s %s %s  %s\n", 
			idx, 
			statusColor, 
			white(fmt.Sprintf("%-*s", maxNameLen, pkg.FullName)), 
			green(pkg.Description))
		count++
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

	var choice int
	_, err := fmt.Sscanf(input, "%d", &choice)
	if err == nil && choice > 0 && choice <= count {
		selected := cleanVersion(pkgs[choice-1].FullName)
		fmt.Printf("\n%s %s %s...\n", cyan("[vinstall]"), white("Iniciando instalação de:"), green(selected))
		runXbps(flags, selected)
	} else {
		fmt.Printf("%s %s\n", red("[!]"), white("Opção inválida."))
	}
}
