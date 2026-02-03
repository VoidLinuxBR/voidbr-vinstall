package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
)

// Constantes de versão e metadados
const (
	Version   = "1.0.4-20260203"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
)

// Definição das funções de cores
var (
	cyan    = color.New(color.Bold, color.FgCyan).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	white   = color.New(color.Bold, color.FgWhite).SprintFunc()
	red     = color.New(color.FgRed).SprintFunc()
	blue    = color.New(color.FgBlue).SprintFunc()
	yellow  = color.New(color.Bold, color.FgYellow).SprintFunc()
	magenta = color.New(color.Bold, color.FgMagenta).SprintFunc()
)

// Package representa a estrutura de um pacote do Void
type Package struct {
	Name        string
	Description string
}

func main() {
	// 1. Verifica argumentos
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	target := os.Args[1]

	// 2. Banner de inicialização
	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s %s %s...\n", cyan("[vinstall]"), white("📦 Verificando:"), yellow(target))

	// 3. Tenta instalar diretamente
	if install(target) {
		fmt.Printf("\n%s %s\n", green("[✔]"), white("Instalação concluída com sucesso!"))
		return
	}

	// 4. Se falhar, busca sugestões (Fuzzy search do XBPS)
	fmt.Printf("\n%s %s '%s'. %s\n", red("[!]"), white("Pacote"), yellow(target), white("Buscando alternativas..."))
	suggestions := fetchSuggestions(target)

	if len(suggestions) == 0 {
		fmt.Printf("%s %s\n", red("[x]"), white("Nenhuma correspondência encontrada no repositório."))
		return
	}

	// 5. Mostra menu se houver sugestões
	displayMenu(suggestions)
}

func printUsage() {
	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s\n\n", cyan(Copyright))
	fmt.Printf("%s vinstall <pacote>\n", yellow("Uso:"))
}

func install(pkg string) bool {
	// Tenta rodar o xbps-install. Redireciona Stdout/Stderr para o usuário ver o progresso
	cmd := exec.Command("sudo", "xbps-install", "-S", pkg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Run()
	return err == nil
}

func fetchSuggestions(query string) []Package {
	// xbps-query -Rs busca pacotes que contenham a string
	cmd := exec.Command("xbps-query", "-Rs", query)
	output, _ := cmd.Output()

	lines := strings.Split(string(output), "\n")
	var pkgs []Package

	for _, line := range lines {
		if line == "" {
			continue
		}
		
		// Formato do XBPS: [-] nome-versão_revisão Descrição
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			fullName := parts[1]
			nameOnly := cleanVersion(fullName)
			desc := strings.Join(parts[2:], " ")
			
			pkgs = append(pkgs, Package{
				Name:        nameOnly,
				Description: desc,
			})
		}
	}
	return pkgs
}

func cleanVersion(fullName string) string {
	// Remove a versão (tudo após o último hífen que precede a versão)
	// Ex: telegram-desktop-4.14.9_1 -> telegram-desktop
	lastDash := strings.LastIndex(fullName, "-")
	if lastDash != -1 {
		return fullName[:lastDash]
	}
	return fullName
}

func displayMenu(pkgs []Package) {
	fmt.Printf("\n%s\n", cyan("Encontrei estas opções no Void Linux:"))
	fmt.Println(white(strings.Repeat("─", 70)))
	
	count := 0
	for i, pkg := range pkgs {
		if i >= 15 { break } // Limite de 15 sugestões
		fmt.Printf("%s %-25s %s\n", yellow(fmt.Sprintf("[%d]", i+1)), green(pkg.Name), white(pkg.Description))
		count++
	}
	fmt.Println(white(strings.Repeat("─", 70)))
	fmt.Printf("%s ", yellow("Digite o número para instalar ou 'q' para sair: "))

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
		selected := pkgs[choice-1].Name
		fmt.Printf("\n%s %s %s...\n", cyan("[vinstall]"), white("Iniciando instalação de:"), green(selected))
		install(selected)
	} else {
		fmt.Printf("%s %s\n", red("[!]"), white("Opção inválida."))
	}
}
