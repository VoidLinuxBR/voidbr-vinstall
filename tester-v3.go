package main

import (
	"bytes"
	"fmt"
	"howett.net/plist"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/fatih/color"
	"github.com/klauspost/compress/zstd"
)

type Package struct {
	Status      string
	FullName    string
	Description string
	Repo        string
}

var (
	cyan   = color.New(color.Bold, color.FgCyan).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	white  = color.New(color.Bold, color.FgWhite).SprintFunc()
	yellow = color.New(color.Bold, color.FgYellow).SprintFunc()
)

// Reverte o escape do XBPS para exibir a URL original
func formatRepoURL(path string) string {
	base := filepath.Base(filepath.Dir(path))
	// Reverte os underscores para caracteres da URL
	// O padrão XBPS usa underscores para tudo que não é alfanumérico
	url := strings.ReplaceAll(base, "___", "://")
	url = strings.ReplaceAll(url, "__", "/")
	url = strings.ReplaceAll(url, "_", "/")
	return url
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Uso: ./tester <termo_de_busca>")
		return
	}
	query := strings.ToLower(os.Args[1])

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
			if err := plist.NewDecoder(bytes.NewReader(data[512:])).Decode(&index); err != nil {
				return
			}

			// Converte o caminho para a URL legível uma única vez
			cleanRepo := formatRepoURL(p)

			var localMatches []Package
			for pkgName, pkgData := range index {
				if strings.Contains(strings.ToLower(pkgName), query) {
					pkg := pkgData.(map[string]interface{})
					localMatches = append(localMatches, Package{
						Status:      "[ ]",
						FullName:    fmt.Sprintf("%v", pkg["pkgver"]),
						Description: pkg["short_desc"].(string),
						Repo:        cleanRepo,
					})
				}
			}

			mu.Lock()
			pkgs = append(pkgs, localMatches...)
			mu.Unlock()
		}(path)
	}
	wg.Wait()

	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i].FullName < pkgs[j].FullName
	})

	if len(pkgs) == 0 {
		fmt.Println("Nenhum pacote encontrado.")
	} else {
		fmt.Printf("\n%s\n", cyan("Resultados encontrados no repositório:"))
		lineSeparator := white("──────────────────────────────────────────────────")
		fmt.Println(lineSeparator)
		for i, p := range pkgs {
			idx := yellow(fmt.Sprintf("[%2d]", i+1))
			fmt.Printf("%s %s %-30s  %s\n", idx, white(p.Status), white(p.FullName), green(p.Description))
			fmt.Printf("%9s%s\n", "", cyan(p.Repo))
		}
		fmt.Println(lineSeparator)
	}
}
