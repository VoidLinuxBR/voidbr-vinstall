// Comando mkiso: reescrita em Go do script mkiso/lib.sh original (bash).
//
// Arquivo único de propósito, sem pacotes internos -- compila direto com:
//   go build -o mkiso mkiso.go
//
// Mantém a mesma arquitetura de orquestração do bash original (chama
// xbps-install, dracut, mksquashfs, xorriso, grub-mkstandalone etc. via
// os/exec), mas com parsing de flags, controle de erros e concorrência
// nativos do Go.
package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)


// ============================================================================
// origem original: body_config.go
// ============================================================================
// Package config centraliza o estado global que, no bash original, vivia em
// variáveis soltas (declare -g) espalhadas pelo mkiso e lib.sh.

// Config agrupa todo o estado mutável de uma execução do mkiso.
type Config struct {
	// Flags de desktop environment (equivalentes a MAKE_X, MAKE_XFCE etc.)
	MakeX             bool
	MakeXfceBase      bool
	MakeXfce          bool
	MakeAwesome       bool
	MakeEnlightenment bool
	MakeFluxbox       bool
	MakePlasma        bool
	MakeGnome         bool
	MakeCinnamon      bool
	MakeMate          bool
	MakeSway          bool
	MakeMango         bool
	MakeHyprland      bool
	MakeOnlyXorg      bool
	MakeFullX         bool

	// Controle geral
	Quiet   bool
	DryRun  bool
	Keep    bool // -K, não remove builddir
	Reuse   bool // -n, reusa squashfs existente
	Version string

	// Nome/título do profile (equivalente a $name/$title/$VOL_ID)
	Name    string
	Title   string
	VolID   string
	Distro  string
	App     string
	Automatic bool

	// Diretórios e caminhos
	RootDir      string
	BuildDir     string
	ImageDir     string
	RootFS       string
	HostDir      string
	BootDir      string
	IsolinuxDir  string
	GrubDir      string
	IsolinuxCfg  string

	// XBPS
	BaseArch          string
	Arch              string
	BaseSystemPkg     string
	XbpsCacheDir      string
	XbpsHostCacheDir  string
	XbpsRepository    string
	ExtraRepository   string
	XbpsInstallCmd    string
	VinstallCmd       string
	XbpsRemoveCmd     string
	XbpsQueryCmd      string
	XbpsRindexCmd     string
	XbpsUhelperCmd    string
	XbpsReconfigureCmd string

	// Compressão e boot
	InitramfsCompression string
	SquashfsCompression   string
	Keymap                string
	Locale                string
	LinuxVersion          string
	KernelVersion         string
	BootCmdline           string
	BootTitle             string
	SplashImage           string
	SyslinuxDataDir       string
	GrubDataDir           string

	// Saída
	OutputFile string

	// Listas de pacotes (equivalentes às strings REQUIRED_PKGS etc, já como slices)
	RequiredPkgs      []string
	InitramfsPkgs     []string
	AdditionalPkgs    []string
	BasePkgs          []string
	CommonPkgs        []string
	ADistroPkgs       []string
	AExtraCommonPkgs  []string
	AConfigPkgs       []string
	ASkelPkgs         []string
	AExtraPkgs        []string
	PackageList       []string
	IgnorePkgs        []string
	IncludeDirs       []string
	ServiceList       []string

	// Passos (equivalente a STEP_COUNT/CURRENT_STEP)
	StepCount   int
	CurrentStep int

	// Logging
	BootLog string
	Logger  string
	LBind   bool
}

// New cria uma Config com os mesmos valores padrão que o mkiso original define
// via declare -g no topo do script.
func NewConfig() *Config {
	return &Config{
		StepCount:           59,
		VolID:               "VOIDBR_LIVE",
		InitramfsCompression: "xz",
		SquashfsCompression:  "zstd",
		Name:                 "base",
	}
}

// ============================================================================
// origem original: body_colors.go
// ============================================================================
// Package colors replica sh_setVarColors() do mkiso: em vez de chamar `tput`
// via subprocesso a cada cor (como o bash fazia), usamos os códigos ANSI
// diretamente -- mais rápido e sem depender de `tput` estar no PATH.


// Palette contém todas as cores/estilos usados pelo mkiso original.
type Palette struct {
	Reset      string
	Rst        string
	Bold       string
	Underline  string
	NoUnderline string
	Reverse    string

	Black       string
	Red         string
	Green       string
	Yellow      string
	Blue        string
	Pink        string
	Magenta     string
	Cyan        string
	White       string
	Gray        string
	Orange      string
	Purple      string
	Violet      string
	LightRed    string
	LightGreen  string
	LightYellow string
	LightBlue   string
	LightMagenta string
	LightCyan   string
	BrightWhite string

	// Símbolos compostos (equivalentes a $TICK, $CROSS, $MID, $WARN, $INFO)
	Tick  string
	Cross string
	Mid   string
	Warn  string
	Info  string
}

// noColor retorna uma Palette com todos os campos vazios, para quando a saída
// não é um terminal (equivalente a quando tput falha silenciosamente).
func noColor() Palette {
	return Palette{}
}

// New monta a paleta de cores. Se `enable` for false (ex.: saída redirecionada
// para arquivo, ou --quiet), retorna tudo em branco, sem códigos de escape.
func NewPalette(enable bool) Palette {
	if !enable {
		return noColor()
	}

	const esc = "\x1b["
	p := Palette{
		Reset:       esc + "0m",
		Rst:         esc + "0m",
		Bold:        esc + "1m",
		Underline:   esc + "4m",
		NoUnderline: esc + "24m",
		Reverse:     esc + "7m",

		Black:        esc + "1m" + esc + "30m",
		Red:          esc + "1m" + esc + "38;5;196m",
		Green:        esc + "32m",
		Yellow:       esc + "1m" + esc + "33m",
		Blue:         esc + "34m",
		Pink:         esc + "35m",
		Magenta:      esc + "35m",
		Cyan:         esc + "36m",
		White:        esc + "37m",
		Gray:         esc + "38;5;8m",
		Orange:       esc + "38;5;202m",
		Purple:       esc + "38;5;125m",
		Violet:       esc + "38;5;61m",
		LightRed:     esc + "38;5;9m",
		LightGreen:   esc + "38;5;10m",
		LightYellow:  esc + "38;5;11m",
		LightBlue:    esc + "38;5;12m",
		LightMagenta: esc + "38;5;13m",
		LightCyan:    esc + "38;5;14m",
		BrightWhite:  esc + "38;5;15m",
	}

	clrkey := p.White
	p.Tick = fmt.Sprintf("%s[%s✓✓✓%s]%s", clrkey, p.Green, clrkey, p.Rst)
	p.Cross = fmt.Sprintf("%s[%s✗✗✗%s]%s", clrkey, p.Red, clrkey, p.Rst)
	p.Mid = fmt.Sprintf("%s[%s✗✗%s✓%s]%s", clrkey, p.Red, p.Green, clrkey, p.Rst)
	p.Warn = fmt.Sprintf("%s[%s⚠  %s]%s", clrkey, p.Yellow, clrkey, p.Yellow)
	p.Info = fmt.Sprintf("%s[%s➡  %s]%s", clrkey, p.Yellow, clrkey, p.Rst)

	return p
}

// IsTerminal detecta se stdout é um terminal interativo (equivalente a checar
// se `tput` teria efeito real, em vez de rodar num pipe/arquivo).
func IsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// ============================================================================
// origem original: body_logging.go
// ============================================================================
// Package logging replica as funções de impressão do mkiso/lib.sh: msg(),
// log_ok(), log_err(), log_mid(), log_warn(), print_step(), msgDot(),
// replicate() e variantes "_tab".


// Logger agrupa o estado necessário para replicar print_step/msgDot (que no
// bash dependiam das globais $CURRENT_STEP e $STEP_COUNT).
type Logger struct {
	C           Palette
	Quiet       bool
	CurrentStep int
	StepCount   int
}

func NewLogger(c Palette, quiet bool, stepCount int) *Logger {
	return &Logger{C: c, Quiet: quiet, StepCount: stepCount}
}

// Msg equivale a msg() do mkiso: echo -e "${INFO} ${*}${reset}"
func (l *Logger) Msg(format string, a ...any) {
	fmt.Printf("%s %s%s\n", l.C.Info, fmt.Sprintf(format, a...), l.C.Reset)
}

// MsgNoNewline equivale a msg() do lib.sh: echo -n -e "${INFO} ${*}${reset}"
// (lib.sh sobrescrevia msg() do mkiso; mantemos os dois nomes explícitos em
// vez de deixar uma sobrescrever a outra silenciosamente, como acontecia no
// bash original -- ver análise de inconsistências).
func (l *Logger) MsgNoNewline(format string, a ...any) {
	fmt.Printf("%s %s%s", l.C.Info, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) LogOk(format string, a ...any) {
	fmt.Printf("%s %s%s\n", l.C.Tick, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) LogErr(format string, a ...any) {
	fmt.Printf("%s %s%s\n", l.C.Cross, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) LogMid(format string, a ...any) {
	fmt.Printf("%s %s%s\n", l.C.Mid, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) LogWarn(format string, a ...any) {
	fmt.Printf("%s %s%s\n", l.C.Warn, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) MsgTab(format string, a ...any) {
	fmt.Printf("  %s %s%s\n", l.C.Info, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) LogOkTab(format string, a ...any) {
	fmt.Printf("  %s %s%s\n", l.C.Tick, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) LogErrTab(format string, a ...any) {
	fmt.Printf("  %s %s%s\n", l.C.Cross, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) LogMidTab(format string, a ...any) {
	fmt.Printf("  %s %s%s\n", l.C.Mid, fmt.Sprintf(format, a...), l.C.Reset)
}

func (l *Logger) LogWarnTab(format string, a ...any) {
	fmt.Printf("  %s %s%s\n", l.C.Warn, fmt.Sprintf(format, a...), l.C.Reset)
}

// InfoMsg equivale ao info_msg() do mkiso (versão simples, texto amarelo).
func (l *Logger) InfoMsg(format string, a ...any) {
	fmt.Printf("%s%s%s\n", l.C.Yellow, fmt.Sprintf(format, a...), l.C.Reset)
}

// PrintStep equivale a print_step()+msgDot() combinados: incrementa o passo
// atual e imprime "=>[N/TOTAL] mensagem", com suporte ao formato
// "titulo: detalhe" alinhado (como o msgDot original fazia).
func (l *Logger) PrintStep(msg string) {
	l.CurrentStep++
	const pad = 40

	if idx := strings.Index(msg, ":"); idx >= 0 {
		left := msg[:idx]
		right := msg[idx+1:]
		fmt.Printf("=>%s[%d/%d]%s %-*s :%s%s%s\n",
			l.C.Cyan, l.CurrentStep, l.StepCount, l.C.Reset,
			pad, left,
			l.C.Yellow, right, l.C.Reset)
		return
	}
	fmt.Printf("=>%s[%d/%d]%s %s\n", l.C.Cyan, l.CurrentStep, l.StepCount, l.C.Reset, msg)
}

// Replicate equivale a replicate(): imprime uma linha inteira preenchida com
// um caractere (usada como separador visual entre etapas).
func Replicate(c Palette, char string, width int) {
	if char == "" {
		char = "#"
	}
	if width <= 0 {
		width = terminalWidth()
	}
	fmt.Printf("%s%s%s\n", c.Green, strings.Repeat(char, width), c.Rst)
}

// terminalWidth tenta obter a largura do terminal; usa 80 como fallback
// (equivalente ao `tput cols` do bash, sem precisar chamar `tput`).
func terminalWidth() int {
	// Implementação mínima: em bash o script usava `tput cols`. Para manter o
	// binário livre dessa dependência externa, usamos um valor fixo razoável;
	// pode ser substituído por golang.org/x/term.GetSize se desejado.
	if w := os.Getenv("COLUMNS"); w != "" {
		var n int
		if _, err := fmt.Sscanf(w, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 80
}

// Die imprime uma mensagem de erro e finaliza o processo com código 1 --
// equivalente a die() do lib.sh (a versão "vencedora", já que lib.sh definia
// die() duas vezes e a segunda sobrescrevia a primeira no bash original).
func Die(c Palette, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s%s%s\n", c.Cross, fmt.Sprintf(format, a...), c.Reset)
	os.Exit(1)
}

// ============================================================================
// origem original: body_distro.go
// ============================================================================
// Package distro replica detect_distro(): lê o campo ID= de /etc/os-release
// (com fallback para /usr/lib/os-release), em vez de usar `uname -n`
// (hostname), que era o bug original do mkiso.


// Detect retorna o ID da distro (ex.: "void", "arch", "debian") ou "unknown"
// se nenhum dos dois arquivos existir ou não tiver o campo ID=.
func Detect() string {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if id, ok := readID(path); ok {
			return id
		}
	}
	return "unknown"
}

func readID(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") {
			value := strings.TrimPrefix(line, "ID=")
			value = strings.Trim(value, `"`)
			if value != "" {
				return value, true
			}
		}
	}
	return "", false
}

// ============================================================================
// origem original: body_privilege.go
// ============================================================================
// Package privilege replica sh_checkroot()/elevate_to_root(): se o processo
// não estiver rodando como root, tenta se re-executar via sudo (ou su como
// fallback), preservando os argumentos originais.


// CheckRoot verifica se o processo é root; se não for, tenta elevar
// privilégios re-executando o próprio binário via sudo/su. Se conseguir,
// syscall.Exec substitui o processo atual (não retorna). Se não conseguir
// elevar de forma nenhuma, termina o processo com die().
//
// dieFn é injetada para evitar dependência circular com o pacote logging.
func CheckRoot(args []string, dieFn func(format string, a ...any)) {
	if os.Geteuid() == 0 {
		return
	}
	elevateToRoot(args, dieFn)
}

func elevateToRoot(args []string, dieFn func(format string, a ...any)) {
	fmt.Printf("This script must be run as root. Elevating privileges...\n")

	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}

	// Tenta sudo primeiro.
	if sudoPath, lookErr := exec.LookPath("sudo"); lookErr == nil {
		argv := append([]string{sudoPath, self}, args...)
		env := os.Environ()
		// Se syscall.Exec suceder, ele substitui o processo e esta função
		// nunca retorna. Só chegamos na linha seguinte se ele falhar.
		_ = syscall.Exec(sudoPath, argv, env)
	}

	// Fallback: su -c "self arg1 arg2 ...". Diferente do sudo (que recebe os
	// argumentos como array, preservando espaços dentro de cada um), o `su -c`
	// espera uma ÚNICA string de comando -- por isso re-escapamos cada
	// argumento antes de juntar, evitando o bug de quoting que o
	// `su -c "$0 $*"` tinha na versão bash original.
	if suPath, lookErr := exec.LookPath("su"); lookErr == nil {
		cmd := shellJoin(append([]string{self}, args...))
		argv := []string{suPath, "-c", cmd}
		env := os.Environ()
		_ = syscall.Exec(suPath, argv, env)
	}

	dieFn("Error: Unable to elevate privileges. Run manually as root.")
}

// shellJoin re-escapa cada argumento (aspas simples) antes de juntar com
// espaço, para que `su -c` reconstrua a linha de comando corretamente mesmo
// quando algum argumento contém espaços ou caracteres especiais.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

// ============================================================================
// origem original: body_xbps.go
// ============================================================================
// Package xbps replica as chamadas ao xbps-install do mkiso original:
// cmd_install(), cmd_install_force(), install_packages(), ignore_packages()
// e copy_void_keys().


// Options agrupa os parâmetros necessários para montar a linha de comando do
// xbps-install (equivalente às globais $BASE_ARCH, $ROOTFS, $XBPS_REPOSITORY,
// $XBPS_CACHEDIR, $XBPS_INSTALL_CMD do bash original).
type Options struct {
	InstallCmd string
	BaseArch   string
	RootFS     string
	Repository string
	CacheDir   string
}

func (o Options) baseArgs(force bool) []string {
	args := []string{
		"--ignore-file-conflicts",
		"--unpack-only",
		"--rootdir", o.RootFS,
	}
	if o.Repository != "" {
		args = append(args, splitRepository(o.Repository)...)
	}
	args = append(args, "--cachedir", o.CacheDir, "--update")
	if force {
		args = append(args, "--force", "--force")
	}
	args = append(args, "--yes")
	return args
}

// splitRepository quebra a string "--repository=X --repository=Y" (como o
// bash monta) em argumentos separados para exec.Command.
func splitRepository(repo string) []string {
	var out []string
	cur := ""
	for _, r := range repo {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// Install equivale a cmd_install(): instala uma lista de pacotes SEM --force.
func (o Options) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	return o.run(false, pkgs)
}

// InstallForce equivale a cmd_install_force(): instala com --force --force
// (usado para os pacotes de config/skel que sobrescrevem arquivos de
// propósito).
func (o Options) InstallForce(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	return o.run(true, pkgs)
}

func (o Options) run(force bool, pkgs []string) error {
	installCmd := o.InstallCmd
	if installCmd == "" {
		installCmd = "xbps-install"
	}
	args := o.baseArgs(force)
	args = append(args, pkgs...)

	cmd := exec.Command(installCmd, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "XBPS_ARCH="+o.BaseArch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CopyVoidKeys equivale a copy_void_keys(): copia as chaves xbps (keys/*.plist)
// para dentro de <destino>/var/db/xbps/keys.
func CopyVoidKeys(dest string) error {
	target := filepath.Join(dest, "var", "db", "xbps", "keys")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("copy_void_keys: mkdir %s: %w", target, err)
	}
	matches, err := filepath.Glob("keys/*.plist")
	if err != nil {
		return fmt.Errorf("copy_void_keys: glob keys/*.plist: %w", err)
	}
	for _, src := range matches {
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("copy_void_keys: ler %s: %w", src, err)
		}
		dstPath := filepath.Join(target, filepath.Base(src))
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return fmt.Errorf("copy_void_keys: escrever %s: %w", dstPath, err)
		}
	}
	return nil
}

// IgnorePackages equivale a ignore_packages(): grava um "ignorepkg=<pkg>" por
// linha em <rootfs>/etc/xbps.d/mkiso-ignore.conf.
func IgnorePackages(rootfs string, pkgs []string) error {
	dir := filepath.Join(rootfs, "etc", "xbps.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ignore_packages: mkdir %s: %w", dir, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "mkiso-ignore.conf"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("ignore_packages: abrir arquivo: %w", err)
	}
	defer f.Close()
	for _, pkg := range pkgs {
		if _, err := fmt.Fprintf(f, "ignorepkg=%s\n", pkg); err != nil {
			return fmt.Errorf("ignore_packages: escrever: %w", err)
		}
	}
	return nil
}

// SyncRootdir equivale ao "xbps-install -Syu -r <rootdir> -R <repo>" usado em
// sh_install_rootfs() para sincronizar o índice de pacotes de um rootdir.
func SyncRootdir(installCmd, arch, rootdir, repository string) error {
	if installCmd == "" {
		installCmd = "xbps-install"
	}
	args := []string{"-Syu", "-r", rootdir}
	args = append(args, splitRepository(repository)...)

	cmd := exec.Command(installCmd, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "XBPS_ARCH="+arch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ============================================================================
// origem original: body_mount.go
// ============================================================================
// Package mount replica mount_pseudofs()/umount_pseudofs() do mkiso.
//
// NOTA DE DESIGN: o mkiso e o lib.sh originais tinham DUAS versões
// conflitantes dessas funções (o lib.sh sobrescrevia silenciosamente a do
// mkiso, montando /dev,/proc,/sys como somente-leitura e sem a flag de
// controle). Nesta reescrita optamos pela semântica do mkiso original
// (bind read-write, com flag de estado), por ser a que evita quebrar passos
// como o dracut que precisam escrever em /dev dentro do chroot.


// Pseudofs cuida do estado de montagem de dev/proc/sys dentro de um rootfs.
type Pseudofs struct {
	RootFS string
	bound  bool // equivalente a $LBIND
}

func NewPseudofs(rootfs string) *Pseudofs {
	return &Pseudofs{RootFS: rootfs}
}

// Mount equivale a mount_pseudofs(): bind-mount de /dev, /proc, /sys dentro
// do rootfs, em modo leitura-escrita, com propagação "rslave" (evita que o
// unmount se propague de volta para o host).
func (p *Pseudofs) Mount() error {
	if p.bound {
		return nil
	}
	for _, f := range []string{"proc", "sys", "dev"} {
		target := filepath.Join(p.RootFS, f)
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("mount_pseudofs: mkdir %s: %w", target, err)
		}
		if err := runCmd("mount", "--rbind", "/"+f, target); err != nil {
			return fmt.Errorf("mount_pseudofs: bind %s: %w", f, err)
		}
		// ESSENCIAL: evita propagação de desmontagem para o host.
		if err := runCmd("mount", "--make-rslave", target); err != nil {
			return fmt.Errorf("mount_pseudofs: make-rslave %s: %w", f, err)
		}
	}
	p.bound = true
	return nil
}

// Umount equivale a umount_pseudofs(): desmonta na ordem inversa (dev, proc,
// sys), tentando `umount -R` e caindo para `umount -l` (lazy) se necessário.
func (p *Pseudofs) Umount() {
	if !p.bound {
		return
	}
	for _, f := range []string{"dev", "proc", "sys"} {
		target := filepath.Join(p.RootFS, f)
		if !isMountpoint(target) {
			continue
		}
		if err := runCmd("umount", "-R", target); err != nil {
			_ = runCmd("umount", "-l", target)
		}
	}
	p.bound = false
}

// ForceCheckAndUmount ignora a flag `bound` e verifica diretamente no
// filesystem se algo ficou montado (ex.: de uma execução anterior que
// travou), desmontando se necessário. Equivale ao truque "LBIND=true antes de
// chamar umount_pseudofs" que usamos no bash para o mesmo propósito.
func (p *Pseudofs) ForceCheckAndUmount() {
	p.bound = true
	p.Umount()
}

func isMountpoint(path string) bool {
	return exec.Command("mountpoint", "-q", path).Run() == nil
}

// ============================================================================
// origem original: body_mirrors.go
// ============================================================================
// Package mirrors replica select_mirrors_dialog() e test_repo_online() do
// lib.sh original.
//
// Diferença notável em relação ao bash: aqui a função NUNCA é chamada
// automaticamente ao "carregar" o pacote (o bash original tinha o bug de
// chamar select_mirrors_dialog() incondicionalmente ao dar `source` no
// lib.sh, antes até da checagem de dependências -- ver análise de
// inconsistências). Nesta reescrita, quem decide quando chamar Select() é o
// main(), depois do parsing de flags.


// Mirror representa uma entrada candidata de mirror.
type Mirror struct {
	Label string
	Base  string
}

// DefaultMirrors replica a lista fixa usada tanto no modo --automatic quanto
// como opções do dialog interativo.
func DefaultMirrors() []Mirror {
	return []Mirror{
		{Label: "Mirror Brasil VoidLinux", Base: "/vg/void-mirror/extra"},
		{Label: "Mirror Brasil VoidLinux", Base: "/vg/void-mirror"},
		{Label: "Mirror Brasil VoidBR", Base: "https://void.voidbr.org/voidlinux"},
		{Label: "Mirror Brasil VoidLinux", Base: "https://void.voidlinux.com.br/voidlinux"},
		{Label: "Mirror Brasil ChiliLinux", Base: "https://void.chililinux.com/voidlinux"},
		{Label: "Mirror Oficial Fastly", Base: "https://repo-fastly.voidlinux.org"},
	}
}

// TestRepoOnline equivale a test_repo_online(): verifica se
// "<repo>/x86_64-repodata" responde (via HTTP HEAD com timeout de 20s para
// URLs remotas, ou os.Stat para caminhos locais).
func TestRepoOnline(repo string) bool {
	url := strings.TrimSuffix(repo, "/") + "/x86_64-repodata"

	if strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") {
		client := http.Client{Timeout: 20 * time.Second}
		resp, err := client.Head(url)
		if err != nil {
			fmt.Printf("Testando %s => OFFLINE\n", url)
			return false
		}
		defer resp.Body.Close()
		ok := resp.StatusCode >= 200 && resp.StatusCode < 400
		status := "OFFLINE"
		if ok {
			status = "ONLINE"
		}
		fmt.Printf("Testando %s => %s\n", url, status)
		return ok
	}

	_, err := os.Stat(url)
	ok := err == nil
	status := "OFFLINE"
	if ok {
		status = "ONLINE"
	}
	fmt.Printf("Testando %s => %s\n", url, status)
	return ok
}

// buildRepositoryArgs equivale ao bloco "case $mirror in ..." do bash, que
// monta uma ou mais entradas --repository= por mirror escolhido, com regras
// diferentes por mirror.
func buildRepositoryArgs(m Mirror) []string {
	switch m.Base {
	case "/vg/void-mirror/extra":
		return []string{"--repository=" + m.Base + "/current"}
	case "/vg/void-mirror":
		return []string{
			"--repository=" + m.Base + "/voidlinux/current",
			"--repository=" + m.Base + "/extra",
			"--repository=" + m.Base + "/voidlinux/current/nonfree",
			"--repository=" + m.Base + "/voidlinux/current/multilib",
			"--repository=" + m.Base + "/voidlinux/current/multilib/nonfree",
		}
	case "https://void.chililinux.com/voidlinux":
		return []string{"--repository=" + m.Base + "/extra"}
	case "https://void.voidbr.org/voidlinux", "https://void.voidlinux.com.br/voidlinux":
		return []string{
			"--repository=" + m.Base + "/current",
			"--repository=" + m.Base + "/extra",
			"--repository=" + m.Base + "/current/nonfree",
			"--repository=" + m.Base + "/current/multilib",
			"--repository=" + m.Base + "/current/multilib/nonfree",
		}
	default:
		return []string{
			"--repository=" + m.Base + "/current",
			"--repository=" + m.Base + "/current/nonfree",
			"--repository=" + m.Base + "/current/multilib",
			"--repository=" + m.Base + "/current/multilib/nonfree",
		}
	}
}

// Select equivale a select_mirrors_dialog(): em modo automático usa a lista
// fixa de mirrors; senão, chama o binário `dialog` para o usuário escolher.
// Ao cancelar o dialog, finaliza o processo (equivalente ao "exit 1" que
// corrigimos no bash -- não apenas um "return").
//
// Retorna a string final pronta para ser usada como argumento --repository=
// do xbps-install (múltiplas entradas separadas por espaço).
func Select(automatic bool) string {
	var chosen []Mirror

	if automatic {
		chosen = DefaultMirrors()
	} else {
		chosen = runDialog()
	}

	var repoArgs []string
	for _, m := range chosen {
		if !TestRepoOnline(m.Base + "/current") {
			continue
		}
		repoArgs = append(repoArgs, buildRepositoryArgs(m)...)
	}
	return strings.Join(repoArgs, " ")
}

// runDialog invoca o binário `dialog` externo para exibir a checklist de
// mirrors, exatamente como o bash original fazia. Se o usuário cancelar,
// finaliza o processo imediatamente (exit 1).
func runDialog() []Mirror {
	all := DefaultMirrors()

	args := []string{
		"--stdout",
		"--clear",
		"--backtitle", "Void Linux Mirror Selection",
		"--title", "Selecione um ou mais mirrors",
		"--checklist", "Use ESPAÇO para marcar e ENTER para confirmar:",
		"0", "0", "0",
	}
	for i, m := range all {
		status := "off"
		// replica o comportamento original: os dois primeiros e o Fastly
		// vêm marcados "on" por padrão.
		if i == 0 || i == 1 || m.Base == "https://repo-fastly.voidlinux.org" {
			status = "on"
		}
		args = append(args, m.Base, m.Label, status)
	}

	cmd := exec.Command("dialog", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fmt.Println("Cancelado.")
		os.Exit(1)
	}

	selectedBase := strings.TrimSpace(string(out))
	var result []Mirror
	for _, m := range all {
		if m.Base == selectedBase {
			result = append(result, m)
		}
	}
	return result
}

// ============================================================================
// origem original: body_packages.go
// ============================================================================
// Package packages replica as funções sh_choose_packages_*() do mkiso
// original, que populavam as variáveis globais REQUIRED_PKGS, ADDITIONAL_PKGS,
// COMMON_PKGS, ADISTRO_PKGS, AEXTRA_COMMON_PKGS, ACONFIG_PKGS, ASKEL_PKGS,
// AEXTRA_PKGS via `+=` sucessivos.

// Selection agrupa todas as listas de pacotes resultantes da escolha de
// desktop environment -- equivalente ao conjunto de variáveis globais que o
// bash populava.
type Selection struct {
	Required     []string
	Initramfs    []string
	Additional   []string
	Base         []string
	Common       []string
	ADistro      []string
	AExtraCommon []string
	AConfig      []string
	ASkel        []string
	AExtra       []string
}

// DesktopChoice replica as flags MAKE_* do mkiso.
type DesktopChoice struct {
	X             bool
	XfceBase      bool
	Xfce          bool
	Awesome       bool
	Enlightenment bool
	Fluxbox       bool
	Plasma        bool
	Gnome         bool
	Cinnamon      bool
	Mate          bool
	Sway          bool
	Mango         bool
	Hyprland      bool
	OnlyXorg      bool
	FullX         bool
}

// Choose equivale a sh_configure_packages(): monta a Selection final de
// acordo com as flags escolhidas na linha de comando.
func Choose(d DesktopChoice) Selection {
	var s Selection

	chooseRequired(&s)
	chooseAdditional(&s)
	chooseVoidbrBase(&s)

	if !d.X {
		return s
	}

	chooseCommonXBase(&s, d.Gnome)
	chooseVoidbrX(&s)
	if !d.OnlyXorg {
		chooseCommonXAdvanced(&s)
	}

	switch {
	case d.FullX:
		chooseXfceBase(&s)
		chooseXfce(&s)
		chooseGnome(&s)
		choosePlasma(&s)
		chooseCinnamon(&s)
		chooseMate(&s)
		chooseAwesome(&s)
		chooseEnlightenment(&s)
		chooseFluxbox(&s)
		chooseSway(&s)
		chooseMango(&s)
		chooseHyprland(&s)
	case d.Enlightenment:
		chooseEnlightenment(&s)
	case d.Awesome:
		chooseAwesome(&s)
	case d.Fluxbox:
		chooseFluxbox(&s)
	case d.Plasma:
		choosePlasma(&s)
	case d.Gnome:
		chooseGnome(&s)
	case d.XfceBase:
		chooseXfceBase(&s)
	case d.Xfce:
		chooseXfceBase(&s)
		chooseXfce(&s)
	case d.Cinnamon:
		chooseCinnamon(&s)
	case d.Mate:
		chooseMate(&s)
	case d.Sway:
		chooseSway(&s)
	case d.Mango:
		chooseMango(&s)
	case d.Hyprland:
		chooseHyprland(&s)
	}

	return s
}

func chooseRequired(s *Selection) {
	s.Required = append(s.Required,
		"base-files", "voidbr-vinstall", "coreutils", "binutils", "bash",
		"syslinux", "grub-i386-efi", "grub-x86_64-efi", "memtest86+",
		"squashfs-tools", "xorriso", "plymouth",
	)
	s.Initramfs = append(s.Initramfs,
		"device-mapper", "dhclient", "dracut", "dracut-network",
	)
}

func chooseAdditional(s *Selection) {
	s.Additional = append(s.Additional,
		"bash-completion", "sed", "tar", "gawk", "NetworkManager", "iw",
		"wireless_tools", "wpa_supplicant", "grc", "ncurses", "dialog",
		"gettext", "pv", "curl", "wget", "nano", "bc", "parted", "gptfdisk",
		"e2fsprogs", "lvm2", "xfsprogs", "jfsutils", "btrfs-progs",
		"f2fs-tools", "nilfs-utils", "ntfs-3g", "xz", "zstd", "pigz",
		"cryptsetup", "socklog-void", "chrony", "openssh", "dhcpcd", "htop",
		"tree", "rsync", "duf", "umc", "fzf", "exa", "fastfetch", "grub",
	)
	s.AConfig = append(s.AConfig, "voidbr-base-config", "voidbr-nano-config")
}

func chooseVoidbrBase(s *Selection) {
	s.Additional = append(s.Additional,
		"void-install", "voidbr-vinstall", "voidbr-grub-theme",
		"voidbr-utils", "chili-utils", "chili-clonedisk",
	)
	s.ASkel = append(s.ASkel, "voidbr-plymouth-theme")
}

func chooseVoidbrX(s *Selection) {
	s.AExtraCommon = append(s.AExtraCommon,
		"voidbr-wallpapers", "voidbr-tokyo-theme", "voidbr-professional-theme",
		"voidbr-beauty-icons", "voidbr-dracula-theme", "voidbr-webapps",
		"chili-iso2usb",
	)
}

func chooseCommonXBase(s *Selection, gnome bool) {
	s.Common = append(s.Common,
		"xorg", "xorg-apps", "xorg-fonts", "xauth", "xterm", "dbus-x11",
		"dbus-elogind", "elogind", "polkit-elogind", "xdg-user-dirs",
		"xdg-user-dirs-gtk", "xfce4-terminal", "libavif", "libheif",
		"gdk-pixbuf", "libheif-pixbuf-loader", "xorg-server-xwayland",
	)
	if gnome {
		s.Common = append(s.Common, "gdm")
	} else {
		s.Common = append(s.Common,
			"lightdm", "lightdm-gtk-greeter", "lightdm-gtk-greeter-settings",
			"arc-theme", "papirus-icon-theme", "dejavu-fonts-ttf",
		)
		s.AExtraCommon = append(s.AExtraCommon, "voidbr-lightdm-themes")
	}
}

func chooseCommonXAdvanced(s *Selection) {
	s.Common = append(s.Common,
		"cool-retro-term", "terminus-font", "firefox", "firefox-i18n-pt-BR",
		"noto-fonts-emoji", "ttf-ubuntu-font-family", "font-iosevka",
		"nerd-fonts-symbols-ttf", "ttf-jetbrains-mono-nerd", "font-awesome",
		"Adapta", "adwaita-icon-theme", "adwaita-plus",
		"network-manager-applet", "pulseaudio", "pavucontrol", "pasystray",
		"gst-plugins-bad1", "gst-plugins-good1", "gst-plugins-ugly1",
		"gst-plugins-base1", "xfce4-pulseaudio-plugin", "gparted",
		"octoxbps", "mate-calc", "7zip", "p7zip", "file-roller", "avahi",
		"avahi-discover", "gvfs", "gvfs-smb", "gvfs-cdda", "inetutils",
		"iputils",
	)
}

func chooseAwesome(s *Selection) {
	s.Additional = append(s.Additional, "awesome", "polybar", "pcmanfm")
}

func choosePlasma(s *Selection) {
	s.Additional = append(s.Additional, "kde5", "konsole", "kate", "dolphin")
	s.ASkel = append(s.ASkel, "voidbr-plasma-config")
}

func chooseGnome(s *Selection) {
	s.ADistro = append(s.ADistro,
		"gnome", "gnome-keyring", "accountsservice",
		"voidbr-change-gnome-wallpaper",
	)
	s.ASkel = append(s.ASkel, "voidbr-gnome-config")
}

func chooseFluxbox(s *Selection) {
	s.ADistro = append(s.ADistro, "fluxbox", "polybar", "pcmanfm", "xdgmenumaker")
}

func chooseEnlightenment(s *Selection) {
	s.ADistro = append(s.ADistro,
		"enlightenment", "efl", "rage", "terminology", "exquisite", "connman",
	)
	s.ASkel = append(s.ASkel, "voidbr-enlightenment-config")
}

func chooseXfceBase(s *Selection) {
	s.ADistro = append(s.ADistro,
		"xfce4", "Thunar", "xfce4-plugins", "xfce4-screenshooter",
		"xfce4-dict", "xfce4-datetime-plugin",
	)
}

func chooseXfce(s *Selection) {
	s.ADistro = append(s.ADistro, "thunar-archive-plugin", "thunar-volman", "plank")
	s.AExtra = append(s.AExtra, "voidbr-xfwm-axiom-theme")
	s.AConfig = append(s.AConfig, "voidbr-dracula-theme")
	s.ASkel = append(s.ASkel, "voidbr-xfce-config")
}

func chooseCinnamon(s *Selection) {
	s.ADistro = append(s.ADistro, "cinnamon-all", "plank", "playerctl")
	s.ASkel = append(s.ASkel, "voidbr-cinnamon-config")
}

func chooseMate(s *Selection) {
	s.ADistro = append(s.ADistro,
		"mate", "engrampa", "eom", "mate-applets", "mate-calc",
		"mate-indicator-applet", "mate-media", "mate-power-manager",
		"mate-sensors-applet", "mate-screensaver", "mate-system-monitor",
		"mate-terminal", "mate-utils", "mozo", "pluma", "caja-extensions",
		"yelp", "xreader", "plank", "brisk-menu", "mate-backgrounds",
		"mate-common",
	)
	s.AExtra = append(s.AExtra,
		"mate-icon-theme-faenza", "mate-media", "mate-netbook",
		"mate-power-manager", "mate-screensaver", "mate-sensors-applet",
		"mate-system-monitor", "mate-terminal", "mate-utils",
		"caja-image-converter", "caja-open-terminal", "caja-sendto",
		"caja-share", "caja-wallpaper", "caja-xattr-tags", "engrampa",
		"eom", "mozo", "pluma", "plank", "brisk-menu",
	)
	s.ASkel = append(s.ASkel, "voidbr-mate-config")
}

func chooseMango(s *Selection) {
	s.ADistro = append(s.ADistro, "voidbr-mango")
	s.ASkel = append(s.ASkel, "voidbr-mango-config")
}

func chooseSway(s *Selection) {
	s.ADistro = append(s.ADistro, "voidbr-sway")
	s.ASkel = append(s.ASkel, "voidbr-sway-config")

	// NOTA: no mkiso original, esta função tinha um `return` logo acima,
	// seguido da lista abaixo -- que por isso NUNCA executava (código morto
	// inalcançável). Preservada aqui apenas como comentário/referência, a
	// pedido, sem efeito nenhum na Selection:
	//
	// s.ADistro = append(s.ADistro, "sway")
	// s.AConfig = append(s.AConfig,
	// 	"dbus",                    // Message bus system
	// 	"elogind",                 // Gerenciador de login e sessões (seat management)
	// 	"foot",                    // Terminal leve e otimizado para Wayland
	// 	"wofi",                    // Lançador de aplicativos e menu de busca
	// 	"rofi",
	// 	"grim",                    // Utilitário para captura de tela (screenshot)
	// 	"slurp",                   // Seleção de área para captura de tela
	// 	"brightnessctl",           // Controle de brilho via linha de comando
	// 	"eom",                     // Visualizador de imagens (Eye of MATE)
	// 	"fuse-exfat",              // Suporte para montagem de sistemas exFAT
	// 	"ffmpeg",                  // Ferramentas para manipulação de áudio e vídeo
	// 	"mesa-dri",                // Drivers gráficos (Direct Rendering Infrastructure)
	// 	"mesa-vaapi",              // Aceleração de vídeo via hardware (VA-API)
	// 	"mesa-demos",              // Coleção de ferramentas de teste para o Mesa
	// 	"psmisc",                  // Utilitários de processo (killall, fuser, pstree)
	// 	"cpupower",                // Ferramenta de gerenciamento de frequência da CPU
	// 	"noto-fonts-ttf",          // Fontes Noto da Google
	// 	"font-awesome",            // Fontes de ícones para barras e interfaces
	// 	"optipng",                 // Otimizador de compressão para arquivos PNG
	// 	"sassc",                   // Compilador CSS (SASS)
	// 	"jq",                      // Processador de arquivos JSON via CLI
	// 	"swaylock",                // Bloqueador de tela para Wayland
	// 	"swayidle",                // Gerenciador de inatividade (idle) e bloqueio
	// 	"wl-clipboard",            // Ferramentas de clipboard (wl-copy/wl-paste)
	// 	"sway-audio-idle-inhibit", // Inibe o bloqueio/hibernação durante reprodução de áudio
	// 	"Waybar",                  // Barra de status customizável para Wayland
	// 	"wmenu",                   // Menu de seleção estilo dmenu/wofi para Wayland
	// 	"xdg-utils",               // Ferramentas de integração com a área de trabalho
	// 	"xdg-desktop-portal",      // Camada de comunicação entre apps e o desktop
	// 	"xdg-desktop-portal-gtk",  // Implementação GTK do portal XDG
	// 	"xdg-desktop-portal-wlr",  // Implementação WLR (Sway) do portal XDG
	// 	"rtkit",                   // Gerenciamento de prioridade de tempo real
	// 	"mate-polkit",             // Agente de autenticação para permissões root
	// 	"kanshi",                  // Gerenciador dinâmico de monitores
	// 	"Thunar",                  // Gerenciador de arquivos (XFCE)
	// 	"thunar-archive-plugin",   // Integração de compactação no Thunar
	// 	"engrampa",                // Gerenciador de arquivos compactados
	// 	"cliphist",                // Histórico da área de transferência
	// 	"mate-calc",               // Calculadora do ambiente MATE
	// 	"pluma",                   // Editor de texto leve (MATE)
	// 	"nwg-look",                // Configurador de temas GTK para Wayland
	// 	"atril",                   // Visualizador de documentos (PDFs)
	// 	"mako",                    // Servidor de notificações Wayland
	// 	"pipewire",                // Servidor de áudio moderno
	// 	"wireplumber",             // Gerenciador de sessão para PipeWire
	// 	"pavucontrol",             // Volume control applet for Pipewire
	// 	"alsa-pipewire",
	// 	"pulseaudio-utils",
	// 	"libspa-bluetooth",
	// 	"libjack-pipewire",
	// 	"alsa-plugins-pulseaudio",
	// 	"alsa-utils",
	// 	"firefox",                 // Mozilla Firefox web browser
	// 	"firefox-i18n-pt-BR",      // Firefox Portuguese (Brazilian) language pack
	// 	"lm_sensors",              // Utilities to read temperature/voltage/fan sensors
	// 	"xdg-user-dirs-gtk",       // GTK+ tool to help manage user directories
	// 	"xdg-user-dirs",           // Tool to help manage user directories
	// 	"acpi",
	// 	"acpid",
	// )
	// s.ASkel = append(s.ASkel, "voidbr-sway-config")
}

func chooseHyprland(s *Selection) {
	s.ADistro = append(s.ADistro, "voidbr-hyprland")
	s.ASkel = append(s.ASkel, "voidbr-hyprland-config")

	// NOTA: mesma situação do chooseSway acima -- código morto inalcançável
	// no original (havia um `return` antes deste bloco), preservado só como
	// comentário/referência:
	//
	// s.ADistro = append(s.ADistro, "hyprland")
	// s.AConfig = append(s.AConfig,
	// 	"hyprpaper",
	// 	"hyprlock",
	// 	"hypridle",
	// 	"hyprlauncher",
	// 	"hyprpicker-0.4.7_3",
	// 	"Waybar",
	// 	"rofi",
	// 	"wofi",
	// 	"mako",
	// 	"grim",
	// 	"slurp",
	// 	"swappy",
	// 	"wl-clipboard",
	// 	"brightnessctl",
	// 	"playerctl",
	// 	"xdg-desktop-portal-hyprland",
	// 	"xorg-server-xwayland",
	// 	"lua55",
	// 	"dbus",           // Message bus system
	// 	"elogind",        // Gerenciador de login e sessões (seat management)
	// 	"foot",           // Terminal leve e otimizado para Wayland
	// 	"eom",            // Visualizador de imagens (Eye of MATE)
	// 	"fuse-exfat",     // Suporte para montagem de sistemas exFAT
	// 	"ffmpeg",         // Ferramentas para manipulação de áudio e vídeo
	// 	"mesa-dri",       // Drivers gráficos (Direct Rendering Infrastructure)
	// 	"mesa-vaapi",     // Aceleração de vídeo via hardware (VA-API)
	// 	"mesa-demos",     // Coleção de ferramentas de teste para o Mesa
	// 	"psmisc",         // Utilitários de processo (killall, fuser, pstree)
	// 	"cpupower",       // Ferramenta de gerenciamento de frequência da CPU
	// 	"noto-fonts-ttf", // Fontes Noto da Google
	// 	"font-awesome",   // Fontes de ícones para barras e interfaces
	// 	"optipng",        // Otimizador de compressão para arquivos PNG
	// 	"sassc",          // Compilador CSS (SASS)
	// 	"jq",             // Processador de arquivos JSON via CLI
	// 	"sway-audio-idle-inhibit",
	// 	"xdg-desktop-portal-gtk", // Implementação GTK do portal XDG
	// 	"rtkit",                  // Gerenciamento de prioridade de tempo real
	// 	"mate-polkit",            // Agente de autenticação para permissões root
	// 	"mate-calc",              // Calculadora do ambiente MATE
	// 	"kanshi",                 // Gerenciador dinâmico de monitores
	// 	"Thunar",                 // Gerenciador de arquivos (XFCE)
	// 	"thunar-archive-plugin",  // Integração de compactação no Thunar
	// 	"engrampa",               // Gerenciador de arquivos compactados
	// 	"cliphist",               // Histórico da área de transferência
	// 	"pluma",                  // Editor de texto leve (MATE)
	// 	"nwg-look",               // Configurador de temas GTK para Wayland
	// 	"atril",                  // Visualizador de documentos (PDFs)
	// 	"mako",                   // Servidor de notificações Wayland
	// 	"pipewire",               // Servidor de áudio moderno
	// 	"wireplumber",            // Gerenciador de sessão para PipeWire
	// 	"pavucontrol",            // Volume control applet for Pipewire
	// 	"alsa-pipewire",
	// 	"pulseaudio-utils",
	// 	"libspa-bluetooth",
	// 	"libjack-pipewire",
	// 	"alsa-plugins-pulseaudio",
	// 	"alsa-utils",
	// 	"firefox",            // Mozilla Firefox web browser
	// 	"firefox-i18n-pt-BR", // Firefox Portuguese (Brazilian) language pack
	// 	"lm_sensors",         // Utilities to read temperature/voltage/fan sensors
	// 	"xdg-user-dirs-gtk",  // GTK+ tool to help manage user directories
	// 	"xdg-user-dirs",      // Tool to help manage user directories
	// 	"acpi",
	// 	"acpid",
	// )
	// s.ASkel = append(s.ASkel, "voidbr-hyprland-config")
}

// ============================================================================
// origem original: body_system.go
// ============================================================================
// Package system replica as funções de configuração de serviços/plymouth/
// skel do mkiso original: configure_plymouth, configure_service_*,
// enable_services, install_skel_root, sh_reconfigure_pass1/2, finish_clean,
// verificar_espaco_livre e sh_configure_void_install_desktop.

func writeFile(path, content string, mode os.FileMode) error {
	return os.WriteFile(path, []byte(content), mode)
}

const plymouthLogScript = `#!/usr/bin/env bash

file=/var/log/boot.log
plymouth display-message --text="Booting Void Linux"
dmesg -f daemon,user,auth | while read -r line; do
  plymouth display-message --text="${line:0:50}"
  echo "${line:0:50}"
  sleep 0.2
  if ! plymouth --ping; then
    echo "Plymouth morreu, saindo..."
    break
  fi
done
plymouth display-message --text="Initialization completed"
`

// ConfigurePlymouth equivale a configure_plymouth().
func ConfigurePlymouth(rootfs string) error {
	dracutConfDir := filepath.Join(rootfs, "etc", "dracut.conf.d")
	if err := os.MkdirAll(dracutConfDir, 0o755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dracutConfDir, "plymouth.conf"), `add_dracutmodules+=" plymouth "`+"\n", 0o644); err != nil {
		return err
	}

	if err := runCmd("chroot", rootfs, "env", "-i", "plymouth-set-default-theme", "voidbr"); err != nil {
		return fmt.Errorf("falha ao configurar tema plymouth: %w", err)
	}

	binDir := filepath.Join(rootfs, "usr", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	scriptPath := filepath.Join(binDir, "plymouth-show-log-voidlinux.sh")
	if err := writeFile(scriptPath, plymouthLogScript, 0o755); err != nil {
		return err
	}

	runitFile := filepath.Join(rootfs, "etc", "runit", "1")
	data, err := os.ReadFile(runitFile)
	if err == nil && !strings.Contains(string(data), "plymouth-show-log-voidlinux") {
		injected := insertAfterShebang(string(data),
			"[ -r /usr/bin/plymouth-show-log-voidlinux.sh ] && . /usr/bin/plymouth-show-log-voidlinux.sh\n")
		if err := os.WriteFile(runitFile, []byte(injected), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// insertAfterShebang replica o `sed -i '/^#!/a\...' `: insere uma linha logo
// após a primeira linha que começa com "#!".
func insertAfterShebang(content, toInsert string) string {
	lines := strings.Split(content, "\n")
	var out []string
	inserted := false
	for _, l := range lines {
		out = append(out, l)
		if !inserted && strings.HasPrefix(l, "#!") {
			out = append(out, strings.TrimSuffix(toInsert, "\n"))
			inserted = true
		}
	}
	return strings.Join(out, "\n")
}

const bootLoggerScript = `#!/bin/sh
LOG_FILE="/var/log/boot.log"
mkdir -p /var/log
exec dmesg -w >> "$LOG_FILE" 2>&1
`

// ConfigureServiceBootLogger equivale a configure_service_boot_logger().
func ConfigureServiceBootLogger(rootfs string) error {
	dir := filepath.Join(rootfs, "etc", "sv", "boot-logger")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "run"), bootLoggerScript, 0o755); err != nil {
		return err
	}
	return symlinkService(rootfs, "boot-logger")
}

// ConfigureServicePlymouthLog equivale a configure_service_plymouth_log().
func ConfigureServicePlymouthLog(rootfs string) error {
	dir := filepath.Join(rootfs, "etc", "sv", "plymouth-log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	script := "#!/bin/sh\n[ -r ./conf ] && . ./conf\nexec /usr/bin/plymouth-show-log-voidlinux.sh\n"
	if err := writeFile(filepath.Join(dir, "run"), script, 0o755); err != nil {
		return err
	}
	return symlinkService(rootfs, "plymouth-log")
}

// ConfigureServicePlymouthStop equivale a configure_service_plymouth_stop().
func ConfigureServicePlymouthStop(rootfs string) error {
	dir := filepath.Join(rootfs, "etc", "sv", "plymouth-stop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	script := "#!/bin/sh\n[ -r ./conf ] && . ./conf\nexec /usr/bin/plymouth --quit\n"
	if err := writeFile(filepath.Join(dir, "run"), script, 0o755); err != nil {
		return err
	}
	return symlinkService(rootfs, "plymouth-stop")
}

func symlinkService(rootfs, service string) error {
	link := filepath.Join(rootfs, "etc", "runit", "runsvdir", "default", service)
	target := filepath.Join("/etc/sv", service)
	_ = os.Remove(link)
	return os.Symlink(target, link)
}

// ConfigureEtcRunitCoreServicesQuitPlymouth equivale a
// configure_etc_runit_core_services_quit_plymouth().
func ConfigureEtcRunitCoreServicesQuitPlymouth(rootfs string) error {
	dir := filepath.Join(rootfs, "etc", "runit", "core-services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	script := "#!/bin/sh\nif command -v plymouth >/dev/null; then\n    plymouth quit\nfi\n"
	return writeFile(filepath.Join(dir, "99-voidbr-plymouth-quit.sh"), script, 0o755)
}

// EnableServices equivale a enable_services(): cria o symlink de cada
// serviço em /etc/runit/runsvdir/default, se o serviço existir em /etc/sv.
func EnableServices(rootfs string, services []string) {
	for _, service := range services {
		if service == "" {
			continue
		}
		svDir := filepath.Join(rootfs, "etc", "sv", service)
		if _, err := os.Stat(svDir); err != nil {
			fmt.Printf("service %s not found in %s/etc/sv\n", service, rootfs)
			continue
		}
		link := filepath.Join(rootfs, "etc", "runit", "runsvdir", "default", service)
		_ = os.Remove(link)
		if err := os.Symlink(filepath.Join("/etc/sv", service), link); err != nil {
			fmt.Printf("failed to enable service %s (broken symlink)\n", service)
		}
	}
}

// InstallSkelRoot equivale a install_skel_root(): copia os dotfiles de skel
// para /root (ignorando erros individuais, como o `|| true` do bash).
func InstallSkelRoot(rootfs string) {
	patterns := []string{
		".bash*", ".git-prompt.sh", ".dialogrc", ".dircolors", ".ps*",
	}
	skelDir := filepath.Join(rootfs, "etc", "skel")
	rootDir := filepath.Join(rootfs, "root")
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(skelDir, pattern))
		for _, m := range matches {
			dst := filepath.Join(rootDir, filepath.Base(m))
			_ = copyFileOrDir(m, dst)
		}
	}
}

func copyFileOrDir(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			target := filepath.Join(dst, rel)
			if fi.IsDir() {
				return os.MkdirAll(target, fi.Mode())
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, fi.Mode())
		})
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}

const voidInstallDesktop = `[Desktop Entry]
Name=VoidBR Install
Comment=Executa o instalador VoidBR Linux
Exec=xfce4-terminal --geometry=162x50 --hide-menubar --hide-toolbar --disable-server --zoom=0 -x sudo void-install
Type=Application
DesktopNames=VoidInstall
`

// ConfigureVoidInstallDesktop equivale a sh_configure_void_install_desktop().
func ConfigureVoidInstallDesktop(rootfs string) error {
	dir := filepath.Join(rootfs, "usr", "share", "xsessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "void-install.desktop"), voidInstallDesktop, 0o644)
}

// ReconfigurePass1 equivale a sh_reconfigure_pass1().
func ReconfigurePass1(rootfs, locale string) error {
	_ = runCmd("xbps-reconfigure", "-r", rootfs, "-f", "base-files")
	if err := runCmd("chroot", rootfs, "env", "-i", "xbps-reconfigure", "-f", "base-files"); err != nil {
		return err
	}
	localesFile := filepath.Join(rootfs, "etc", "default", "libc-locales")
	data, err := os.ReadFile(localesFile)
	if err != nil {
		return nil // arquivo pode não existir, equivalente ao `[ -f ... ]` do bash
	}
	uncommented := uncommentLocale(string(data), locale)
	return os.WriteFile(localesFile, []byte(uncommented), 0o644)
}

func uncommentLocale(content, locale string) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "#") {
			rest := l[1:]
			if strings.HasPrefix(rest, locale) {
				lines[i] = rest
			}
		}
	}
	return strings.Join(lines, "\n")
}

// ReconfigurePass2 equivale a sh_reconfigure_pass2().
func ReconfigurePass2(rootfs string) error {
	return runCmd("chroot", rootfs, "env", "-i", "xbps-reconfigure", "-a")
}

// FinishClean equivale a finish_clean().
func FinishClean(rootfs string) {
	_ = runCmd("xbps-remove", "-r", rootfs, "-Ooy")
	_ = os.RemoveAll(filepath.Join(rootfs, "var", "cache"))
	_ = os.RemoveAll(filepath.Join(rootfs, "run"))
	_ = os.RemoveAll(filepath.Join(rootfs, "var", "run"))
}

// VerificarEspacoLivre equivale a verificar_espaco_livre(): garante ao menos
// 15GB livres no diretório de build, usando syscall.Statfs (sem precisar
// invocar `df` externo).
func VerificarEspacoLivre(dir string, dieFn func(format string, a ...any)) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		dieFn("Não foi possível verificar espaço livre em %s: %v", dir, err)
		return
	}
	freeMB := int64(stat.Bavail) * int64(stat.Bsize) / (1024 * 1024)
	if freeMB < 15360 {
		dieFn("Espaço insuficiente em %s. São necessários pelo menos 15GB livres.", dir)
	}
}

// ============================================================================
// origem original: body_build.go
// ============================================================================
// Package build replica as funções de geração de imagem do mkiso original:
// generate_initramfs, generate_isolinux_boot, generate_grub_efi_boot,
// generate_squashfs, generate_iso_image, get_compression_options,
// cleanup_rootfs, configure_plymouth e as cópias de arquivos de boot.


// Paths agrupa os diretórios usados pelas etapas de build (equivalente às
// globais $ROOTFS, $HOSTDIR, $BOOT_DIR, $IMAGEDIR, $ISOLINUX_DIR, $GRUB_DIR).
type Paths struct {
	RootFS      string
	HostDir     string
	BootDir     string
	ImageDir    string
	IsolinuxDir string
	GrubDir     string
	BuildDir    string
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCmdIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// copyFile copia um arquivo preservando o modo de permissão.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// copyDirAll copia recursivamente todo o conteúdo de src para dst
// (equivalente a `cp -a`/`cp -Rpva`), preservando symlinks.
func copyDirAll(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode())
		default:
			return copyFile(path, target)
		}
	})
}

// CopyDracutFiles equivale a copy_dracut_files(): copia dracut/vmklive/* para
// dentro do rootfs de destino, em usr/lib/dracut/modules.d/01vmklive, e cria
// o marcador noautologin se necessário.
func CopyDracutFiles(rootfs string, noAutologin bool) error {
	target := filepath.Join(rootfs, "usr", "lib", "dracut", "modules.d", "01vmklive")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := copyDirAll("dracut/vmklive", target); err != nil {
		return err
	}
	if noAutologin {
		f, err := os.Create(filepath.Join(target, "noautologin"))
		if err != nil {
			return err
		}
		f.Close()
	}
	return nil
}

// CopyAutoinstallerFiles equivale a copy_autoinstaller_files().
func CopyAutoinstallerFiles(rootfs string) error {
	target := filepath.Join(rootfs, "usr", "lib", "dracut", "modules.d", "01autoinstaller")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	return copyDirAll("dracut/autoinstaller", target)
}

// CopyIncludeDirectories equivale a copy_include_directories(): copia o
// conteúdo (não o diretório em si) de cada includedir para dentro do rootfs.
func CopyIncludeDirectories(rootfs string, includeDirs []string) error {
	for _, dir := range includeDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("copy_include_directories: %s: %w", dir, err)
		}
		for _, e := range entries {
			src := filepath.Join(dir, e.Name())
			dst := filepath.Join(rootfs, e.Name())
			if err := copyDirAll(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

// GenerateInitramfs equivale a generate_initramfs(): roda o dracut dentro do
// chroot e move o initrd/vmlinuz gerados para o diretório de boot da ISO.
func GenerateInitramfs(p Paths, kernelVersion, compression string) error {
	if kernelVersion == "" {
		entries, err := os.ReadDir(filepath.Join(p.RootFS, "lib", "modules"))
		if err != nil || len(entries) == 0 {
			return fmt.Errorf("erro: kernel não foi encontrado instalado em %s/lib/modules", p.RootFS)
		}
		kernelVersion = entries[0].Name()
	}

	args := []string{
		p.RootFS, "env", "PYTHONWARNINGS=ignore::SyntaxWarning",
		"/usr/bin/dracut",
		"-N",
		"--" + compression,
		"--add-drivers", "ahci",
		"--force-add", "vmklive autoinstaller",
		"--omit", "systemd", "/boot/initrd",
		kernelVersion,
	}
	cmd := exec.Command("chroot", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to generate the initramfs: %w", err)
	}

	initrdSrc := filepath.Join(p.RootFS, "boot", "initrd")
	if _, err := os.Stat(initrdSrc); err != nil {
		return fmt.Errorf("erro crítico: o arquivo %s não foi gerado pelo dracut", initrdSrc)
	}

	if err := os.Rename(initrdSrc, filepath.Join(p.BootDir, "initrd")); err != nil {
		return err
	}
	if err := copyFile(
		filepath.Join(p.RootFS, "boot", "vmlinuz-"+kernelVersion),
		filepath.Join(p.BootDir, "vmlinuz"),
	); err != nil {
		return err
	}
	return copyDirAll(
		filepath.Join(p.RootFS, "boot", "grub", "themes"),
		filepath.Join(p.BootDir, "grub", "themes"),
	)
}

// CleanupRootfs equivale a cleanup_rootfs(): remove pacotes de initramfs
// órfãos (ou marca como automático se ainda tiver revdeps) e apaga os módulos
// dracut temporários copiados para o rootfs.
func CleanupRootfs(rootfs string, initramfsPkgs []string) error {
	for _, pkg := range initramfsPkgs {
		revdeps, _ := exec.Command("xbps-query", "-r", rootfs, "-X", pkg).Output()
		if strings.TrimSpace(string(revdeps)) != "" {
			_ = runCmd("xbps-pkgdb", "-r", rootfs, "-m", "auto", pkg)
		} else {
			_ = runCmd("xbps-remove", "-r", rootfs, "-Ry", pkg)
		}
	}
	_ = os.RemoveAll(filepath.Join(rootfs, "usr", "lib", "dracut", "modules.d", "01vmklive"))
	_ = os.RemoveAll(filepath.Join(rootfs, "usr", "lib", "dracut", "modules.d", "01autoinstaller"))
	return nil
}

// GetCompressionOptions equivale a get_compression_options(): traduz o nome
// do algoritmo de compressão nas flags correspondentes do mksquashfs.
func GetCompressionOptions(kind string) ([]string, error) {
	switch kind {
	case "gzip":
		return []string{"-noappend", "-comp", "gzip", "-Xcompression-level", "1"}, nil
	case "xz":
		return []string{"-noappend", "-b", "1048576", "-comp", "xz", "-Xdict-size", "100%"}, nil
	case "zstd":
		return []string{"-noappend", "-b", "1M", "-comp", "zstd", "-Xcompression-level", "3"}, nil
	case "lzma":
		return []string{"-noappend", "-b", "1M", "-comp", "lzma"}, nil
	case "lz4":
		return []string{"-noappend", "-b", "1M", "-comp", "lz4", "-Xhc"}, nil
	case "bzip2":
		return []string{"-noappend", "-b", "1M", "-comp", "bzip2"}, nil
	case "lzo":
		return []string{"-noappend", "-b", "1M", "-comp", "lzo"}, nil
	default:
		return nil, fmt.Errorf("tipo de compressão squashfs desconhecido: %q (use lz4|gzip|bzip2|xz|zstd|lzma|lzo)", kind)
	}
}

// dirSizeMB replica `du --apparent-size -sm`.
func dirSizeMB(dir string) (int64, error) {
	var total int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total / (1024 * 1024), err
}

// GenerateSquashfs equivale a generate_squashfs() já otimizado: usa
// `mke2fs -d` para popular a imagem ext3 direto do ROOTFS (sem
// mount+cp+umount), sem journal, com folga de ~1.15x + 256M em vez de 2x, e
// mksquashfs com -processors explícito.
func GenerateSquashfs(p Paths, squashCompression string, nproc int) error {
	rootfsSize, err := dirSizeMB(p.RootFS)
	if err != nil {
		return fmt.Errorf("generate_squashfs: du: %w", err)
	}

	liveOSDir := filepath.Join(p.BuildDir, "tmp", "LiveOS")
	if err := os.MkdirAll(liveOSDir, 0o755); err != nil {
		return err
	}

	imgSize := (rootfsSize*115)/100 + 256
	imgPath := filepath.Join(liveOSDir, "ext3fs.img")

	if err := runCmd("truncate", "-s", fmt.Sprintf("%dM", imgSize), imgPath); err != nil {
		return fmt.Errorf("generate_squashfs: truncate: %w", err)
	}

	// mke2fs -d popula o filesystem DIRETO a partir do ROOTFS, sem precisar
	// montar via loop + cp -a + umount. Journal desabilitado (^has_journal)
	// porque essa imagem só existe para ser lida uma vez pelo mksquashfs e
	// depois é apagada.
	if err := runCmd("mkfs.ext3", "-F", "-q", "-O", "^has_journal", "-d", p.RootFS, "-m1", imgPath); err != nil {
		return fmt.Errorf("generate_squashfs: mkfs.ext3 -d: %w", err)
	}

	liveOSOutDir := filepath.Join(p.ImageDir, "LiveOS")
	if err := os.MkdirAll(liveOSOutDir, 0o755); err != nil {
		return err
	}

	squashOut := filepath.Join(liveOSOutDir, "squashfs.img")
	if nproc <= 0 {
		nproc = 1
	}
	args := []string{
		filepath.Join(p.BuildDir, "tmp"), squashOut,
		"-noappend", "-b", "1M", "-comp", squashCompression, "-Xcompression-level", "1",
		"-processors", strconv.Itoa(nproc),
	}
	if err := runCmd(filepath.Join(p.HostDir, "usr/bin/mksquashfs"), args...); err != nil {
		return fmt.Errorf("failed to generate squashfs image: %w", err)
	}

	if err := os.Chmod(squashOut, 0o444); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(p.BuildDir, "tmp"))
}

// GenerateIsolinuxBoot equivale a generate_isolinux_boot().
func GenerateIsolinuxBoot(p Paths, syslinuxDataDir, splashImage, kernelVersion, keymap, arch, locale, bootTitle, bootCmdline, hostDir string) error {
	files := []string{"isolinux.bin", "ldlinux.c32", "libcom32.c32", "vesamenu.c32", "libutil.c32", "chain.c32", "reboot.c32", "poweroff.c32"}
	for _, f := range files {
		if err := copyFile(filepath.Join(syslinuxDataDir, f), filepath.Join(p.IsolinuxDir, f)); err != nil {
			return fmt.Errorf("generate_isolinux_boot: copiar %s: %w", f, err)
		}
	}
	if err := copyFile("isolinux/isolinux.cfg.in", filepath.Join(p.IsolinuxDir, "isolinux.cfg")); err != nil {
		return err
	}
	if err := copyFile(splashImage, filepath.Join(p.IsolinuxDir, filepath.Base(splashImage))); err != nil {
		return err
	}

	cfgPath := filepath.Join(p.IsolinuxDir, "isolinux.cfg")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	replacer := strings.NewReplacer(
		"@@SPLASHIMAGE@@", filepath.Base(splashImage),
		"@@KERNVER@@", kernelVersion,
		"@@KEYMAP@@", keymap,
		"@@ARCH@@", arch,
		"@@LOCALE@@", locale,
		"@@BOOT_TITLE@@", bootTitle,
		"@@BOOT_CMDLINE@@", bootCmdline,
	)
	if err := os.WriteFile(cfgPath, []byte(replacer.Replace(string(data))), 0o644); err != nil {
		return err
	}

	return copyFile(filepath.Join(hostDir, "boot/memtest86+/memtest.bin"), filepath.Join(p.BootDir, "memtest.bin"))
}

// GenerateGrubEfiBoot equivale a generate_grub_efi_boot(). Os dois
// grub-mkstandalone (i386-efi e x86_64-efi) rodam em paralelo via
// goroutines, já que são independentes entre si -- equivalente ao "&"/"wait"
// que usamos na versão bash otimizada.
func GenerateGrubEfiBoot(p Paths, grubDataDir, splashImage, kernelVersion, keymap, arch, locale, bootTitle, bootCmdline, syslinuxDataDir, hostDir string) error {
	if err := copyFile("grub/grub.cfg", filepath.Join(p.GrubDir, "grub.cfg")); err != nil {
		return err
	}
	if err := copyFile("grub/grub_void.cfg.in", filepath.Join(p.GrubDir, "grub_void.cfg")); err != nil {
		return err
	}

	cfgPath := filepath.Join(p.GrubDir, "grub_void.cfg")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	replacer := strings.NewReplacer(
		"@@SPLASHIMAGE@@", filepath.Base(splashImage),
		"@@KERNVER@@", kernelVersion,
		"@@KEYMAP@@", keymap,
		"@@ARCH@@", arch,
		"@@BOOT_TITLE@@", bootTitle,
		"@@BOOT_CMDLINE@@", bootCmdline,
		"@@LOCALE@@", locale,
	)
	if err := os.WriteFile(cfgPath, []byte(replacer.Replace(string(data))), 0o644); err != nil {
		return err
	}

	fontsDir := filepath.Join(p.GrubDir, "fonts")
	if err := os.MkdirAll(fontsDir, 0o755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(grubDataDir, "unicode.pf2"), filepath.Join(fontsDir, "unicode.pf2")); err != nil {
		return err
	}

	_ = runCmd("modprobe", "-q", "loop")

	efibootImg := filepath.Join(p.GrubDir, "efiboot.img")
	if err := runCmd("truncate", "-s", "32M", efibootImg); err != nil {
		return err
	}
	if err := runCmd("mkfs.vfat", "-F12", "-S", "512", "-n", "GRUB_UEFI", efibootImg); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp(os.Getenv("HOME"), "grub-efi-")
	if err != nil {
		return err
	}
	loopOut, err := exec.Command("losetup", "--show", "--find", efibootImg).Output()
	if err != nil {
		return fmt.Errorf("generate_grub_efi_boot: losetup: %w", err)
	}
	loopDevice := strings.TrimSpace(string(loopOut))

	cleanup := func() {
		_ = runCmd("umount", tmpDir)
		_ = runCmd("losetup", "--detach", loopDevice)
	}

	if err := runCmd("mount", "-o", "rw,flush", "-t", "vfat", loopDevice, tmpDir); err != nil {
		_ = runCmd("losetup", "--detach", loopDevice)
		return err
	}

	if err := copyDirAll(filepath.Join(p.ImageDir, "boot"), filepath.Join(hostDir, "boot")); err != nil {
		cleanup()
		return err
	}

	// Os dois grub-mkstandalone são independentes (diretório/formato/saída
	// diferentes) -- rodam em paralelo para ganhar tempo.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	targets := []struct {
		dir, format, out string
	}{
		{"/usr/lib/grub/i386-efi", "i386-efi", "/tmp/bootia32.efi"},
		{"/usr/lib/grub/x86_64-efi", "x86_64-efi", "/tmp/bootx64.efi"},
	}
	wg.Add(2)
	for i, t := range targets {
		i, t := i, t
		go func() {
			defer wg.Done()
			errs[i] = runCmdIn(hostDir, "xbps-uchroot", hostDir, "grub-mkstandalone", "--",
				"--directory="+t.dir, "--format="+t.format, "--output="+t.out, "boot/grub/grub.cfg")
		}()
	}
	wg.Wait()

	if errs[0] != nil || errs[1] != nil {
		cleanup()
		return fmt.Errorf("failed to generate EFI loader: i386=%v x86_64=%v", errs[0], errs[1])
	}

	efiBootDir := filepath.Join(tmpDir, "EFI", "BOOT")
	if err := os.MkdirAll(efiBootDir, 0o755); err != nil {
		cleanup()
		return err
	}
	if err := copyFile(filepath.Join(hostDir, "tmp/bootia32.efi"), filepath.Join(efiBootDir, "BOOTIA32.EFI")); err != nil {
		cleanup()
		return err
	}
	if err := copyFile(filepath.Join(hostDir, "tmp/bootx64.efi"), filepath.Join(efiBootDir, "BOOTX64.EFI")); err != nil {
		cleanup()
		return err
	}
	cleanup()

	return copyFile(filepath.Join(hostDir, "boot/memtest86+/memtest.efi"), filepath.Join(p.BootDir, "memtest.efi"))
}

// GenerateIsoImage equivale a generate_iso_image().
func GenerateIsoImage(p Paths, hostDir, syslinuxDataDir, volID, outputFile string) error {
	xorrisoBin := filepath.Join(hostDir, "usr/bin/xorriso")
	args := []string{
		"-as", "mkisofs",
		"-iso-level", "3",
		"-rock",
		"-joliet",
		"-max-iso9660-filenames",
		"-omit-period",
		"-omit-version-number",
		"-relaxed-filenames",
		"-allow-lowercase",
		"-volid", volID,
		"-eltorito-boot", "boot/isolinux/isolinux.bin",
		"-eltorito-catalog", "boot/isolinux/boot.cat",
		"-no-emul-boot",
		"-boot-load-size", "4", "-boot-info-table",
		"-eltorito-alt-boot", "-e", "boot/grub/efiboot.img",
		"-isohybrid-gpt-basdat",
		"-no-emul-boot",
		"-isohybrid-mbr", filepath.Join(syslinuxDataDir, "isohdpfx.bin"),
		"-output", outputFile, p.ImageDir,
	}
	if err := runCmd(xorrisoBin, args...); err != nil {
		return fmt.Errorf("failed to generate ISO image: %w", err)
	}
	return nil
}

// ============================================================================
// origem original: body_main.go
// ============================================================================
// Comando mkiso: reescrita em Go do script mkiso/lib.sh original (bash).
//
// Mantém a mesma arquitetura de orquestração (chama xbps-install, dracut,
// mksquashfs, xorriso, grub-mkstandalone etc. via os/exec), mas com parsing
// de flags, controle de erros e concorrência nativos do Go.


const version = "0.06.28-20260628"

func main() {
	cfg := NewConfig()
	cfg.App = filepath.Base(os.Args[0])

	// --automatic precisa ser detectado ANTES de qualquer coisa, porque tanto
	// a elevação de privilégio (re-exec) quanto a seleção de mirrors dependem
	// dele, e queremos preservar os argumentos originais numa eventual
	// re-execução como root.
	originalArgs := append([]string{}, os.Args[1:]...)
	args, automatic := extractAutomatic(originalArgs)
	cfg.Automatic = automatic

	pal := NewPalette(IsTerminal())

	// -h/--help e --version são tratados antes de qualquer trabalho pesado
	// (mesmo padrão do bash original).
	for _, a := range args {
		if a == "-h" || a == "--help" {
			printUsage(cfg.App)
			os.Exit(0)
		}
		if a == "--version" {
			printVersion(pal, cfg.App)
			os.Exit(0)
		}
	}

	// Eleva privilégios se necessário, preservando os argumentos ORIGINAIS
	// (incluindo --automatic), igual fizemos na versão bash.
	CheckRoot(originalArgs, func(format string, a ...any) {
		Die(pal, format, a...)
	})

	cfg.Distro = Detect()

	positional := parseFlags(args, cfg)

	log := NewLogger(pal, cfg.Quiet, cfg.StepCount)

	dependencies := []string{"rsync", "sed", "grep", "cat", "umount", "losetup", "tput", "chroot", "mkfs.vfat", "tee"}
	checkDependencies(dependencies, cfg.Distro, log, pal)

	applyPositionalDesktop(cfg, positional)

	// Checagem de root extra (cinto e suspensórios, como decidido): a esta
	// altura já deveria ser sempre root, já que CheckRoot rodou
	// antes -- mas mantemos como camada de segurança adicional.
	if os.Geteuid() != 0 {
		Die(pal, "Must be run as root, exiting...")
	}

	// Seleção de mirrors: chamada explicitamente aqui (depois de todo o
	// parsing de flags), para não disparar em opções rápidas como -h/-V, e
	// para combinar corretamente com -r (ExtraRepository).
	mirrorRepos := Select(cfg.Automatic)
	cfg.XbpsRepository = strings.TrimSpace(cfg.ExtraRepository + " " + mirrorRepos)

	configDistroName(cfg)
	configureRootfs(cfg, log, pal)
	pf := NewPseudofs(cfg.RootFS)
	pf.ForceCheckAndUmount()

	configurePackages(cfg)

	installRootfs(cfg, log, pal, pf)
	makeIso(cfg, log, pal, pf)

	nproc := runtime.NumCPU()
	if err := GenerateSquashfs(buildPaths(cfg), cfg.SquashfsCompression, nproc); err != nil {
		Die(pal, "%v", err)
	}
	if err := GenerateIsoImage(buildPaths(cfg), cfg.HostDir, cfg.SyslinuxDataDir, cfg.VolID, cfg.OutputFile); err != nil {
		Die(pal, "%v", err)
	}
	pf.Umount()

	hsize := humanSize(cfg.OutputFile)
	fmt.Printf("Iso file created: %s%s (%s) successfully.%s\n", pal.Green, cfg.OutputFile, hsize, pal.Reset)
}

// extractAutomatic equivale ao pre-parse de --automatic no bash: remove a
// flag da lista de argumentos e retorna se ela estava presente.
func extractAutomatic(args []string) ([]string, bool) {
	var out []string
	automatic := false
	for _, a := range args {
		if a == "--automatic" {
			automatic = true
			continue
		}
		out = append(out, a)
	}
	return out, automatic
}

func printVersion(pal Palette, app string) {
	fmt.Printf("%s%s%s v%s%s\n", pal.Bold, pal.Cyan, app, version, pal.Reset)
	fmt.Printf("%s%sCopyright (C) 2023 vcatafesta@gmail.com\n", pal.Bold, pal.Black)
	fmt.Println("Licença GPL v3+: GNU GPL versão 3 ou posterior <https://gnu.org/licenses/gpl.html>")
	fmt.Println("Este é um software livre: você é livre para alterá-lo e redistribuí-lo.")
	fmt.Printf("NÃO HÁ QUALQUER GARANTIA, na máxima extensão permitida em lei.%s\n", pal.Reset)
}

func printUsage(app string) {
	fmt.Printf(`Usage: %s [options]

Generates a basic live ISO image of Void Linux.
This ISO image can be written to a CD/DVD-ROM or any USB stick.

OPTIONS
  -a <arch>          Set XBPS_ARCH in the ISO image
  -b <system-pkg>    Set an alternative base package (default: base-system)
  -r <repo>          Use this XBPS repository (can be used multiple times)
  -c <cachedir>      XBPS cache directory (default: ./xbps-cachedir-<arch>)
  -D <dir>           Build root directory (default: /lfs/voidlinux, também via $ROOTDIR)
  -k <keymap>        Default keymap (default: br-abnt2)
  -l <locale>        Default locale (default: pt_BR.UTF-8)

  -i <type>          Initramfs compression (lz4|gzip|bzip2|xz|zstd|lzma|lzo)
  -s <type>          Squashfs compression (lz4|gzip|bzip2|xz|zstd|lzma|lzo)

  -o <file>          Output ISO filename
  -p "<pkg> ..."     Additional packages
  -g "<pkg> ..."     Ignore packages
  -I <dir>           Include directory into ROOTFS (pode repetir)

  -Z "<service> ..." Enable services
  -C "<arg> ..."     Extra kernel cmdline args
  -T <title>         Bootloader title (default: Void Linux)
  -v linux<ver>      Custom kernel version

  -K                 Do not remove builddir
  -n                 Reuse existing squashfs
  -Q                 Quiet mode

Desktop presets:
  -W|awesome        AWESOME
  -N|cinnamon       CINNAMON
  -E|enlightenment  ENLIGHTENMENT
  -F|fluxbox        FLUXBOX
  -G|gnome          GNOME
  -P|plasma|kde     KDE PLASMA
  -X|xfce-base      XFCE (base)
  -Y|xfce           XFCE
  -M|mate           MATE
  sway              SWAY (sem flag curta: -S já é usado por outra coisa no bash original)
  -O|mango          MANGO
  -H|hyprland       HYPRLAND
  -A|fullx          ALL X

  -h                 Show this help and exit
  -V, --version      Show version and exit
`, app)
}

// checkDependencies equivale a sh_checkDependencies(): confere se os
// binários necessários estão no PATH.
func checkDependencies(deps []string, distroID string, log *Logger, pal Palette) {
	var missing []string
	for _, d := range deps {
		if _, err := lookPath(d); err != nil {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return
	}
	log.LogErr("não encontrei o(s) comando(s): %s", strings.Join(missing, ", "))
	if distroID != "void" {
		Die(pal, "ERRO: Instalação abortada...")
	}
	// No Void, poderíamos oferecer instalar automaticamente via xbps-install,
	// mas isso exigiria um prompt interativo -- mantido simples aqui.
	Die(pal, "Instale manualmente: %s", strings.Join(missing, " "))
}

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func humanSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	size := info.Size()
	units := []string{"B", "K", "M", "G", "T"}
	f := float64(size)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return strconv.FormatFloat(f, 'f', 1, 64) + units[i]
}

func buildPaths(cfg *Config) Paths {
	return Paths{
		RootFS:      cfg.RootFS,
		HostDir:     cfg.HostDir,
		BootDir:     cfg.BootDir,
		ImageDir:    cfg.ImageDir,
		IsolinuxDir: cfg.IsolinuxDir,
		GrubDir:     cfg.GrubDir,
		BuildDir:    cfg.BuildDir,
	}
}

func configDistroName(cfg *Config) {
	switch {
	case cfg.MakeFullX:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Full X", "VOIDBR_LIVE_FULLX", "fullx"
	case cfg.MakeEnlightenment:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Enlightenment", "VOIDBR_LIVE_ENLIGHTENMENT", "enlightenment"
	case cfg.MakeAwesome:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Awesome", "VOIDBR_LIVE_AWESOME", "awesome"
	case cfg.MakeFluxbox:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Fluxbox", "VOIDBR_LIVE_FLUXBOX", "fluxbox"
	case cfg.MakePlasma:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live KDE Plasma", "VOIDBR_LIVE_PLASMA", "plasma"
	case cfg.MakeGnome:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live GNOME", "VOIDBR_LIVE_GNOME", "gnome"
	case cfg.MakeXfceBase:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Xfce Base", "VOIDBR_LIVE_XFCE_BASE", "xfce-base"
	case cfg.MakeXfce:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live XFCE", "VOIDBR_LIVE_XFCE", "xfce"
	case cfg.MakeCinnamon:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Cinnamon", "VOIDBR_LIVE_CINNAMON", "cinnamon"
	case cfg.MakeMate:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Mate", "VOIDBR_LIVE_MATE", "mate"
	case cfg.MakeSway:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Sway", "VOIDBR_LIVE_SWAY", "sway"
	case cfg.MakeMango:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Mango", "VOIDBR_LIVE_MANGO", "mango"
	case cfg.MakeHyprland:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Hyprland", "VOIDBR_LIVE_HYPRLAND", "hyprland"
	case cfg.MakeOnlyXorg:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Install", "VOIDBR_LIVE_INSTALL", "install"
	default:
		cfg.Title, cfg.VolID, cfg.Name = "VoidBR Linux Live Base", "VOIDBR_LIVE_BASE", "base"
	}
}

func configurePackages(cfg *Config) {
	choice := DesktopChoice{
		X: cfg.MakeX, XfceBase: cfg.MakeXfceBase, Xfce: cfg.MakeXfce,
		Awesome: cfg.MakeAwesome, Enlightenment: cfg.MakeEnlightenment,
		Fluxbox: cfg.MakeFluxbox, Plasma: cfg.MakePlasma, Gnome: cfg.MakeGnome,
		Cinnamon: cfg.MakeCinnamon, Mate: cfg.MakeMate, Sway: cfg.MakeSway,
		Mango: cfg.MakeMango, Hyprland: cfg.MakeHyprland,
		OnlyXorg: cfg.MakeOnlyXorg, FullX: cfg.MakeFullX,
	}
	sel := Choose(choice)
	cfg.RequiredPkgs = sel.Required
	cfg.InitramfsPkgs = sel.Initramfs
	cfg.AdditionalPkgs = sel.Additional
	cfg.CommonPkgs = sel.Common
	cfg.ADistroPkgs = sel.ADistro
	cfg.AExtraCommonPkgs = sel.AExtraCommon
	cfg.AConfigPkgs = sel.AConfig
	cfg.ASkelPkgs = sel.ASkel
	cfg.AExtraPkgs = sel.AExtra
}

func configureRootfs(cfg *Config, log *Logger, pal Palette) {
	cfg.BootCmdline = strings.TrimSpace(cfg.BootCmdline + " rd.live.overlay.overlayfs=1")

	if cfg.BaseArch == "" {
		cfg.BaseArch = uhelperArch()
	}
	cfg.Arch = uhelperArch()
	if cfg.XbpsCacheDir == "" {
		cwd, _ := os.Getwd()
		cfg.XbpsCacheDir = filepath.Join(cwd, "xbps-cachedir-"+cfg.BaseArch)
	}
	if cfg.XbpsHostCacheDir == "" {
		cwd, _ := os.Getwd()
		cfg.XbpsHostCacheDir = filepath.Join(cwd, "xbps-cachedir-"+cfg.Arch)
	}
	if cfg.Keymap == "" {
		cfg.Keymap = "br-abnt2"
	}
	if cfg.Locale == "" {
		cfg.Locale = "pt_BR.UTF-8"
	}
	if cfg.InitramfsCompression == "" {
		cfg.InitramfsCompression = "xz"
	}
	if cfg.SquashfsCompression == "" {
		cfg.SquashfsCompression = "zstd"
	}
	if cfg.BaseSystemPkg == "" {
		cfg.BaseSystemPkg = "base-system"
	}
	if cfg.BootTitle == "" {
		cfg.BootTitle = cfg.Title
	}

	cfg.PackageList = append([]string{cfg.BaseSystemPkg}, cfg.PackageList...)

	if cfg.RootDir == "" {
		if env := os.Getenv("ROOTDIR"); env != "" {
			cfg.RootDir = env
		} else {
			cfg.RootDir = "/lfs/voidlinux"
		}
	}

	cfg.BuildDir = filepath.Join(cfg.RootDir, cfg.Name)
	if err := os.MkdirAll(cfg.BuildDir, 0o755); err != nil {
		Die(pal, "mkdir builddir: %v", err)
	}
	cfg.BuildDir, _ = filepath.Abs(cfg.BuildDir)

	cfg.ImageDir = filepath.Join(cfg.BuildDir, "image")
	cfg.RootFS = filepath.Join(cfg.BuildDir, "rootfs")
	cfg.HostDir = filepath.Join(cfg.RootDir, "hostdir")
	cfg.BootDir = filepath.Join(cfg.ImageDir, "boot")
	cfg.IsolinuxDir = filepath.Join(cfg.BootDir, "isolinux")
	cfg.GrubDir = filepath.Join(cfg.BootDir, "grub")
	cfg.IsolinuxCfg = filepath.Join(cfg.IsolinuxDir, "isolinux.cfg")

	if cfg.XbpsInstallCmd == "" {
		cfg.XbpsInstallCmd = "xbps-install"
	}
	if cfg.SyslinuxDataDir == "" {
		cfg.SyslinuxDataDir = filepath.Join(cfg.HostDir, "usr/lib/syslinux")
	}
	if cfg.GrubDataDir == "" {
		cfg.GrubDataDir = filepath.Join(cfg.HostDir, "usr/share/grub")
	}
	if cfg.SplashImage == "" {
		cfg.SplashImage = "data/splash.png"
	}
	if cfg.OutputFile == "" {
		cfg.OutputFile = fmt.Sprintf("voidbr-live-%s-%s-%s-%s.iso", cfg.Name, cfg.BaseArch, kernelPlaceholder(), timestamp())
	}

	for _, dir := range []string{cfg.RootFS, cfg.HostDir, cfg.IsolinuxDir, cfg.GrubDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			Die(pal, "mkdir %s: %v", dir, err)
		}
	}

	VerificarEspacoLivre(cfg.RootDir, func(format string, a ...any) { Die(pal, format, a...) })

	printResume(cfg, log)
}

func kernelPlaceholder() string { return "" }

func timestamp() string {
	return time.Now().Format("20060102-1504")
}

func printResume(cfg *Config, log *Logger) {
	log.PrintStep(fmt.Sprintf("Building ISO     => %s", cfg.Title))
	log.PrintStep(fmt.Sprintf("Versao app       => %s v%s", cfg.App, version))
	log.PrintStep(fmt.Sprintf("Profile          => %s", cfg.Name))
	log.PrintStep(fmt.Sprintf("ROOTFS           => %s", cfg.RootFS))
	log.PrintStep(fmt.Sprintf("HOSTDIR          => %s", cfg.HostDir))
	log.PrintStep(fmt.Sprintf("Output file ISO  => %s", cfg.OutputFile))
	log.PrintStep(fmt.Sprintf("Compression SFS  => %s", cfg.SquashfsCompression))
	log.PrintStep(fmt.Sprintf("Compression RAM  => %s", cfg.InitramfsCompression))
	log.PrintStep(fmt.Sprintf("ISO LABEL        => %s", cfg.VolID))
}

func installRootfs(cfg *Config, log *Logger, pal Palette, pf *Pseudofs) {
	if err := CopyVoidKeys(cfg.RootFS); err != nil {
		Die(pal, "%v", err)
	}
	if err := CopyVoidKeys(cfg.HostDir); err != nil {
		Die(pal, "%v", err)
	}

	opts := Options{
		InstallCmd: cfg.XbpsInstallCmd,
		BaseArch:   cfg.Arch,
		RootFS:     cfg.HostDir,
		Repository: cfg.XbpsRepository,
		CacheDir:   cfg.XbpsHostCacheDir,
	}
	if err := opts.Install(cfg.RequiredPkgs); err != nil {
		Die(pal, "install_prereqs (REQUIRED_PKGS): %v", err)
	}
	if err := opts.Install(cfg.BasePkgs); err != nil {
		Die(pal, "install_prereqs (BASE_PKGS): %v", err)
	}

	if err := SyncRootdir(cfg.XbpsInstallCmd, cfg.BaseArch, cfg.RootFS, cfg.XbpsRepository); err != nil {
		Die(pal, "sync rootfs: %v", err)
	}
	if err := SyncRootdir(cfg.XbpsInstallCmd, cfg.Arch, cfg.HostDir, cfg.XbpsRepository); err != nil {
		Die(pal, "sync hostdir: %v", err)
	}

	configureLinux(cfg, pal)

	os.MkdirAll(filepath.Join(cfg.RootFS, "etc"), 0o755)
	copyIfExists("data/motd", filepath.Join(cfg.RootFS, "etc", "motd"))
	copyIfExists("data/issue", filepath.Join(cfg.RootFS, "etc", "issue"))

	if len(cfg.IgnorePkgs) > 0 {
		if err := IgnorePackages(cfg.RootFS, cfg.IgnorePkgs); err != nil {
			Die(pal, "%v", err)
		}
	}

	installPackages(cfg, pal, pf)
	InstallSkelRoot(cfg.RootFS)
	if err := ConfigureVoidInstallDesktop(cfg.RootFS); err != nil {
		Die(pal, "%v", err)
	}
	if err := ReconfigurePass2(cfg.RootFS); err != nil {
		Die(pal, "%v", err)
	}
	FinishClean(cfg.RootFS)
}

func installPackages(cfg *Config, pal Palette, pf *Pseudofs) {
	opts := Options{
		InstallCmd: cfg.XbpsInstallCmd,
		BaseArch:   cfg.BaseArch,
		RootFS:     cfg.RootFS,
		Repository: cfg.XbpsRepository,
		CacheDir:   cfg.XbpsCacheDir,
	}

	if err := opts.Install([]string{"xbps"}); err != nil {
		Die(pal, "instalar xbps: %v", err)
	}

	if err := pf.Mount(); err != nil {
		Die(pal, "%v", err)
	}

	// Grupos "normais" unificados numa única transação (ganho de velocidade:
	// evita reprocessar repodata/dependências a cada grupo separado).
	all := concatAll(cfg.PackageList, cfg.InitramfsPkgs, cfg.AdditionalPkgs,
		cfg.CommonPkgs, cfg.ADistroPkgs, cfg.AExtraCommonPkgs, cfg.AConfigPkgs, cfg.AExtraPkgs)
	if err := opts.Install(all); err != nil {
		Die(pal, "instalar pacotes (grupo unificado): %v", err)
	}

	// ASKEL_PKGS continua em transação separada (precisa de --force --force),
	// mas agora tudo de uma vez, não mais um por um.
	if len(cfg.ASkelPkgs) > 0 {
		if err := opts.InstallForce(cfg.ASkelPkgs); err != nil {
			Die(pal, "instalar ASKEL_PKGS (forçado): %v", err)
		}
	}
}

func concatAll(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

func configureLinux(cfg *Config, pal Palette) {
	if cfg.LinuxVersion == "" {
		cfg.LinuxVersion = "linux"
	}
	cfg.KernelVersion = cfg.LinuxVersion // simplificado: resolução real de versão via xbps-query ficaria aqui
	cfg.OutputFile = fmt.Sprintf("voidbr-live-%s-%s-%s-%s.iso", cfg.Name, cfg.BaseArch, cfg.KernelVersion, timestamp())
}

func copyIfExists(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0o644)
}

func makeIso(cfg *Config, log *Logger, pal Palette, pf *Pseudofs) {
	if err := pf.Mount(); err != nil {
		Die(pal, "%v", err)
	}

	defaultServices := []string{
		"agetty-tty1", "agetty-tty2", "agetty-tty3", "agetty-tty4", "agetty-tty5", "agetty-tty6",
		"dbus", "socklog-unix", "nanoklogd", "udevd", "sshd", "NetworkManager", "dhcpcd", "chronyd",
	}
	var additional []string
	if cfg.MakeX {
		if cfg.MakeGnome {
			additional = []string{"gdm", "avahi-daemon"}
		} else {
			additional = []string{"lightdm", "avahi-daemon"}
		}
	}
	var extra []string
	if cfg.MakeEnlightenment {
		extra = []string{"acpid", "connmand"}
	}

	all := concatAll(defaultServices, cfg.ServiceList, additional, extra)
	EnableServices(cfg.RootFS, all)

	if len(cfg.IncludeDirs) > 0 {
		if err := CopyIncludeDirectories(cfg.RootFS, cfg.IncludeDirs); err != nil {
			Die(pal, "%v", err)
		}
	}

	if err := ConfigurePlymouth(cfg.RootFS); err != nil {
		Die(pal, "%v", err)
	}
	if err := ConfigureEtcRunitCoreServicesQuitPlymouth(cfg.RootFS); err != nil {
		Die(pal, "%v", err)
	}

	noAutologin := cfg.MakeFullX
	if err := CopyDracutFiles(cfg.RootFS, noAutologin); err != nil {
		Die(pal, "%v", err)
	}
	if err := CopyAutoinstallerFiles(cfg.RootFS); err != nil {
		Die(pal, "%v", err)
	}
	if err := GenerateInitramfs(buildPaths(cfg), cfg.KernelVersion, cfg.InitramfsCompression); err != nil {
		Die(pal, "%v", err)
	}
	if err := GenerateIsolinuxBoot(buildPaths(cfg), cfg.SyslinuxDataDir, cfg.SplashImage, cfg.KernelVersion, cfg.Keymap, cfg.BaseArch, cfg.Locale, cfg.BootTitle, cfg.BootCmdline, cfg.HostDir); err != nil {
		Die(pal, "%v", err)
	}
	if err := GenerateGrubEfiBoot(buildPaths(cfg), cfg.GrubDataDir, cfg.SplashImage, cfg.KernelVersion, cfg.Keymap, cfg.BaseArch, cfg.Locale, cfg.BootTitle, cfg.BootCmdline, cfg.SyslinuxDataDir, cfg.HostDir); err != nil {
		Die(pal, "%v", err)
	}

	if err := CleanupRootfs(cfg.RootFS, cfg.InitramfsPkgs); err != nil {
		Die(pal, "%v", err)
	}

	_ = os.Remove(filepath.Join(cfg.RootFS, "etc", "d", "00-repository-main.conf"))
}

func applyPositionalDesktop(cfg *Config, positional []string) {
	for _, arg := range positional {
		switch strings.ToLower(arg) {
		case "xorg", "install":
			cfg.MakeX, cfg.MakeOnlyXorg = true, true
		case "fluxbox":
			cfg.MakeX, cfg.MakeFluxbox = true, true
		case "xfce":
			cfg.MakeX, cfg.MakeXfce = true, true
		case "xfce-base":
			cfg.MakeX, cfg.MakeXfceBase = true, true
		case "plasma", "kde":
			cfg.MakeX, cfg.MakePlasma = true, true
		case "gnome":
			cfg.MakeX, cfg.MakeGnome = true, true
		case "mate":
			cfg.MakeX, cfg.MakeMate = true, true
		case "sway":
			cfg.MakeX, cfg.MakeSway = true, true
		case "mango":
			cfg.MakeX, cfg.MakeMango = true, true
		case "hyprland":
			cfg.MakeX, cfg.MakeHyprland = true, true
		case "cinnamon":
			cfg.MakeX, cfg.MakeCinnamon = true, true
		case "awesome":
			cfg.MakeX, cfg.MakeAwesome = true, true
		case "enlightenment":
			cfg.MakeX, cfg.MakeEnlightenment = true, true
		case "fullx":
			cfg.MakeX, cfg.MakeFullX = true, true
		default:
			cfg.MakeX, cfg.MakeFullX = false, false
		}
	}
}

// parseFlags equivale ao `while getopts ...` do bash. O pacote flag padrão do
// Go não suporta nativamente flags de char único agrupadas como getopts,
// então implementamos um parser manual simplificado.
func parseFlags(args []string, cfg *Config) []string {
	var positional []string
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-a":
			cfg.BaseArch = next(args, &i)
		case a == "-b":
			cfg.BaseSystemPkg = next(args, &i)
		case a == "-r":
			cfg.ExtraRepository = strings.TrimSpace("--repository=" + next(args, &i) + " " + cfg.ExtraRepository)
		case a == "-c":
			cfg.XbpsCacheDir = next(args, &i)
		case a == "-D":
			cfg.RootDir = next(args, &i)
		case a == "-g":
			cfg.IgnorePkgs = append(cfg.IgnorePkgs, next(args, &i))
		case a == "-K":
			cfg.Keep = true
		case a == "-k":
			cfg.Keymap = next(args, &i)
		case a == "-l":
			cfg.Locale = next(args, &i)
		case a == "-I":
			cfg.IncludeDirs = append(cfg.IncludeDirs, next(args, &i))
		case a == "-i":
			cfg.InitramfsCompression = next(args, &i)
		case a == "-s":
			cfg.SquashfsCompression = next(args, &i)
		case a == "-Z":
			cfg.ServiceList = append(cfg.ServiceList, next(args, &i))
		case a == "-o":
			cfg.OutputFile = next(args, &i)
		case a == "-p":
			cfg.PackageList = append(cfg.PackageList, next(args, &i))
		case a == "-C":
			cfg.BootCmdline = strings.TrimSpace(cfg.BootCmdline + " " + next(args, &i))
		case a == "-T":
			cfg.BootTitle = next(args, &i)
		case a == "-x", a == "-X":
			cfg.MakeX, cfg.MakeXfceBase = true, true
		case a == "-y", a == "-Y":
			cfg.MakeX, cfg.MakeXfce = true, true
		case a == "-n", a == "-N":
			cfg.MakeX, cfg.MakeCinnamon = true, true
		case a == "-m", a == "-M":
			cfg.MakeX, cfg.MakeMate = true, true
		case a == "-S":
			cfg.MakeX, cfg.MakeSway = true, true
		case a == "-O":
			cfg.MakeX, cfg.MakeMango = true, true
		case a == "-H":
			cfg.MakeX, cfg.MakeHyprland = true, true
		case a == "-w", a == "-W":
			cfg.MakeX, cfg.MakeAwesome = true, true
		case a == "-e", a == "-E":
			cfg.MakeX, cfg.MakeEnlightenment = true, true
		case a == "-f", a == "-F":
			cfg.MakeX, cfg.MakeFluxbox = true, true
		case a == "-G":
			cfg.MakeX, cfg.MakeGnome = true, true
		case a == "-P":
			cfg.MakeX, cfg.MakePlasma = true, true
		case a == "-A":
			cfg.MakeX, cfg.MakeFullX = true, true
		case a == "-q", a == "-Q":
			cfg.Quiet = true
		case a == "-v":
			cfg.LinuxVersion = next(args, &i)
		case a == "-V", a == "--version":
			printVersion(NewPalette(IsTerminal()), cfg.App)
			os.Exit(0)
		case a == "-h", a == "--help":
			printUsage(cfg.App)
			os.Exit(0)
		default:
			positional = append(positional, a)
		}
		i++
	}
	return positional
}

func next(args []string, i *int) string {
	*i++
	if *i < len(args) {
		return args[*i]
	}
	return ""
}

// uhelperArch equivale a `xbps-uhelper arch`, com fallback para `uname -m`
// se o comando falhar (mesmo comportamento do bash: `xbps-uhelper arch
// 2>/dev/null || uname -m`).
func uhelperArch() string {
	if out, err := exec.Command("xbps-uhelper", "arch").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return "x86_64"
}
