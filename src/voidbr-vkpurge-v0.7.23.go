/*
   voidbr-vkpurge
   Remove kernels órfãos do Void Linux com segurança.
   Site:      https://chililinux.com
   GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall
   Created:   sáb 09 mai 2026 10:57:53 -04
   Updated:   sex 24 jul 2026 22:47:58 -04
   Version:   0.7.23
   Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const (
	appName = "voidbr-vkpurge"
	version = "0.7.23"
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

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
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

// =========================================================
// USO
// =========================================================

func usage() {
	fmt.Printf("%s%s v%s%s\n", colorCyan, appName, version, colorReset)
	fmt.Printf("%sLimpa kernels órfãos do Void Linux com segurança%s\n\n", colorWhite, colorReset)
	fmt.Printf("%sUso:%s\n", colorYellow, colorReset)
	fmt.Printf("  %s%s%s %slist%s [versão]        # Lista kernels órfãos (filtra por versão se fornecido)\n", colorGreen, appName, colorReset, colorCyan, colorReset)
	fmt.Printf("  %s%s%s %srm%s all               # Remove todos os kernels órfãos detectados\n", colorGreen, appName, colorReset, colorRed, colorReset)
	fmt.Printf("  %s%s%s %srm%s <versão>          # Remove uma versão específica de kernel\n", colorGreen, appName, colorReset, colorRed, colorReset)
	fmt.Printf("  %s%s%s %s--cleanup%s            # Varredura e remoção automática de órfãos\n", colorGreen, appName, colorReset, colorMagenta, colorReset)
	fmt.Printf("  %s%s%s %s--force%s              # Força a regeneração do bootloader\n", colorGreen, appName, colorReset, colorMagenta, colorReset)
	fmt.Printf("  %s%s%s %s--dry-run%s            # Simula as ações sem alterar o sistema\n\n", colorGreen, appName, colorReset, colorMagenta, colorReset)
	fmt.Printf("%sExemplos:%s\n", colorYellow, colorReset)
	fmt.Printf("  %s list\n", appName)
	fmt.Printf("  %s rm all\n", appName)
	fmt.Printf("  %s rm 6.6.32_1\n", appName)
	fmt.Printf("  %s --cleanup\n", appName)
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
			warn("[DRY-RUN] Hook seria executado: %s", name)
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
	result := map[string]bool{}

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

	re := regexp.MustCompile(`vmlinu[xz]-(\S+)`)
	for _, line := range strings.Split(string(out), "\n") {
		if m := re.FindStringSubmatch(line); len(m) == 2 {
			result[m[1]] = true
		}
	}
	return result, nil
}

func kernelVersionFromFile(path string) string {
	base := filepath.Base(path)
	idx := strings.LastIndex(base, "-")
	if idx == -1 {
		return base
	}
	return base[idx+1:]
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
func kernelVersionExists(kver string) bool {
	candidates := []string{
		"/boot/vmlinuz-" + kver,
		"/boot/vmlinux-" + kver,
		"/usr/lib/modules/" + kver,
	}
	for _, c := range candidates {
		if _, err := os.Lstat(c); err == nil {
			return true
		}
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

	running := runningKernelVersion()
	installed, installedErr := installedKernelVersions()

	set := map[string]bool{}
	for _, arg := range patterns {
		pattern := arg
		if pattern == "" || pattern == "all" {
			pattern = "*"
		}
		for _, glob := range []string{"/boot/vmlinuz-*", "/boot/vmlinux-*"} {
			files, _ := filepath.Glob(glob)
			for _, f := range files {
				kver := kernelVersionFromFile(f)
				if kver == running {
					continue
				}
				if installed[kver] {
					continue
				}
				if match, _ := filepath.Match(pattern, kver); match {
					set[kver] = true
				}
			}
		}
	}

	result := make([]string, 0, len(set))
	for k := range set {
		result = append(result, k)
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
		warn("[DRY-RUN] Removeria: %s", clean)
		return nil
	}

	warn("Removendo: %s", clean)
	return os.RemoveAll(clean)
}

func removeKernel(kver string, dryRun bool) {
	fun("Kernel %s será enviado para o além.", kver)
	runHooks("pre-remove", kver, dryRun)

	paths := []string{
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

	for _, p := range paths {
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
		msg("[DRY-RUN] Bootloader seria atualizado agora.")
	} else {
		msg("Atualizando bootloader...")
	}
	fun("Nada de rodar grub-mkconfig 37 vezes.")

	os.Setenv("KERNEL_ACTION", "bulk")
	runHooks("post-remove", "", dryRun)

	if dryRun {
		ok("[DRY-RUN] Simulação concluída — nenhuma alteração foi feita no sistema.")
	} else {
		ok("Bootloader e initramfs atualizados.")
	}
}

// =========================================================
// CLEANUP AUTOMÁTICO
// =========================================================

func cleanupOrphans(dryRun bool) (bool, error) {
	msg("Varrendo o sistema em busca de versões órfãs...")

	versions := map[string]bool{}

	globs := []string{
		"/boot/vmlinuz-*",
		"/boot/vmlinux-*",
		"/boot/initramfs-*",
		"/boot/config-*",
	}
	for _, g := range globs {
		files, _ := filepath.Glob(g)
		for _, f := range files {
			base := strings.TrimSuffix(filepath.Base(f), ".img")
			idx := strings.LastIndex(base, "-")
			if idx == -1 {
				continue
			}
			versions[base[idx+1:]] = true
		}
	}

	if entries, err := os.ReadDir("/usr/lib/modules"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				versions[e.Name()] = true
			}
		}
	}

	running := runningKernelVersion()
	installed, err := installedKernelVersions()
	if err != nil {
		// Não é seguro prosseguir: sem saber quais kernels ainda
		// pertencem a pacotes instalados, poderíamos apagar um kernel
		// de fallback válido (ver BUG 12 no rodapé do arquivo).
		return false, fmt.Errorf("não foi possível verificar quais kernels pertencem a pacotes instalados: %w", err)
	}

	keys := make([]string, 0, len(versions))
	for k := range versions {
		keys = append(keys, k)
	}
	sortVersions(keys)

	removedAny := false
	for _, kver := range keys {
		if kver == running {
			continue
		}
		if installed[kver] {
			continue
		}

		fmt.Println()

		orphanFiles, _ := filepath.Glob("/boot/*" + kver + "*")
		names := make([]string, 0, len(orphanFiles))
		for _, o := range orphanFiles {
			names = append(names, filepath.Base(o))
		}
		warn("Arquivos órfãos detectados: %s", strings.Join(names, " "))

		removeKernel(kver, dryRun)
		removedAny = true
	}

	return removedAny, nil
}

// =========================================================
// MAIN
// =========================================================

func main() {
	setupColors()

	var (
		optCleanup   bool
		optForce     bool
		optDryRun    bool
		command      string
		targetKernel string
	)

	args := os.Args[1:]
	i := 0
	for i < len(args) {
		a := args[i]
		switch a {
		case "--cleanup":
			optCleanup = true
			i++
		case "--force":
			optForce = true
			i++
		case "--dry-run":
			optDryRun = true
			i++
		case "-h", "--help":
			usage()
			return
		case "list", "rm":
			command = a
			i++
			if i < len(args) && !strings.HasPrefix(args[i], "-") {
				targetKernel = args[i]
				i++
			}
		default:
			usage()
			return
		}
	}

	switch {
	case optCleanup:
		if os.Geteuid() != 0 {
			elevateToRoot(os.Args[1:])
		}
		if err := checkBoot(); err != nil {
			die("%v", err)
		}

		removedAny, err := cleanupOrphans(optDryRun)
		if err != nil {
			die("%v — abortando por segurança, nada foi removido.", err)
		}
		if removedAny || optForce {
			if optForce {
				if optDryRun {
					msg("[DRY-RUN] --force ativo: regeneração do bootloader seria forçada.")
				} else {
					msg("Forçando regeneração do bootloader...")
				}
			}
			finalizeSystem(optDryRun)
		} else {
			msg("Sistema já está limpo. Nada a fazer.")
		}

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
		var toRemove []string

		if targetKernel == "all" {
			result, err := listKernels([]string{"all"})
			if err != nil {
				die("%v — abortando por segurança, nada foi removido.", err)
			}
			toRemove = result
			if len(toRemove) == 0 {
				msg("Nenhum kernel órfão encontrado.")
				os.Exit(0)
			}
		} else {
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
			installed, err := installedKernelVersions()
			if err != nil {
				die("%v — abortando por segurança, nada foi removido.", err)
			}
			if installed[targetKernel] {
				die("Recusando remover '%s': ainda pertence a um pacote instalado. Desinstale o pacote do kernel com xbps-remove primeiro.", targetKernel)
			}
			toRemove = []string{targetKernel}
		}

		for idx, kver := range toRemove {
			if idx > 0 {
				fmt.Println()
			}
			removeKernel(kver, optDryRun)
		}
		finalizeSystem(optDryRun)

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
