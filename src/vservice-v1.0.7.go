/*
    vservice
    Gerenciador de serviços (runit)

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   qua 24 jun 2026 12:58:00 -04
    Version:   1.0.7
    Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

const (
	SvDir     = "/etc/sv"
	ActiveDir = "/var/service"
	LogFile   = "/var/log/vservice.log"
)

var (
	ProtectedServices = []string{"dbus", "udevd", "socklog-unix", "nanoklogd", "agetty-tty1"}
	DryRun            = false
)

const (
	Reset  = "\033[0m"
	Red    = "\033[1;31m"
	Green  = "\033[1;32m"
	Yellow = "\033[1;33m"
	Blue   = "\033[1;34m"
)

func msg(s string)      { fmt.Printf("%s✔%s %s\n", Green, Reset, s) }
func warn(s string)     { fmt.Printf("%s⚠%s %s\n", Yellow, Reset, s) }
func showErr(s string)  { fmt.Fprintf(os.Stderr, "%s✘%s %s\n", Red, Reset, s) }
func info(s string)     { fmt.Printf("%s➜%s %s\n", Blue, Reset, s) }

func usage() {
	fmt.Printf("Uso: vservice {comando} [serviço...]\n\n")
	fmt.Printf("Comandos:\n")
	fmt.Printf("  enable, add      - habilita serviço\n")
	fmt.Printf("  disable          - desabilita serviço\n")
	fmt.Printf("  remove, rm       - remove serviço\n")
	fmt.Printf("  start, st, up    - inicia serviço\n")
	fmt.Printf("  stop, down       - para serviço\n")
	fmt.Printf("  restart          - reinicia serviço\n")
	fmt.Printf("  status           - mostra status\n")
	fmt.Printf("  list             - lista serviços\n")
	fmt.Printf("  archive-logs     - rotaciona/comprime logs\n")
	fmt.Printf("\nOpções:\n")
	fmt.Printf("  --dry-run            - simula comandos\n")
	fmt.Printf("  --install-completion - instala autocompletar\n")
	os.Exit(1)
}

func runCmd(name string, args ...string) error {
	if DryRun {
		fmt.Printf("%s[DRY-RUN]%s Executando: %s %s\n", Yellow, Reset, name, strings.Join(args, " "))
		return nil
	}
	return exec.Command(name, args...).Run()
}

func archiveLogs() {
	if _, err := os.Stat(LogFile); os.IsNotExist(err) {
		warn("Nenhum log encontrado para arquivar.")
		return
	}
	archive := fmt.Sprintf("%s.%s.gz", LogFile, time.Now().Format("20060102-150405"))
	info("Arquivando log em " + archive)
	
	if !DryRun {
		// Correção: usamos sh -c para garantir que o redirecionamento funcione
		cmd := fmt.Sprintf("gzip -c %s > %s", LogFile, archive)
		if err := exec.Command("sh", "-c", cmd).Run(); err != nil {
			showErr("Erro ao comprimir logs: " + err.Error())
			return
		}
		os.Truncate(LogFile, 0)
		msg("Logs arquivados com sucesso.")
	} else {
		fmt.Printf("%s[DRY-RUN]%s Comprimiria %s para %s e limparia original\n", Yellow, Reset, LogFile, archive)
	}
}

func fzfSelect(action string) string {
	files, _ := filepath.Glob(filepath.Join(SvDir, "*"))
	var filtered []string
	for _, f := range files {
		name := filepath.Base(f)
		_, err := os.Lstat(filepath.Join(ActiveDir, name))
		isActive := err == nil
		if (action == "add" && !isActive) || (action == "remove" && isActive) || (action == "other") {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 0 {
		warn("Nenhum serviço disponível para esta ação.")
		return ""
	}
	cmd := exec.Command("fzf", "--prompt=Selecione: ", "--height=40%", "--reverse")
	cmd.Stdin = strings.NewReader(strings.Join(filtered, "\n"))
	out, err := cmd.Output()
	if err != nil { return "" }
	return strings.TrimSpace(string(out))
}

func installCompletion() {
	path := "/usr/share/bash-completion/completions/vservice"
	content := `_vservice_completions() {
    local opts="enable add disable remove rm start st up stop down restart status list archive-logs"
    if [ ${COMP_CWORD} -eq 1 ]; then
        COMPREPLY=( $(compgen -W "${opts}" -- ${COMP_WORDS[COMP_CWORD]}) )
    else
        COMPREPLY=( $(compgen -W "$(ls /etc/sv)" -- ${COMP_WORDS[COMP_CWORD]}) )
    fi
}
complete -F _vservice_completions vservice`
	runCmd("sh", "-c", "echo '"+content+"' > "+path)
	msg("Autocompletar instalado em " + path)
}

func checkServiceHealth(s string) bool {
	runFile := filepath.Join(SvDir, s, "run")
	if _, e := os.Stat(runFile); os.IsNotExist(e) {
		showErr("arquivo 'run' ausente em " + filepath.Join(SvDir, s))
		return false
	}
	info, _ := os.Stat(runFile)
	if info.Mode()&0111 == 0 {
		showErr("serviço '" + s + "' sem permissão de execução")
		return false
	}
	return true
}

func shLog(t, m string) {
	u, _ := user.Current()
	runCmd("logger", "-p", "user.notice", "-t", "vservice", fmt.Sprintf("%s: %s por %s", t, m, u.Username))
	f, _ := os.OpenFile(LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		fmt.Fprintf(f, "[%s] [%s] %s (user: %s)\n", time.Now().Format("2006-01-02 15:04:05"), t, m, u.Username)
	}
}

func checkRoot() {
	u, _ := user.Current()
	if u.Uid != "0" {
		if path, e := exec.LookPath("sudo"); e == nil {
			args := append([]string{os.Args[0]}, os.Args[1:]...)
			cmd := exec.Command(path, args...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			cmd.Run()
		} else {
			showErr("precisa ser root")
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func isProtected(s string) bool {
	for _, p := range ProtectedServices {
		if s == p { return true }
	}
	return false
}

func waitForService(s string) {
	fmt.Printf("%sAguardando %s%s%s... ", Blue, Yellow, s, Blue)
	for i := 0; i < 14; i++ {
		out, _ := exec.Command("sv", "status", s).Output()
		if strings.Contains(string(out), "run:") {
			fmt.Printf("%sOK%s\n", Green, Reset)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("%sTIMEOUT%s\n", Red, Reset)
}

func showStatus(s string) {
	if _, e := exec.LookPath("vsv"); e == nil {
		out, _ := exec.Command("vsv").Output()
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, s) { fmt.Println(line) }
		}
	} else {
		runCmd("sv", "status", s)
	}
}

func doAdd(s string) {
	if !checkServiceHealth(s) { return }
	active := filepath.Join(ActiveDir, s)
	if _, e := os.Lstat(active); e == nil {
		if !DryRun {
			warn("serviço '" + s + "' já habilitado")
			return
		}
		fmt.Printf("%s[DRY-RUN]%s Serviço '%s' já está habilitado.\n", Yellow, Reset, s)
	}
	info("Adicionando serviço " + s)
	runCmd("ln", "-s", filepath.Join(SvDir, s), active)
	shLog("ENABLE", s)
	if !DryRun {
		waitForService(s)
		showStatus(s)
	}
}

func doRemove(s string) {
	if isProtected(s) {
		showErr("operação negada: '" + s + "' é protegido.")
		return
	}
	active := filepath.Join(ActiveDir, s)
	if _, e := os.Lstat(active); os.IsNotExist(e) {
		if !DryRun {
			warn("serviço '" + s + "' não habilitado")
			return
		}
		fmt.Printf("%s[DRY-RUN]%s Serviço '%s' não está habilitado.\n", Yellow, Reset, s)
	}
	runCmd("sv", "stop", s)
	runCmd("rm", active)
	msg("serviço '" + s + "' removido")
	shLog("REMOVE", s)
}

func doSvCommand(cmd, s string) {
	if _, e := os.Stat(filepath.Join(SvDir, s)); os.IsNotExist(e) {
		showErr("serviço '" + s + "' não encontrado")
		return
	}
	if (cmd == "stop" || cmd == "down") && isProtected(s) {
		showErr("operação negada: '" + s + "' é crítico.")
		return
	}
	info("sv " + cmd + " " + s)
	runCmd("sv", cmd, s)
	shLog("COMMAND", cmd + " em " + s)
	if !DryRun && strings.Contains("start st up restart", cmd) { waitForService(s) }
	if !DryRun { showStatus(s) }
}

func main() {
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--dry-run" {
			DryRun = true
			os.Args = append(os.Args[:i], os.Args[i+1:]...)
			i--
		}
	}
	if len(os.Args) < 2 { usage() }

	switch os.Args[1] {
	case "--install-completion":
		checkRoot()
		installCompletion()
		os.Exit(0)
	case "archive-logs":
		checkRoot()
		archiveLogs()
		os.Exit(0)
	case "list":
		checkRoot()
		if _, e := exec.LookPath("vsv"); e == nil {
			runCmd("vsv")
		} else {
			runCmd("sv", "status", ActiveDir+"/*")
		}
		os.Exit(0)
	}

	checkRoot()
	action := os.Args[1]
	if len(os.Args) < 3 {
		fzfAction := "other"
		if action == "add" || action == "enable" { fzfAction = "add" } else if action == "remove" || action == "rm" { fzfAction = "remove" }
		s := fzfSelect(fzfAction)
		if s == "" { os.Exit(0) }
		os.Args = append(os.Args, s)
	}

	for _, s := range os.Args[2:] {
		switch action {
		case "add", "enable", "install": doAdd(s)
		case "disable": doSvCommand("stop", s)
		case "remove", "rm": doRemove(s)
		default: doSvCommand(action, s)
		}
	}
}
