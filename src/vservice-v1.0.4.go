/*
    vservice
    Gerenciador de serviços (runit)

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   qua 24 jun 2026 12:20:00 -04
    Version:   1.0.4
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

var ProtectedServices = []string{"dbus", "udevd", "socklog-unix", "nanoklogd", "agetty-tty1"}

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
	fmt.Printf("\nOpções:\n")
	fmt.Printf("  --install-completion - instala autocompletar no bash\n")
	os.Exit(1)
}

// Nova função: Seleção interativa com fzf
func fzfSelect() string {
	files, _ := filepath.Glob(filepath.Join(SvDir, "*"))
	var services []string
	for _, f := range files {
		services = append(services, filepath.Base(f))
	}

	cmd := exec.Command("fzf", "--prompt=Selecione o serviço: ", "--height=40%", "--reverse")
	cmd.Stdin = strings.NewReader(strings.Join(services, "\n"))
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func installCompletion() {
	path := "/usr/share/bash-completion/completions/vservice"
	content := `_vservice_completions() {
    local opts="enable add disable remove rm start st up stop down restart status list"
    if [ ${COMP_CWORD} -eq 1 ]; then
        COMPREPLY=( $(compgen -W "${opts}" -- ${COMP_WORDS[COMP_CWORD]}) )
    else
        COMPREPLY=( $(compgen -W "$(ls /etc/sv)" -- ${COMP_WORDS[COMP_CWORD]}) )
    fi
}
complete -F _vservice_completions vservice`
	f, err := os.Create(path)
	if err != nil {
		showErr("Erro ao criar completion: " + err.Error())
		return
	}
	defer f.Close()
	f.WriteString(content)
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
	exec.Command("logger", "-p", "user.notice", "-t", "vservice", fmt.Sprintf("%s: %s por %s", t, m, u.Username)).Run()
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
		} else if path, e := exec.LookPath("su"); e == nil {
			cmd := exec.Command(path, "-c", strings.Join(os.Args, " "))
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
	fmt.Printf("%sAguardando inicialização de %s%s%s... ", Blue, Yellow, s, Blue)
	for i := 0; i < 14; i++ {
		out, _ := exec.Command("sv", "status", s).Output()
		if strings.Contains(string(out), "run:") && strings.Contains(string(out), "(pid") {
			fmt.Printf("%sOK%s\n", Green, Reset)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Printf("%sTIMEOUT (Verifique os logs)%s\n", Red, Reset)
}

func showStatus(s string) {
	if _, e := exec.LookPath("vsv"); e == nil {
		out, _ := exec.Command("vsv").Output()
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, s) { fmt.Println(line) }
		}
	} else {
		cmd := exec.Command("sv", "status", s)
		cmd.Stdout = os.Stdout
		cmd.Run()
	}
}

func doEnable(s string) {
	if !checkServiceHealth(s) { return }
	active := filepath.Join(ActiveDir, s)
	if _, e := os.Lstat(active); e == nil {
		warn("serviço '" + s + "' já está habilitado")
	} else {
		os.Symlink(filepath.Join(SvDir, s), active)
		msg("serviço '" + s + "' habilitado")
		shLog("ENABLE", s)
	}
	waitForService(s)
	showStatus(s)
}

func doRemove(s string) {
	if isProtected(s) {
		showErr("operação negada: '" + s + "' é protegido.")
		return
	}
	active := filepath.Join(ActiveDir, s)
	if _, e := os.Lstat(active); e == nil {
		exec.Command("sv", "stop", s).Run()
		os.Remove(active)
		msg("serviço '" + s + "' removido")
		shLog("REMOVE", s)
	} else {
		warn("serviço '" + s + "' não está habilitado")
	}
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
	if (cmd == "start" || cmd == "st" || cmd == "up" || cmd == "restart") {
		if !checkServiceHealth(s) { return }
		if _, e := os.Lstat(filepath.Join(ActiveDir, s)); os.IsNotExist(e) {
			showErr("serviço '" + s + "' não está habilitado. Use: vservice enable " + s)
			return
		}
	}
	info("sv " + cmd + " " + s)
	exec.Command("sv", cmd, s).Run()
	shLog("COMMAND", cmd + " em " + s)
	if strings.Contains("start st up restart", cmd) {
		waitForService(s)
	}
	showStatus(s)
}

func main() {
	if len(os.Args) < 2 { usage() }

	if os.Args[1] == "--install-completion" {
		checkRoot()
		installCompletion()
		os.Exit(0)
	}

	if os.Args[1] == "list" {
		checkRoot()
		if _, e := exec.LookPath("vsv"); e == nil {
			cmd := exec.Command("vsv")
			cmd.Stdout = os.Stdout
			cmd.Run()
		} else {
			exec.Command("sv", "status", ActiveDir+"/*").Run()
		}
		os.Exit(0)
	}

	checkRoot()
	action := os.Args[1]

	// Se não forneceu serviço, abre o menu interativo
	if len(os.Args) < 3 {
		s := fzfSelect()
		if s == "" {
			os.Exit(0)
		}
		// Executa a ação para o serviço selecionado
		switch action {
		case "add", "enable", "install": doEnable(s)
		case "disable": doSvCommand("stop", s)
		case "remove", "rm": doRemove(s)
		default: doSvCommand(action, s)
		}
	} else {
		// Modo normal (argumentos passados)
		for _, s := range os.Args[2:] {
			switch action {
			case "add", "enable", "install": doEnable(s)
			case "disable": doSvCommand("stop", s)
			case "remove", "rm": doRemove(s)
			default: doSvCommand(action, s)
			}
		}
	}
}
