/*
   xbps-query -R -p run_depends -s sdbus-cpp

   voidbr-vinstall
   Wrapper para o Void xbps-query e xbps-install

   Site:      https://chililinux.com
   Site:      https://voidbr.org
   Site:      https://voidlinux.com.br
   GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

   Created:   ter 03 fev 2026 13:08:22 -04
   Updated:   seg 03 ago 2026 00:00:00 -04
   Version:   1.6.5
   Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"howett.net/plist"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/fatih/color"
	"github.com/klauspost/compress/zstd"
)

const (
	Version     = "1.6.5"
	Copyright   = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
	execTimeout = 10 * time.Second
)

var (
	cyan       = color.New(color.Bold, color.FgCyan).SprintFunc()
	green      = color.New(color.FgGreen).SprintFunc()
	white      = color.New(color.Bold, color.FgWhite).SprintFunc()
	red        = color.New(color.Bold, color.FgRed).SprintFunc()
	yellow     = color.New(color.Bold, color.FgYellow).SprintFunc()
	blue       = color.New(color.Bold, color.FgBlue).SprintFunc()
	magenta    = color.New(color.Bold, color.FgMagenta).SprintFunc()
	black      = color.New(color.Bold, color.FgBlack).SprintFunc()
	bold       = color.New(color.Bold).SprintFunc()
	reverse    = color.New(color.ReverseVideo).SprintFunc()
	green_bold = color.New(color.Bold, color.FgGreen).SprintFunc()
)

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

type Package struct {
	Status        string
	FullName      string
	Description   string
	Maintainer    string
	Repo          string
	SizeDownload  int64
	SizeInstalled int64
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage()
		return
	}
	var flags []string
	var targets []string
	mode := "install"
	searchRemote := false
	filter := ""
	syncFlavor := "" // "sy" | "syy" | "sf" — só vira modo "sync" se não houver targets

	fmt.Print("\033[36m")
	defer fmt.Print("\033[0m")

	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			printUsage()
			return
		case "-v", "--version":
			fmt.Printf("%s %s\n", white("vinstall"), cyan("v"+Version))
			return
		case "--history":
			mode = "history"
		case "-Sy":
			syncFlavor = "sy"
			flags = append(flags, arg)
		case "-Syy":
			syncFlavor = "syy"
			flags = append(flags, arg)
		case "-Sf":
			syncFlavor = "sf"
			flags = append(flags, arg)
		case "-Scc":
			mode = "clean"
		case "-F":
			mode = "find"
		case "-FR":
			mode = "find"
			searchRemote = true
		case "-Li":
			mode = "list-installed"
		case "-Lo":
			mode = "list-orphans"
		case "-Qs":
			mode = "query-search"
		case "-Ss":
			mode = "remote-search"
		case "-Sss":
			mode = "remote-search-detailed"
		case "-Ssi":
			mode = "remote-search"
			filter = "installed"
		case "-Ssu":
			mode = "remote-search"
			filter = "missing"
		default:
			if strings.HasPrefix(arg, "-X") || strings.HasPrefix(arg, "-x") {
				// -X/-x ativam o modo remove. Qualquer sufixo é repassado
				// como flag real para o xbps-remove — mesmo esquema de
				// passthrough já usado com o xbps-install (ex.: "-XR" vira
				// a flag "-R", recursivo; "-xf" vira "-f", força). Sem
				// sufixo (arg == "-X" ou "-x"), só ativa o modo remove.
				mode = "remove"
				suffix := strings.TrimPrefix(strings.TrimPrefix(arg, "-X"), "-x")
				if suffix != "" {
					flags = append(flags, "-"+suffix)
				}
			} else if strings.HasPrefix(arg, "-Q") {
				mode = "query-generic"
				filter = strings.Replace(arg, "-Q", "-", 1)
			} else if strings.HasPrefix(arg, "-") {
				flags = append(flags, arg)
			} else {
				targets = append(targets, arg)
			}
		}
	}

	// -Sy/-Syy/-Sf só disparam o modo "sync" do vinstall quando usadas
	// sozinhas (sem pacotes-alvo). Se vierem acompanhadas de um pacote
	// (ex.: "vinstall -Sy firefox"), a flag já está em `flags` e segue o
	// fluxo normal de instalação, igual usar o xbps-install nativo.
	if syncFlavor != "" && len(targets) == 0 {
		mode = "sync"
	}

	switch mode {
	case "history":
		showHistory()
	case "sync":
		syncAndCheckUpdates(syncFlavor)
	case "clean":
		cleanXbpsCache()
	case "find":
		if len(targets) > 0 {
			findProvides(targets[0], searchRemote)
		}
	case "list-installed":
		listLocal("installed", "")
	case "list-orphans":
		listLocal("orphans", "")
	case "query-search":
		if len(targets) > 0 {
			listLocal("search", targets[0])
		}
	case "remote-search":
		if len(targets) > 0 {
			pkgs := fetchSuggestions(targets[0])
			pkgs = uniquePackagesExact(pkgs)
			sort.Slice(pkgs, func(i, j int) bool {
				return pkgs[i].FullName < pkgs[j].FullName
			})
			if filter != "" {
				pkgs = filterPackages(pkgs, filter)
			}
			displaySearch(pkgs, "Resultados (Rápido):")
		}
	case "remote-search-detailed":
		if len(targets) > 0 {
			remoteSearchDetailed(targets[0])
		}
	case "remove":
		if len(targets) > 0 {
			runBinary("xbps-remove", flags, targets)
		}
	case "query-generic":
		runBinary("xbps-query", []string{filter}, targets)
	default:
		// --ignore-file-conflicts é necessária em dois cenários:
		//   1) qualquer flag que contenha "u" (ex.: -Syu, -u), pois updates
		//      frequentemente trocam donos de arquivo entre pacotes;
		//   2) instalação de pacotes-alvo em geral, pois alguns pacotes
		//      compartilham arquivos idênticos.
		// A flag deve ser adicionada NO MÁXIMO UMA VEZ.
		needsIgnoreConflicts := len(targets) > 0
		if !needsIgnoreConflicts {
			for _, f := range flags {
				if strings.Contains(f, "u") {
					needsIgnoreConflicts = true
					break
				}
			}
		}
		if needsIgnoreConflicts {
			flags = append(flags, "--ignore-file-conflicts")
		}

		if len(targets) > 0 {
			if !runBinary("xbps-install", flags, targets) {
				// Sugestões só fazem sentido quando o pacote realmente não
				// existe no pool de repositórios. Essa checagem é feita
				// via um processo `xbps-query` totalmente separado, DEPOIS
				// que o xbps-install já terminou — não pela saída dele —
				// justamente pra runBinary poder manter stdin/stdout/stderr
				// herdados diretamente do terminal (fd real, sem pipe).
				// Isso é essencial pra hooks .hook (Type=Package,
				// When=PostTransaction) que disparam outro processo
				// interativo (ex.: "xbps-remove pacote") continuarem
				// enxergando um TTY de verdade e mostrando o prompt de
				// confirmação normalmente.
				if !packageExistsInRepo(targets[0]) {
					suggestions := fetchSuggestions(targets[0])
					if len(suggestions) > 0 {
						displayMenu(suggestions, flags)
					}
				}
			} else {
				for _, t := range targets {
					checkAndEnableService(t)
				}
			}
		} else if len(flags) > 0 {
			runBinary("xbps-install", flags, []string{})
		}
	}
}

// --- UTILITÁRIOS GERAIS ---

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func getTerminalWidth() int {
	ws := &winsize{}
	retCode, _, _ := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdin),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)))

	if int(retCode) == 0 {
		return int(ws.Col)
	}
	return 80
}

func cleanVersion(fullName string) string {
	if i := strings.LastIndex(fullName, "-"); i != -1 {
		return fullName[:i]
	}
	return fullName
}

func runBinary(bin string, flags []string, pkgs []string) bool {
	fmt.Printf("%s %s %s %s\n", cyan(">>>"), cyan(bin), yellow(fmt.Sprint(flags)), magenta(fmt.Sprint(pkgs)))
	fmt.Print("\033[36m")
	defer fmt.Print("\033[0m")
	params := []string{bin}
	params = append(params, flags...)
	params = append(params, pkgs...)
	cmd := exec.Command("sudo", params...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() && status.Signal() == syscall.SIGINT {
					fmt.Printf("\n%s %s\n", red("[!]"), white("Operação cancelada pelo usuário."))
					os.Exit(1)
				}
			}
		}
		return false
	}
	return true
}

// packageExistsInRepo confirma, via `xbps-query -R <pkgname>`, se o pacote
// existe em algum repositório ativo. Usada exclusivamente para decidir se
// vale a pena oferecer sugestões (fetchSuggestions/displayMenu) depois que
// um `xbps-install` falha — SEM inspecionar a saída do próprio
// xbps-install, que precisa continuar rodando com stdio herdado
// diretamente do terminal (ver comentário em runBinary/main sobre hooks
// .hook interativos).
func packageExistsInRepo(pkgname string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "xbps-query", "-R", pkgname)
	return cmd.Run() == nil
}

// --- FUNÇÕES DE BUSCA DETALHADA E UTILITÁRIOS (INTACTOS) ---

func toInt64(v interface{}) int64 {
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(val.Uint())
	case reflect.Float32, reflect.Float64:
		return int64(val.Float())
	}
	return 0
}

func truncate(s string, max int) string {
	if max < 3 {
		return ""
	}
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func printPackage(w *bufio.Writer, i int, p Package, width int) {
	idxText := fmt.Sprintf("[%d]", i+1)
	paddingSize := len(idxText) + 1 + 3 + 1
	lenPrefix := paddingSize + len(p.FullName) + 2
	maxDesc := width - lenPrefix
	descExibida := truncate(p.Description, maxDesc)

	fmt.Fprintf(w, "%s %s %s  %s\n", yellow(idxText), p.Status, white(p.FullName), green(descExibida))
	fmt.Fprintf(w, "%*s%s | %s (%s / %s)\n", paddingSize, "", cyan(p.Repo), magenta(p.Maintainer), yellow(formatBytes(p.SizeDownload)), magenta(formatBytes(p.SizeInstalled)))
}

func getInstalledPackages() map[string]bool {
	installed := make(map[string]bool)
	out, err := exec.Command("xbps-query", "-l").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", yellow("[!]"), white("falha ao listar pacotes instalados (xbps-query -l)"), err)
		return installed
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			installed[fields[1]] = true
		}
	}
	return installed
}

// getInstalledPkgverByName retorna um mapa nome-do-pacote -> pkgver completo
// instalado (ex.: "firefox" -> "firefox-128.0_1"). Usado para comparar com a
// versão disponível no repositório e detectar atualizações.
func getInstalledPkgverByName() map[string]string {
	result := make(map[string]string)
	out, err := exec.Command("xbps-query", "-l").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", yellow("[!]"), white("falha ao listar pacotes instalados (xbps-query -l)"), err)
		return result
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			pkgver := fields[1]
			name := cleanVersion(pkgver)
			result[name] = pkgver
		}
	}
	return result
}

// isNewerVersion usa o comparador oficial do xbps (xbps-uhelper cmpver) para
// confirmar que reqVer é de fato mais recente que instVer, em vez de apenas
// "diferente". Isso evita listar como "atualização disponível" um pacote
// cujo repositório está desatualizado em relação ao que já está instalado
// (o que seria um downgrade, não um upgrade).
//
// Exit status do xbps-uhelper cmpver: 255 = instver < reqver,
// 0 = instver == reqver, 1 = instver > reqver.
func isNewerVersion(instVer, reqVer string) bool {
	cmd := exec.Command("xbps-uhelper", "cmpver", instVer, reqVer)
	err := cmd.Run()
	if err == nil {
		return false // exit 0: versões iguais
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode() == 255
	}
	fmt.Fprintf(os.Stderr, "%s %s: %v\n", yellow("[!]"), white("falha ao comparar versões via xbps-uhelper"), err)
	return false // não conseguiu confirmar; por segurança, não lista como upgrade
}

// extractVersion retorna a parte de versão/revisão de um pkgver
// (ex.: "firefox-128.0_1" -> "128.0_1"). Complementa cleanVersion, que
// retorna a outra metade (o nome do pacote).
func extractVersion(pkgver string) string {
	if i := strings.LastIndex(pkgver, "-"); i != -1 {
		return pkgver[i+1:]
	}
	return pkgver
}

func getActiveRepos() map[string]string {
	repoMap := make(map[string]string)
	out, err := exec.Command("xbps-query", "-L").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", yellow("[!]"), white("falha ao listar repositórios (xbps-query -L)"), err)
		return repoMap
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			repoMap[strings.NewReplacer(":", "_", "/", "_", ".", "_").Replace(fields[1])] = fields[1]
		}
	}
	return repoMap
}

// repodataArch tenta determinar o sufixo de arquitetura usado no nome do
// arquivo de índice do repositório (ex.: x86_64-repodata,
// x86_64-musl-repodata, aarch64-repodata), em vez de assumir "x86_64" fixo.
func repodataArch() string {
	out, err := exec.Command("xbps-query", "-p", "architecture", "xbps").Output()
	arch := strings.TrimSpace(string(out))
	if err != nil || arch == "" {
		// fallback: tenta uname -m
		out2, err2 := exec.Command("uname", "-m").Output()
		if err2 == nil {
			arch = strings.TrimSpace(string(out2))
		}
	}
	if arch == "" {
		arch = "x86_64"
	}
	return arch
}

func remoteSearchDetailed(query string) {
	repoMap := getActiveRepos()
	installed := getInstalledPackages()
	arch := repodataArch()
	var pkgs []Package
	var mu sync.Mutex
	var wg sync.WaitGroup

	queryLower := strings.ToLower(query)
	queryClean := strings.ReplaceAll(queryLower, " ", "")

	for dirName, repoURL := range repoMap {
		repoPath := filepath.Join("/var/db/xbps/", dirName, arch+"-repodata")
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			// fallback para x86_64 caso a detecção de arquitetura falhe
			// em algum repositório específico (ex.: repo multilib)
			repoPath = filepath.Join("/var/db/xbps/", dirName, "x86_64-repodata")
			if _, err2 := os.Stat(repoPath); os.IsNotExist(err2) {
				continue
			}
		}

		wg.Add(1)
		go func(p, url string) {
			defer wg.Done()
			data, err := readRepodataPlist(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s %s (%s): %v\n", yellow("[!]"), white("falha ao ler repodata"), url, err)
				return
			}

			var index map[string]interface{}
			if err := plist.NewDecoder(bytes.NewReader(data)).Decode(&index); err != nil {
				fmt.Fprintf(os.Stderr, "%s %s (%s): %v\n", yellow("[!]"), white("falha ao decodificar plist"), url, err)
				return
			}

			var local []Package
			for _, pkgData := range index {
				pkg, ok := pkgData.(map[string]interface{})
				if !ok {
					continue
				}

				sizeDown := toInt64(pkg["filename-size"])
				sizeInst := toInt64(pkg["installed_size"])

				pkgVerStr := fmt.Sprintf("%v", pkg["pkgver"])
				fullText := pkgVerStr + " " + fmt.Sprint(pkg["short_desc"]) + " " +
					fmt.Sprint(pkg["maintainer"]) + " " + url

				fullTextClean := strings.ToLower(strings.ReplaceAll(fullText, " ", ""))

				if strings.Contains(fullTextClean, queryClean) {
					status := red("[-]")
					if installed[pkgVerStr] {
						status = green("[✔]")
					}
					local = append(local, Package{
						Status:        status,
						FullName:      pkgVerStr,
						Description:   fmt.Sprint(pkg["short_desc"]),
						Maintainer:    fmt.Sprintf("%v", pkg["maintainer"]),
						Repo:          url,
						SizeDownload:  sizeDown,
						SizeInstalled: sizeInst,
					})
				}
			}

			if len(local) > 0 {
				mu.Lock()
				pkgs = append(pkgs, local...)
				mu.Unlock()
			}
		}(repoPath, repoURL)
	}
	wg.Wait()
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].FullName < pkgs[j].FullName })

	if len(pkgs) == 0 {
		fmt.Println("Nenhum pacote encontrado.")
	} else {
		fmt.Printf("\n%s\n", cyan("Resultados encontrados nos repositórios:"))
		width := getTerminalWidth()
		fmt.Println(white(strings.Repeat("─", width)))
		writer := bufio.NewWriter(os.Stdout)
		for i, p := range pkgs {
			printPackage(writer, i, p, width)
		}
		writer.Flush()
		fmt.Println(white(strings.Repeat("─", width)))
	}
}

// readRepodataPlist descomprime o arquivo x86_64-repodata (zstd) e extrai o
// conteúdo do arquivo "index.plist" de dentro do tar embutido, usando o
// pacote archive/tar em vez de assumir um offset fixo de 512 bytes. Isso
// evita quebrar quando o tar usa headers PAX estendidos (nomes/paths longos).
func readRepodataPlist(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	zr, err := zstd.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("index.plist não encontrado no repodata")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == "index.plist" {
			return io.ReadAll(tr)
		}
	}
}

// syncAndCheckUpdates sincroniza os índices dos repositórios e lista o que
// mudaria, sem instalar nada. O comportamento varia pelo flavor:
//
//	"sy":  sync normal + só lista atualizações reais (repo mais novo)
//	"syy": sync FORÇADO (ignora cache em disco, --force) + só atualizações reais
//	"sf":  sync FORÇADO + lista TODAS as diferenças de versão, incluindo
//	       downgrades (repo com versão menor que a instalada)
func syncAndCheckUpdates(flavor string) {
	forceSync := flavor == "syy" || flavor == "sf"
	showDowngrades := flavor == "sf"

	fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Sincronizando índices dos repositórios..."))
	syncFlags := []string{"-S"}
	if forceSync {
		syncFlags = append(syncFlags, "--force")
	}
	if !runBinary("xbps-install", syncFlags, []string{}) {
		fmt.Printf("%s %s\n", red("[!]"), white("Falha ao sincronizar índices."))
		return
	}
	fmt.Println()

	repoMap := getActiveRepos()
	installed := getInstalledPkgverByName()
	arch := repodataArch()

	var updates []Package
	var mu sync.Mutex
	var wg sync.WaitGroup

	for dirName, repoURL := range repoMap {
		repoPath := filepath.Join("/var/db/xbps/", dirName, arch+"-repodata")
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			repoPath = filepath.Join("/var/db/xbps/", dirName, "x86_64-repodata")
			if _, err2 := os.Stat(repoPath); os.IsNotExist(err2) {
				continue
			}
		}

		wg.Add(1)
		go func(p, url string) {
			defer wg.Done()
			data, err := readRepodataPlist(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s %s (%s): %v\n", yellow("[!]"), white("falha ao ler repodata"), url, err)
				return
			}
			var index map[string]interface{}
			if err := plist.NewDecoder(bytes.NewReader(data)).Decode(&index); err != nil {
				fmt.Fprintf(os.Stderr, "%s %s (%s): %v\n", yellow("[!]"), white("falha ao decodificar plist"), url, err)
				return
			}

			var local []Package
			for _, pkgData := range index {
				pkg, ok := pkgData.(map[string]interface{})
				if !ok {
					continue
				}
				pkgVerStr := fmt.Sprintf("%v", pkg["pkgver"])
				name := cleanVersion(pkgVerStr)

				installedVer, isInstalled := installed[name]
				if !isInstalled || installedVer == pkgVerStr {
					continue // não instalado, ou já na versão mais recente
				}

				isUpgrade := isNewerVersion(installedVer, pkgVerStr)
				if !isUpgrade && !showDowngrades {
					// O repositório tem uma versão diferente, mas não mais
					// nova que a instalada (mirror desatualizado, por
					// exemplo). Só é exibido no modo -Sf.
					continue
				}

				status := "upgrade"
				if !isUpgrade {
					status = "downgrade"
				}

				local = append(local, Package{
					Status:        status,
					FullName:      pkgVerStr,
					Description:   installedVer, // reaproveita o campo p/ guardar a versão instalada
					Maintainer:    fmt.Sprintf("%v", pkg["maintainer"]),
					Repo:          url,
					SizeDownload:  toInt64(pkg["filename-size"]),
					SizeInstalled: toInt64(pkg["installed_size"]),
				})
			}

			if len(local) > 0 {
				mu.Lock()
				updates = append(updates, local...)
				mu.Unlock()
			}
		}(repoPath, repoURL)
	}
	wg.Wait()

	if len(updates) == 0 {
		fmt.Printf("%s %s\n", green("[✔]"), white("Sistema já está atualizado."))
		return
	}

	// Pacotes disponíveis em mais de um repositório (ex.: repo principal +
	// multilib) geram entradas duplicadas com o mesmo pkgver de destino.
	// FullName já identifica pacote+versão de forma única, então dedup por
	// esse campo remove as repetições sem afetar atualizações distintas.
	updates = uniquePackagesExact(updates)

	sort.Slice(updates, func(i, j int) bool { return updates[i].FullName < updates[j].FullName })

	upgradeCount, downgradeCount := 0, 0
	for _, p := range updates {
		if p.Status == "downgrade" {
			downgradeCount++
		} else {
			upgradeCount++
		}
	}

	width := getTerminalWidth()
	if showDowngrades && downgradeCount > 0 {
		fmt.Printf("%s (%s %s, %s %s):\n", cyan("Pacotes com diferença de versão"),
			yellow(strconv.Itoa(upgradeCount)), green("atualizações"),
			yellow(strconv.Itoa(downgradeCount)), red("downgrades"))
	} else {
		fmt.Printf("%s (%s):\n", cyan("Pacotes com atualização disponível"), yellow(strconv.Itoa(len(updates))))
	}
	fmt.Println(white(strings.Repeat("─", width)))

	// Mesma técnica usada em displaySearch (-Ss): medir a largura de cada
	// coluna a partir do texto PURO (sem códigos ANSI de cor) e só depois
	// aplicar a cor sobre o texto já paddado. Colorir antes de medir faz o
	// %-*s contar os bytes de escape como parte do comprimento visível,
	// quebrando o alinhamento.
	type updateRow struct {
		name, oldVer, newVer, size, repo string
		isUpgrade                        bool
		download                         int64
	}
	rows := make([]updateRow, 0, len(updates))
	maxNameLen, maxOldLen, maxNewLen, maxSizeLen := 0, 0, 0, 0
	for _, p := range updates {
		r := updateRow{
			name:      cleanVersion(p.FullName),
			oldVer:    extractVersion(p.Description),
			newVer:    extractVersion(p.FullName),
			size:      formatBytes(p.SizeDownload),
			repo:      p.Repo,
			isUpgrade: p.Status != "downgrade",
			download:  p.SizeDownload,
		}
		if len(r.name) > maxNameLen {
			maxNameLen = len(r.name)
		}
		if len(r.oldVer) > maxOldLen {
			maxOldLen = len(r.oldVer)
		}
		if len(r.newVer) > maxNewLen {
			maxNewLen = len(r.newVer)
		}
		if len(r.size) > maxSizeLen {
			maxSizeLen = len(r.size)
		}
		rows = append(rows, r)
	}
	if maxNameLen > 40 {
		maxNameLen = 40
	}

	w := bufio.NewWriter(os.Stdout)
	var totalDownload int64
	for i, r := range rows {
		idx := yellow(fmt.Sprintf("[%2d]", i+1))
		namePadded := fmt.Sprintf("%-*s", maxNameLen, r.name)
		oldPadded := fmt.Sprintf("%*s", maxOldLen, r.oldVer)  // alinhado à direita
		newPadded := fmt.Sprintf("%-*s", maxNewLen, r.newVer) // alinhado à esquerda
		sizePadded := fmt.Sprintf("%*s", maxSizeLen, r.size)  // alinhado à direita
		repoShown := truncate(r.repo, 45)

		newColored := green(newPadded)
		tag := ""
		if !r.isUpgrade {
			newColored = red(newPadded)
			tag = "  " + red("[DOWNGRADE]")
		}
		fmt.Fprintf(w, "%s %s %s → %s  (%s)  %s%s\n", idx, white(namePadded), yellow(oldPadded), newColored, magenta(sizePadded), cyan(repoShown), tag)
		totalDownload += r.download
	}
	w.Flush()

	fmt.Println(white(strings.Repeat("─", width)))
	fmt.Printf("%s %s\n\n", white("Total a baixar:"), cyan(formatBytes(totalDownload)))
	if downgradeCount > 0 {
		fmt.Printf("%s %s\n", yellow("[!]"), white("Itens marcados como [DOWNGRADE] não serão instalados por 'vinstall -Syu'."))
		fmt.Printf("%s %s\n", yellow("[!]"), white("Void permite downgrade, mas exige --force/-f. Para aplicar um específico:"))
		fmt.Printf("    %s\n", cyan("vinstall -f <pacote>"))
	}
	fmt.Printf("%s %s\n", yellow("[!]"), white("Rode 'vinstall -Syu' para aplicar as atualizações."))
}

// --- FUNÇÕES DE BUSCA RÁPIDA E OUTROS ---

func fetchSuggestions(query string) []Package {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "xbps-query", "-Rs", query)
	out, _ := cmd.Output()
	var pkgs []Package
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		status := line[0:4]
		rest := strings.TrimSpace(line[4:])
		name, desc, _ := strings.Cut(rest, " ")
		if name != "" {
			pkgs = append(pkgs, Package{Status: strings.TrimSpace(status), FullName: name, Description: strings.TrimSpace(desc)})
		}
	}
	return pkgs
}

func displaySearch(pkgs []Package, title string) {
	if len(pkgs) == 0 {
		return
	}
	width := getTerminalWidth()
	lineSeparator := white(strings.Repeat("─", width))
	maxNameLen := 0
	for _, p := range pkgs {
		if len(p.FullName) > maxNameLen {
			maxNameLen = len(p.FullName)
		}
	}
	if maxNameLen > 50 {
		maxNameLen = 50
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	if title != "" {
		fmt.Fprintf(w, "\n%s\n", cyan(title))
	}
	fmt.Fprintf(w, "%s\n", lineSeparator)

	for i, p := range pkgs {
		idx := yellow(fmt.Sprintf("[%2d]", i+1))
		statusDisplay := "[-] "
		statusColor := red(statusDisplay)
		if strings.Contains(p.Status, "*") {
			statusDisplay = "[✓] "
			statusColor = green_bold(statusDisplay)
		}
		fmt.Fprintf(w, "%s %s %s  %s\n", idx, statusColor, white(fmt.Sprintf("%-*s", maxNameLen, p.FullName)), green(p.Description))
	}
	fmt.Fprintln(w, lineSeparator)
}

func displayMenu(pkgs []Package, flags []string) {
	if len(pkgs) == 0 {
		return
	}
	displaySearch(pkgs, "\nSugestões encontradas no repositório:")
	fmt.Printf("%s", yellow("Selecione o número para instalar ou 'q' para sair: "))
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "q" || input == "" {
		return
	}
	choice, _ := strconv.Atoi(input)
	if choice > 0 && choice <= len(pkgs) {
		name := cleanVersion(pkgs[choice-1].FullName)
		if runBinary("xbps-install", flags, []string{name}) {
			checkAndEnableService(name)
		}
	}
}

func uniquePackagesExact(pkgs []Package) []Package {
	keys := make(map[string]bool)
	var list []Package
	for _, p := range pkgs {
		if !keys[p.FullName] {
			keys[p.FullName] = true
			list = append(list, p)
		}
	}
	return list
}

func filterPackages(pkgs []Package, mode string) []Package {
	var filtered []Package
	for _, p := range pkgs {
		isInstalled := strings.Contains(p.Status, "*")
		if (mode == "installed" && isInstalled) || (mode == "missing" && !isInstalled) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// --- FUNÇÕES DE SISTEMA, LIMPEZA E FIND ---

func findProvides(file string, searchRemote bool) {
	fmt.Printf("%s %s '%s'...\n", cyan("[vinstall]"), white("Procurando pacote que contém:"), yellow(file))

	fmt.Printf("%s %s %s\n", cyan(">>>"), cyan("grep local /var/db/xbps/.*-files.plist"), yellow(file))
	fmt.Printf("%s %s %s %s\n", cyan(">>>"), cyan("xbps-query"), cyan("-o"), yellow(file))

	xPath, xlocateErr := exec.LookPath("xlocate")
	if xlocateErr == nil {
		checkXlocateIndex()
		fmt.Printf("%s %s %s\n", cyan(">>>"), cyan("xlocate"), yellow(file))
	}
	if searchRemote {
		fmt.Printf("%s %s %s %s\n", cyan(">>>"), cyan("xbps-query"), cyan("-Ro"), yellow(file))
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	found := false
	var lines []string

	emit := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		found = true
		lines = append(lines, s)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if searchInLocalPlist(file) {
			mu.Lock()
			found = true
			mu.Unlock()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
		defer cancel()
		outLoc, _ := exec.CommandContext(ctx, "xbps-query", "-o", file).Output()
		if resLoc := strings.TrimSpace(string(outLoc)); resLoc != "" {
			emit(green(resLoc + " (instalado)"))
		}
	}()

	if xlocateErr == nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
			defer cancel()
			output, _ := exec.CommandContext(ctx, xPath, file).Output()
			res := strings.TrimSpace(string(output))
			if res == "" {
				return
			}
			seen := make(map[string]bool)
			mu.Lock()
			for _, line := range strings.Split(res, "\n") {
				if line != "" && !seen[line] {
					found = true
					lines = append(lines, green(line))
					seen[line] = true
				}
			}
			mu.Unlock()
		}()
	}

	if searchRemote {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
			defer cancel()
			output, _ := exec.CommandContext(ctx, "xbps-query", "-Ro", file).Output()
			if res := strings.TrimSpace(string(output)); res != "" {
				emit(green(res))
			}
		}()
	}

	wg.Wait()

	for _, l := range lines {
		fmt.Println(l)
	}
	if !found {
		fmt.Printf("%s %s\n", red("[!]"), white("Nenhum pacote encontrado. Use -FR para busca profunda."))
	}
}

func listLocal(mode string, query string) {
	var cmd *exec.Cmd
	switch mode {
	case "installed":
		fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Listando pacotes instalados:"))
		cmd = exec.Command("xbps-query", "-l")
	case "orphans":
		fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Listando pacotes órfãos:"))
		cmd = exec.Command("xbps-query", "-O")
	case "search":
		fmt.Printf("%s %s '%s'...\n", cyan("[vinstall]"), white("Buscando localmente por:"), yellow(query))
		cmd = exec.Command("xbps-query", "-l")
	}

	output, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", yellow("[!]"), white("falha ao executar xbps-query"), err)
		return
	}
	lines := strings.Split(string(output), "\n")

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()

	count := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		if mode == "search" && !strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
			continue
		}
		fmt.Fprintln(w, white(line))
		count++
	}
	fmt.Fprintf(w, "\n%s %s: %s\n", yellow("[!]"), white("Total:"), cyan(strconv.Itoa(count)))
}

func cleanXbpsCache() {
	cachePath := "/var/cache/xbps"
	if os.Geteuid() != 0 {
		fmt.Printf("%s %s\n", yellow("[vinstall]"), white("A limpeza do cache requer privilégios de root."))
		cmd := exec.Command("sudo", os.Args[0], "-Scc")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Run()
		return
	}

	files, err := os.ReadDir(cachePath)
	if err != nil {
		return
	}

	var pkgCount int
	var totalSize int64
	fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Iniciando limpeza do cache..."))

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		if strings.HasSuffix(name, ".xbps") || strings.HasSuffix(name, ".sig2") {
			info, err := file.Info()
			if err == nil {
				totalSize += info.Size()
				if os.Remove(filepath.Join(cachePath, name)) == nil && strings.HasSuffix(name, ".xbps") {
					pkgCount++
				}
			}
		}
	}

	fmt.Printf("\n%s %s\n", green("[✔]"), white("Limpeza concluída!"))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Removidos:"), cyan(strconv.Itoa(pkgCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Espaço livre:"), green(formatBytes(totalSize)))

	cmd := exec.Command("xbps-query", "-O")
	out, _ := cmd.Output()
	if orphans := strings.TrimSpace(string(out)); orphans != "" {
		fmt.Printf("%s %s\n%s\n", yellow("[!]"), white("Órfãos encontrados:"), cyan(orphans))
		fmt.Printf("%s ", white("Remover órfãos? [s/N]: "))
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(ans)); a == "s" || a == "sim" {
			runBinary("xbps-remove", []string{"-o"}, []string{})
		}
	}
}

func checkAndEnableService(pkgName string) {
	servicePath := filepath.Join("/etc/sv", pkgName)
	targetPath := filepath.Join("/var/service", pkgName)
	if info, err := os.Stat(servicePath); err == nil && info.IsDir() {
		if _, err := os.Lstat(targetPath); os.IsNotExist(err) {
			fmt.Printf("\n%s %s '%s'. Ativar? [s/N]: ", yellow("[!]"), white("Serviço disponível para"), cyan(pkgName))
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(input)); a == "s" || a == "sim" {
				exec.Command("sudo", "ln", "-s", servicePath, targetPath).Run()
			}
		}
	}
}

func showHistory() {
	logPath := "/var/log/socklog/xbps/current"
	file, err := os.Open(logPath)
	if err != nil {
		if os.IsPermission(err) {
			runBinary(os.Args[0], []string{"--history"}, []string{})
			return
		}
		return
	}
	defer file.Close()
	fmt.Printf("\n%s %s\n", cyan("[vinstall]"), white("Histórico:"))
	width := getTerminalWidth()
	fmt.Println(white(strings.Repeat("─", width)))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 25 {
			line = line[25:]
		}
		if strings.Contains(line, "installed") {
			fmt.Println(green(line))
		} else if strings.Contains(line, "removed") {
			fmt.Println(red(line))
		} else {
			fmt.Println(white(line))
		}
	}
	fmt.Println(white(strings.Repeat("─", width)))
}

func searchInLocalPlist(file string) bool {
	dbPath := "/var/db/xbps"
	entries, err := os.ReadDir(dbPath)
	if err != nil {
		return false
	}
	foundLocal := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-files.plist") {
			content, err := os.ReadFile(filepath.Join(dbPath, e.Name()))
			if err != nil {
				continue
			}
			data := string(content)
			if strings.Contains(data, file) {
				lines := strings.Split(data, "\n")
				for _, line := range lines {
					if strings.Contains(line, "<string>") && strings.Contains(line, file) {
						pkgName := e.Name()
						pkgName = strings.TrimPrefix(pkgName, ".")
						pkgName = strings.TrimSuffix(pkgName, "-files.plist")
						fmt.Println(green(pkgName + " (instalado localmente via plist)"))
						foundLocal = true
						break
					}
				}
			}
		}
	}
	return foundLocal
}

func checkXlocateIndex() {
	home, _ := os.UserHomeDir()
	indexPath := filepath.Join(home, ".cache/xlocate.git/FETCH_HEAD")
	info, err := os.Stat(indexPath)
	if err == nil {
		if time.Since(info.ModTime()).Hours() > 168 {
			fmt.Printf("%s %s\n", yellow("[TIP]"), white("Índice xlocate antigo. Considere 'xlocate -S'."))
		}
	}
}

func printUsage() {
	fmt.Printf("%s %s\n", white("vinstall"), cyan("v"+Version))
	fmt.Printf("%s\n\n", cyan(Copyright))
	fmt.Printf("%s vinstall [flags] <pacote>\n", yellow("Uso:"))
	fmt.Printf("%s\n\n", white("==> O vinstall aceita todas as flags nativas do xbps-install"))
	fmt.Println("Exemplos:")
	fmt.Printf("  %s %-15s\n", green("vinstall"), white("telegram"))
	fmt.Printf("  %s %-15s\n", green("vinstall"), white("-Syu"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-X"), white("pacote (Remover)"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-XR"), white("pacote (Remover recursivamente)"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-f"), white("pacote (Força reinstalação/downgrade)"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-F"), white("ifconfig (Busca local)"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-FR"), white("ifconfig (Busca remota)"))
	fmt.Println("\nAtalhos de Consulta:")
	fmt.Printf("  %-20s %s\n", green("-Li"), white("Lista todos os pacotes instalados"))
	fmt.Printf("  %-20s %s\n", green("-Lo"), white("Lista apenas pacotes órfãos"))
	fmt.Printf("  %-20s %s\n", green("-Ss <query>"), white("Busca termo nos repositórios (Rápido)"))
	fmt.Printf("  %-20s %s\n", green("-Sss <query>"), white("Busca detalhada nos repositórios (Full Text)"))
	fmt.Printf("  %-20s %s\n", green("-Ssi <query>"), white("Busca termo nos pacotes instalados"))
	fmt.Printf("  %-20s %s\n", green("-Ssu <query>"), white("Busca termo nos pacotes NÃO instalados"))
	fmt.Printf("  %-20s %s\n", green("-Sy"), white("Sincroniza índices e lista atualizações disponíveis"))
	fmt.Printf("  %-20s %s\n", green("-Syy"), white("Igual ao -Sy, mas força resync ignorando cache local"))
	fmt.Printf("  %-20s %s\n", green("-Sf"), white("Igual ao -Syy, mas mostra TODAS as diferenças (inclui downgrades)"))
	fmt.Printf("  %-20s %s\n", white("                    "), white("Downgrades exigem --force/-f para serem instalados:"))
	fmt.Printf("  %-20s %s\n", white("                    "), cyan("vinstall -f pacote-versao_revisao"))
	fmt.Println("\nRemoção:")
	fmt.Printf("  %-20s %s\n", green("-X"), white("Remove um pacote"))
	fmt.Printf("  %-20s %s\n", green("-XR"), white("Remove um pacote e suas dependências órfãs (recursivo)"))
	fmt.Printf("  %-20s %s\n", white("                    "), white("Qualquer sufixo após -X/-x é repassado como flag"))
	fmt.Printf("  %-20s %s\n", white("                    "), white("real para o xbps-remove."))
	fmt.Println("\nManutenção:")
	fmt.Printf("  %-20s %s\n", green("-Scc"), white("Limpa cache e órfãos"))
	fmt.Printf("  %-20s %s\n", green("--history"), white("Mostra histórico de transações"))
	fmt.Println()
}

/*
   CHANGELOG

   [1.6.5] - 2026-08-04
   Fixed:
     - REGRESSÃO da 1.6.4: a captura de stderr do xbps-install (runInstall)
       trocava cmd.Stderr de um fd real (os.Stderr) por um io.MultiWriter,
       o que força o Go a criar um pipe interno em vez de repassar o
       descritor de terminal para o processo filho. Isso quebrava
       ferramentas disparadas por hooks .hook (Type=Package,
       When=PostTransaction, ex.: "xbps-remove pacote") que decidem
       mostrar prompt de confirmação checando isatty(stderr): com o
       pipe, o xbps-remove via "não é terminal" e pulava a confirmação
       silenciosamente.
     - Revertido runBinary/uso no fluxo de instalação para stdio 100%
       herdado do terminal (igual pré-1.6.4). A detecção de "pacote não
       encontrado" (que decide se mostra sugestões) agora é feita por
       packageExistsInRepo(), um `xbps-query -R <pkgname>` rodado em
       processo totalmente separado DEPOIS que o xbps-install já
       terminou, sem tocar nos descritores dele.
     - Removidas runInstall() e isPackageNotFoundError() (introduzidas na
       1.6.4), substituídas por packageExistsInRepo().

   [1.6.4] - 2026-08-03
   Fixed:
     - Instalação de pacote-alvo (fluxo padrão) mostrava sugestões de
       pacotes parecidos (fetchSuggestions/displayMenu) para QUALQUER
       falha do xbps-install — inclusive espaço em disco insuficiente,
       conflito de arquivos ou permissão negada — e não apenas quando o
       pacote realmente não existia no pool de repositórios. Adicionada
       runInstall(), que capturava o stderr do xbps-install, e
       isPackageNotFoundError(), que só liberava as sugestões quando a
       mensagem "Package '<nome>' not found in repository pool." estava
       presente na saída. [Revertido na 1.6.5, ver acima.]

   [1.6.3] - 2026-07-24
   Fixed:
     - -X e -x deixaram de ser case exato no switch: agora usam HasPrefix,
       então qualquer sufixo (ex.: -XR, -xf) ativa mode=remove E repassa
       o sufixo como flag real pro xbps-remove ("-R" = recursivo, "-f" =
       força), igual ao passthrough já existente para o xbps-install.
       Antes, "-XR" caía no default como flag solta e nunca acionava o
       modo remove; um "vinstall -XR pacote" tentaria instalar em vez de
       remover.
   Changed:
     - --help atualizado com exemplo de -XR e nota explicando o
       passthrough de sufixos em -X/-x.

   [1.6.2] - 2026-07-24
   Changed:
     - Adicionados https://voidbr.org e https://voidlinux.com.br como
       Site: extras no cabeçalho do arquivo, junto do chililinux.com já
       existente.

   [1.6.1] - 2026-07-24
   Changed:
     - --help agora documenta explicitamente que downgrade é permitido no
       Void, mas exige --force/-f (ex.: "vinstall -f pacote"). Adicionado
       exemplo na seção de exemplos e nota logo abaixo do -Sf.
     - A mensagem final do -Sf, quando há downgrades na lista, agora
       explica isso diretamente e mostra o comando pra aplicar.

   [1.6.0] - 2026-07-24
   Changed:
     - -Sy, -Syy e -Sf agora têm comportamentos distintos e reais:
         -Sy:  sync normal + só lista atualizações reais (repo mais novo)
         -Syy: sync FORÇADO (--force, ignora cache local do repodata,
               espelhando o "-Syy" do pacman) + mesma listagem do -Sy
         -Sf:  sync FORÇADO + lista TODAS as diferenças de versão,
               incluindo downgrades (repo com versão menor que a
               instalada), marcados com [DOWNGRADE] em vermelho e
               excluídos do resumo de "total a baixar" recomendado
     - As três flags só ativam o modo sync do vinstall quando usadas SEM
       pacotes-alvo (ex.: "vinstall -Sy"). Se vier acompanhada de um
       pacote (ex.: "vinstall -Sy firefox"), segue o fluxo normal de
       instalação, repassando a flag como passthrough real para o
       xbps-install (sync + instala o pacote).

   [1.5.0] - 2026-07-24
   Added:
     - Coluna de repositório na listagem do -Sy, mostrando de qual repo
       vem cada atualização (mesma ideia do -Sss). Reaproveita o campo
       Package.Repo, que já existia mas não era exibido nesse modo.
     - -Syy agora funciona como alias de -Sy (mesmo comportamento).

   [1.4.3] - 2026-07-24
   Fixed:
     - -Sy listava pacotes como "atualização disponível" mesmo quando o
       repositório tinha uma versão MAIS ANTIGA que a instalada (mirror
       desatualizado), o que aparecia como um downgrade disfarçado de
       upgrade. Agora usa xbps-uhelper cmpver (comparador oficial de
       versões do XBPS) para só listar pacotes onde a versão do repo é
       de fato mais nova que a instalada.

   [1.4.2] - 2026-07-24
   Fixed:
     - Alinhamento do -Sy estendido para todas as colunas (nome, versão
       antiga, versão nova, tamanho), não só o nome. A largura de cada
       coluna agora é medida sobre o texto puro (sem cor) e o padding é
       aplicado antes de colorir — mesma técnica de displaySearch (-Ss).
       Versões são alinhadas à direita, nome e versão nova à esquerda.

   [1.4.1] - 2026-07-24
   Fixed:
     - Corrigido desalinhamento na listagem do -Sy: o padding (%-*s) estava
       sendo aplicado depois de colorir o nome do pacote com white(), então
       os códigos de escape ANSI eram contados como parte do comprimento
       visível, quebrando o alinhamento das colunas. Agora o nome é paddado
       primeiro (texto puro) e só depois recebe a cor.
     - Removidas entradas duplicadas na listagem do -Sy: pacotes presentes
       em mais de um repositório (ex.: repo principal + multilib) apareciam
       uma vez por repositório. Aplicado uniquePackagesExact() sobre a lista
       de atualizações, deduplicando por FullName (pacote+versão).

   [1.4.0] - 2026-07-24
   Added:
     - Nova flag -Sy: sincroniza os índices dos repositórios (xbps-install -S)
       e lista os pacotes com atualização disponível (versão instalada vs.
       versão no repositório, com tamanho total de download), sem instalar
       nada. Reaproveita o parser de repodata (readRepodataPlist).
     - getInstalledPkgverByName(): mapeia nome do pacote -> pkgver instalado,
       usado para comparação de versões no modo -Sy.
     - extractVersion(): extrai a parte de versão/revisão de um pkgver.

   [1.3.12] - 2026-07-24
   Fixed:
     - Corrigida duplicação da flag --ignore-file-conflicts: antes era
       adicionada duas vezes ao instalar pacotes com flags contendo "u"
       (ex.: -Syu). Agora é adicionada no máximo uma vez, cobrindo tanto
       o caso de update (-u) quanto instalação de pacotes-alvo em geral.
     - Substituído o parsing manual do repodata (offset fixo de 512 bytes)
       por leitura real via archive/tar (readRepodataPlist), evitando falha
       silenciosa quando o tar usa headers PAX estendidos.
     - Detecção de arquitetura (repodataArch): x86_64-repodata deixou de ser
       hardcoded; agora tenta detectar via `xbps-query -p architecture xbps`
       ou `uname -m`, com fallback para x86_64.
     - Erros de xbps-query (getInstalledPackages, getActiveRepos, listLocal)
       agora são reportados em stderr em vez de ignorados silenciosamente.
*/
