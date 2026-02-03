/*
    voidbr-vinstall
    Wrapper para o Void xbps-query e xbps-install

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   ter 03 fev 2026 14:10:14 -04
    Version:   1.0.7-20260203
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
)

// Metadados para exibição
const (
	Version   = "1.0.7-20260203"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
)

// Definição das funções de cores
var (
	cyan    = color.New(color.Bold, color.FgCyan).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	white   = color.New(color.Bold, color.FgWhite).SprintFunc()
	red     = color.New(color.FgRed).SprintFunc()
	yellow  = color.New(color.Bold, color.FgYellow).SprintFunc()
)

// Package representa a estrutura de um pacote do Void
type Package struct {
	Name        string
	Description string
}

func main() {
	// Definição de Flags
	syncFlag := flag.Bool("y", false, "Sincroniza os repositórios antes de instalar")
	versionFlag := flag.Bool("v", false, "Exibe a versão do vinstall")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("vinstall v%s\n", Version)
		return
	}

	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		return
	}

	target := args[0]

	// Banner principal
	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s %s %s...\n", cyan("[vinstall]"), white("📦 Verificando:"), yellow(target))

	// 1. Tentativa de instalação direta
	if install(target, *syncFlag) {
		fmt.Printf("\n%s %s\n", green("[✔]"), white("Instalação concluída com sucesso!"))
		return
	}

	// 2. Busca de sugestões em caso de falha (Fuzzy search)
	fmt.Printf("\n%s %s '%s'. %s\n", red("[!]"), white("Pacote"), yellow(target), white("Buscando alternativas..."))
	suggestions := fetchSuggestions(target)

	if len(suggestions) == 0 {
		fmt.Printf("%s %s\n", red("[x]"), white("Nenhuma correspondência encontrada no repositório."))
		return
	}

	// 3. Menu de seleção interativo
	displayMenu(suggestions, *syncFlag)
}

func printUsage() {
	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s\n\n", cyan(Copyright))
	fmt.Printf("%s vinstall [opções] <pacote>\n\n", yellow("Uso:"))
	fmt.Println("Opções:")
	fmt.Printf("  -y    Sincroniza repositórios (xbps-install -S)\n")
	fmt.Printf("  -v    Exibe versão\n")
}

func install(pkg string, sync bool) bool {
	args := []string{"xbps-install"}
	if sync {
		args = append(args, "-S")
	}
	args = append(args, pkg)

	cmd := exec.Command("sudo", args...)
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
		if line == "" {
			continue
		}
		
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
	if lastDash != -1 {
		return fullName[:lastDash]
	}
	return fullName
}

func displayMenu(pkgs []Package, sync bool) {
	fmt.Printf("\n%s\n", cyan("Sugestões encontradas no repositório:"))
	fmt.Println(white(strings.Repeat("─", 75)))
	
	count := 0
	for i, pkg := range pkgs {
		if i >= 15 { break }
		fmt.Printf("%s %-25s %s\n", yellow(fmt.Sprintf("[%d]", i+1)), green(pkg.Name), white(pkg.Description))
		count++
	}
	fmt.Println(white(strings.Repeat("─", 75)))
	fmt.Printf("%s ", yellow("Selecione o número para instalar ou 'q' para sair: "))

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "q" || input == "" {
		fmt.Println(white("Operação cancelada pelo usuário."))
		return
	}

	var choice int
	_, err := fmt.Sscanf(input, "%d", &choice)
	if err == nil && choice > 0 && choice <= count {
		selected := pkgs[choice-1].Name
		fmt.Printf("\n%s %s %s...\n", cyan("[vinstall]"), white("Iniciando instalação de:"), green(selected))
		install(selected, sync)
	} else {
		fmt.Printf("%s %s\n", red("[!]"), white("Escolha inválida."))
	}
}
