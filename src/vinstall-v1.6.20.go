/*
   xbps-query -R -p run_depends -s sdbus-cpp

   voidbr-vinstall
   Wrapper para o Void xbps-query e xbps-install

   Site:      https://chililinux.com
   Site:      https://voidbr.org
   Site:      https://voidlinux.com.br
   GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

   Created:   ter 03 fev 2026 13:08:22 -04
   Updated:   qua 21 out 2026 00:50:00 -04
   Version:   1.6.20
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
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
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
	Version   = "1.6.20"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
	// CopyrightTeam aparece no --help
	CopyrightTeam = "Copyright (c) 2026-2026, VoidBR Development Team <https://voidbr.org>"
	execTimeout   = 10 * time.Second
	remoteTimeout = 2 * time.Minute // -FR (xlocate / xbps-query -Ro) é lento; 10s cortava a busca
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
	findMode := "exact" // "exact" | "any" | "regex" (-F/-FR, -Fa/-FRa, -Fx/-FRx)
	filter := ""
	syncFlavor := "" // "sy" | "syy" | "sf" — só vira modo "sync" se não houver targets

	fmt.Print("\033[36m")
	defer fmt.Print("\033[0m")

	for i := 0; i < len(args); i++ {
		arg := args[i]
		// Opções longas do xbps-install que recebem valor separado por
		// espaço (ex.: "--parallel-downloads 8"): junta opção e valor em
		// "--opcao=valor", para o valor não virar nome de pacote.
		if valueOptions[arg] && i+1 < len(args) {
			i++
			flags = append(flags, arg+"="+args[i])
			continue
		}
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
		case "-Sc":
			mode = "clean-light"
		case "-Scc":
			mode = "clean"
		case "-F":
			mode = "find"
		case "-FR":
			mode = "find"
			searchRemote = true
		case "-Fa":
			mode = "find"
			findMode = "any"
		case "-FRa":
			mode = "find"
			searchRemote = true
			findMode = "any"
		case "-Fx":
			mode = "find"
			findMode = "regex"
		case "-FRx":
			mode = "find"
			searchRemote = true
			findMode = "regex"
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
	case "clean-light":
		cleanXbpsCacheLight()
	case "find":
		if len(targets) > 0 {
			findProvides(targets[0], searchRemote, findMode)
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
			before := getInstalledPkgverByName()
			runBinary("xbps-remove", flags, targets)
			printRemovalSummary(removedPackages(before, getInstalledPkgverByName()), targets)
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
				// em "--opcao=valor" só o nome da opção conta, para um
				// valor como "--repository=https://...linux..." não
				// ativar a flag por engano
				if strings.HasPrefix(f, "--") {
					f, _, _ = strings.Cut(f, "=")
				}
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

// valueOptions são as opções longas do xbps-install que recebem um valor.
// Na forma "--opcao valor" o vinstall junta as duas em "--opcao=valor"
// (ver main). As formas curtas (-R, -r, -C, -c) ficam de fora porque o
// vinstall já usa -R e outras letras em atalhos próprios (-XR, -FR).
var valueOptions = map[string]bool{
	"--parallel-downloads": true,
	"--repository":         true,
	"--rootdir":            true,
	"--config":             true,
	"--cachedir":           true,
}

// parallelDownloadsMax é o limite do xbps-install (XBPS_PARALLEL_DOWNLOADS_MAX).
const parallelDownloadsMax = 16

// parallelDownloadsFromEnv converte XBPS_PARALLEL_DOWNLOADS em
// "--parallel-downloads=<n>" para o xbps-install. A variável não chega
// sozinha ao xbps-install porque o sudo limpa o ambiente (env_reset).
// Se a linha de comando já tiver --parallel-downloads, ela vale (mesma
// prioridade do xbps-install). Valor inválido é avisado e ignorado.
func parallelDownloadsFromEnv(flags []string) []string {
	val, ok := os.LookupEnv("XBPS_PARALLEL_DOWNLOADS")
	if !ok || val == "" {
		return flags
	}
	for _, f := range flags {
		if f == "--parallel-downloads" || strings.HasPrefix(f, "--parallel-downloads=") {
			return flags
		}
	}
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || n < 1 || n > parallelDownloadsMax {
		fmt.Printf("%s %s\n", yellow("[!]"),
			white(fmt.Sprintf("ignorando XBPS_PARALLEL_DOWNLOADS inválido `%s' (deve ser 1-%d)", val, parallelDownloadsMax)))
		return flags
	}
	out := append([]string{}, flags...)
	return append(out, fmt.Sprintf("--parallel-downloads=%d", n))
}

func runBinary(bin string, flags []string, pkgs []string) bool {
	if bin == "xbps-install" {
		flags = parallelDownloadsFromEnv(flags)
	}
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

// removedPackages compara dois snapshots de getInstalledPkgverByName()
// (antes e depois de um xbps-remove) e retorna os pkgver que sumiram.
// É a contagem real do que foi removido, e não uma previsão feita antes.
func removedPackages(before, after map[string]string) []string {
	var removed []string
	for name, pkgver := range before {
		if _, ok := after[name]; !ok {
			removed = append(removed, pkgver)
		}
	}
	sort.Strings(removed)
	return removed
}

// printRemovalSummary mostra quantos pacotes saíram num -X/-XR e quantos
// deles eram órfãos (removidos sem terem sido pedidos como alvo). O alvo
// pode ter sido informado pelo nome ("firefox") ou pelo pkgver completo.
func printRemovalSummary(removed []string, targets []string) {
	if len(removed) == 0 {
		return
	}
	asked := make(map[string]bool)
	for _, t := range targets {
		asked[t] = true
	}
	orphanCount := 0
	for _, pkgver := range removed {
		if !asked[pkgver] && !asked[cleanVersion(pkgver)] {
			orphanCount++
		}
	}
	fmt.Printf("\n%s %s %s\n", yellow("[!]"), white("Pacotes removidos:"), cyan(strconv.Itoa(len(removed))))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Órfãos removidos:"), cyan(strconv.Itoa(orphanCount)))
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

// extractPkgverFromCacheFile extrai o pkgver a partir do nome de um arquivo
// de cache do xbps (ex.: "firefox-128.0_1.x86_64.xbps" -> "firefox-128.0_1").
// O padrão do xbps é "<pkgver>.<arch>.xbps", então basta remover o sufixo
// ".xbps" e cortar no último ponto restante (a arquitetura não tem pontos).
func extractPkgverFromCacheFile(name string) string {
	base := strings.TrimSuffix(name, ".xbps")
	idx := strings.LastIndex(base, ".")
	if idx == -1 {
		return ""
	}
	return base[:idx]
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

// fileQuery é o termo do -F/-FR, no estilo do "pacman -F". Modos:
//   - "exact" (-F/-FR): o termo em qualquer parte do NOME do arquivo, sem
//     olhar os diretórios ("ifconfig" casa /usr/bin/ifconfig e
//     .../man8/ifconfig.8, mas não /usr/share/ifconfig-doc/README). Com
//     "/" no termo, vale o caminho todo ("bin/ifconfig");
//   - "any" (-Fa/-FRa): o termo em QUALQUER parte do caminho;
//   - "regex" (-Fx/-FRx): expressão regular aplicada ao caminho completo.
type fileQuery struct {
	term  string
	mode  string
	regex bool
	re    *regexp.Regexp
}

func newFileQuery(term, mode string) (fileQuery, error) {
	q := fileQuery{term: term, mode: mode, regex: mode == "regex"}
	if q.regex {
		re, err := regexp.Compile(term)
		if err != nil {
			return q, err
		}
		q.re = re
	}
	return q, nil
}

// hasDir: o termo tem "/", ou seja, fala de diretório também.
func (q fileQuery) hasDir() bool {
	return strings.Contains(q.term, "/")
}

// match diz se um caminho casa com o termo.
func (q fileQuery) match(path string) bool {
	if q.regex {
		return q.re.MatchString(path)
	}
	if q.mode == "any" || q.hasDir() {
		return strings.Contains(path, q.term)
	}
	return strings.Contains(filepath.Base(path), q.term)
}

// ownedArgs: argumentos do xbps-query -o. O -o compara o padrão com o
// caminho COMPLETO (fnmatch); "*/nome" casa o nome em qualquer diretório.
// No modo regex, usa o --regex (ERE) do próprio xbps-query.
func (q fileQuery) ownedArgs() []string {
	if q.regex {
		return []string{"--regex", "-o", q.term}
	}
	// "*termo*" pega também diretórios; no modo exato, findLocal filtra
	// depois pelo nome do arquivo (match)
	return []string{"-o", "*" + q.term + "*"}
}

// xlocateArgs: argumentos do xlocate (git grep sobre os caminhos).
// Modo exato: "termo[^/]*$", ou seja, o termo dentro do último segmento
// (nome do arquivo); modo "any" ou termo com "/": texto fixo (-F).
func (q fileQuery) xlocateArgs() []string {
	if q.regex {
		return []string{"-E", q.term}
	}
	if q.mode == "any" || q.hasDir() {
		return []string{"-F", q.term}
	}
	return []string{"-E", regexp.QuoteMeta(q.term) + "[^/]*$"}
}

// findProvides: -F busca só nos pacotes instalados; -FR só nos repositórios.
// O modo (exact/any/regex) define como o termo casa com o caminho.
func findProvides(term string, searchRemote bool, mode string) {
	q, err := newFileQuery(term, mode)
	if err != nil {
		fmt.Printf("%s %s: %v\n", red("[!]"), white("expressão regular inválida"), err)
		return
	}
	if searchRemote {
		findRemote(q)
	} else {
		findLocal(q)
	}
}

// ownedLinePath extrai o caminho de uma linha do xbps-query -o
// ("pkgver: /caminho (pkgver)" ou "pkgver: /link -> /alvo (pkgver)").
func ownedLinePath(line string) string {
	_, rest, ok := strings.Cut(line, ": ")
	if !ok {
		return ""
	}
	if i := strings.LastIndex(rest, " ("); i != -1 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, " -> "); i != -1 {
		rest = rest[:i]
	}
	return rest
}

// fileRow é uma linha do resultado do -F/-FR: pacote, caminho e se o
// pacote está instalado.
type fileRow struct {
	pkg       string
	path      string
	installed bool
}

// addFileRow acrescenta a linha se o par pacote+caminho ainda não apareceu
// (o plist e o xbps-query -o, ou dois índices xlocate, repetem resultados).
func addFileRow(rows []fileRow, seen map[string]bool, r fileRow) []fileRow {
	key := r.pkg + "\x00" + r.path
	if r.pkg == "" || r.path == "" || seen[key] {
		return rows
	}
	seen[key] = true
	return append(rows, r)
}

// printFileRows mostra o resultado do -F/-FR numa tabela única, no estilo
// do "pacman -F": [✓]/[-]  pacote  caminho. Padding sobre o texto puro e
// cor depois, para o ANSI não quebrar o alinhamento.
func printFileRows(rows []fileRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].pkg != rows[j].pkg {
			return rows[i].pkg < rows[j].pkg
		}
		return rows[i].path < rows[j].path
	})
	maxPkg := 0
	pkgs := map[string]bool{}
	for _, r := range rows {
		if len(r.pkg) > maxPkg {
			maxPkg = len(r.pkg)
		}
		pkgs[r.pkg] = true
	}
	if maxPkg > 50 {
		maxPkg = 50
	}

	width := getTerminalWidth()
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprintln(w, white(strings.Repeat("─", width)))
	for _, r := range rows {
		status := red("[-]")
		if r.installed {
			status = green_bold("[✓]")
		}
		fmt.Fprintf(w, "%s %s  %s\n", status, white(fmt.Sprintf("%-*s", maxPkg, r.pkg)), green(r.path))
	}
	fmt.Fprintln(w, white(strings.Repeat("─", width)))
	fmt.Fprintf(w, "%s %s %s %s %s\n", yellow("[!]"), cyan(strconv.Itoa(len(rows))), white("arquivo(s) em"), cyan(strconv.Itoa(len(pkgs))), white("pacote(s)"))
}

// -F: só pacotes instalados — plist local (/var/db/xbps/.*-files.plist) + xbps-query -o
func findLocal(q fileQuery) {
	fmt.Printf("%s %s '%s' %s\n", cyan("[vinstall]"), white("Procurando"), yellow(q.term), white("nos pacotes instalados:"))

	var rows []fileRow
	seen := map[string]bool{}

	// o plist dá só o nome do pacote; usa a versão instalada para casar
	// com as linhas do xbps-query -o
	pkgver := getInstalledPkgverByName()
	fmt.Printf("%s %s %s\n", cyan(">>>"), cyan("grep local /var/db/xbps/.*-files.plist"), yellow(q.term))
	for _, r := range searchInLocalPlist(q) {
		if v, ok := pkgver[r.pkg]; ok {
			r.pkg = v
		}
		rows = addFileRow(rows, seen, r)
	}

	args := q.ownedArgs()
	fmt.Printf("%s %s %s\n", cyan(">>>"), cyan("xbps-query"), yellow(strings.Join(args, " ")))
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "xbps-query", args...).Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg, _, ok := strings.Cut(line, ": ")
		path := ownedLinePath(line)
		if !ok || path == "" {
			continue
		}
		// confere o caminho: no modo exato descarta o que casou só no
		// diretório; no modo regex, vale o --regex do próprio xbps
		if !q.regex && !q.match(path) {
			continue
		}
		rows = addFileRow(rows, seen, fileRow{pkg: pkg, path: path, installed: true})
	}

	if len(rows) == 0 {
		fmt.Printf("%s %s\n", red("[!]"), white("Nenhum pacote instalado contém esse arquivo. Use -FR para buscar nos repositórios."))
		return
	}
	printFileRows(rows)
}

// xlocateIndex descreve um índice de arquivos do xlocate.
// repos vazio = origem padrão do xlocate (índice oficial do Void).
// Com mais de um repo, os seguintes são reserva: o xlocate -S tenta um
// por vez, na ordem, até um responder.
type xlocateIndex struct {
	name  string   // nome exibido
	repos []string // XLOCATE_REPO, em ordem de preferência
	git   string   // XLOCATE_GIT (cópia local)
}

// xlocateDefaultRepo é a origem padrão do xlocate (índice oficial do Void).
const xlocateDefaultRepo = "https://repo-default.voidlinux.org/xlocate/xlocate.git"

// userCacheDir retorna ${XDG_CACHE_HOME:-~/.cache}.
func userCacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache")
}

// xlocateRemote é um índice xlocate encontrado num servidor: URL final
// (depois de redirects) e o commit do branch master.
type xlocateRemote struct {
	url  string
	hash string
}

// xlocateIndexes descobre os índices a partir dos repositórios do xbps.d.
// Convenção: o índice de um repositório fica DENTRO dele, em
// <repo>/xlocate/xlocate.git (é onde o voidbr-xlocate-index grava). Assim
// o índice acompanha o repositório em qualquer host ou mirror. Para
// repositórios http(s), também tenta subir pelo caminho da URL (ex.:
// https://host/a/b -> host/a/b/xlocate, host/a/xlocate, host/xlocate).
// Repositórios locais (diretório) são lidos direto do disco. Servidores com o MESMO índice (mesmo
// commit: um mirror do outro, ex.: voidbr.org e voidlinux.com.br) viram
// um índice só, com as URLs como origens alternativas no xlocate -S —
// assim os resultados não se repetem e, se um servidor cair, usa o outro.
// O índice oficial do Void (~/.cache/xlocate.git) entra sempre.
func xlocateIndexes() []xlocateIndex {
	cacheDir := userCacheDir()
	remotes := discoverXlocateRemotes(cacheDir)

	void := xlocateIndex{name: "Void", git: filepath.Join(cacheDir, "xlocate.git"), repos: []string{xlocateDefaultRepo}}
	voidHash := ""
	for _, r := range remotes {
		if r.url == xlocateDefaultRepo {
			voidHash = r.hash
		}
	}

	indexes := []xlocateIndex{void}
	byHash := map[string]int{} // hash -> posição em indexes
	if voidHash != "" {
		byHash[voidHash] = 0
	}
	for _, r := range remotes {
		if r.url == xlocateDefaultRepo {
			continue
		}
		if i, ok := byHash[r.hash]; ok {
			indexes[i].repos = append(indexes[i].repos, r.url)
			if i != 0 {
				indexes[i].name += " | " + xlocateHost(r.url)
			}
			continue
		}
		byHash[r.hash] = len(indexes)
		indexes = append(indexes, xlocateIndex{
			name:  xlocateHost(r.url),
			repos: []string{r.url},
			git:   filepath.Join(cacheDir, "xlocate-"+xlocateCacheName(r.url)+".git"),
		})
	}
	return indexes
}

// xlocateHost extrai o host de uma URL, para exibição. Para repositório
// local, retorna o próprio caminho.
func xlocateHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// xlocateCacheName gera o nome da cópia local a partir de host+caminho,
// para que dois índices no mesmo host não usem o mesmo diretório.
// Ex.: https://void.voidbr.org/voidlinux/extra/xlocate/xlocate.git ->
// void_voidbr_org_voidlinux_extra
func xlocateCacheName(raw string) string {
	name := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		name = u.Host + u.Path
	}
	name = strings.TrimSuffix(name, "/xlocate/xlocate.git")
	name = strings.Trim(name, "/")
	return strings.NewReplacer(".", "_", ":", "_", "/", "_").Replace(name)
}

// isVoidOfficialRepo: repositórios oficiais do Void (e seus mirrors) usam o
// layout <base>/current[/...]. O índice deles é o oficial, que já entra
// sempre; procurar índice em mirror só baixaria uma cópia repetida e
// possivelmente atrasada.
func isVoidOfficialRepo(path string) bool {
	path = "/" + strings.Trim(path, "/") + "/"
	return strings.Contains(path, "/current/")
}

// discoverXlocateRemotes lista os índices xlocate dos repositórios do
// xbps.d. O resultado fica em ~/.cache/vinstall/xlocate-indexes por 24h,
// para não consultar os servidores a cada busca; se a lista de
// repositórios mudar, descobre de novo na hora. Sem rede, reaproveita a
// última lista salva.
func discoverXlocateRemotes(cacheDir string) []xlocateRemote {
	cacheFile := filepath.Join(cacheDir, "vinstall", "xlocate-indexes")

	var repos []string
	out, _ := exec.Command("xbps-query", "-L").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 {
			repos = append(repos, fields[1])
		}
	}
	signature := "repos\t" + strings.Join(repos, " ")

	if info, err := os.Stat(cacheFile); err == nil && time.Since(info.ModTime()) < 24*time.Hour {
		if sig, remotes := readXlocateRemotes(cacheFile); sig == signature {
			return remotes
		}
	}

	fmt.Printf("%s %s\n", cyan(">>>"), cyan("procurando índices xlocate nos repositórios do xbps.d"))

	var remotes []xlocateRemote
	seen := map[string]bool{}
	add := func(r xlocateRemote) {
		if !seen[r.url] {
			seen[r.url] = true
			remotes = append(remotes, r)
		}
	}

	// candidatos http(s), na ordem dos repositórios; o oficial entra primeiro
	var candidates [][]string
	candidates = append(candidates, []string{xlocateDefaultRepo})
	var local []xlocateRemote
	for _, repo := range repos {
		if strings.HasPrefix(repo, "/") {
			// repositório local: índice direto do disco
			if r, ok := probeXlocateLocal(filepath.Join(repo, "xlocate", "xlocate.git")); ok {
				local = append(local, r)
			}
			continue
		}
		u, err := url.Parse(repo)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || isVoidOfficialRepo(u.Path) {
			continue
		}
		var list []string
		path := strings.Trim(u.Path, "/")
		for {
			base := u.Scheme + "://" + u.Host
			if path != "" {
				base += "/" + path
			}
			list = append(list, base+"/xlocate/xlocate.git")
			if path == "" {
				break
			}
			if i := strings.LastIndex(path, "/"); i != -1 {
				path = path[:i]
			} else {
				path = ""
			}
		}
		candidates = append(candidates, list)
	}

	client := &http.Client{Timeout: execTimeout}
	results := make([]*xlocateRemote, len(candidates))
	var wg sync.WaitGroup
	for i, list := range candidates {
		wg.Add(1)
		go func(i int, list []string) {
			defer wg.Done()
			for _, cand := range list {
				if r, ok := probeXlocateRemote(client, cand); ok {
					results[i] = &r
					return
				}
			}
		}(i, list)
	}
	wg.Wait()

	for _, r := range results {
		if r != nil {
			add(*r)
		}
	}
	for _, r := range local {
		add(r)
	}

	if len(remotes) == 0 {
		_, old := readXlocateRemotes(cacheFile) // sem rede: usa a última lista salva
		return old
	}
	var buf strings.Builder
	buf.WriteString(signature + "\n")
	for _, r := range remotes {
		fmt.Fprintf(&buf, "%s\t%s\n", r.hash, r.url)
	}
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err == nil {
		os.WriteFile(cacheFile, []byte(buf.String()), 0o644)
	}
	return remotes
}

// probeXlocateLocal confere um índice xlocate num diretório local (repo
// git bare) e retorna o commit do master.
func probeXlocateLocal(gitDir string) (xlocateRemote, bool) {
	out, err := exec.Command("git", "--git-dir="+gitDir, "rev-parse", "--verify", "-q", "refs/heads/master").Output()
	hash := strings.TrimSpace(string(out))
	if err != nil || len(hash) != 40 {
		return xlocateRemote{}, false
	}
	return xlocateRemote{url: gitDir, hash: hash}, true
}

// readXlocateRemotes lê a lista salva: primeira linha "repos<TAB>..." com
// os repositórios usados na descoberta, depois hash<TAB>url por linha.
func readXlocateRemotes(cacheFile string) (string, []xlocateRemote) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return "", nil
	}
	signature := ""
	var remotes []xlocateRemote
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "repos\t") {
			signature = line
			continue
		}
		hash, u, ok := strings.Cut(line, "\t")
		if ok && hash != "" && u != "" {
			remotes = append(remotes, xlocateRemote{url: u, hash: hash})
		}
	}
	return signature, remotes
}

// probeXlocateRemote confere se existe um índice xlocate (repositório git
// servido por HTTP) em cand, lendo cand/info/refs. Retorna a URL final,
// depois de redirects (ex.: voidbr.org -> www.voidbr.org), e o commit do
// master.
func probeXlocateRemote(client *http.Client, cand string) (xlocateRemote, bool) {
	resp, err := client.Get(cand + "/info/refs")
	if err != nil {
		return xlocateRemote{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return xlocateRemote{}, false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "refs/heads/master" && len(fields[0]) == 40 &&
			strings.Trim(fields[0], "0123456789abcdef") == "" {
			final := strings.TrimSuffix(resp.Request.URL.String(), "/info/refs")
			return xlocateRemote{url: final, hash: fields[0]}, true
		}
	}
	return xlocateRemote{}, false
}

// env monta o ambiente do xlocate apontando para este índice e, se repo
// não for vazio, para essa origem.
func (x xlocateIndex) env(repo string) []string {
	env := append(os.Environ(), "XLOCATE_GIT="+x.git)
	if repo != "" {
		env = append(env, "XLOCATE_REPO="+repo)
	}
	return env
}

// -FR: repositórios, via xlocate, nos índices do Void e do VoidBR.
// O xbps-query -Ro NÃO é usado: em modo repositório ele baixa a lista de
// arquivos de cada pacote pela rede, um por um, e não termina em tempo útil.
// Sem resultado no xlocate, cai no xbps-query -Rs, que procura pelo NOME do
// pacote em todos os repositórios configurados.
func findRemote(q fileQuery) {
	fmt.Printf("%s %s '%s' %s\n", cyan("[vinstall]"), white("Procurando"), yellow(q.term), white("nos repositórios remotos:"))

	var rows []fileRow
	seen := map[string]bool{}
	installed := getInstalledPkgverByName()
	if xPath, err := exec.LookPath("xlocate"); err == nil {
		for _, idx := range xlocateIndexes() {
			ensureXlocateIndex(xPath, idx)
			args := q.xlocateArgs()
			fmt.Printf("%s %s %s %s\n", cyan(">>>"), cyan("xlocate"), yellow(strings.Join(args, " ")), cyan("("+idx.name+")"))
			ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
			cmd := exec.CommandContext(ctx, xPath, args...)
			cmd.Env = idx.env("")
			out, _ := cmd.Output()
			cancel()
			// saída do xlocate: "pkgver  caminho" (colunas separadas por espaços)
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				pkg := fields[0]
				path := strings.TrimSpace(strings.TrimPrefix(line, pkg))
				_, isInst := installed[cleanVersion(pkg)]
				rows = addFileRow(rows, seen, fileRow{pkg: pkg, path: path, installed: isInst})
			}
		}
	} else {
		fmt.Printf("%s %s\n", yellow("[!]"), white("xlocate não encontrado (pacote xtools); busca por arquivo nos repositórios indisponível."))
	}

	if len(rows) > 0 {
		printFileRows(rows)
		return
	}
	if q.regex {
		fmt.Printf("%s %s\n", red("[!]"), white("Nenhum arquivo encontrado nos repositórios."))
		return
	}

	fmt.Printf("%s %s %s %s\n", cyan(">>>"), cyan("xbps-query"), cyan("-Rs"), yellow(q.term))
	pkgs := uniquePackagesExact(fetchSuggestions(q.term))
	if len(pkgs) == 0 {
		fmt.Printf("%s %s\n", red("[!]"), white("Nenhum pacote encontrado nos repositórios."))
		return
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].FullName < pkgs[j].FullName })
	displaySearch(pkgs, "Nenhum arquivo encontrado; pacotes com esse nome:")
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

	installed := getInstalledPackages()           // pkgver -> true
	installedByName := getInstalledPkgverByName() // nome -> pkgver

	var pkgCount, notInstalledCount, oldCount int
	var totalSize int64
	fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Iniciando limpeza do cache..."))

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		isPkg := strings.HasSuffix(name, ".xbps")
		if !isPkg && !strings.HasSuffix(name, ".sig2") {
			continue
		}
		info, err := file.Info()
		if err != nil {
			continue
		}
		if os.Remove(filepath.Join(cachePath, name)) != nil {
			continue
		}
		totalSize += info.Size()
		if !isPkg {
			continue
		}
		pkgCount++
		// Classifica pelo NOME do pacote, não pelo pkgver exato: um pacote
		// instalado em outra versão é "versão antiga", não "não instalado".
		pkgver := extractPkgverFromCacheFile(name)
		if pkgver == "" || installed[pkgver] {
			continue
		}
		if _, ok := installedByName[cleanVersion(pkgver)]; ok {
			oldCount++
		} else {
			notInstalledCount++
		}
	}

	// Os órfãos são removidos ANTES do resumo, e a contagem vem da
	// diferença real entre os pacotes instalados antes e depois do
	// xbps-remove. Se a remoção falhar, o resumo mostra 0, não a previsão
	// do xbps-query -O.
	// -y: o -Scc promete limpeza sem perguntar; sem ele o xbps-remove
	// pedia "Do you want to continue? [Y/n]" quando havia órfãos.
	orphanCount := 0
	out, _ := exec.Command("xbps-query", "-O").Output()
	if strings.TrimSpace(string(out)) != "" {
		before := getInstalledPkgverByName()
		runBinary("xbps-remove", []string{"-o", "-y"}, []string{})
		orphanCount = len(removedPackages(before, getInstalledPkgverByName()))
	}

	fmt.Printf("\n%s %s\n", green("[✔]"), white("Limpeza concluída!"))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Removidos:"), cyan(strconv.Itoa(pkgCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Não instalados removidos:"), cyan(strconv.Itoa(notInstalledCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Versões antigas removidas:"), cyan(strconv.Itoa(oldCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Órfãos removidos:"), cyan(strconv.Itoa(orphanCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Espaço liberado:"), green(formatBytes(totalSize)))
}

// cleanXbpsCacheLight (-Sc) é a limpeza leve do cache, no estilo do
// "pacman -Sc": mantém em /var/cache/xbps os arquivos da versão que está
// instalada de cada pacote e remove o resto — pacotes que não estão
// instalados e versões antigas de pacotes instalados. Não mexe em órfãos;
// para limpeza total (cache inteiro + órfãos) use -Scc.
func cleanXbpsCacheLight() {
	cachePath := "/var/cache/xbps"
	if os.Geteuid() != 0 {
		fmt.Printf("%s %s\n", yellow("[vinstall]"), white("A limpeza do cache requer privilégios de root."))
		cmd := exec.Command("sudo", os.Args[0], "-Sc")
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

	installed := getInstalledPackages()           // pkgver -> true
	installedByName := getInstalledPkgverByName() // nome -> pkgver

	var pkgCount, notInstalledCount, oldCount, keptCount int
	var totalSize int64
	fmt.Printf("%s %s\n", cyan("[vinstall]"), white("Iniciando limpeza leve do cache..."))

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		isPkg := strings.HasSuffix(name, ".xbps")
		if !isPkg && !strings.HasSuffix(name, ".sig2") {
			continue
		}
		// A assinatura (.xbps.sig2) segue o mesmo destino do pacote.
		pkgver := extractPkgverFromCacheFile(strings.TrimSuffix(name, ".sig2"))
		if pkgver == "" || installed[pkgver] {
			if isPkg && pkgver != "" {
				keptCount++
			}
			continue
		}
		info, err := file.Info()
		if err != nil {
			continue
		}
		if os.Remove(filepath.Join(cachePath, name)) != nil {
			continue
		}
		totalSize += info.Size()
		if isPkg {
			pkgCount++
			if _, ok := installedByName[cleanVersion(pkgver)]; ok {
				oldCount++
			} else {
				notInstalledCount++
			}
		}
	}

	fmt.Printf("\n%s %s\n", green("[✔]"), white("Limpeza concluída!"))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Removidos:"), cyan(strconv.Itoa(pkgCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Não instalados removidos:"), cyan(strconv.Itoa(notInstalledCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Versões antigas removidas:"), cyan(strconv.Itoa(oldCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Mantidos (instalados):"), cyan(strconv.Itoa(keptCount)))
	fmt.Printf("%s %s %s\n", yellow("[!]"), white("Espaço liberado:"), green(formatBytes(totalSize)))
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

// searchInLocalPlist procura nos arquivos /var/db/xbps/.<pkg>-files.plist
// dos pacotes instalados e devolve um fileRow (nome do pacote + caminho)
// para cada caminho que casa com o termo.
func searchInLocalPlist(q fileQuery) []fileRow {
	dbPath := "/var/db/xbps"
	entries, err := os.ReadDir(dbPath)
	if err != nil {
		return nil
	}
	var rows []fileRow
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "-files.plist") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dbPath, e.Name()))
		if err != nil {
			continue
		}
		pkgName := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "."), "-files.plist")
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "<string>/") || !strings.HasSuffix(line, "</string>") {
				continue
			}
			path := strings.TrimSuffix(strings.TrimPrefix(line, "<string>"), "</string>")
			if q.match(path) {
				rows = append(rows, fileRow{pkg: pkgName, path: path, installed: true})
			}
		}
	}
	return rows
}

// ensureXlocateIndex atualiza um índice do xlocate (xlocate -S) quando ele
// não existe ou tem mais de 24h. O xlocate -S grava FETCH_HEAD a cada sync.
// Tenta as origens na ordem; se uma falhar (fora do ar), passa para a
// próxima. Se todas falharem, a busca segue com a cópia local que existir.
func ensureXlocateIndex(xPath string, idx xlocateIndex) {
	info, err := os.Stat(filepath.Join(idx.git, "FETCH_HEAD"))
	if err == nil && time.Since(info.ModTime()) < 24*time.Hour {
		return
	}

	repos := idx.repos
	if len(repos) == 0 {
		repos = []string{""} // origem padrão do xlocate
	}
	for i, repo := range repos {
		label := idx.name
		if repo != "" {
			label += ": " + repo
		}
		fmt.Printf("%s %s %s\n", cyan(">>>"), cyan("xlocate -S"), cyan("("+label+")"))

		ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
		cmd := exec.CommandContext(ctx, xPath, "-S")
		cmd.Env = idx.env(repo)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		cancel()
		if err == nil {
			return
		}
		fmt.Printf("%s %s (%s): %v\n", yellow("[!]"), white("falha ao atualizar o índice do xlocate"), label, err)
		if i < len(repos)-1 {
			fmt.Printf("%s %s\n", yellow("[!]"), white("Tentando a próxima origem..."))
		}
	}
}

// usageColW é a largura da coluna das opções no --help.
const usageColW = 28

// usageOpt imprime uma linha "  opção   descrição" alinhada. O preenchimento
// é calculado no texto puro, antes de colorir: com %-20s sobre o texto já
// colorido, os códigos ANSI contavam como caracteres e desalinhavam tudo.
// Opção maior que a coluna desce a descrição para a linha de baixo.
func usageOpt(opt, desc string) {
	n := len([]rune(opt))
	if n >= usageColW {
		fmt.Printf("  %s\n  %s%s\n", green(opt), strings.Repeat(" ", usageColW), white(desc))
		return
	}
	fmt.Printf("  %s%s%s\n", green(opt), strings.Repeat(" ", usageColW-n), white(desc))
}

// usageMore continua a descrição da linha anterior.
func usageMore(desc string) {
	fmt.Printf("  %s%s\n", strings.Repeat(" ", usageColW), white(desc))
}

func usageSection(title string) {
	fmt.Printf("\n%s\n", yellow(title))
}

func printUsage() {
	fmt.Printf("%s %s - %s\n", white("vinstall"), cyan("v"+Version), white("wrapper para xbps-install, xbps-query e xbps-remove"))
	fmt.Printf("%s\n\n", cyan(CopyrightTeam))
	fmt.Printf("%s vinstall [opções] [pacote...]\n", yellow("Uso:"))
	fmt.Printf("     %s\n", white("==> O vinstall aceita todas as flags nativas do xbps-install"))
	fmt.Printf("     %s\n", white("Opções que o vinstall não conhece são repassadas ao xbps-install."))

	usageSection("Instalar e atualizar:")
	usageOpt("<pacote...>", "Instala; se o nome não existir, sugere pacotes")
	usageOpt("-Syu", "Sincroniza e atualiza o sistema")
	usageOpt("-f <pacote>", "Força reinstalação ou downgrade")
	usageOpt("-y", "Responde sim a tudo")
	usageOpt("--parallel-downloads <n>", "Downloads simultâneos (1-16); aceita também =<n>")

	usageSection("Sincronizar:")
	usageOpt("-Sy", "Sincroniza índices e lista as atualizações")
	usageOpt("-Syy", "Igual ao -Sy, forçando o resync")
	usageOpt("-Sf", "Igual ao -Syy, listando também downgrades")
	usageMore("(downgrade: vinstall -f pacote-versao_revisao)")

	usageSection("Buscar:")
	usageOpt("-Ss <termo>", "Nos repositórios (rápido)")
	usageOpt("-Sss <termo>", "Nos repositórios, detalhado (texto completo)")
	usageOpt("-Ssi <termo>", "Nos repositórios, só os instalados")
	usageOpt("-Ssu <termo>", "Nos repositórios, só os NÃO instalados")
	usageOpt("-Qs <termo>", "Nos pacotes instalados")
	usageOpt("-F <arquivo>", "Qual pacote instalado tem o arquivo (só no nome)")
	usageOpt("-FR <arquivo>", "Qual pacote dos repositórios tem o arquivo (só no nome)")
	usageOpt("-Fa <termo>", "Igual ao -F, termo em qualquer parte do caminho")
	usageOpt("-FRa <termo>", "Igual ao -FR, termo em qualquer parte do caminho")
	usageOpt("-Fx <regex>", "Igual ao -F, com expressão regular no caminho")
	usageOpt("-FRx <regex>", "Igual ao -FR, com expressão regular no caminho")
	usageOpt("-Q<opção> [pacote]", "Repassa ao xbps-query (ex.: -Qx pacote = -x)")

	usageSection("Listar:")
	usageOpt("-Li", "Pacotes instalados")
	usageOpt("-Lo", "Pacotes órfãos")

	usageSection("Remover:")
	usageOpt("-X <pacote>", "Remove o pacote")
	usageOpt("-XR <pacote>", "Remove o pacote e as dependências órfãs")
	usageOpt("-X<flags> <pacote>", "Flags após -X vão ao xbps-remove (-Xy = -y)")

	usageSection("Manutenção:")
	usageOpt("-Sc", "Limpa do cache só não instalados e versões antigas")
	usageOpt("-Scc", "Esvazia o cache e remove os órfãos, sem perguntar")
	usageOpt("--history", "Histórico de transações")

	usageSection("Outras:")
	usageOpt("-h, --help", "Esta ajuda")
	usageOpt("-v, --version", "Versão do vinstall")

	usageSection("Variáveis de ambiente:")
	usageOpt("XBPS_PARALLEL_DOWNLOADS=<n>", "Igual a --parallel-downloads <n>")
	usageMore("Sem ela nem a opção, vale o /etc/xbps.d/xbps.conf")

	usageSection("Exemplos:")
	usageOpt("vinstall telegram", "Instala o telegram")
	usageOpt("vinstall -Syu", "Atualiza o sistema")
	usageOpt("vinstall -XR telegram", "Remove o telegram e as dependências")
	usageOpt("vinstall -F ifconfig", "Descobre o pacote do ifconfig")
	fmt.Println()
}

/*
   CHANGELOG

   [1.6.20] - 2026-10-21
   Changed:
     - -F/-FR mostram o resultado numa tabela única e alinhada, no estilo
       do "pacman -F": "[✓]/[-]  pacote  caminho", ordenada e sem
       repetição, com o total de arquivos e pacotes no fim (fileRow,
       addFileRow, printFileRows). Antes, o plist e o xbps-query -o
       mostravam o mesmo arquivo duas vezes, em formatos diferentes.
     - -FR marca com [✓] os pacotes que já estão instalados (pelo nome).
     - searchInLocalPlist devolve as linhas em vez de imprimir direto.

   [1.6.19] - 2026-10-21
   Changed:
     - -F/-FR: o termo casa em qualquer parte do NOME do arquivo, mas não
       nos diretórios ("ifconfig" casa /usr/bin/ifconfig e ifconfig.8,
       não /usr/share/ifconfig-doc/README). Na 1.6.18 era nome exato.
       Com "/" no termo, vale o caminho todo. -Fa/-FRa e -Fx/-FRx
       continuam valendo para o caminho inteiro.

   [1.6.18] - 2026-10-21
   Changed:
     - -F/-FR passam a casar o NOME EXATO do arquivo, como o "pacman -F"
       ("ifconfig" casa /usr/bin/ifconfig, mas não ifconfig.8). Com "/"
       no termo, casa o final do caminho ("bin/ifconfig"). Antes, casava
       qualquer caminho que contivesse o termo.
     - searchInLocalPlist mostra "pacote  caminho" de cada arquivo que
       casa, em vez de só o nome do pacote.
   Added:
     - -Fa/-FRa: o termo em qualquer parte do caminho (o comportamento
       antigo do -F/-FR). Usa xbps-query -o '*termo*' e xlocate -F.
     - -Fx/-FRx: expressão regular aplicada ao caminho completo (como o
       "pacman -Fx"). Usa xbps-query --regex -o e xlocate -E.
     - fileQuery (match, ownedArgs, xlocateArgs) e ownedLinePath();
       substituem ownedPattern.

   [1.6.17] - 2026-10-21
   Changed:
     - Cabeçalho do -F/-FR reescrito: "Procurando 'x' nos pacotes
       instalados:" e "Procurando 'x' nos repositórios remotos:".

   [1.6.16] - 2026-10-20
   Fixed:
     - A descoberta de índices da 1.6.15 não achava o índice do VoidBR,
       porque ele estava em outro host (www.voidbr.org) e não junto do
       repositório (void.voidbr.org/voidlinux/extra). Convenção nova: o
       índice fica DENTRO do repositório, em <repo>/xlocate/xlocate.git,
       e acompanha o repositório em qualquer host ou mirror
       (ex.: void.chililinux.com).
     - Dois índices no mesmo host usavam o mesmo diretório local. O nome
       da cópia agora usa host+caminho (xlocateCacheName).
     - A lista salva em ~/.cache/vinstall/xlocate-indexes é refeita na
       hora se os repositórios do xbps.d mudarem, sem esperar 24h.
   Added:
     - Repositórios locais (diretório, ex.: /vg/void-mirror/extra) também
       entram: o índice é lido direto do disco (probeXlocateLocal).
   Changed:
     - Repositórios oficiais do Void e mirrors (<base>/current/...) não
       são sondados: o índice oficial já entra sempre, e o de um mirror
       seria uma cópia repetida (isVoidOfficialRepo).

   [1.6.15] - 2026-10-20
   Changed:
     - -FR descobre os índices xlocate a partir dos repositórios do
       xbps.d, em vez de uma lista fixa no código. Para cada repositório
       http(s), procura xlocate/xlocate.git subindo pelo caminho da URL
       (discoverXlocateRemotes, probeXlocateRemote). A lista fica em
       ~/.cache/vinstall/xlocate-indexes por 24h; sem rede, reaproveita a
       última salva.
     - Servidores com o mesmo índice (mesmo commit, ex.: voidbr.org e o
       mirror voidlinux.com.br) viram um índice só, exibido com os dois
       hosts e usando um como reserva do outro no xlocate -S. O índice
       oficial do Void entra sempre.
     - O índice do VoidBR passa a ficar em ~/.cache/xlocate-<host>.git;
       o antigo ~/.cache/xlocate-voidbr.git pode ser apagado.

   [1.6.14] - 2026-10-20
   Added:
     - -FR busca também no índice xlocate do repositório VoidBR
       (https://www.voidbr.org/xlocate/xlocate.git, cópia local em
       ~/.cache/xlocate-voidbr.git), além do índice oficial do Void.
       O índice é gerado no servidor pelo voidbr-xlocate-index.
     - Se o voidbr.org estiver fora do ar, o xlocate -S tenta o mirror
       https://voidlinux.com.br/xlocate/xlocate.git. Se nenhum responder,
       a busca usa a cópia local que existir.
     - Cada índice é atualizado sozinho (xlocate -S) quando não existe
       ou tem mais de 24h (xlocateIndexes, ensureXlocateIndex).

   [1.6.13] - 2026-10-20
   Fixed:
     - -FR demorava até estourar o timeout: o xbps-query -Ro baixa a lista
       de arquivos de cada pacote do repositório pela rede. Removido; -FR
       usa só o xlocate.
   Changed:
     - -FR atualiza sozinho o índice do xlocate (xlocate -S) quando ele
       não existe ou tem mais de 24h (ensureXlocateIndex), no lugar do
       aviso "[TIP] Índice xlocate antigo" (checkXlocateIndex removida).
     - -FR sem resultado no xlocate cai no xbps-query -Rs, listando os
       pacotes com esse nome em todos os repositórios (inclusive extras).

   [1.6.12] - 2026-10-20
   Fixed:
     - -F/-FR não achavam nada passando só o nome (ex.: "conectux"): o
       xbps-query -o/-Ro casa o padrão com o caminho COMPLETO do arquivo.
       Sem curinga, o padrão agora vira "*nome*" (ownedPattern).
     - -FR só usava o xlocate quando ele existia, e o índice do xlocate
       cobre só os repositórios oficiais do Void. Agora roda xlocate e
       xbps-query -Ro em paralelo e junta os resultados, cobrindo também
       repositórios extras (ex.: VoidBR).

   [1.6.11] - 2026-10-20
   Changed:
     - -F busca só nos pacotes instalados: plist local
       (/var/db/xbps/.*-files.plist) + xbps-query -o. Não usa mais xlocate.
     - -FR busca só nos repositórios: xlocate se disponível, senão
       xbps-query -Ro, com timeout de 2 min (antes 10s cortava a busca).
     - Buscas sequenciais, sem goroutines; " (instalado)" agora sai em
       cada linha do xbps-query -o, não só na última.

   [1.6.10] - 2026-10-19
   Fixed:
     - -Scc mostrava "Órfãos removidos:" com a contagem do xbps-query -O
       feita ANTES de rodar o xbps-remove -o -y, e ignorava o resultado
       da remoção: se ela falhasse, o número aparecia mesmo assim. Agora
       os órfãos são removidos primeiro e a contagem vem da diferença
       real entre os pacotes instalados antes e depois (removedPackages).
       O resumo passou a sair depois da saída do xbps-remove, mantendo a
       ordem das linhas.
     - -Scc contava versões antigas de pacotes instalados como "Não
       instalados removidos", porque comparava pelo pkgver exato. Agora
       classifica pelo nome do pacote e mostra as duas linhas separadas:
       "Não instalados removidos:" e "Versões antigas removidas:".
     - -Scc somava em "Espaço liberado:" o tamanho de arquivos que
       falhavam ao ser apagados. Agora só conta o que foi removido.
   Added:
     - -X/-XR agora mostram, ao final, "Pacotes removidos:" e "Órfãos
       removidos:" (pacotes que saíram sem terem sido pedidos como alvo,
       ex.: dependências removidas pelo -R). Antes não havia contagem.
     - removedPackages() e printRemovalSummary().
     - Nova opção -Sc: limpeza leve do cache, no estilo do "pacman -Sc".
       Remove de /var/cache/xbps só os pacotes não instalados e as
       versões antigas de pacotes instalados, mantendo os arquivos da
       versão instalada. Não remove órfãos (isso continua com -Scc).
       Antes, -Sc era repassado ao xbps-install, onde o -c é
       --cachedir <dir> e exigia um diretório como argumento.
     - cleanXbpsCacheLight().

   [1.6.9] - 2026-10-16
   Fixed:
     - --help voltou a mostrar "==> O vinstall aceita todas as flags
       nativas do xbps-install", removida por engano na 1.6.8. Fica junto
       com a linha "Opções que o vinstall não conhece são repassadas ao
       xbps-install.".

   [1.6.8] - 2026-10-16
   Fixed:
     - -Scc ainda perguntava "Do you want to continue? [Y/n]" quando havia
       órfãos: a remoção chamava `xbps-remove -o` sem -y. Agora passa
       `-o -y`, cumprindo o "sem perguntar" do --help.
     - --help desalinhado: o preenchimento %-20s era aplicado sobre o texto
       já colorido, e os códigos ANSI contavam como caracteres. Agora a
       coluna é calculada no texto puro (usageOpt).
   Changed:
     - --help reorganizado por seções (Instalar e atualizar, Sincronizar,
       Buscar, Listar, Remover, Manutenção, Outras, Variáveis de ambiente,
       Exemplos), com colunas alinhadas. Passou a listar opções que já
       existiam mas não apareciam: -Qs, -Q<opção>, -y, -h e -v.
     - --help mostra "Copyright (c) 2026-2026, VoidBR Development Team
       <https://voidbr.org>" no lugar do Copyright pessoal, que continua
       no cabeçalho do código-fonte.

   [1.6.7] - 2026-10-16
   Fixed:
     - "--parallel-downloads 8" (valor separado por espaço) fazia o "8"
       virar nome de pacote. Opções longas do xbps-install que recebem
       valor (--parallel-downloads, --repository, --rootdir, --config,
       --cachedir) agora levam o argumento seguinte junto, repassadas
       como "--opcao=valor". A forma com "=" continua funcionando.
     - XBPS_PARALLEL_DOWNLOADS era descartada pelo sudo (env_reset) e
       não chegava ao xbps-install. Agora o vinstall lê a variável e
       repassa como --parallel-downloads=<n> em toda chamada ao
       xbps-install, exceto quando a linha de comando já traz
       --parallel-downloads (que tem prioridade, como no xbps-install).
       Valor fora de 1-16 é avisado e ignorado.
     - A detecção de "u" para --ignore-file-conflicts considerava o
       valor de opções "--opcao=valor" (ex.: uma URL de --repository com
       "linux"); agora só o nome da opção é verificado.
   Changed:
     - --help ganhou a seção "Downloads paralelos".

   [1.6.6] - 2026-08-10
   Changed:
     - -Scc deixou de perguntar "Remover órfãos? [s/N]" antes de remover
       pacotes órfãos. Agora, ao encontrar órfãos, remove automaticamente
       via `xbps-remove -o`, sem interação — limpeza geral (cache +
       órfãos) completa em uma única chamada de -Scc, sem mensagens
       extras de status.
     - Resumo final do -Scc agora mostra, além do total de cache
       removido, a contagem de pacotes de cache que não estavam
       instalados ("Não instalados removidos:") e a contagem de órfãos
       removidos ("Órfãos removidos:"). Nova extractPkgverFromCacheFile()
       extrai o pkgver do nome do arquivo de cache (padrão
       "<pkgver>.<arch>.xbps") e compara contra getInstalledPackages()
       para essa contagem.
     - Ordem do resumo ajustada: "Órfãos removidos:" aparece antes de
       "Espaço liberado:" (a contagem de órfãos, via xbps-query -O, é
       calculada antes de imprimir o resumo; a remoção efetiva continua
       ocorrendo só depois de todo o resumo ser exibido).
     - Rótulo "Espaço livre:" trocado para "Espaço liberado:", já que o
       valor exibido é o tamanho total removido do cache, não o espaço
       livre disponível no disco.
     - --help atualizado: entrada do -Scc deixa explícito que a remoção
       de órfãos é automática.

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
