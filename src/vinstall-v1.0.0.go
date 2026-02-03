package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Package struct {
	Name        string
	Description string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Uso: vinstall <nome-do-pacote>")
		return
	}

	target := os.Args[1]

	// 1. Tenta a instalação direta primeiro
	if install(target) {
		return
	}

	// 2. Se falhar, busca sugestões
	fmt.Printf("\n[!] '%s' não encontrado. Buscando sugestões...\n", target)
	suggestions := fetchSuggestions(target)

	if len(suggestions) == 0 {
		fmt.Println("[x] Nenhuma correspondência encontrada.")
		return
	}

	// 3. Exibe o menu
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
		if line == "" {
			continue
		}
		
		// O Void retorna: [-] nome-versao Descrição
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			fullName := parts[1]
			// Limpa a versão do nome (ex: telegram-desktop-4.1.1_1 -> telegram-desktop)
			nameOnly := cleanVersion(fullName)
			
			desc := strings.Join(parts[2:], " ")
			pkgs = append(pkgs, Package{Name: nameOnly, Description: desc})
		}
	}
	return pkgs
}

func cleanVersion(fullName string) string {
	// O Void separa o nome da versão pelo último '-' antes do primeiro número
	// Mas o xbps-query -Rs facilita: o nome termina onde a versão começa.
	// Vamos remover o sufixo após o último hífen que contenha números.
	lastDash := strings.LastIndex(fullName, "-")
	if lastDash != -1 {
		return fullName[:lastDash]
	}
	return fullName
}

func displayMenu(pkgs []Package) {
	fmt.Println("\nSelecione uma opção para instalar:")
	fmt.Println(strings.Repeat("-", 60))
	
	for i, pkg := range pkgs {
		if i >= 10 { break } // Limita a 10 sugestões para não poluir
		fmt.Printf("[%d] %-20s - %s\n", i+1, pkg.Name, pkg.Description)
	}
	fmt.Println(strings.Repeat("-", 60))
	fmt.Print("Digite o número ou 'q' para sair: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "q" {
		return
	}

	var choice int
	_, err := fmt.Sscanf(input, "%d", &choice)
	if err == nil && choice > 0 && choice <= len(pkgs) {
		selected := pkgs[choice-1].Name
		fmt.Printf("\n[vinstall] Instalando %s...\n", selected)
		install(selected)
	}
}
