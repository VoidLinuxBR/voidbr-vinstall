package main

import (
	"bytes"
	"fmt"
	"howett.net/plist"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type Index map[string]interface{}

// Busca todos os arquivos de repodata existentes no diretório de cache do XBPS
func GetRepoIndexPaths() []string {
	var paths []string
	// Procura recursivamente por arquivos x86_64-repodata dentro de /var/db/xbps/
	filepath.Walk("/var/db/xbps/", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, "x86_64-repodata") {
			paths = append(paths, path)
		}
		return nil
	})
	return paths
}

func LoadRepositoryIndex(path string) (Index, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader, err := zstd.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	const offset = 512
	if len(data) <= offset {
		return nil, fmt.Errorf("arquivo muito curto")
	}

	var result Index
	decoder := plist.NewDecoder(bytes.NewReader(data[offset:]))
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Uso: ./tester <termo_de_busca>")
		return
	}
	query := strings.ToLower(os.Args[1])

	repoPaths := GetRepoIndexPaths()
	foundAny := false

	for _, path := range repoPaths {
		index, err := LoadRepositoryIndex(path)
		if err != nil {
			continue
		}

		for pkgName, pkgData := range index {
			if strings.Contains(strings.ToLower(pkgName), query) {
				pkg := pkgData.(map[string]interface{})
				desc, _ := pkg["short_desc"].(string)
				
				fmt.Printf("\033[1;32m%s\033[0m\n", pkgName)
				fmt.Printf("    Repo:    %s\n", path)
				fmt.Printf("    Version: %v\n", pkg["pkgver"])
				fmt.Printf("    Desc:    %s\n\n", desc)
				foundAny = true
			}
		}
	}

	if !foundAny {
		fmt.Println("Nenhum pacote encontrado.")
	}
}
