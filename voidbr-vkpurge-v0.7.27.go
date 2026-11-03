/*
   voidbr-vkpurge
   Remove kernels órfãos do Void Linux com segurança.
   Site:      https://chililinux.com
   GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall
   Created:   sáb 09 mai 2026 10:57:53 -04
   Updated:   ter 03 nov 2026 01:00:00 -04
   Version:   0.7.27
   Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>

   Changelog:
     0.7.27 - ter 03 nov 2026
       - Confirmação [s/N] antes de qualquer remoção (`rm all`,
         `rm <versão>`, `--cleanup`, `--keep`), com resumo das versões,
         caminhos e tamanho. Novo -y/--yes para confirmar sem perguntar;
         sem terminal e sem -y, recusa.
       - Kernel de reserva: recusa desinstalar pacotes se o sistema
         ficar só com o kernel em execução.
       - Snapshot do snapper (config root) antes da primeira remoção,
         se o snapper estiver configurado; se falhar, aborta. Novo
         --no-snapshot para pular.
       - Novo `--keep N`: mantém os N kernels instalados mais novos
         (e sempre o em execução) e desinstala os outros, com headers,
         numa pergunta só, limpando as sobras depois.
       - Novo `status`: tabela com todos os kernels (estado, pacote,
         headers, tamanho) e total ocupado por órfãos.
       - Relatório do espaço liberado no fim de cada remoção.
       - DKMS: não foi preciso implementar nada; o hook
         /etc/kernel.d/pre-remove/20-dkms do pacote dkms já roda
         `dkms remove` para a versão, e o vkpurge já executa esse hook.

     0.7.26 - ter 03 nov 2026
       - `rm <versão>` de um kernel que ainda pertence a pacote
         instalado não recusa mais direto: informa o pacote dono e
         pergunta "Deseja desinstalar o pacote X? [s/N]". Se sim, roda
         `vinstall -Xy X`, confirma no xbps que saiu e limpa as sobras
         da versão (módulos DKMS etc.) e atualiza o bootloader.
       - Se o pacote de headers do kernel (<pacote>-headers, ex.:
         linux7.2-headers) estiver instalado, é desinstalado junto, na
         mesma pergunta e no mesmo `vinstall -Xy`.
       - Nunca desinstala o pacote do kernel em execução: além da
         recusa pela versão (uname -r), recusa também se o pacote for o
         mesmo que fornece o kernel ativo.
       - Sem terminal (script/pipe) não pergunta e recusa como antes;
         em --dry-run só mostra o que seria perguntado e executado.
       - isTerminal passa a usar ioctl(TCGETS) (como isatty); antes
         considerava /dev/null um terminal por ser dispositivo de
         caractere.

     0.7.25 - ter 03 nov 2026
       - `rm <versão>` só aceita entrada de /usr/lib/modules se for um
         diretório; antes qualquer arquivo ali era tratado como kernel
         órfão e apagado, com hooks rodando com a "versão" falsa.
       - `list`, `rm all` e `--cleanup` agora usam a mesma varredura
         de órfãos (vmlinuz, vmlinux, initramfs, config e diretórios
         de módulos). Antes `list` e `rm all` só olhavam vmlinu[xz] e
         não viam sobras como /usr/lib/modules/<versão> (DKMS etc.)
         que o `--cleanup` removia.
       - Versões encontradas no disco com nome inválido passam a ser
         ignoradas na varredura, como já era feito na linha de comando.
       - Versão extraída do nome do arquivo pelo prefixo (vmlinuz-,
         initramfs- etc.) e não mais pelo último "-", que quebrava
         kernels com hífen na versão (ex.: 6.6.32-voidbr).
       - "Arquivos órfãos detectados" no --cleanup mostra os caminhos
         completos que serão removidos (antes saía vazio quando só
         sobrava /usr/lib/modules/<versão>, e o glob /boot/*<versão>*
         podia listar arquivos de outra versão, ex.: 6.6.3_1 e 6.6.3_10).

     0.7.24 - sex 24 jul 2026
       - Tag [DRY-RUN] agora sempre em amarelo, em qualquer mensagem
         (hooks, remoção de arquivos, atualização de bootloader).
       - Linha em branco separando o processamento de cada kernel em
         --cleanup e rm all, para facilitar a leitura da saída.

     0.7.23 - sex 24 jul 2026
       - Reescrita completa do voidbr-vkpurge (bash) para Go, mantendo
         toda a lógica e o comportamento originais.
       - Corrigidos bugs de segurança: --dry-run que não bloqueava
         mensagens afirmando ações que não ocorreram; comando `rm` sem
         lógica de remoção real; possível path traversal via nome de
         versão de kernel; falha do xbps-query tratada como "nenhum
         kernel instalado"; ausência de checagem de /boot montado;
         filtro de kernels instalados aplicado de forma inconsistente
         entre `list` e `rm`.
*/
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

const (
	appName = "voidbr-vkpurge"
	version = "0.7.27"
)

// =========================================================
// CORES / TERMINAL
// =========================================================

var (
	colorReset   string
	colorBold    string
	colorRed     string
	colorGreen   string
	colorYellow  string
	colorCyan    string
	colorWhite   string
	colorMagenta string
)

// isTerminal diz se f é um terminal de verdade (equivalente a isatty):
// pede os atributos do terminal com ioctl(TCGETS). Só checar se é
// dispositivo de caractere não basta, porque /dev/null também é.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

func setupColors() {
	if !isTerminal(os.Stdout) {
		return
	}
	colorReset = "\033[0m"
	colorBold = "\033[1m"
	colorRed = "\033[1;38;5;196m"
	colorGreen = "\033[1;38;5;2m"
	colorYellow = "\033[1;38;5;3m"
	colorCyan = "\033[38;5;6m"
	colorWhite = "\033[38;5;7m"
	colorMagenta = "\033[38;5;5m"
}

// =========================================================
// LOG
// =========================================================

func msg(format string, args ...interface{}) {
	fmt.Printf("%s==>%s %s%s%s\n", colorCyan, colorReset, colorWhite, fmt.Sprintf(format, args...), colorReset)
}

func ok(format string, args ...interface{}) {
	fmt.Printf("%s==>%s %s\n", colorGreen, colorReset, fmt.Sprintf(format, args...))
}

func warn(format string, args ...interface{}) {
	fmt.Printf("%s==>%s %s\n", colorYellow, colorReset, fmt.Sprintf(format, args...))
}

func errMsg(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "%s==>%s %s\n", colorRed, colorReset, fmt.Sprintf(format, args...))
}

func fun(format string, args ...interface{}) {
	fmt.Printf("%s==>%s %s\n", colorMagenta, colorReset, fmt.Sprintf(format, args...))
}

func die(format string, args ...interface{}) {
	errMsg(format, args...)
	os.Exit(1)
}

// drTag retorna a marca "[DRY-RUN]" sempre em amarelo, independente da
// cor da função de log usada em volta (msg, warn, ok etc.), para que a
// simulação fique visualmente óbvia em qualquer mensagem.
func drTag() string {
	return colorYellow + "[DRY-RUN]" + colorReset
}

// =========================================================
// USO
// =========================================================

func usage() {
	fmt.Printf("%s%s v%s%s\n", colorCyan, appName, version, colorReset)
	fmt.Printf("%sLimpa kernels órfãos do Void Linux com segurança%s\n\n", colorWhite, colorReset)
	fmt.Printf("%sUso:%s\n", colorYellow, colorReset)
	fmt.Printf("  %s%s%s %sstatus%s               # Mostra todos os kernels: estado, pacote, headers e tamanho\n", colorGreen, appName, colorReset, colorCyan, colorReset)
	fmt.Printf("  %s%s%s %slist%s [versão]        # Lista kernels órfãos (filtra por versão se fornecido)\n", colorGreen, appName, colorReset, colorCyan, colorReset)
	fmt.Printf("  %s%s%s %srm%s all               # Remove todos os kernels órfãos detectados\n", colorGreen, appName, colorReset, colorRed, colorReset)
	fmt.Printf("  %s%s%s %srm%s <versão>          # Remove uma versão (se instalada, oferece desinstalar o pacote e os headers)\n", colorGreen, appName, colorReset, colorRed, colorReset)
	fmt.Printf("  %s%s%s %s--keep%s N             # Mantém os N kernels instalados mais novos e desinstala os outros\n", colorGreen, appName, colorReset, colorRed, colorReset)
	fmt.Printf("  %s%s%s %s--cleanup%s            # Varredura e remoção automática de órfãos\n", colorGreen, appName, colorReset, colorMagenta, colorReset)
	fmt.Printf("  %s%s%s %s--force%s              # Força a regeneração do bootloader\n", colorGreen, appName, colorReset, colorMagenta, colorReset)
	fmt.Printf("  %s%s%s %s--dry-run%s            # Simula as ações sem alterar o sistema\n", colorGreen, appName, colorReset, colorMagenta, colorReset)
	fmt.Printf("  %s%s%s %s-y%s, %s--yes%s            # Confirma tudo sem perguntar (para scripts)\n", colorGreen, appName, colorReset, colorMagenta, colorReset, colorMagenta, colorReset)
	fmt.Printf("  %s%s%s %s--no-snapshot%s        # Não cria snapshot do snapper antes de remover\n\n", colorGreen, appName, colorReset, colorMagenta, colorReset)
	fmt.Printf("%sSegurança:%s\n", colorYellow, colorReset)
	fmt.Printf("  - O kernel em execução e o pacote dele nunca são removidos.\n")
	fmt.Printf("  - Nunca deixa o sistema só com o kernel em execução: precisa sobrar um de reserva.\n")
	fmt.Printf("  - Toda remoção mostra o que vai sair e pede confirmação [s/N] (ou -y).\n")
	fmt.Printf("  - Com snapper (config root), cria um snapshot antes de remover.\n\n")
	fmt.Printf("%sExemplos:%s\n", colorYellow, colorReset)
	fmt.Printf("  %s status\n", appName)
	fmt.Printf("  %s list\n", appName)
	fmt.Printf("  %s rm all\n", appName)
	fmt.Printf("  %s rm 6.6.32_1\n", appName)
	fmt.Printf("  %s --keep 2\n", appName)
	fmt.Printf("  %s --keep 2 --dry-run\n", appName)
	fmt.Printf("  %s --cleanup\n", appName)
	fmt.Printf("  %s --cleanup -y\n", appName)
	fmt.Printf("  %s --cleanup --force\n", appName)
	fmt.Printf("  %s --cleanup --dry-run\n", appName)
	os.Exit(1)
}

// =========================================================
// ROOT
// =========================================================

func requireRoot() {
	if os.Geteuid() != 0 {
		errMsg("Execute como root.")
		os.Exit(1)
	}
}

// elevateToRoot tenta reexecutar o programa como root via sudo, e em
// seguida via su, replicando o comportamento do script original.
func elevateToRoot(args []string) {
	msg("Este script precisa ser executado como root. Elevando privilégios...")

	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}

	if sudoPath, err := exec.LookPath("sudo"); err == nil {
		argv := append([]string{"sudo", exe}, args...)
		_ = syscall.Exec(sudoPath, argv, os.Environ())
		// se chegou aqui, exec falhou; tenta su a seguir
	}

	if suPath, err := exec.LookPath("su"); err == nil {
		full := exe
		for _, a := range args {
			full += " " + shellQuote(a)
		}
		argv := []string{"su", "-c", full}
		_ = syscall.Exec(suPath, argv, os.Environ())
	}

	die("Não foi possível elevar privilégios. Execute manualmente como root.")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// =========================================================
// /BOOT
// =========================================================

func checkBoot() error {
	running := runningKernelVersion()
	if running == "" {
		// não foi possível determinar o kernel em execução; não bloqueia
		return nil
	}
	_, errZ := os.Stat("/boot/vmlinuz-" + running)
	_, errX := os.Stat("/boot/vmlinux-" + running)
	if os.IsNotExist(errZ) && os.IsNotExist(errX) {
		return fmt.Errorf("/boot parece não estar montado")
	}
	return nil
}

// =========================================================
// HOOKS
// =========================================================

func runHooks(hookdir, kver string, dryRun bool) {
	dir := filepath.Join("/etc/kernel.d", hookdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		// diretório de hooks não existe: comportamento normal, ignora
		return
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 == 0 {
			// não executável
			continue
		}

		if dryRun {
			warn("%s Hook seria executado: %s", drTag(), name)
			continue
		}

		msg("Executando hook: %s", name)
		cmd := exec.Command(path, "kernel", kver)
		cmd.Dir = "/"
		cmd.Env = append(os.Environ(), "ROOTDIR=.")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			warn("Hook falhou (%s): %v", name, err)
		}
	}
}

// =========================================================
// KERNEL: consultas
// =========================================================

func runningKernelVersion() string {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// installedKernelVersions retorna o conjunto de versões de kernel que
// pertencem a pacotes atualmente instalados, segundo o xbps-query.
// Um erro aqui significa que NÃO é seguro assumir nada sobre quais
// kernels pertencem a pacotes instalados; o chamador deve tratar isso
// como "não sei" e não como "nenhum kernel está instalado" (ver BUG 12
// no rodapé do arquivo).
func installedKernelVersions() (map[string]bool, error) {
	pkgs, err := installedKernelPackages()
	result := map[string]bool{}
	for kver := range pkgs {
		result[kver] = true
	}
	return result, err
}

// installedKernelPackages retorna, para cada versão de kernel que
// pertence a um pacote instalado, o pkgver dono dela (ex.:
// "7.2.15_1" -> "linux7.2-7.2.15_1"), a partir das linhas do
// `xbps-query -o`, no formato:
//
//	linux7.2-7.2.15_1: /boot/vmlinuz-7.2.15_1 (regular file)
func installedKernelPackages() (map[string]string, error) {
	result := map[string]string{}

	if _, err := exec.LookPath("xbps-query"); err != nil {
		return result, fmt.Errorf("xbps-query não encontrado no PATH")
	}

	out, err := exec.Command("xbps-query", "-o", "/boot/vmlinu[xz]-*").CombinedOutput()
	if err != nil {
		// xbps-query pode sair com código != 0 quando nada casa com o
		// padrão informado; nesse caso a saída normalmente vem vazia, o
		// que não é uma falha real, apenas "nenhum arquivo encontrado".
		if len(strings.TrimSpace(string(out))) == 0 {
			return result, nil
		}
		return result, fmt.Errorf("xbps-query falhou: %w", err)
	}

	re := regexp.MustCompile(`^(\S+):\s+\S*/vmlinu[xz]-(\S+)`)
	for _, line := range strings.Split(string(out), "\n") {
		if m := re.FindStringSubmatch(strings.TrimSpace(line)); len(m) == 3 {
			result[m[2]] = m[1]
		}
	}
	return result, nil
}

// pkgNameFromPkgver tira a versão do pkgver ("linux7.2-7.2.15_1" ->
// "linux7.2") usando o parser oficial do xbps (`xbps-uhelper
// getpkgname`). Um pkgver que o xbps não reconhece vira erro, nunca um
// nome "chutado".
func pkgNameFromPkgver(pkgver string) (string, error) {
	out, err := exec.Command("xbps-uhelper", "getpkgname", pkgver).Output()
	name := strings.TrimSpace(string(out))
	if err != nil || name == "" {
		return "", fmt.Errorf("não foi possível obter o nome do pacote de %q via xbps-uhelper", pkgver)
	}
	return name, nil
}

// installedPkgver devolve o pkgver de um pacote instalado
// (`xbps-query -p pkgver <nome>`), ou "" se ele não estiver instalado.
// A resposta só é aceita se o xbps-uhelper confirmar que é do pacote
// pedido; qualquer saída inesperada conta como "instalado", para que
// a limpeza das sobras nunca apague arquivos de um pacote na dúvida.
func installedPkgver(name string) string {
	out, err := exec.Command("xbps-query", "-p", "pkgver", name).Output()
	pkgver := strings.TrimSpace(string(out))
	if err != nil && pkgver == "" {
		return ""
	}
	if got, err := pkgNameFromPkgver(pkgver); err != nil || got != name {
		return name + " (resposta inesperada do xbps-query)"
	}
	return pkgver
}

// askYesNo faz uma pergunta [s/N]. Só "s", "sim", "y" ou "yes" (sem
// diferenciar maiúsculas) contam como sim; Enter ou qualquer outra
// coisa é não.
func askYesNo(question string) bool {
	fmt.Printf("%s==>%s %s %s[s/N]%s ", colorYellow, colorReset, question, colorBold, colorReset)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "sim", "y", "yes":
		return true
	}
	return false
}

// =========================================================
// OPÇÕES GLOBAIS
// =========================================================

// runOpts reúne as opções de linha de comando que mudam o
// comportamento das operações destrutivas.
type runOpts struct {
	dryRun     bool // --dry-run: só simula
	yes        bool // -y/--yes: responde sim a todas as confirmações
	noSnapshot bool // --no-snapshot: não cria snapshot antes de remover
}

// =========================================================
// CONFIRMAÇÃO
// =========================================================

// confirm pede confirmação [s/N] antes de uma ação destrutiva.
//   - --dry-run: só mostra a pergunta e segue (nada será feito);
//   - -y/--yes: responde sim sem perguntar;
//   - sem terminal e sem -y: encerra, para nunca travar nem assumir sim.
func confirm(question string, o runOpts) bool {
	if o.dryRun {
		warn("%s Perguntaria: %s", drTag(), question)
		return true
	}
	if o.yes {
		msg("%s %s(-y)%s sim", question, colorBold, colorReset)
		return true
	}
	if !isTerminal(os.Stdin) {
		die("Sem terminal para confirmar \"%s\". Use -y para confirmar sem perguntar.", question)
	}
	return askYesNo(question)
}

// =========================================================
// TAMANHO EM DISCO
// =========================================================

// pathSize soma o tamanho de um arquivo ou de tudo dentro de um
// diretório, sem seguir symlinks.
func pathSize(p string) int64 {
	var total int64
	_ = filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// kernelSize soma o tamanho de todos os caminhos de uma versão de
// kernel que existem no disco (ver kernelPaths).
func kernelSize(kver string) int64 {
	var total int64
	for _, p := range existingKernelPaths(kver) {
		total += pathSize(p)
	}
	return total
}

// pad completa s com espaços até n caracteres (não bytes: o %-Ns do
// fmt conta bytes e desalinha palavras com acento, como "execução").
func pad(s string, n int) string {
	if c := utf8.RuneCountInString(s); c < n {
		return s + strings.Repeat(" ", n-c)
	}
	return s
}

// humanSize formata bytes em KiB/MiB/GiB, com vírgula decimal.
func humanSize(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return strings.Replace(fmt.Sprintf("%.1f %s", f, units[i]), ".", ",", 1)
}

// =========================================================
// SNAPSHOT (snapper)
// =========================================================

var snapshotDone bool

// takeSnapshot cria um snapshot do snapper (config "root") antes da
// primeira ação destrutiva da execução. Sem snapper ou sem a config
// root, não faz nada. Se o snapper existe mas a criação falha, aborta:
// quem configurou snapshots espera ter um antes de remover kernels.
func takeSnapshot(desc string, o runOpts) {
	if snapshotDone || o.noSnapshot {
		return
	}
	snapshotDone = true

	snapper, err := exec.LookPath("snapper")
	if err != nil {
		return
	}
	if _, err := os.Stat("/etc/snapper/configs/root"); err != nil {
		return
	}

	if o.dryRun {
		warn("%s Criaria snapshot do snapper: %s", drTag(), desc)
		return
	}

	msg("Criando snapshot do snapper: %s", desc)
	out, err := exec.Command(snapper, "-c", "root", "create", "-d", desc, "-c", "number", "-p").CombinedOutput()
	if err != nil {
		die("Falha ao criar o snapshot (%v): %s — nada foi removido. Use --no-snapshot para continuar sem snapshot.",
			err, strings.TrimSpace(string(out)))
	}
	ok("Snapshot #%s criado.", strings.TrimSpace(string(out)))
}

// =========================================================
// PACOTES DE KERNEL
// =========================================================

// installedKernelPkgnames devolve os nomes dos pacotes de kernel
// instalados (ex.: linux7.2, linux7.3), a partir do mapa versão->pkgver.
func installedKernelPkgnames(pkgs map[string]string) (map[string]bool, error) {
	names := map[string]bool{}
	for _, pkgver := range pkgs {
		name, err := pkgNameFromPkgver(pkgver)
		if err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, nil
}

// uninstallKernelPackages desinstala, com `vinstall -Xy`, os pacotes
// donos das versões informadas e os respectivos <pacote>-headers
// instalados, numa pergunta só. Antes disso:
//   - recusa se algum pacote for o do kernel em execução (camada 2);
//   - recusa se, depois da remoção, não sobrar nenhum kernel instalado
//     além do que está rodando (kernel de reserva);
//   - cria o snapshot (se houver snapper).
//
// Só retorna se os pacotes foram removidos (ou, em --dry-run, se
// seriam); em qualquer outro caso encerra o programa.
func uninstallKernelPackages(kvers []string, pkgs map[string]string, running string, o runOpts) {
	installedNames, err := installedKernelPkgnames(pkgs)
	if err != nil {
		die("%v — abortando por segurança, nada foi removido.", err)
	}

	runName := ""
	if runPkgver, found := pkgs[running]; found {
		if runName, err = pkgNameFromPkgver(runPkgver); err != nil {
			die("%v — não dá para identificar o pacote do kernel em execução; abortando.", err)
		}
	}

	kernelNames := []string{}
	seen := map[string]bool{}
	for _, kver := range kvers {
		pkgver := pkgs[kver]
		warn("'%s' pertence ao pacote instalado %s.", kver, pkgver)
		name, err := pkgNameFromPkgver(pkgver)
		if err != nil {
			die("%v — abortando por segurança, nada foi removido.", err)
		}
		if pkgver == pkgs[running] || name == runName {
			die("Recusando desinstalar %s: é o pacote do kernel em execução (%s).", name, running)
		}
		if !seen[name] {
			seen[name] = true
			kernelNames = append(kernelNames, name)
		}
	}

	// Kernel de reserva: além do kernel em execução, precisa sobrar
	// pelo menos um kernel instalado para escolher no boot.
	reserve := []string{}
	for name := range installedNames {
		if name != runName && !seen[name] {
			reserve = append(reserve, name)
		}
	}
	if len(reserve) == 0 {
		die("Recusando desinstalar %s: o sistema ficaria só com o kernel em execução (%s), sem kernel de reserva para o boot.",
			strings.Join(kernelNames, " e "), running)
	}

	// Headers: <pacote>-headers sai junto, se instalado.
	toUninstall := []string{}
	headers := []string{}
	for _, name := range kernelNames {
		toUninstall = append(toUninstall, name)
		h := name + "-headers"
		if hdrPkgver := installedPkgver(h); hdrPkgver != "" {
			warn("O pacote de headers %s também está instalado e será removido junto.", hdrPkgver)
			toUninstall = append(toUninstall, h)
			headers = append(headers, h)
		}
	}
	names := strings.Join(toUninstall, " ")

	question := fmt.Sprintf("Deseja desinstalar o pacote %s?", names)
	if len(toUninstall) > 1 {
		question = fmt.Sprintf("Deseja desinstalar os pacotes %s?", strings.Join(toUninstall, ", "))
	}
	if !confirm(question, o) {
		msg("Cancelado. Nada foi removido.")
		os.Exit(0)
	}

	takeSnapshot("voidbr-vkpurge: antes de desinstalar "+names, o)

	if o.dryRun {
		warn("%s Executaria: vinstall -Xy %s", drTag(), names)
		return
	}

	vinstall, err := exec.LookPath("vinstall")
	if err != nil {
		die("vinstall não encontrado no PATH. Desinstale com: xbps-remove %s", names)
	}

	msg("Executando: vinstall -Xy %s", names)
	cmd := exec.Command(vinstall, append([]string{"-Xy"}, toUninstall...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		die("Falha ao desinstalar %s (%v). Nada mais foi removido.", names, err)
	}

	// Confirma no xbps que nada ficou instalado antes de mexer nas
	// sobras: a limpeza apaga /boot, módulos e /usr/src/kernel-headers-*.
	after, err := installedKernelVersions()
	if err != nil {
		die("%v — abortando a limpeza das sobras por segurança.", err)
	}
	for _, kver := range kvers {
		if after[kver] {
			die("'%s' ainda aparece como instalado depois do vinstall; abortando a limpeza das sobras.", kver)
		}
	}
	for _, h := range headers {
		if installedPkgver(h) != "" {
			die("%s ainda aparece como instalado depois do vinstall; abortando a limpeza das sobras.", h)
		}
	}
	ok("Desinstalado: %s.", names)
}

// =========================================================
// REMOÇÃO DE KERNELS (sobras / órfãos)
// =========================================================

// measureKernels mede o espaço ocupado por cada versão, para o resumo
// antes da remoção e para o relatório do espaço liberado no fim.
func measureKernels(kvers []string) map[string]int64 {
	sizes := map[string]int64{}
	for _, k := range kvers {
		sizes[k] = kernelSize(k)
	}
	return sizes
}

// showSummary lista as versões que serão removidas, com tamanho e
// caminhos, e o total.
func showSummary(title string, kvers []string, sizes map[string]int64) {
	var total int64
	msg("%s", title)
	for _, k := range kvers {
		total += sizes[k]
		fmt.Printf("    %s%-20s%s %10s   %s\n", colorYellow, k, colorReset, humanSize(sizes[k]),
			strings.Join(existingKernelPaths(k), " "))
	}
	msg("Total: %s%s%s", colorBold, humanSize(total), colorReset)
}

// purgeKernels remove todas as sobras das versões informadas, atualiza
// o bootloader e mostra o espaço liberado. Se `confirmed` for falso,
// mostra o resumo e pede confirmação antes (e cria o snapshot);
// `before` traz os tamanhos medidos antes (nil = medir agora).
func purgeKernels(kvers []string, before map[string]int64, confirmed bool, o runOpts) {
	if before == nil {
		before = measureKernels(kvers)
	}

	if !confirmed {
		fmt.Println()
		showSummary("Kernels que serão removidos:", kvers, before)
		question := "Remover este kernel?"
		if len(kvers) > 1 {
			question = fmt.Sprintf("Remover estes %d kernels?", len(kvers))
		}
		if !confirm(question, o) {
			msg("Cancelado. Nada foi removido.")
			os.Exit(0)
		}
		takeSnapshot("voidbr-vkpurge: antes de remover "+strings.Join(kvers, " "), o)
	}

	for _, kver := range kvers {
		fmt.Println()
		removeKernel(kver, o.dryRun)
	}
	fmt.Println()
	finalizeSystem(o.dryRun)

	var freed int64
	for _, k := range kvers {
		if o.dryRun {
			freed += before[k]
		} else if d := before[k] - kernelSize(k); d > 0 {
			freed += d
		}
	}
	if o.dryRun {
		ok("%s Seriam liberados: %s%s%s", drTag(), colorBold, humanSize(freed), colorReset)
	} else {
		ok("Espaço liberado: %s%s%s", colorBold, humanSize(freed), colorReset)
	}
}

// validKernelVersionRe define o formato aceito para uma versão de kernel
// informada pelo usuário na linha de comando: apenas caracteres comuns
// em versões (letras, dígitos, ponto, underscore, mais e hífen). Isso
// bloqueia barras, espaços e sequências como ".." que poderiam ser
// usadas para escapar do diretório /boot via concatenação de caminho
// (ver BUG 11 no rodapé do arquivo).
var validKernelVersionRe = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

func isValidKernelVersion(kver string) bool {
	if kver == "" {
		return false
	}
	if strings.Contains(kver, "..") {
		return false
	}
	return validKernelVersionRe.MatchString(kver)
}

// kernelVersionExists verifica se existe algum vestígio no disco da
// versão de kernel informada (imagem em /boot ou diretório de módulos).
// Em /usr/lib/modules só conta se for um diretório de verdade: um
// arquivo solto ou symlink ali não é um kernel (ver BUG 14 no rodapé).
func kernelVersionExists(kver string) bool {
	for _, c := range []string{"/boot/vmlinuz-" + kver, "/boot/vmlinux-" + kver} {
		if _, err := os.Lstat(c); err == nil {
			return true
		}
	}
	if info, err := os.Lstat("/usr/lib/modules/" + kver); err == nil && info.IsDir() {
		return true
	}
	return false
}

// splitVersionChunks quebra uma string em pedaços numéricos e
// não-numéricos, para permitir comparação "natural" (equivalente a
// `sort -V` do coreutils).
func splitVersionChunks(s string) []string {
	re := regexp.MustCompile(`\d+|\D+`)
	return re.FindAllString(s, -1)
}

func versionLess(a, b string) bool {
	ac := splitVersionChunks(a)
	bc := splitVersionChunks(b)
	n := len(ac)
	if len(bc) < n {
		n = len(bc)
	}
	for i := 0; i < n; i++ {
		if ac[i] == bc[i] {
			continue
		}
		an, aerr := strconv.Atoi(ac[i])
		bn, berr := strconv.Atoi(bc[i])
		if aerr == nil && berr == nil {
			return an < bn
		}
		return ac[i] < bc[i]
	}
	return len(ac) < len(bc)
}

func sortVersions(v []string) {
	sort.Slice(v, func(i, j int) bool { return versionLess(v[i], v[j]) })
}

// listKernels retorna a lista (ordenada, sem duplicatas) de kernels
// órfãos que casam com os padrões informados. Um padrão vazio ou "all"
// equivale a "*" (todos). O kernel em execução e kernels pertencentes a
// pacotes instalados nunca são retornados, independentemente do padrão
// usado (ver BUG 5 no rodapé do arquivo).
//
// Se não for possível determinar com segurança quais kernels pertencem
// a pacotes instalados, um erro é retornado junto com a lista parcial;
// chamadores que forem executar uma remoção devem abortar nesse caso
// (ver BUG 12).
func listKernels(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		patterns = []string{"all"}
	}

	orphans, err := findOrphanVersions()

	result := make([]string, 0, len(orphans))
	for _, kver := range orphans {
		for _, arg := range patterns {
			pattern := arg
			if pattern == "" || pattern == "all" {
				pattern = "*"
			}
			if match, _ := filepath.Match(pattern, kver); match {
				result = append(result, kver)
				break
			}
		}
	}
	return result, err
}

// diskKernelVersions varre o disco e devolve todas as versões de kernel
// que deixaram algum vestígio: vmlinuz, vmlinux, initramfs e config em
// /boot, e diretórios em /usr/lib/modules. Nomes que não passam em
// isValidKernelVersion são ignorados.
func diskKernelVersions() map[string]bool {
	versions := map[string]bool{}

	// A versão é tudo que vem depois do prefixo, e não só o que vem
	// depois do último "-": versões como "6.6.32-voidbr" têm hífen
	// (ver BUG 16 no rodapé).
	prefixes := []string{"vmlinuz-", "vmlinux-", "initramfs-", "config-"}
	for _, prefix := range prefixes {
		files, _ := filepath.Glob("/boot/" + prefix + "*")
		for _, f := range files {
			kver := strings.TrimPrefix(filepath.Base(f), prefix)
			if prefix == "initramfs-" {
				kver = strings.TrimSuffix(kver, ".img")
			}
			if isValidKernelVersion(kver) {
				versions[kver] = true
			}
		}
	}

	if entries, err := os.ReadDir("/usr/lib/modules"); err == nil {
		for _, e := range entries {
			if e.IsDir() && isValidKernelVersion(e.Name()) {
				versions[e.Name()] = true
			}
		}
	}

	return versions
}

// findOrphanVersions é a única fonte de "kernels órfãos" do programa,
// usada por `list`, `rm all` e `--cleanup` (ver BUG 15 no rodapé).
// Devolve, ordenadas, as versões encontradas no disco que não são o
// kernel em execução nem pertencem a pacote instalado. Se não der para
// consultar o xbps, devolve a lista sem esse filtro junto com o erro;
// quem for remover algo deve abortar nesse caso (ver BUG 12).
func findOrphanVersions() ([]string, error) {
	running := runningKernelVersion()
	installed, installedErr := installedKernelVersions()

	result := []string{}
	for kver := range diskKernelVersions() {
		if kver == running || installed[kver] {
			continue
		}
		result = append(result, kver)
	}
	sortVersions(result)
	return result, installedErr
}

// =========================================================
// REMOÇÃO DE ARQUIVOS
// =========================================================

var allowedPrefixes = []string{
	"/boot/",
	"/usr/lib/modules/",
	"/usr/src/",
	"/usr/lib/debug/",
}

func removePath(target string, dryRun bool) error {
	// Normaliza o caminho (resolve "..", "//" etc.) ANTES de checar o
	// prefixo seguro. Checar o prefixo sobre o caminho não-normalizado
	// permitiria, em tese, que uma versão de kernel maliciosa como
	// "../../etc/passwd" produzisse "/boot/vmlinuz-../../etc/passwd" —
	// que passa no teste de prefixo textual mas resolve para fora de
	// /boot (ver BUG 11 no rodapé do arquivo).
	clean := filepath.Clean(target)

	if _, err := os.Lstat(clean); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	safe := false
	for _, p := range allowedPrefixes {
		if strings.HasPrefix(clean, p) {
			safe = true
			break
		}
	}
	if !safe {
		return fmt.Errorf("recusando remover caminho inseguro: %s", clean)
	}

	if dryRun {
		warn("%s Removeria: %s", drTag(), clean)
		return nil
	}

	warn("Removendo: %s", clean)
	return os.RemoveAll(clean)
}

// kernelPaths lista todos os caminhos que pertencem a uma versão de
// kernel. É a mesma lista usada para remover e para mostrar os órfãos.
func kernelPaths(kver string) []string {
	return []string{
		fmt.Sprintf("/boot/config-%s", kver),
		fmt.Sprintf("/boot/System.map-%s", kver),
		fmt.Sprintf("/boot/vmlinuz-%s", kver),
		fmt.Sprintf("/boot/vmlinux-%s", kver),
		fmt.Sprintf("/boot/initramfs-%s.img", kver),
		fmt.Sprintf("/usr/lib/modules/%s", kver),
		fmt.Sprintf("/usr/src/kernel-headers-%s", kver),
		fmt.Sprintf("/usr/lib/debug/boot/vmlinuz-%s", kver),
		fmt.Sprintf("/usr/lib/debug/usr/lib/modules/%s", kver),
		fmt.Sprintf("/boot/dtbs/dtbs-%s", kver),
	}
}

// existingKernelPaths devolve só os caminhos de kernelPaths que existem.
func existingKernelPaths(kver string) []string {
	result := []string{}
	for _, p := range kernelPaths(kver) {
		if _, err := os.Lstat(p); err == nil {
			result = append(result, p)
		}
	}
	return result
}

func removeKernel(kver string, dryRun bool) {
	fun("Kernel %s será enviado para o além.", kver)
	runHooks("pre-remove", kver, dryRun)

	for _, p := range kernelPaths(kver) {
		if err := removePath(p, dryRun); err != nil {
			errMsg("%v", err)
		}
	}
}

// =========================================================
// FINALIZAÇÃO
// =========================================================

func finalizeSystem(dryRun bool) {
	if dryRun {
		msg("%s Bootloader seria atualizado agora.", drTag())
	} else {
		msg("Atualizando bootloader...")
	}
	fun("Nada de rodar grub-mkconfig 37 vezes.")

	os.Setenv("KERNEL_ACTION", "bulk")
	runHooks("post-remove", "", dryRun)

	if dryRun {
		ok("%s Simulação concluída — nenhuma alteração foi feita no sistema.", drTag())
	} else {
		ok("Bootloader e initramfs atualizados.")
	}
}

// =========================================================
// CLEANUP / KEEP / STATUS
// =========================================================

// showStatus mostra todos os kernels (no disco e instalados): estado,
// pacote dono, headers e espaço ocupado. Só leitura; não exige root.
func showStatus() {
	running := runningKernelVersion()
	pkgs, err := installedKernelPackages()
	if err != nil {
		warn("Não foi possível consultar o xbps (%v); o estado \"instalado\" pode estar incompleto.", err)
	}

	all := diskKernelVersions()
	for k := range pkgs {
		all[k] = true
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sortVersions(keys)

	fmt.Printf("%s%s %s %s %s %10s%s\n", colorBold, pad("VERSÃO", 20), pad("ESTADO", 13), pad("PACOTE", 26), pad("HEADERS", 8), "TAMANHO", colorReset)
	var total, orphanTotal int64
	orphans := 0
	for _, k := range keys {
		state, color := "órfão", colorYellow
		pkg := "-"
		if pkgver, found := pkgs[k]; found {
			state, color, pkg = "instalado", colorWhite, pkgver
		}
		if k == running {
			state, color = "em execução", colorGreen
		}

		headers := "-"
		hasHeaderDir := false
		if _, err := os.Lstat("/usr/src/kernel-headers-" + k); err == nil {
			hasHeaderDir = true
		}
		if pkg != "-" {
			headers = "não"
			if name, err := pkgNameFromPkgver(pkg); err == nil && installedPkgver(name+"-headers") != "" {
				headers = "sim"
			} else if hasHeaderDir {
				headers = "sobra"
			}
		} else if hasHeaderDir {
			headers = "sobra"
		}

		size := kernelSize(k)
		total += size
		if state == "órfão" {
			orphans++
			orphanTotal += size
		}
		fmt.Printf("%s %s%s%s %s %s %10s\n", pad(k, 20), color, pad(state, 13), colorReset, pad(pkg, 26), pad(headers, 8), humanSize(size))
	}
	fmt.Println()
	msg("%d kernel(s), %s no total.", len(keys), humanSize(total))
	if orphans > 0 {
		warn("%d órfão(s) ocupando %s — remova com: %s --cleanup", orphans, humanSize(orphanTotal), appName)
	}
}

// keepNewest mantém os N kernels instalados mais novos (e sempre o que
// está em execução) e desinstala os pacotes dos demais, com headers,
// numa pergunta só; depois limpa as sobras de cada versão. Se os N
// mantidos não incluírem nenhum kernel além do que está rodando,
// mantém também o mais novo dos outros como reserva.
func keepNewest(n int, o runOpts) {
	running := runningKernelVersion()
	pkgs, err := installedKernelPackages()
	if err != nil {
		die("%v — abortando por segurança, nada foi removido.", err)
	}

	installed := make([]string, 0, len(pkgs))
	for k := range pkgs {
		installed = append(installed, k)
	}
	sortVersions(installed)
	// do mais novo para o mais antigo
	for i, j := 0, len(installed)-1; i < j; i, j = i+1, j-1 {
		installed[i], installed[j] = installed[j], installed[i]
	}

	keep := map[string]bool{running: true}
	for i := 0; i < n && i < len(installed); i++ {
		keep[installed[i]] = true
	}
	hasReserve := false
	for k := range keep {
		if k != running && pkgs[k] != "" {
			hasReserve = true
		}
	}
	if !hasReserve {
		for _, k := range installed {
			if k != running {
				keep[k] = true
				warn("Mantendo também %s como kernel de reserva (além do kernel em execução).", k)
				break
			}
		}
	}

	kept := []string{}
	candidates := []string{}
	for _, k := range installed {
		if keep[k] {
			kept = append(kept, k)
		} else {
			candidates = append(candidates, k)
		}
	}
	sortVersions(kept)
	sortVersions(candidates)

	msg("Kernel em execução: %s", running)
	msg("Mantendo: %s", strings.Join(kept, " "))
	if len(candidates) == 0 {
		msg("Nada a desinstalar: só existem os kernels mantidos.")
		return
	}

	before := measureKernels(candidates)
	fmt.Println()
	showSummary("Kernels instalados que serão desinstalados:", candidates, before)
	uninstallKernelPackages(candidates, pkgs, running, o)
	purgeKernels(candidates, before, true, o)
}

// =========================================================
// MAIN
// =========================================================

func main() {
	setupColors()

	var (
		optCleanup   bool
		optForce     bool
		optKeep      int
		command      string
		targetKernel string
		o            runOpts
	)

	args := os.Args[1:]
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--cleanup":
			optCleanup = true
			i++
		case a == "--force":
			optForce = true
			i++
		case a == "--dry-run":
			o.dryRun = true
			i++
		case a == "-y" || a == "--yes":
			o.yes = true
			i++
		case a == "--no-snapshot":
			o.noSnapshot = true
			i++
		case a == "--keep" || strings.HasPrefix(a, "--keep="):
			val := strings.TrimPrefix(a, "--keep=")
			if a == "--keep" {
				i++
				if i >= len(args) {
					die("--keep precisa de um número (ex.: --keep 2).")
				}
				val = args[i]
			}
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				die("--keep precisa de um número maior que zero, recebido: %q", val)
			}
			optKeep = n
			i++
		case a == "-h" || a == "--help":
			usage()
			return
		case a == "list" || a == "rm" || a == "status":
			command = a
			i++
			if a != "status" && i < len(args) && !strings.HasPrefix(args[i], "-") {
				targetKernel = args[i]
				i++
			}
		default:
			usage()
			return
		}
	}

	modes := 0
	for _, on := range []bool{optCleanup, optKeep > 0, command != ""} {
		if on {
			modes++
		}
	}
	if modes > 1 {
		die("Use só um entre: list, rm, status, --cleanup, --keep.")
	}

	switch {
	case optCleanup:
		if os.Geteuid() != 0 {
			elevateToRoot(os.Args[1:])
		}
		if err := checkBoot(); err != nil {
			die("%v", err)
		}

		msg("Varrendo o sistema em busca de versões órfãs...")
		orphans, err := findOrphanVersions()
		if err != nil {
			// Não é seguro prosseguir: sem saber quais kernels ainda
			// pertencem a pacotes instalados, poderíamos apagar um kernel
			// de fallback válido (ver BUG 12 no rodapé do arquivo).
			die("não foi possível verificar quais kernels pertencem a pacotes instalados: %v — abortando por segurança, nada foi removido.", err)
		}
		if len(orphans) == 0 {
			if !optForce {
				msg("Sistema já está limpo. Nada a fazer.")
				return
			}
			if o.dryRun {
				msg("%s --force ativo: regeneração do bootloader seria forçada.", drTag())
			} else {
				msg("Forçando regeneração do bootloader...")
			}
			finalizeSystem(o.dryRun)
			return
		}
		if optForce {
			if o.dryRun {
				msg("%s --force ativo: regeneração do bootloader seria forçada.", drTag())
			} else {
				msg("--force ativo: o bootloader será regenerado.")
			}
		}
		purgeKernels(orphans, nil, false, o)

	case optKeep > 0:
		if os.Geteuid() != 0 {
			elevateToRoot(os.Args[1:])
		}
		if err := checkBoot(); err != nil {
			die("%v", err)
		}
		keepNewest(optKeep, o)

	case command == "rm":
		if os.Geteuid() != 0 {
			elevateToRoot(os.Args[1:])
		}
		if err := checkBoot(); err != nil {
			die("%v", err)
		}
		if targetKernel == "" {
			die("Você precisa especificar uma versão ou 'all'.")
		}

		running := runningKernelVersion()

		if targetKernel == "all" {
			result, err := listKernels([]string{"all"})
			if err != nil {
				die("%v — abortando por segurança, nada foi removido.", err)
			}
			if len(result) == 0 {
				msg("Nenhum kernel órfão encontrado.")
				os.Exit(0)
			}
			purgeKernels(result, nil, false, o)
			return
		}

		// Validações de segurança antes de remover uma versão
		// específica indicada manualmente pelo usuário (ver BUGS
		// 11, 12 e 13 no rodapé do arquivo).
		if !isValidKernelVersion(targetKernel) {
			die("Versão de kernel inválida: %q", targetKernel)
		}
		if targetKernel == running {
			die("Recusando remover o kernel em execução: %s", running)
		}
		if !kernelVersionExists(targetKernel) {
			die("Nenhum arquivo encontrado para a versão '%s'. Nada a remover.", targetKernel)
		}
		pkgs, err := installedKernelPackages()
		if err != nil {
			die("%v — abortando por segurança, nada foi removido.", err)
		}
		if _, found := pkgs[targetKernel]; found {
			before := measureKernels([]string{targetKernel})
			fmt.Println()
			showSummary("Kernel instalado que será desinstalado:", []string{targetKernel}, before)
			uninstallKernelPackages([]string{targetKernel}, pkgs, running, o)
			purgeKernels([]string{targetKernel}, before, true, o)
			return
		}
		purgeKernels([]string{targetKernel}, nil, false, o)

	case command == "list":
		var patterns []string
		if targetKernel != "" {
			patterns = []string{targetKernel}
		}
		result, err := listKernels(patterns)
		if err != nil {
			warn("Não foi possível confirmar quais kernels pertencem a pacotes instalados (%v); a lista abaixo pode incluir kernels ainda instalados.", err)
		}
		if len(result) == 0 {
			msg("Nenhum kernel órfão encontrado.")
		} else {
			fmt.Printf("Kernels órfãos:\n%s%s%s\n", colorYellow, strings.Join(result, "\n"), colorReset)
		}

	case command == "status":
		showStatus()

	default:
		usage()
	}
}

// =========================================================
// BUGS CORRIGIDOS EM RELAÇÃO AO SCRIPT BASH ORIGINAL
// =========================================================
//
// 1. `--dry-run` era reconhecido na linha de comando (OPT_DRY_RUN) mas
//    nunca era consultado em nenhuma outra parte do script: nada de
//    fato simulava a execução. Agora `dryRun` é propagado para
//    removePath, removeKernel, runHooks e finalizeSystem.
//
// 2. O comando `rm <versão>` / `rm all` era um stub vazio
//    ("Sua lógica de remoção aqui" / "..."), ou seja, nunca removia
//    nada de fato. Implementada a remoção real.
//
// 3. `sh_checkDependencies` chamava uma função `msg_info` que nunca
//    foi definida no script (só existiam `msg`, `ok`, `warn`, `err`,
//    `fun`), o que causaria erro em tempo de execução se esse caminho
//    fosse alcançado. Como o binário Go não depende de pacotes
//    externos, esse problema deixa de existir.
//
// 4. `check_boot()` existia mas nunca era chamada em lugar algum do
//    fluxo principal. Agora é chamada antes de qualquer operação
//    destrutiva (`--cleanup` e `rm`).
//
// 5. Em `list_kernels`, o filtro que exclui kernels pertencentes a
//    pacotes instalados só era aplicado quando o padrão era "*"; ao
//    passar uma versão específica, esse filtro era ignorado, podendo
//    listar (e, em teoria, permitir remover) um kernel que ainda
//    pertence a um pacote instalado. Agora o filtro é sempre aplicado.
//
// 6. Esse mesmo filtro comparava, de forma frágil, o caminho completo
//    do arquivo contra a saída bruta (texto) do `xbps-query` via
//    `case "$installed" in *"$k"*)`, o que raramente funcionava como
//    esperado. Substituído por um conjunto (map) de versões realmente
//    extraídas da saída do xbps-query.
//
// 7. `rm <versão>` não tinha nenhuma proteção contra o usuário
//    especificar, por engano, a versão do kernel atualmente em
//    execução. Adicionada uma recusa explícita nesse caso.
//
// 8. A ordenação de versões usava ordenação puramente lexicográfica em
//    alguns pontos, diferente do `sort -uV` (ordenação de versão) do
//    script original. Implementada uma comparação natural/por versão.
//
// 9. Os hooks `pre-remove`/`post-remove` sempre eram executados de
//    verdade, mesmo em uma futura simulação de dry-run (que, como
//    dito no item 1, nunca chegava a existir). Agora, em modo
//    --dry-run, os hooks são apenas anunciados, não executados.
//
// 10. Mesmo depois de blindar a execução real contra --dry-run, as
//     mensagens ainda afirmavam categoricamente que ações tinham
//     ocorrido ("Executando hook: X", "Atualizando bootloader...",
//     "Bootloader e initramfs atualizados.", "Forçando regeneração
//     do bootloader...") mesmo quando nada de fato foi alterado.
//     Corrigido para que toda mensagem em modo --dry-run deixe claro
//     que se trata de uma simulação.
//
// 11. `remove_path` (bash) e a primeira versão de `removePath` (Go)
//     validavam o caminho apenas comparando o PREFIXO TEXTUAL contra
//     "/boot/", "/usr/lib/modules/" etc., sem normalizar o caminho
//     antes. Uma versão de kernel como "../../etc/passwd" produziria
//     "/boot/vmlinuz-../../etc/passwd": textualmente começa com
//     "/boot/" (passa no teste), mas o sistema operacional resolveria
//     isso para FORA de /boot. Corrigido com `filepath.Clean` antes da
//     checagem de prefixo, e com validação estrita do formato de
//     versão (`isValidKernelVersion`) antes de aceitar qualquer valor
//     vindo da linha de comando.
//
// 12. Tanto o script bash original quanto a primeira versão em Go
//     tratavam falha do `xbps-query` (binário ausente, erro, banco de
//     dados corrompido) como "nenhum kernel pertence a pacote
//     instalado" — um modo de falha "aberto", que na prática permite
//     remover um kernel de fallback que ainda está instalado, apenas
//     porque não foi possível confirmar isso. Corrigido para que essa
//     situação seja tratada como erro explícito, abortando qualquer
//     operação destrutiva (`--cleanup`, `rm all`, `rm <versão>`) em vez
//     de assumir que é seguro prosseguir. Para o comando `list`
//     (somente leitura), o comportamento é mais brando: mostra um
//     aviso, mas ainda exibe a lista.
//
// 13. `rm <versão>` (além de ser um stub vazio — item 2) nunca checava
//     se a versão informada realmente existia no disco, nem se ainda
//     pertencia a um pacote instalado. Corrigido: agora recusa
//     versões que não existem em /boot ou /usr/lib/modules, e recusa
//     remover qualquer versão que o xbps ainda considere instalada,
//     orientando o uso de `xbps-remove` nesse caso.
//
// 14. (0.7.25) `kernelVersionExists` aceitava qualquer entrada de
//     /usr/lib/modules (checava só com Lstat). Com isso,
//     `rm arquivo-solto` passava em todas as validações, rodava os
//     hooks de pre/post-remove com "arquivo-solto" como versão de
//     kernel e apagava /usr/lib/modules/arquivo-solto. Agora só um
//     diretório conta, como já fazia o `--cleanup`.
//
// 15. (0.7.25) `list` e `rm all` usavam `listKernels`, que só olhava
//     /boot/vmlinu[xz]-*, enquanto `--cleanup` também olhava initramfs,
//     config e /usr/lib/modules. Sobras comuns depois de um xbps-remove
//     (ex.: /usr/lib/modules/<versão> com módulos DKMS) apareciam como
//     "Nenhum kernel órfão encontrado" no `list`/`rm all`, mas eram
//     removidas pelo `--cleanup`. Agora os três usam
//     `findOrphanVersions`, a mesma varredura.
//
// 16. (0.7.25) A versão era extraída do nome do arquivo cortando no
//     último "-". Para kernels com hífen na versão (ex.:
//     /boot/vmlinuz-6.6.32-voidbr) isso dava "voidbr", que nunca
//     batia com o kernel em execução nem com o xbps, e aparecia como
//     órfão falso. Agora a versão é o que vem depois do prefixo
//     conhecido (vmlinuz-, vmlinux-, initramfs-, config-).
//
// 17. (0.7.26) `rm <versão>` de kernel ainda instalado só recusava
//     (item 13). Agora pergunta se deve desinstalar o pacote dono via
//     `vinstall -Xy` e limpa as sobras. O nome do pacote vem do
//     `xbps-uhelper getpkgname` e não de cortar o pkgver no último
//     "-": num teste com kernel cuja versão tem hífen, o corte manual
//     deu nomes diferentes para o mesmo pacote e a proteção do kernel
//     ativo deixou passar a remoção do pacote em uso. Se o xbps não
//     confirmar o nome, a operação é abortada. O pacote <nome>-headers,
//     se instalado, sai junto; se continuar instalado depois do
//     vinstall, a limpeza é abortada para não apagar
//     /usr/src/kernel-headers-<versão> de um pacote ainda instalado.
//
// 18. (0.7.26) isTerminal testava só "é dispositivo de caractere", o
//     que vale também para /dev/null: `rm X </dev/null` fazia a
//     pergunta em vez de recusar por falta de terminal. Agora usa
//     ioctl(TCGETS), como o isatty da libc.
//
// 19. (0.7.27) `rm all`, `rm <versão órfã>` e `--cleanup` apagavam
//     direto, sem mostrar o que ia sair nem pedir confirmação. Agora
//     toda remoção mostra o resumo (versões, caminhos, tamanho) e pede
//     [s/N]; sem terminal exige -y. A desinstalação de pacotes também
//     passou a exigir um kernel de reserva além do que está rodando.
