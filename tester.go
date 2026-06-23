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
	FullName      string
	Description   string
	SizeDownload  int64
	SizeInstalled int64
	IsInstalled   bool
}

var (
	cyan    = color.New(color.Bold, color.FgCyan).SprintFunc()
	green   = color.New(color.Bold, color.FgGreen).SprintFunc()
	white   = color.New(color.Bold, color.FgWhite).SprintFunc()
	yellow  = color.New(color.Bold, color.FgYellow).SprintFunc()
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
	return fmt.Sprintf("%dKB", b/1024)
}

func getInstalledPackages() map[string]bool {
	installed := make(map[string]bool)
	cmd := exec.Command("xbps-query", "-l")
	out, _ := cmd.Output()
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
	repoMap := getActiveRepos()
	installed := getInstalledPackages()

	type RepoResult struct {
		URL      string
		Packages []Package
	}

	var results []RepoResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	for dirName, repoURL := range repoMap {
		repoPath := filepath.Join("/var/db/xbps/", dirName, "x86_64-repodata")
		if _, err := os.Stat(repoPath); os.IsNotExist(err) { continue }

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

			var pkgs []Package
			for _, pkgData := range index {
				pkg := pkgData.(map[string]interface{})
				pkgVer := fmt.Sprintf("%v", pkg["pkgver"])
				if strings.Contains(strings.ToLower(pkgVer), query) {
					pkgs = append(pkgs, Package{
						FullName:      pkgVer,
						Description:   pkg["short_desc"].(string),
						SizeDownload:  toInt64(pkg["filename-size"]),
						SizeInstalled: toInt64(pkg["installed_size"]),
						IsInstalled:   installed[pkgVer],
					})
				}
			}

			if len(pkgs) > 0 {
				sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].FullName < pkgs[j].FullName })
				mu.Lock()
				results = append(results, RepoResult{URL: url, Packages: pkgs})
				mu.Unlock()
			}
		}(repoPath, repoURL)
	}
	wg.Wait()

	if len(results) == 0 {
		fmt.Println("Nenhum pacote encontrado.")
	} else {
		maxNameLen := 0
		for _, repo := range results {
			for _, p := range repo.Packages {
				if len(p.FullName) > maxNameLen { maxNameLen = len(p.FullName) }
			}
		}

		counter := 1
		for _, repo := range results {
      fmt.Printf("%10s%s\n", "", cyan(repo.URL))
			for _, p := range repo.Packages {
				status := red("[-]")
				if p.IsInstalled { status = green("[*]") }

				// Fixando 6 caracteres para cada lado: %6.6s / %6.6s
				sDown := formatBytes(p.SizeDownload)
				sInst := formatBytes(p.SizeInstalled)
				sizeStr := fmt.Sprintf("[%6.6s|%6.6s]", sDown, sInst)

				fmt.Printf(" %-5s %s %-*s  %s %s\n",
					fmt.Sprintf("[%d]", counter),
					status,
					maxNameLen, white(p.FullName),
					yellow(sizeStr),
					green(p.Description))
				counter++
			}
		}
	}
}
