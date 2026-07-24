/*
   xbps-query -R -p run_depends -s sdbus-cpp

   voidbr-vinstall
   Wrapper para o Void xbps-query e xbps-install

   Site:      https://chililinux.com
   GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

   Created:   ter 03 fev 2026 13:08:22 -04
   Updated:   qui 23 jul 2026 21:44:36 -04
   Version:   1.3.10
   Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

package main

import (
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
	Version   = "1.3.6"
	Copyright = "Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>"
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
		case "-Scc":
			mode = "clean"
		case "-X", "-x":
			mode = "remove"
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
			if strings.HasPrefix(arg, "-Q") {
				mode = "query-generic"
				filter = strings.Replace(arg, "-Q", "-", 1)
			} else if strings.HasPrefix(arg, "-") {
				flags = append(flags, arg)
			} else {
				targets = append(targets, arg)
			}
		}
	}
	switch mode {
	case "history":
		showHistory()
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
		for _, f := range flags {
			if strings.Contains(f, "u") {
				flags = append(flags, "--ignore-file-conflicts")
				break
			}
		}

		if len(targets) > 0 {
			flags = append(flags, "--ignore-file-conflicts")
			if !runBinary("xbps-install", flags, targets) {
				suggestions := fetchSuggestions(targets[0])
				if len(suggestions) > 0 {
					displayMenu(suggestions, flags)
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
	out, _ := exec.Command("xbps-query", "-l").Output()
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			installed[fields[1]] = true
		}
	}
	return installed
}

func getActiveRepos() map[string]string {
	repoMap := make(map[string]string)
	out, err := exec.Command("xbps-query", "-L").Output()
	if err != nil {
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

func remoteSearchDetailed(query string) {
	repoMap := getActiveRepos()
	installed := getInstalledPackages()
	var pkgs []Package
	var mu sync.Mutex
	var wg sync.WaitGroup

	queryLower := strings.ToLower(query)
	queryClean := strings.ReplaceAll(queryLower, " ", "")

	for dirName, repoURL := range repoMap {
		repoPath := filepath.Join("/var/db/xbps/", dirName, "x86_64-repodata")
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			continue
		}

		wg.Add(1)
		go func(p, url string) {
			defer wg.Done()
			file, err := os.Open(p)
			if err != nil {
				return
			}
			defer file.Close()
			reader, err := zstd.NewReader(file)
			if err != nil {
				return
			}
			defer reader.Close()
			data, err := io.ReadAll(reader)
			if err != nil || len(data) <= 512 {
				return
			}
			var index map[string]interface{}
			if err := plist.NewDecoder(bytes.NewReader(data[512:])).Decode(&index); err != nil {
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

				fullText := fmt.Sprintf("%v %v %v %d %s %d %s %v",
					pkg["pkgver"],
					pkg["short_desc"],
					pkg["maintainer"],
					sizeDown,
					strings.ReplaceAll(formatBytes(sizeDown), " ", ""),
					sizeInst,
					strings.ReplaceAll(formatBytes(sizeInst), " ", ""),
					url,
				)

				fullTextClean := strings.ToLower(strings.ReplaceAll(fullText, " ", ""))

				if strings.Contains(fullTextClean, queryClean) {
					pkgVer := fmt.Sprintf("%v", pkg["pkgver"])
					status := red("[-]")
					if installed[pkgVer] {
						status = green("[✔]")
					}
					local = append(local, Package{
						Status:        status,
						FullName:      pkgVer,
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

	output, _ := cmd.Output()
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
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-F"), white("ifconfig (Busca local)"))
	fmt.Printf("  %s %-15s %s\n", green("vinstall"), white("-FR"), white("ifconfig (Busca remota)"))
	fmt.Println("\nAtalhos de Consulta:")
	fmt.Printf("  %-20s %s\n", green("-Li"), white("Lista todos os pacotes instalados"))
	fmt.Printf("  %-20s %s\n", green("-Lo"), white("Lista apenas pacotes órfãos"))
	fmt.Printf("  %-20s %s\n", green("-Ss <query>"), white("Busca termo nos repositórios (Rápido)"))
	fmt.Printf("  %-20s %s\n", green("-Sss <query>"), white("Busca detalhada nos repositórios (Full Text)"))
	fmt.Printf("  %-20s %s\n", green("-Ssi <query>"), white("Busca termo nos pacotes instalados"))
	fmt.Printf("  %-20s %s\n", green("-Ssu <query>"), white("Busca termo nos pacotes NÃO instalados"))
	fmt.Println("\nManutenção:")
	fmt.Printf("  %-20s %s\n", green("-Scc"), white("Limpa cache e órfãos"))
	fmt.Printf("  %-20s %s\n", green("--history"), white("Mostra histórico de transações"))
	fmt.Println()
}
