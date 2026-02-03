/*
    voidbr-vinstall
    Wrapper para o Void xbps-query e xbps-install

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   ter 03 fev 2026 15:35:40 -04
    Version:   1.1.0-20260203
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"
)

const (
	Version   = "1.1.0-20260203"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
)

var (
	cyan    = color.New(color.Bold, color.FgCyan).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	white   = color.New(color.Bold, color.FgWhite).SprintFunc()
	red     = color.New(color.FgRed).SprintFunc()
	yellow  = color.New(color.Bold, color.FgYellow).SprintFunc()
	magenta = color.New(color.FgMagenta).SprintFunc()
	blue    = color.New(color.FgBlue).SprintFunc()
)

// Package representa a estrutura de um pacote do Void
type Package struct {
	Name        string
	Description string
}

// ColorWriter intercepta o stream e aplica cores sem quebrar a interatividade do TTY
type ColorWriter struct {
	w       io.Writer
	isError bool
}

func (cw *ColorWriter) Write(p []byte) (n int, err error) {
	line := string(p)

	// Regras de colorização por substituição de palavras-chave
	if strings.Contains(line, "installing") {
		line = strings.ReplaceAll(line, "installing", green("installing"))
	} else if strings.Contains(line, "verifying") {
		line = strings.ReplaceAll(line, "verifying", cyan("verifying"))
	} else if strings.Contains(line, "unpacking") {
		line = strings.ReplaceAll(line, "unpacking", magenta("unpacking"))
	} else if strings.Contains(line, "configuring") {
		line = strings.ReplaceAll(line, "configuring", yellow("configuring"))
	} else if strings.Contains(line, "transaction") {
		line = blue(line)
	}

	if cw.isError {
		return fmt.Fprint(cw.w, red(line))
	}
	// Escreve diretamente no Writer original (os.Stdout/Stderr)
	return fmt.Fprint(cw.w, line)
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		return
	}

	var flags []string
	var targets []string

	// Separação de flags e alvos (pacotes)
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

	// Caso especial: apenas flags (ex: vinstall -Syu)
	if len(targets) == 0 && len(flags) > 0 {
		runXbps(flags, "")
		return
	}

	target := targets[0]

	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s %s %s...\n", cyan("[vinstall]"), white("📦 Verificando:"), yellow(target))

	// 1. Tenta executar o xbps-install original
	if runXbps(flags, target) {
		fmt.Printf("\n%s %s\n", green("[✔]"), white("Operação concluída com sucesso!"))
		return
	}

	// 2. Se falhar, busca sugestões no xbps-query
	fmt.Printf("\n%s %s '%s'. %s\n", red("[!]"), white("Pacote"), yellow(target), white("Buscando alternativas..."))
	suggestions := fetchSuggestions(target)

	if len(suggestions) == 0 {
		fmt.Printf("%s %s\n", red("[x]"), white("Nenhuma correspondência encontrada."))
		return
	}

	// 3. Exibe menu de seleção
	displayMenu(suggestions, flags)
}

func printUsage() {
	fmt.Printf("%s v%s\n", white("vinstall"), Version)
	fmt.Printf("%s\n\n", cyan(Copyright))
	fmt.Printf("%s vinstall [flags] <pacote>\n\n", yellow("Uso:"))
	fmt.Println("Exemplos:")
	fmt.Println("  vinstall telegram          (Instalação simples)")
	fmt.Println("  vinstall -Syu              (Atualiza o sistema)")
	fmt.Println("  vinstall -Sy telegram      (Sincroniza e instala)")
}

func runXbps(flags []string, pkg string) bool {
	params := []string{"xbps-install"}
	params = append(params, flags...)
	if pkg != "" {
		params = append(params, pkg)
	}

	cmd := exec.Command("sudo", params...)

	// Conecta o ColorWriter aos streams de saída
	cmd.Stdout = &ColorWriter{w: os.Stdout, isError: false}
	cmd.Stderr = &ColorWriter{w: os.Stderr, isError: true}
	// Conecta o Stdin diretamente para permitir prompts interativos [Y/n]
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
	if lastDash != -1 {
		return fullName[:lastDash]
	}
	return fullName
}

func displayMenu(pkgs []Package, flags []string) {
	fmt.Printf("\n%s\n", cyan("Sugestões encontradas no repositório:"))
	fmt.Println(white(strings.Repeat("─", 75)))
	
	for i, pkg := range pkgs {
		if i >= 15 { break }
		fmt.Printf("%s %-25s %s\n", yellow(fmt.Sprintf("[%d]", i+1)), green(pkg.Name), white(pkg.Description))
	}
	fmt.Println(white(strings.Repeat("─", 75)))
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
	if err == nil && choice > 0 && choice <= len(pkgs) {
		selected := pkgs[choice-1].Name
		fmt.Printf("\n%s %s %s...\n", cyan("[vinstall]"), white("Iniciando instalação de:"), green(selected))
		runXbps(flags, selected)
	} else {
		fmt.Printf("%s %s\n", red("[!]"), white("Opção inválida."))
	}
}
