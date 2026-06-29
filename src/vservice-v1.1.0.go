/*
    vservice
    Gerenciador de serviços (runit)

    Site:      https://chililinux.com
    GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

    Created:   ter 03 fev 2026 13:08:22 -04
    Updated:   qua 24 jun 2026 16:40:00 -04
    Version:   1.1.0
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
	DryRun            bool
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
	fmt.Printf("%sUso:%s vservice {comando} [serviço...]\n\n", Blue, Reset)
	fmt.Printf("%sComandos:%s\n", Blue, Reset)
	fmt.Printf("  %s%-18s%s - habilita serviço\n", Yellow, "enable, add", Reset)
	fmt.Printf("  %s%-18s%s - desabilita serviço\n", Yellow, "disable", Reset)
	fmt.Printf("  %s%-18s%s - remove serviço\n", Yellow, "remove, rm", Reset)
	fmt.Printf("  %s%-18s%s - inicia serviço\n", Yellow, "start, st, up", Reset)
	fmt.Printf("  %s%-18s%s - para serviço\n", Yellow, "stop, down", Reset)
	fmt.Printf("  %s%-18s%s - reinicia serviço\n", Yellow, "restart", Reset)
	fmt.Printf("  %s%-18s%s - mostra status\n", Yellow, "status", Reset)
	fmt.Printf("  %s%-18s%s - lista serviços\n", Yellow, "list", Reset)
	fmt.Printf("  %s%-18s%s - rotaciona logs\n", Yellow, "archive-logs", Reset)
	fmt.Printf("  %s%-18s%s - checa serviços\n", Yellow, "monitor", Reset)
	fmt.Printf("\n%sOpções:%s\n", Blue, Reset)
	fmt.Printf("  %s%-18s%s - simula execução\n", Yellow, "--dry-run", Reset)
	fmt.Printf("  %s%-18s%s - instala autocompletar\n", Yellow, "--install-completion", Reset)
	os.Exit(0)
}

func monitor() {
	info("Monitorando serviços em " + ActiveDir + "...")
	services, _ := os.ReadDir(ActiveDir)
	for _, s := range services {
		if s.Type()&os.ModeSymlink == 0 { continue }
		out, _ := exec.Command("sv", "status", s.Name()).Output()
		if !strings.HasPrefix(string(out), "run:") {
			showErr("Serviço " + s.Name() + " DOWN ou falhou!")
		} else {
			msg("Serviço " + s.Name() + " (OK)")
		}
	}
}

func archiveLogs() {
	archive := fmt.Sprintf("%s.%s.gz", LogFile, time.Now().Format("20060102-150405"))
	
	if DryRun {
		info("[DRY-RUN] Executaria: gzip -c " + LogFile + " > " + archive)
		info("[DRY-RUN] Executaria: truncate -s 0 " + LogFile)
		return
	}

	info("Arquivando log em " + archive)
	cmd := fmt.Sprintf("gzip -c %s > %s && truncate -s 0 %s", LogFile, archive, LogFile)
	exec.Command("sh", "-c", cmd).Run()
	msg("Logs processados.")
}

func fzfSelect(action string) string {
	files, _ := filepath.Glob(filepath.Join(SvDir, "*"))
	var filtered []string
	for _, f := range files {
		name := filepath.Base(f)
		_, err := os.Lstat(filepath.Join(ActiveDir, name))
		isActive := err == nil
		if (action == "add" && !isActive) || (action == "remove" && isActive) || action == "other" {
			filtered = append(filtered, name)
		}
	}
	if len(filtered) == 0 { return "" }
	cmd := exec.Command("fzf", "--prompt=Selecione: ", "--height=40%", "--reverse")
	cmd.Stdin = strings.NewReader(strings.Join(filtered, "\n"))
	out, err := cmd.Output()
	if err != nil { return "" }
	return strings.TrimSpace(string(out))
}

func installCompletion() {
	path := "/usr/share/bash-completion/completions/vservice"
	content := `_vservice_completions() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local opts="enable add disable remove rm start st up stop down restart status list archive-logs monitor --dry-run --install-completion"
    if [ ${COMP_CWORD} -eq 1 ]; then
        COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
    else
        COMPREPLY=( $(compgen -W "$(ls /etc/sv)" -- ${cur}) )
    fi
}
complete -F _vservice_completions vservice`
	if !DryRun {
		err := os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			showErr("Erro ao instalar autocompletar: " + err.Error())
			return
		}
	}
	msg("Autocompletar instalado em " + path)
}

func shLog(t, m string) {
	if DryRun { return }
	u, _ := user.Current()
	f, _ := os.OpenFile(LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		fmt.Fprintf(f, "[%s] [%s] %s (user: %s)\n", time.Now().Format("2006-01-02 15:04:05"), t, m, u.Username)
	}
}

func checkRoot() {
	u, _ := user.Current()
	if u.Uid != "0" {
		cmd := exec.Command("sudo", append([]string{os.Args[0]}, os.Args[1:]...)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		cmd.Run()
		os.Exit(0)
	}
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
			// Dividimos a linha para pegar a primeira coluna, que é o nome do serviço
			fields := strings.Fields(line)
			if len(fields) > 0 {
				// Remove o ícone se houver (o vsv costuma colocar um checkmark ou X no início)
				// Vamos pegar o campo que contém o nome. Geralmente é o segundo campo se houver ícone,
				// ou o primeiro se for uma linha pura.
				serviceName := fields[0]
				// Se tiver ícone (✔ ou ✘), pegamos o segundo campo
				if serviceName == "✔" || serviceName == "✘" && len(fields) > 1 {
					serviceName = fields[1]
				}

				if serviceName == s {
					fmt.Println(line)
				}
			}
		}
	} else {
		exec.Command("sv", "status", s).Run()
	}
}

func doAdd(s string) {
  if !checkServiceHealth(s) { return }
	active := filepath.Join(ActiveDir, s)
	if _, e := os.Lstat(active); e == nil {
		warn("serviço já habilitado")
		return
	}

	if DryRun {
		info("[DRY-RUN] Executaria: ln -s " + filepath.Join(SvDir, s) + " " + active)
		info("[DRY-RUN] Registraria log: ENABLE " + s)
		info("[DRY-RUN] Executaria: aguardar status de " + s)
		return
	}

	info("Adicionando " + s)
	os.Symlink(filepath.Join(SvDir, s), active)
	shLog("ENABLE", s)
	waitForService(s)
	showStatus(s)
}

func doRemove(s string) {
	for _, p := range ProtectedServices {
		if s == p { showErr("Protegido: " + s); return }
	}
	
	active := filepath.Join(ActiveDir, s)
	
	// 1. Verifica se existe
	if _, err := os.Lstat(active); os.IsNotExist(err) {
		showErr("Serviço '" + s + "' não encontrado em " + ActiveDir)
		return
	}

	// 2. Prévia de simulação
	if DryRun {
		info("[DRY-RUN] Executaria: sv stop " + s)
		info("[DRY-RUN] Executaria: rm " + active)
		return
	}

	// 3. Execução real
	info("Removendo " + s)
	exec.Command("sv", "stop", s).Run()
	os.Remove(active)
	shLog("REMOVE", s)
	
	// 4. Verificação pós-remoção
	if _, err := os.Lstat(active); os.IsNotExist(err) {
		msg("Serviço '" + s + "' removido com sucesso")
	} else {
		showErr("Falha ao remover '" + s + "'")
	}
}

func doSvCommand(cmd, s string) {
	if DryRun {
		info("[DRY-RUN] Executaria: sv " + cmd + " " + s)
		if strings.Contains("start st up restart", cmd) {
			info("[DRY-RUN] Executaria: aguardar status de " + s)
		}
		return
	}

	info("sv " + cmd + " " + s)
	exec.Command("sv", cmd, s).Run()
	shLog("CMD", cmd+" "+s)
	if strings.Contains("start st up restart", cmd) {
		waitForService(s)
	}
	showStatus(s)
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

func main() {
	// 1. Identifica e remove a flag globalmente
	for i, arg := range os.Args {
		if arg == "--dry-run" {
			DryRun = true
			os.Args = append(os.Args[:i], os.Args[i+1:]...)
		}
	}

	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" { usage() }
	action := os.Args[1]

	// 2. O 'list' segue a regra de root, mas agora respeita o DryRun
	if action == "list" {
		if DryRun {
			info("[DRY-RUN] Executaria: vsv ou sv status /var/service/*")
		} else {
			checkRoot()
			if _, e := exec.LookPath("vsv"); e == nil {
				cmd := exec.Command("vsv")
				cmd.Stdout = os.Stdout
				cmd.Run()
			} else {
				exec.Command("sh", "-c", "sv status "+ActiveDir+"/*").Stdout = os.Stdout
				exec.Command("sh", "-c", "sv status "+ActiveDir+"/*").Run()
			}
		}
		os.Exit(0)
	}

	// 3. Só checa root se não for modo simulação
	if !DryRun {
		checkRoot()
	}

	// 4. Fluxo limpo para as funções que já verificam o DryRun internamente
	switch action {
	case "monitor": monitor()
	case "archive-logs": archiveLogs()
	case "--install-completion": installCompletion()
	case "add", "enable":
		if len(os.Args) > 2 {
			for _, s := range os.Args[2:] { doAdd(s) }
		} else {
			s := fzfSelect("add")
			if s != "" { doAdd(s) }
		}
	case "remove", "rm":
		if len(os.Args) > 2 {
			for _, s := range os.Args[2:] { doRemove(s) }
		} else {
			s := fzfSelect("remove")
			if s != "" { doRemove(s) }
		}
	default:
		for _, s := range os.Args[2:] { doSvCommand(action, s) }
	}
}
