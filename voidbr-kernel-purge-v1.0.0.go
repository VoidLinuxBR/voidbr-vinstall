package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ColorReset  = "\033[0m"
	ColorInfo   = "\033[1;34m"
	ColorWarn   = "\033[1;33m"
	ColorError  = "\033[1;31m"
	ColorDryRun = "\033[1;36m"
)

var dryRun = false

func usage() {
	prog := filepath.Base(os.Args[0])
	fmt.Printf("Usage: %s [--dry-run] list [version ...]\n", prog)
	fmt.Printf("       %s [--dry-run] rm all\n", prog)
	fmt.Printf("       %s [--dry-run] rm <version ...>\n", prog)
	os.Exit(1)
}

func getRunningKernel() string {
	out, _ := exec.Command("uname", "-r").Output()
	return strings.TrimSpace(string(out))
}

func listKernels(args []string) []string {
	running := getRunningKernel()
	out, _ := exec.Command("xbps-query", "-o", "/boot/vmlinu[xz]-*").Output()
	installed := string(out)

	var results []string
	patterns := args
	if len(patterns) == 0 {
		patterns = []string{"all"}
	}

	for _, arg := range patterns {
		pattern := arg
		if pattern == "all" {
			pattern = "*"
		}

		files, _ := filepath.Glob("/boot/vmlinu[xz]-*")
		for _, k := range files {
			if strings.Contains(installed, k) || k == "" {
				continue
			}

			kver := strings.TrimPrefix(filepath.Base(k), "vmlinuz-")
			kver = strings.TrimPrefix(kver, "vmlinux-")

			if kver == running {
				continue
			}

			matched, _ := filepath.Match(pattern, kver)
			if matched {
				results = append(results, kver)
			}
		}
	}
	sort.Strings(results)
	return results
}

func runHooks(dir, kver string) {
	hooks, _ := filepath.Glob("/etc/kernel.d/" + dir + "/*")
	for _, d := range hooks {
		info, _ := os.Stat(d)
		if info.Mode()&0111 == 0 {
			continue
		}
		if dryRun {
			fmt.Printf("%s[DRY-RUN]%s Would run hook: %s %s %s\n", ColorDryRun, ColorReset, d, "kernel", kver)
			continue
		}
		fmt.Printf("Running %s kernel hook: %s...\n", dir, filepath.Base(d))
		cmd := exec.Command(d, "kernel", kver)
		cmd.Env = append(os.Environ(), "ROOTDIR=.")
		cmd.Run()
	}
}

func removeKernel(rmkver string) {
	runHooks("pre-remove", rmkver)

	targets := []string{
		"/boot/config-" + rmkver,
		"/boot/System.map-" + rmkver,
		"/boot/vmlinuz-" + rmkver,
		"/boot/vmlinux-" + rmkver,
		"/usr/lib/modules/" + rmkver,
	}

	for _, f := range targets {
		if _, err := os.Stat(f); err == nil {
			if dryRun {
				fmt.Printf("%s[DRY-RUN]%s Would remove: %s\n", ColorDryRun, ColorReset, f)
			} else {
				fmt.Printf("Removing %s...\n", f)
				os.RemoveAll(f)
			}
		}
	}

	runHooks("post-remove", rmkver)

	postTargets := []string{
		"/usr/src/kernel-headers-" + rmkver,
		"/usr/lib/debug/boot/vmlinuz-" + rmkver,
		"/usr/lib/debug/boot/vmlinux-" + rmkver,
		"/usr/lib/debug/usr/lib/modules/" + rmkver,
		"/boot/dtbs/dtbs-" + rmkver,
	}

	for _, f := range postTargets {
		if _, err := os.Stat(f); err == nil {
			if dryRun {
				fmt.Printf("%s[DRY-RUN]%s Would remove: %s\n", ColorDryRun, ColorReset, f)
			} else {
				fmt.Printf("Removing %s...\n", f)
				os.RemoveAll(f)
			}
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	idx := 1
	if os.Args[1] == "--dry-run" {
		dryRun = true
		idx = 2
		if len(os.Args) < 3 {
			usage()
		}
	}

	switch os.Args[idx] {
	case "list":
		args := []string{}
		if len(os.Args) > idx+1 {
			args = os.Args[idx+1:]
		}
		for _, k := range listKernels(args) {
			fmt.Println(k)
		}
	case "rm":
		if len(os.Args) < idx+2 {
			usage()
		}
		if !dryRun && os.Geteuid() != 0 {
			fmt.Printf("%sYou have to run this script as root!%s\n", ColorError, ColorReset)
			os.Exit(1)
		}
		for _, kver := range listKernels(os.Args[idx+1:]) {
			fmt.Printf("Removing kernel %s...\n", kver)
			removeKernel(kver)
		}
	default:
		usage()
	}
}
