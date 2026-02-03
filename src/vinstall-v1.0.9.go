/*
    voidbr-vinstall
    Wrapper para o Void xbps-query e xbps-install

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   ter 03 fev 2026 15:25:12 -04
    Version:   1.0.9-20260203
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
	Version   = "1.0.9-20260203"
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

	if runXbps(flags, target) {
		fmt.Printf("\n%s %s\n", green("[✔]"), white("Operação concluída com sucesso!"))
		return
	}

	fmt.Printf("\n%s %s '%s'. %s\n", red("[!]"), white("Pacote"), yellow(target), white("Buscando alternativas..."))
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
	fmt.Println("  vinstall telegram          (Instalação simples)")
	fmt.Println("  vinstall -Syu              (Atualiza o sistema)")
	fmt.Println("  vinstall -Sy telegram      (Sincroniza e instala)")
}

// runXbps agora processa a saída para dar cor
func runXbps(flags []string, pkg string) bool {
	params := []string{"xbps-install"}
	params = append(params, flags...)
	if pkg != "" {
		params = append(params, pkg)
	}

	cmd := exec.Command("sudo", params...)
	
	// Criamos Pipes para ler a saída
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return false
	}

	// Função anônima para processar e colorir a saída
	colorizer := func(r io.Reader, isError bool) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			
			// Lógica de cores baseada em palavras-chave do XBPS
			if strings.Contains(line, "installing") {
				line = strings.Replace(line, "installing", green("installing"), 1)
			} else if strings.Contains(line, "verifying") {
				line = strings.Replace(line, "verifying", cyan("verifying"), 1)
			} else if strings.Contains(line, "unpacking") {
				line = strings.Replace(line, "unpacking", magenta("unpacking"), 1)
			} else if strings.Contains(line, "configuring") {
				line = strings.Replace(line, "configuring", yellow("configuring"), 1)
			} else if strings.Contains(line, "transaction") {
				line = blue(line)
			}

			if isError {
				fmt.Fprintln(os.Stderr, red(line))
			} else {
				fmt.Println(line)
			}
		}
	}

	go colorizer(stdout, false)
	go colorizer(stderr, true)

	err := cmd.Wait()
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
