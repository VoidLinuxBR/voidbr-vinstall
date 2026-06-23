package main

import (
	"bytes"
	"fmt"
	"howett.net/plist"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"
	"bufio"

	"github.com/fatih/color"
	"github.com/klauspost/compress/zstd"
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

var (
	cyan    = color.New(color.Bold, color.FgCyan).SprintFunc()
	green   = color.New(color.FgGreen).SprintFunc()
	white   = color.New(color.Bold, color.FgWhite).SprintFunc()
	yellow  = color.New(color.Bold, color.FgYellow).SprintFunc()
	magenta = color.New(color.FgMagenta).SprintFunc()
	red     = color.New(color.Bold, color.FgRed).SprintFunc()
)

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

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit { return fmt.Sprintf("%d B", b) }
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func getInstalledPackages() map[string]bool {
	installed := make(map[string]bool)
	cmd := exec.Command("xbps-query", "-l")
	out, _ := cmd.Output()
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			installed[fields[1]] = true
		}
	}
	return installed
}

// Retorna apenas os repositórios ativos (via xbps-query -L)
func getActiveRepos() map[string]string {
	repoMap := make(map[string]string)
	cmd := exec.Command("xbps-query", "-L")
	out, err := cmd.Output()
	if err != nil { return repoMap }
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			url := fields[1]
			// Converte URL para o nome da pasta padrão do XBPS
			dirName := strings.NewReplacer(":", "_", "/", "_", ".", "_").Replace(url)
			repoMap[dirName] = url
		}
	}
	return repoMap
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Uso: ./tester <termo_de_busca>")
		return
	}
	query := strings.ToLower(os.Args[1])
	repoMap := getActiveRepos()
	installed := getInstalledPackages()

	var pkgs []Package
	var mu sync.Mutex
	var wg sync.WaitGroup

	for dirName, repoURL := range repoMap {
		repoPath := filepath.Join("/var/db/xbps/", dirName, "x86_64-repodata")

		// Verifica se o arquivo existe antes de processar
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			continue
		}

		wg.Add(1)
		go func(p, url string) {
			defer wg.Done()
			file, err := os.Open(p)
			if err != nil { return }
			defer file.Close()
			reader, err := zstd.NewReader(file)
			if err != nil { return }
			defer reader.Close()
			data, err := io.ReadAll(reader)
			if err != nil || len(data) <= 512 { return }
			var index map[string]interface{}
			if err := plist.NewDecoder(bytes.NewReader(data[512:])).Decode(&index); err != nil { return }

			for _, pkgData := range index {
				pkg := pkgData.(map[string]interface{})
				pkgVer := fmt.Sprintf("%v", pkg["pkgver"])
				if strings.Contains(strings.ToLower(pkgVer), query) {
//				status := red("[✘]")
					status := red("[-]")
					if installed[pkgVer] {
						status = green("[✔]")
					}

					mu.Lock()
					pkgs = append(pkgs, Package{
						Status:        status,
						FullName:      pkgVer,
						Description:   pkg["short_desc"].(string),
            Maintainer:    fmt.Sprintf("%v", pkg["maintainer"]),
						Repo:          url,
						SizeDownload:  toInt64(pkg["filename-size"]),
						SizeInstalled: toInt64(pkg["installed_size"]),
					})
					mu.Unlock()
				}
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

// 1. Função auxiliar de truncamento (coloque fora do main)
func truncate(s string, max int) string {
	if max < 3 { return "" }
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func printPackage(w *bufio.Writer, i int, p Package, width int) {
	idxText := fmt.Sprintf("[%d]", i+1)

	// Alinhamento exato do início do FullName
	paddingSize := len(idxText) + 1 + 3 + 1

	// Cálculo para truncamento preciso
	lenPrefix := paddingSize + len(p.FullName) + 2
	maxDesc := width - lenPrefix
	descExibida := truncate(p.Description, maxDesc)

	// Linha 1: [idx] [status] FullName  Description
	fmt.Fprintf(w, "%s %s %s  %s\n", yellow(idxText), p.Status, white(p.FullName), green(descExibida))

	// Linha 2: Repo | Maintainer (Size / Size)
	// O paddingSize garante que o Repo comece na mesma coluna do FullName
	fmt.Fprintf(w, "%*s%s | %s (%s / %s)\n", paddingSize, "", cyan(p.Repo), magenta(p.Maintainer), yellow(formatBytes(p.SizeDownload)), magenta(formatBytes(p.SizeInstalled)))
}
