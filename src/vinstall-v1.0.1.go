package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Cores ANSI
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Cyan   = "\033[36m"
)

type Package struct {
	Name        string
	Description string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("%sUso:%s %svinstall <nome-do-pacote>%s\n", Yellow, Reset, Bold, Reset)
		return
	}

	target := os.Args[1]

	fmt.Printf("%s[%svinstall%s]%s 📦 Verificando: %s%s%s...\n", Cyan, Bold, Cyan, Reset, Bold, target, Reset)

	if install(target) {
		fmt.Printf("%s[✔] Instalação concluída com sucesso!%s\n", Green, Reset)
		return
	}

	fmt.Printf("\n%s[!] '%s' não encontrado.%s Buscando alternativas...\n", Red, target, Reset)
	suggestions := fetchSuggestions(target)

	if len(suggestions) == 0 {
		fmt.Printf("%s[x] Nenhuma correspondência encontrada no repositório.%s\n", Red, Reset)
		return
	}

	displayMenu(suggestions)
}

func install(pkg string) bool {
	cmd := exec.Command("sudo", "xbps-install", "-S", pkg)
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
			fullName := parts[1]
			nameOnly := cleanVersion(fullName)
			desc := strings.Join(parts[2:], " ")
			pkgs = append(pkgs, Package{Name: nameOnly, Description: desc})
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

func displayMenu(pkgs []Package) {
	fmt.Printf("\n%s%sEncontrei estas opções no Void Linux:%s\n", Bold, Cyan, Reset)
	fmt.Println(strings.Repeat("─", 65))
	
	for i, pkg := range pkgs {
		if i >= 10 { break }
		fmt.Printf("%s[%d]%s %s%-20s%s %s\n", Yellow, i+1, Reset, Green, pkg.Name, Reset, pkg.Description)
	}
	fmt.Println(strings.Repeat("─", 65))
	fmt.Printf("%sDigite o número ou 'q' para sair: %s", Bold, Reset)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "q" {
		fmt.Println("Saindo...")
		return
	}

	var choice int
	_, err := fmt.Sscanf(input, "%d", &choice)
	if err == nil && choice > 0 && choice <= len(pkgs) {
		selected := pkgs[choice-1].Name
		fmt.Printf("\n%s[vinstall]%s Preparando instalação de: %s%s%s\n", Cyan, Reset, Bold, selected, Reset)
		install(selected)
	} else {
		fmt.Printf("%s[!] Opção inválida.%s\n", Red, Reset)
	}
}
