/*
    voidbr-vinstall
    Wrapper para o Void xbps-query e xbps-install

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   ter 03 fev 2026 16:35:10 -04
    Version:   1.1.2-20260203
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
)

const (
	Version   = "1.1.2-20260203"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
)

var (
	cyan    = color.New(color.Bold, color.FgCyan).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	white   = color.New(color.Bold, color.FgWhite).SprintFunc()
	red     = color.New(color.FgRed).SprintFunc()
	yellow  = color.New(color.Bold, color.FgYellow).SprintFunc()
	magenta = color.New(color.Bold, color.FgMagenta).SprintFunc()
)

type Package struct {
	Name        string
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
		if strings.HasPrefix(arg, "-") {
			if arg == "-v" || arg == "--version" {
				fmt.Printf("vinstall v%s\n", Version)
				return
			}
			flags = append(flags, arg)
		} else {
			targets = append(targets, arg)
		}
	}

	if len(targets) == 0 && len(flags) > 0 {
		runXbps(flags, "")
		return
	}

	target := targets[0]

	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s %s %s...\n", cyan("[vinstall]"), white("📦 Verificando:"), yellow(target))

	// Tenta instalar. Se falhar (pacote não encontrado), o xbps retorna erro 2 ou 19
	if runXbps(flags, target) {
		fmt.Printf("\n%s %s\n", green("[✔]"), white("Operação concluída com sucesso!"))
		return
	}

	// Se chegamos aqui, o pacote provavelmente não existe. Buscamos sugestões.
	fmt.Printf("\n%s %s '%s'. %s\n", red("[!]"), white("Pacote não instalado."), yellow(target), white("Buscando alternativas..."))
	suggestions := fetchSuggestions(target)

	if len(suggestions) == 0 {
		fmt.Printf("%s %s\n", red("[x]"), white("Nenhuma correspondência encontrada."))
		return
	}

	displayMenu(suggestions, flags)
}

func printUsage() {
	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s\n\n", cyan(Copyright))
	fmt.Printf("%s vinstall [flags] <pacote>\n\n", yellow("Uso:"))
	fmt.Println("Exemplos:")
	fmt.Println("  vinstall telegram")
	fmt.Println("  vinstall -Syu")
}

// runXbps sem Pipes para GARANTIR interatividade total
func runXbps(flags []string, pkg string) bool {
	params := []string{"xbps-install"}
	params = append(params, flags...)
	if pkg != "" {
		params = append(params, pkg)
	}

	cmd := exec.Command("sudo", params...)
	
	// Conexão direta com o terminal do sistema (TTY)
	// Isso garante que perguntas [Y/n] funcionem perfeitamente
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	return err == nil
}

func fetchSuggestions(query string) []Package {
	cmd := exec.Command("xbps-query", "-Rs", query)
	output, _ := cmd.Output()
	lines := strings.Split(string(output), "\n")
	var pkgs []Package
	for _, line := range lines {
		if line == "" { continue }
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			pkgs = append(pkgs, Package{
				Name:        cleanVersion(parts[1]),
				Description: strings.Join(parts[2:], " "),
			})
		}
	}
	return pkgs
}

func cleanVersion(fullName string) string {
	lastDash := strings.LastIndex(fullName, "-")
	if lastDash != -1 { return fullName[:lastDash] }
	return fullName
}

func displayMenu(pkgs []Package, flags []string) {
	fmt.Printf("\n%s\n", cyan("Sugestões encontradas:"))
	fmt.Println(white(strings.Repeat("─", 75)))
	for i, pkg := range pkgs {
		if i >= 15 { break }
		fmt.Printf("%s %-25s %s\n", yellow(fmt.Sprintf("[%d]", i+1)), green(pkg.Name), white(pkg.Description))
	}
	fmt.Println(white(strings.Repeat("─", 75)))
	fmt.Printf("%s ", yellow("Selecione o número ou 'q' para sair: "))
	
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "q" || input == "" { return }

	var choice int
	_, err := fmt.Sscanf(input, "%d", &choice)
	if err == nil && choice > 0 && choice <= len(pkgs) {
		selected := pkgs[choice-1].Name
		fmt.Printf("\n%s %s %s...\n", cyan("[vinstall]"), white("Instalando:"), green(selected))
		runXbps(flags, selected)
	}
}
