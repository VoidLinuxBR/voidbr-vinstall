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

	"github.com/fatih/color"
	"github.com/klauspost/compress/zstd"
)

type Package struct {
	Status        string
	FullName      string
	Description   string
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

// Verifica pacotes instalados via xbps-query -l
func getInstalledPackages() map[string]bool {
	installed := make(map[string]bool)
	cmd := exec.Command("xbps-query", "-l")
	out, _ := cmd.Output()
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			// fields[1] é o formato pkg-versao
			installed[fields[1]] = true
		}
	}
	return installed
}

func getRepoMap() map[string]string {
	repoMap := make(map[string]string)
	cmd := exec.Command("xbps-query", "-L")
	out, err := cmd.Output()
	if err != nil { return repoMap }
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			url := fields[1]
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
	repoMap := getRepoMap()
	installed := getInstalledPackages() // Carrega lista de instalados
	
	var repoPaths []string
	filepath.Walk("/var/db/xbps/", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, "x86_64-repodata") {
			repoPaths = append(repoPaths, path)
		}
		return nil
	})

	var pkgs []Package
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, path := range repoPaths {
		wg.Add(1)
		go func(p string) {
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

			dirName := filepath.Base(filepath.Dir(p))
			repoURL, ok := repoMap[dirName]
			if !ok {
				repoURL = strings.Replace(dirName, "___", "://", 1)
				repoURL = strings.ReplaceAll(repoURL, "_", "/")
			}

			for _, pkgData := range index {
				pkg := pkgData.(map[string]interface{})
				pkgVer := fmt.Sprintf("%v", pkg["pkgver"])
				if strings.Contains(strings.ToLower(pkgVer), query) {
					
					status := "[ ]"
					if installed[pkgVer] {
						status = red("[x]") // Marca instalados em vermelho
					}

					mu.Lock()
					pkgs = append(pkgs, Package{
						Status:        status,
						FullName:      pkgVer,
						Description:   pkg["short_desc"].(string),
						Repo:          repoURL,
						SizeDownload:  toInt64(pkg["filename-size"]),
						SizeInstalled: toInt64(pkg["installed_size"]),
					})
					mu.Unlock()
				}
			}
		}(path)
	}
	wg.Wait()
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].FullName < pkgs[j].FullName })

	if len(pkgs) == 0 {
		fmt.Println("Nenhum pacote encontrado.")
	} else {
		fmt.Printf("\n%s\n", cyan("Resultados encontrados no repositório:"))
		lineSeparator := white("──────────────────────────────────────────────────")
		fmt.Println(lineSeparator)
		for i, p := range pkgs {
			idx := yellow(fmt.Sprintf("[%2d]", i+1))
			fmt.Printf("%s %s %-30s  %s\n", idx, p.Status, white(p.FullName), green(p.Description))
			fmt.Printf("%9s%s (%s / %s)\n", "", cyan(p.Repo), yellow(formatBytes(p.SizeDownload)), magenta(formatBytes(p.SizeInstalled)))
		}
		fmt.Println(lineSeparator)
	}
}
