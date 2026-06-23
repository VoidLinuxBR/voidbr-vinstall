package main

import (
	"bytes"
	"fmt"
	"howett.net/plist"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var mu sync.Mutex

func processRepo(path string, query string, wg *sync.WaitGroup) {
	defer wg.Done()

	file, err := os.Open(path)
	if err != nil { return }
	defer file.Close()

	reader, err := zstd.NewReader(file)
	if err != nil { return }
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil || len(data) <= 512 { return }

	var result map[string]interface{}
	if err := plist.NewDecoder(bytes.NewReader(data[512:])).Decode(&result); err != nil {
		return
	}

	for pkgName, pkgData := range result {
		if strings.Contains(strings.ToLower(pkgName), query) {
			pkg := pkgData.(map[string]interface{})
			
			// Protege a saída no terminal com o Mutex
			mu.Lock()
			fmt.Printf("\033[1;32m%s\033[0m\n    Version: %v\n    Repo: %s\n\n", 
				pkgName, pkg["pkgver"], path)
			mu.Unlock()
		}
	}
}

func main() {
	if len(os.Args) < 2 { return }
	query := strings.ToLower(os.Args[1])

	var paths []string
	filepath.Walk("/var/db/xbps/", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, "x86_64-repodata") {
			paths = append(paths, path)
		}
		return nil
	})

	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go processRepo(path, query, &wg)
	}
	wg.Wait()
}
