/*
   xbps-query -R -p run_depends -s sdbus-cpp

   voidbr-vinstall (Rust)
   Wrapper para o Void xbps-install, xbps-query e xbps-remove
   Porte em Rust do vinstall-v1.6.45.go

   Site:      https://chililinux.com
   Site:      https://voidbr.org
   Site:      https://voidlinux.com.br
   GitHub:    https://github.com/voidlinuxbr/voidbr-vinstall

   Created:   sex 06 nov 2026 13:28:10 -04
   Updated:   sex 06 nov 2026 13:28:10 -04
   Version:   1.6.45
   Copyright (C) 2019-2026 Vilmar Catafesta <vcatafesta@gmail.com>
*/

use std::collections::{HashMap, HashSet};
use std::ffi::OsStr;
use std::fmt::Display;
use std::fs;
use std::io::{self, BufRead, BufReader, BufWriter, Cursor, Read, Write};
use std::os::unix::fs::PermissionsExt;
use std::os::unix::process::ExitStatusExt;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitStatus, Stdio};
use std::sync::atomic::{AtomicI32, Ordering};
use std::sync::{mpsc, OnceLock};
use std::thread;
use std::time::{Duration, Instant};

use regex::Regex;

const VERSION: &str = "1.6.45";
// COPYRIGHT_TEAM aparece no --help
const COPYRIGHT_TEAM: &str = "Copyright (c) 2026-2026, VoidBR Development Team <https://voidbr.org>";
const EXEC_TIMEOUT: Duration = Duration::from_secs(10);
// -FR (xlocate / xbps-query -Ro) é lento; 10s cortava a busca
const REMOTE_TIMEOUT: Duration = Duration::from_secs(120);

// --- CORES ---

// no_color: sem cor quando o stdout não é terminal, TERM=dumb ou NO_COLOR
// definido (mesmo critério da fatih/color usada na versão Go). Assim
// "vinstall -F x | grep ..." não recebe códigos ANSI no texto.
fn no_color() -> bool {
    static NO: OnceLock<bool> = OnceLock::new();
    *NO.get_or_init(|| {
        std::env::var("NO_COLOR").map(|v| !v.is_empty()).unwrap_or(false)
            || std::env::var("TERM").map(|t| t == "dumb").unwrap_or(false)
            || unsafe { libc::isatty(libc::STDOUT_FILENO) } != 1
    })
}

fn paint(code: &str, s: impl Display) -> String {
    if no_color() {
        s.to_string()
    } else {
        format!("\x1b[{code}m{s}\x1b[0m")
    }
}

fn cyan(s: impl Display) -> String {
    paint("1;36", s)
}
fn green(s: impl Display) -> String {
    paint("32", s)
}
fn white(s: impl Display) -> String {
    paint("1;37", s)
}
fn red(s: impl Display) -> String {
    paint("1;31", s)
}
fn yellow(s: impl Display) -> String {
    paint("1;33", s)
}
fn magenta(s: impl Display) -> String {
    paint("1;35", s)
}
fn green_bold(s: impl Display) -> String {
    paint("1;32", s)
}

#[derive(Clone, Default)]
struct Package {
    status: String,
    full_name: String,
    description: String,
    maintainer: String,
    repo: String,
    size_download: i64,
    size_installed: i64,
}

fn main() {
    // Rust ignora SIGPIPE por padrão e o println! entra em pânico quando o
    // pipe fecha ("vinstall -Li | head"). Volta ao padrão do Unix (e do Go):
    // o processo termina em silêncio.
    unsafe {
        libc::signal(libc::SIGPIPE, libc::SIG_DFL);
    }
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.is_empty() {
        print_usage();
        return;
    }
    ansi("\x1b[36m");
    run(args);
    ansi("\x1b[0m");
}

fn run(args: Vec<String>) {
    let mut flags: Vec<String> = Vec::new();
    let mut targets: Vec<String> = Vec::new();
    let mut mode = "install";
    let mut search_remote = false;
    let mut find_mode = "exact"; // "exact" | "any" | "regex" (-F/-FR, -Fa/-FRa, -Fx/-FRx)
    let mut filter = String::new();
    let mut sync_flavor = ""; // "sy" | "syy" | "sf" — só vira modo "sync" se não houver targets

    let mut i = 0;
    while i < args.len() {
        let arg = args[i].as_str();
        // Opções longas do xbps-install que recebem valor separado por
        // espaço (ex.: "--parallel-downloads 8"): junta opção e valor em
        // "--opcao=valor", para o valor não virar nome de pacote.
        if is_value_option(arg) && i + 1 < args.len() {
            i += 1;
            flags.push(format!("{arg}={}", args[i]));
            i += 1;
            continue;
        }
        // -R/-r/-C/-c curtos (exatos) recebem valor no xbps-install: junta
        // em "--repository=valor" etc. -XR/-FR não são afetados (strings
        // diferentes). Em modo remove, -R é "recursivo" no xbps-remove,
        // então não consome valor.
        if let Some(long) = short_value_option(arg) {
            if mode != "remove" && i + 1 < args.len() {
                i += 1;
                flags.push(format!("{long}={}", args[i]));
                i += 1;
                continue;
            }
        }
        match arg {
            "-h" | "--help" => {
                print_usage();
                return;
            }
            "-v" | "--version" => {
                println!("{} {}", white("vinstall"), cyan(format!("v{VERSION}")));
                return;
            }
            "--history" => mode = "history",
            "-Sy" => {
                sync_flavor = "sy";
                flags.push(arg.to_string());
            }
            "-Syy" => {
                sync_flavor = "syy";
                flags.push(arg.to_string());
            }
            "-Sf" => {
                sync_flavor = "sf";
                flags.push(arg.to_string());
            }
            "-Sc" => mode = "clean-light",
            "-Scc" => mode = "clean",
            "-F" => mode = "find",
            "-FR" => {
                mode = "find";
                search_remote = true;
            }
            "-Fa" => {
                mode = "find";
                find_mode = "any";
            }
            "-FRa" => {
                mode = "find";
                search_remote = true;
                find_mode = "any";
            }
            "-Fx" => {
                mode = "find";
                find_mode = "regex";
            }
            "-FRx" => {
                mode = "find";
                search_remote = true;
                find_mode = "regex";
            }
            "-Li" => mode = "list-installed",
            "-Lo" => mode = "list-orphans",
            "-Si" => mode = "info-remote",
            "-Qi" => mode = "info-local",
            "-Qs" => mode = "query-search",
            "-Ss" => mode = "remote-search",
            "-Sss" => mode = "remote-search-detailed",
            "-Ssi" => {
                mode = "remote-search";
                filter = "installed".into();
            }
            "-Ssu" => {
                mode = "remote-search";
                filter = "missing".into();
            }
            _ => {
                if arg.starts_with("-X") || arg.starts_with("-x") {
                    // -X/-x ativam o modo remove. Qualquer sufixo é repassado
                    // como flag real para o xbps-remove (ex.: "-XR" vira a
                    // flag "-R", recursivo; "-xf" vira "-f", força). Sem
                    // sufixo, só ativa o modo remove.
                    mode = "remove";
                    let suffix = &arg[2..];
                    if !suffix.is_empty() {
                        flags.push(format!("-{suffix}"));
                    }
                } else if arg.starts_with("-Q") {
                    mode = "query-generic";
                    filter = arg.replacen("-Q", "-", 1);
                } else if arg.starts_with('-') {
                    flags.push(arg.to_string());
                } else {
                    targets.push(arg.to_string());
                }
            }
        }
        i += 1;
    }

    // -Sy/-Syy/-Sf só disparam o modo "sync" do vinstall quando usadas
    // sozinhas (sem pacotes-alvo). Com um pacote (ex.: "vinstall -Sy
    // firefox"), a flag já está em `flags` e segue o fluxo normal de
    // instalação, igual usar o xbps-install nativo.
    if !sync_flavor.is_empty() && targets.is_empty() {
        mode = "sync";
    }

    match mode {
        "history" => show_history(&targets),
        "sync" => sync_and_check_updates(sync_flavor),
        "clean" => clean_xbps_cache(),
        "clean-light" => clean_xbps_cache_light(),
        "find" => {
            require_targets(&targets, "informe o arquivo a procurar (ex.: vinstall -F ifconfig)");
            // aceita vários termos; sai com 1 se algum não for encontrado,
            // como o "pacman -F", para uso em script ("vinstall -F x || ...")
            let mut all_found = true;
            for (i, t) in targets.iter().enumerate() {
                if i > 0 {
                    println!();
                }
                if !find_provides(t, search_remote, find_mode) {
                    all_found = false;
                }
            }
            if !all_found {
                exit_with(1);
            }
        }
        "list-installed" => list_local("installed", &[]),
        "list-orphans" => list_local("orphans", &[]),
        "query-search" => {
            require_targets(&targets, "informe o termo a procurar (ex.: vinstall -Qs firefox)");
            list_local("search", &targets);
        }
        "remote-search" => {
            require_targets(&targets, "informe o termo a procurar (ex.: vinstall -Ss firefox)");
            // vários termos: mostra o que casar com QUALQUER um deles
            let mut pkgs = Vec::new();
            for t in &targets {
                pkgs.extend(fetch_suggestions(t));
            }
            let mut pkgs = unique_packages_exact(pkgs);
            sort_by_name(&mut pkgs);
            if !filter.is_empty() {
                pkgs = filter_packages(pkgs, &filter);
            }
            if pkgs.is_empty() {
                println!("{} {}", red("[!]"), white("Nenhum pacote encontrado."));
                show_did_you_mean(&targets, &filter);
                exit_with(1);
            }
            display_search(&pkgs, "Resultados (Rápido):");
        }
        "remote-search-detailed" => {
            require_targets(&targets, "informe o termo a procurar (ex.: vinstall -Sss firefox)");
            remote_search_detailed(&targets);
        }
        "remove" => {
            require_targets(&targets, "informe o pacote a remover (ex.: vinstall -X firefox)");
            let before = get_installed_pkgver_by_name();
            let ok = run_binary("xbps-remove", &flags, &targets);
            print_removal_summary(&removed_packages(&before, &get_installed_pkgver_by_name()), &targets);
            if !ok {
                fail_exit();
            }
        }
        "info-remote" | "info-local" => {
            require_targets(&targets, "informe o pacote (ex.: vinstall -Si firefox)");
            if !show_package_info(&targets, mode == "info-remote") {
                exit_with(2); // algum não encontrado; mesmo código do xbps-query
            }
        }
        "query-generic" => {
            // xbps-query só consulta: roda direto, sem sudo
            let mut a = vec![filter.clone()];
            a.extend(targets.iter().cloned());
            if !run_query(&a) {
                exit_with(1);
            }
        }
        _ => install(flags, targets),
    }
}

// install é o fluxo padrão: instalar pacotes ou repassar flags ao
// xbps-install (ex.: -Syu).
fn install(mut flags: Vec<String>, targets: Vec<String>) {
    // --ignore-file-conflicts é necessária em dois cenários:
    //   1) qualquer flag que contenha "u" (ex.: -Syu, -u), pois updates
    //      frequentemente trocam donos de arquivo entre pacotes;
    //   2) instalação de pacotes-alvo em geral, pois alguns pacotes
    //      compartilham arquivos idênticos.
    // A flag deve ser adicionada NO MÁXIMO UMA VEZ.
    // -f/--force (forçar reinstalação/downgrade) também não pede
    // confirmação: acrescenta o -y, se ainda não estiver lá
    let has_force = flags.iter().any(|f| f == "-f" || f == "--force");
    let has_yes = flags.iter().any(|f| f == "-y" || f == "--yes");
    if has_force && !has_yes {
        flags.push("-y".into());
    }

    let mut needs_ignore_conflicts = !targets.is_empty();
    if !needs_ignore_conflicts {
        // em "--opcao=valor" só o nome da opção conta, para um valor como
        // "--repository=https://...linux..." não ativar a flag por engano
        needs_ignore_conflicts = flags.iter().any(|f| {
            let name = if f.starts_with("--") { f.split('=').next().unwrap_or(f) } else { f.as_str() };
            name.contains('u')
        });
    }
    if needs_ignore_conflicts {
        flags.push("--ignore-file-conflicts".into());
    }

    if targets.is_empty() {
        if !flags.is_empty() && !run_binary("xbps-install", &flags, &[]) {
            fail_exit();
        }
        return;
    }

    // Confere ANTES de chamar o xbps-install se cada pacote existe nos
    // repositórios (xbps-query -R, processo separado). Assim quem informa o
    // pacote inexistente é o vinstall, para todos de uma vez, e o
    // xbps-install só roda com nomes válidos — com stdin/stdout/stderr
    // herdados do terminal, como os hooks .hook interativos precisam.
    let (valid, missing): (Vec<String>, Vec<String>) =
        targets.iter().cloned().partition(|t| package_exists_in_repo(t, &flags));

    let mut names = valid.clone();
    if !missing.is_empty() {
        // Avisa cada pacote que não existe, no estilo do pacman. Com vários
        // pacotes pedidos, segue instalando os que existem, sem sugestões.
        // Com um pacote só, mostra as sugestões e o menu para escolher.
        for t in &missing {
            println!("{} {} {}", red("erro:"), white("alvo não encontrado:"), yellow(t));
        }
        let mut chosen = Vec::new();
        if targets.len() == 1 {
            let t = &targets[0];
            // primeiro os nomes parecidos (erro de digitação), depois os que
            // contêm o termo
            let mut contains = unique_packages_exact(fetch_suggestions(t));
            sort_by_name(&mut contains);
            let mut similar = Vec::new();
            for name in corrected_terms(t) {
                similar.extend(fetch_suggestions(&name));
            }
            sort_by_name(&mut similar);
            similar.extend(contains);
            let suggestions = unique_packages_exact(similar);
            if suggestions.is_empty() {
                println!("{} {} '{}'.", yellow("[!]"), white("Sem sugestões para"), cyan(t));
            } else if stdin_is_terminal() {
                chosen = select_from_menu(&suggestions, t);
            } else {
                // sem terminal: só informa, sem número e sem prompt
                display_search_list(&suggestions, &format!("Sugestões para '{t}':"), false);
            }
        }
        if valid.is_empty() && chosen.is_empty() {
            exit_with(2); // nada a instalar; mesmo código do xbps-install
        }
        names.extend(chosen);
    }

    if !run_binary("xbps-install", &flags, &names) {
        fail_exit();
    }
    // na simulação (-n/--dry-run) nada foi instalado: não oferece ativar
    // serviços
    if !is_dry_run(&flags) {
        for name in &names {
            check_and_enable_service(name);
        }
    }
}

// --- UTILITÁRIOS GERAIS ---

// stdin_is_terminal diz se a entrada padrão é um terminal. Sem terminal
// (script, cron, pipe), o vinstall não faz perguntas: não lê a entrada,
// para não consumir dados que eram de outro comando.
fn stdin_is_terminal() -> bool {
    unsafe { libc::isatty(libc::STDIN_FILENO) == 1 }
}

// is_dry_run diz se as opções pedem simulação (-n/--dry-run do
// xbps-install/xbps-remove), inclusive agrupada com outras letras (-Syun).
fn is_dry_run(flags: &[String]) -> bool {
    flags
        .iter()
        .any(|f| f == "--dry-run" || (f.starts_with('-') && !f.starts_with("--") && f[1..].contains('n')))
}

// exit_with sai com o código dado, resetando a cor antes.
fn exit_with(code: i32) -> ! {
    ansi("\x1b[0m");
    std::process::exit(code)
}

// require_targets avisa e sai com 1 quando a opção precisa de um pacote ou
// termo e nenhum foi informado.
fn require_targets(targets: &[String], msg: &str) {
    if targets.is_empty() {
        println!("{} {}", red("[!]"), white(msg));
        exit_with(1);
    }
}

// show_package_info mostra as informações de cada pacote, como o pacman:
// -Si (remote=true) do repositório, via "xbps-query -R"; -Qi do pacote
// instalado, via "xbps-query". Sem sudo. Retorna false se algum pacote não
// foi encontrado.
fn show_package_info(pkgs: &[String], remote: bool) -> bool {
    let mut all_found = true;
    for (i, p) in pkgs.iter().enumerate() {
        if i > 0 {
            println!();
        }
        let args: Vec<&str> = if remote { vec!["-R", p] } else { vec![p] };
        let out = capture("xbps-query", &args);
        if out.err.is_some() || out.stdout.trim().is_empty() {
            println!("{} {} {}", red("erro:"), white("alvo não encontrado:"), yellow(p));
            all_found = false;
            continue;
        }
        println!("{} {} {}", cyan(">>>"), cyan("xbps-query"), yellow(args.join(" ")));
        // "chave: valor" com a chave colorida; linhas de continuação
        // (listas, ex.: run_depends) vêm indentadas e saem como estão
        for line in out.stdout.trim_end_matches('\n').split('\n') {
            match line.split_once(':') {
                Some((key, value)) if !line.starts_with(' ') && !line.starts_with('\t') => {
                    println!("{}:{}", cyan(key), white(value))
                }
                _ => println!("{}", green(line)),
            }
        }
    }
    all_found
}

// run_query roda o xbps-query direto, sem sudo: ele só consulta e não
// precisa de root.
fn run_query(args: &[String]) -> bool {
    println!("{} {} {}", cyan(">>>"), cyan("xbps-query"), yellow(args.join(" ")));
    flush();
    Command::new("xbps-query").args(args).status().map(|s| s.success()).unwrap_or(false)
}

// ansi imprime um código de cor cru só quando a saída é um terminal.
fn ansi(code: &str) {
    if !no_color() {
        print!("{code}");
        flush();
    }
}

fn flush() {
    let _ = io::stdout().flush();
}

fn format_bytes(b: i64) -> String {
    const UNIT: i64 = 1024;
    if b < UNIT {
        return format!("{b} B");
    }
    let (mut div, mut exp) = (UNIT, 0usize);
    let mut n = b / UNIT;
    while n >= UNIT {
        div *= UNIT;
        exp += 1;
        n /= UNIT;
    }
    format!("{:.2} {}B", b as f64 / div as f64, b"KMGTPE"[exp] as char)
}

fn terminal_width() -> usize {
    let mut ws: libc::winsize = unsafe { std::mem::zeroed() };
    let ret = unsafe { libc::ioctl(libc::STDIN_FILENO, libc::TIOCGWINSZ, &mut ws) };
    if ret == 0 {
        ws.ws_col as usize
    } else {
        80
    }
}

fn separator(width: usize) -> String {
    white("─".repeat(width))
}

// clean_version tira a versão do pkgver ("firefox-128.0_1" -> "firefox").
fn clean_version(full_name: &str) -> &str {
    match full_name.rfind('-') {
        Some(i) => &full_name[..i],
        None => full_name,
    }
}

// extract_version retorna a parte de versão/revisão de um pkgver
// ("firefox-128.0_1" -> "128.0_1"). Complementa clean_version.
fn extract_version(pkgver: &str) -> &str {
    match pkgver.rfind('-') {
        Some(i) => &pkgver[i + 1..],
        None => pkgver,
    }
}

// is_value_option: opções longas do xbps-install que recebem um valor. Na
// forma "--opcao valor" o vinstall junta as duas em "--opcao=valor".
fn is_value_option(arg: &str) -> bool {
    matches!(arg, "--parallel-downloads" | "--repository" | "--rootdir" | "--config" | "--cachedir")
}

// short_value_option mapeia as opções curtas do xbps-install que recebem
// valor para a forma longa. Só casam exatas (ex.: "-R"), então atalhos
// próprios do vinstall como -XR e -FR não são afetados.
fn short_value_option(arg: &str) -> Option<&'static str> {
    match arg {
        "-R" => Some("--repository"),
        "-r" => Some("--rootdir"),
        "-C" => Some("--config"),
        "-c" => Some("--cachedir"),
        _ => None,
    }
}

// PARALLEL_DOWNLOADS_MAX é o limite do xbps-install (XBPS_PARALLEL_DOWNLOADS_MAX).
const PARALLEL_DOWNLOADS_MAX: i64 = 16;

// parallel_downloads_from_env converte XBPS_PARALLEL_DOWNLOADS em
// "--parallel-downloads=<n>" para o xbps-install. A variável não chega
// sozinha ao xbps-install porque o sudo limpa o ambiente (env_reset). Se a
// linha de comando já tiver --parallel-downloads, ela vale. Valor inválido
// é avisado e ignorado.
fn parallel_downloads_from_env(flags: &[String]) -> Vec<String> {
    let mut out = flags.to_vec();
    let val = match std::env::var("XBPS_PARALLEL_DOWNLOADS") {
        Ok(v) if !v.is_empty() => v,
        _ => return out,
    };
    if flags.iter().any(|f| f == "--parallel-downloads" || f.starts_with("--parallel-downloads=")) {
        return out;
    }
    match val.trim().parse::<i64>() {
        Ok(n) if (1..=PARALLEL_DOWNLOADS_MAX).contains(&n) => out.push(format!("--parallel-downloads={n}")),
        _ => println!(
            "{} {}",
            yellow("[!]"),
            white(format!("ignorando XBPS_PARALLEL_DOWNLOADS inválido `{val}' (deve ser 1-{PARALLEL_DOWNLOADS_MAX})"))
        ),
    }
    out
}

// LAST_EXIT_CODE guarda o código de saída do último comando rodado pelo
// run_binary, para o vinstall sair com o mesmo código do xbps.
static LAST_EXIT_CODE: AtomicI32 = AtomicI32::new(0);

// go_list formata uma lista como o fmt.Sprint do Go: "[a b c]".
fn go_list(v: &[String]) -> String {
    format!("[{}]", v.join(" "))
}

// run_binary roda o binário via sudo com stdin/stdout/stderr herdados do
// terminal (os hooks .hook interativos precisam de isatty()).
fn run_binary(bin: &str, flags: &[String], pkgs: &[String]) -> bool {
    let flags = if bin == "xbps-install" { parallel_downloads_from_env(flags) } else { flags.to_vec() };
    println!("{} {} {} {}", cyan(">>>"), cyan(bin), yellow(go_list(&flags)), magenta(go_list(pkgs)));
    ansi("\x1b[36m");
    let status = Command::new("sudo").arg(bin).args(&flags).args(pkgs).status();
    let ok = match status {
        Ok(st) if st.success() => {
            LAST_EXIT_CODE.store(0, Ordering::Relaxed);
            true
        }
        Ok(st) => {
            if st.signal() == Some(libc::SIGINT) {
                println!("\n{} {}", red("[!]"), white("Operação cancelada pelo usuário."));
                exit_with(1);
            }
            LAST_EXIT_CODE.store(st.code().filter(|c| *c > 0).unwrap_or(1), Ordering::Relaxed);
            false
        }
        Err(_) => {
            LAST_EXIT_CODE.store(1, Ordering::Relaxed);
            false
        }
    };
    ansi("\x1b[0m");
    ok
}

// fail_exit sai com o código do último comando que falhou (no mínimo 1).
fn fail_exit() -> ! {
    let code = LAST_EXIT_CODE.load(Ordering::Relaxed);
    exit_with(if code == 0 { 1 } else { code })
}

// package_exists_in_repo confirma, via `xbps-query -R <pkgname>`, se o
// pacote existe em algum repositório ativo, SEM inspecionar a saída do
// próprio xbps-install, que precisa rodar com stdio herdado do terminal.
fn package_exists_in_repo(pkgname: &str, flags: &[String]) -> bool {
    // repassa as opções que mudam onde o xbps procura, para conferir nos
    // mesmos repositórios que o xbps-install vai usar
    let mut args: Vec<String> = vec!["-R".into()];
    for f in flags {
        let Some((name, value)) = f.split_once('=') else { continue };
        match name {
            "--repository" => args.push(format!("--repository={value}")),
            "--rootdir" => args.extend(["-r".into(), value.to_string()]),
            "--config" => args.extend(["-C".into(), value.to_string()]),
            _ => {}
        }
    }
    args.push(pkgname.to_string());
    let mut cmd = Command::new("xbps-query");
    cmd.args(&args);
    output_timeout(cmd, EXEC_TIMEOUT).1
}

// --- EXECUÇÃO DE COMANDOS ---

// Saída de um comando: stdout (mesmo se falhou) e o erro, se houve.
struct Out {
    stdout: String,
    err: Option<String>,
}

fn status_desc(st: ExitStatus) -> String {
    match (st.code(), st.signal()) {
        (Some(c), _) => format!("exit status {c}"),
        (None, Some(9)) => "signal: killed".into(),
        (None, Some(s)) => format!("signal: {s}"),
        _ => "erro desconhecido".into(),
    }
}

// capture roda o comando e captura o stdout (stderr descartado, stdin nulo).
fn capture<I, S>(prog: &str, args: I) -> Out
where
    I: IntoIterator<Item = S>,
    S: AsRef<OsStr>,
{
    match Command::new(prog).args(args).stdin(Stdio::null()).stderr(Stdio::null()).output() {
        Ok(o) => Out {
            stdout: String::from_utf8_lossy(&o.stdout).into_owned(),
            err: if o.status.success() { None } else { Some(status_desc(o.status)) },
        },
        Err(e) => Out { stdout: String::new(), err: Some(format!("exec: \"{prog}\": {e}")) },
    }
}

// output_timeout roda o comando com limite de tempo e captura o stdout.
// Retorna (stdout, sucesso). Estourando o tempo, mata o processo.
fn output_timeout(mut cmd: Command, dur: Duration) -> (String, bool) {
    cmd.stdin(Stdio::null()).stdout(Stdio::piped()).stderr(Stdio::null());
    let Ok(mut child) = cmd.spawn() else { return (String::new(), false) };
    let mut stdout = child.stdout.take().expect("stdout com pipe");
    let (tx, rx) = mpsc::channel();
    thread::spawn(move || {
        let mut buf = Vec::new();
        let _ = stdout.read_to_end(&mut buf);
        let _ = tx.send(buf);
    });
    let deadline = Instant::now() + dur;
    let status = loop {
        match child.try_wait() {
            Ok(Some(st)) => break Some(st),
            Ok(None) if Instant::now() >= deadline => {
                let _ = child.kill();
                let _ = child.wait();
                break None;
            }
            Ok(None) => thread::sleep(Duration::from_millis(5)),
            Err(_) => break None,
        }
    };
    // terminou normalmente: lê até o fim; foi morto: não espera netos
    // que ainda segurem o pipe
    let buf = match status {
        Some(_) => rx.recv().unwrap_or_default(),
        None => rx.recv_timeout(Duration::from_secs(1)).unwrap_or_default(),
    };
    (String::from_utf8_lossy(&buf).into_owned(), status.map(|s| s.success()).unwrap_or(false))
}

// run_timeout roda o comando (stdio já configurado) com limite de tempo.
fn run_timeout(cmd: &mut Command, dur: Duration) -> Result<(), String> {
    let mut child = cmd.spawn().map_err(|e| e.to_string())?;
    let deadline = Instant::now() + dur;
    loop {
        match child.try_wait() {
            Ok(Some(st)) if st.success() => return Ok(()),
            Ok(Some(st)) => return Err(status_desc(st)),
            Ok(None) if Instant::now() >= deadline => {
                let _ = child.kill();
                let _ = child.wait();
                return Err("signal: killed".into());
            }
            Ok(None) => thread::sleep(Duration::from_millis(20)),
            Err(e) => return Err(e.to_string()),
        }
    }
}

// look_path procura um executável no PATH (como exec.LookPath do Go).
fn look_path(name: &str) -> Option<PathBuf> {
    let path = std::env::var_os("PATH")?;
    std::env::split_paths(&path).map(|d| d.join(name)).find(|p| {
        fs::metadata(p).map(|m| m.is_file() && m.permissions().mode() & 0o111 != 0).unwrap_or(false)
    })
}

fn not_exist(p: &Path) -> bool {
    matches!(fs::metadata(p), Err(e) if e.kind() == io::ErrorKind::NotFound)
}

fn younger_than(p: &Path, dur: Duration) -> bool {
    fs::metadata(p)
        .and_then(|m| m.modified())
        .ok()
        .and_then(|t| t.elapsed().ok())
        .map(|e| e < dur)
        .unwrap_or(false)
}

// --- FUNÇÕES DE BUSCA DETALHADA E UTILITÁRIOS ---

// truncate corta s em max bytes, terminando com "...", sem quebrar um
// caractere UTF-8 no meio.
fn truncate(s: &str, max: isize) -> String {
    if max < 3 {
        return String::new();
    }
    let max = max as usize;
    if s.len() <= max {
        return s.to_string();
    }
    let mut end = max - 3;
    while !s.is_char_boundary(end) {
        end -= 1;
    }
    format!("{}...", &s[..end])
}

fn print_package(w: &mut impl Write, i: usize, p: &Package, width: usize) {
    let idx_text = format!("[{}]", i + 1);
    let padding = idx_text.len() + 1 + 3 + 1;
    let len_prefix = padding + p.full_name.len() + 2;
    let desc = truncate(&p.description, width as isize - len_prefix as isize);
    let _ = writeln!(w, "{} {} {}  {}", yellow(&idx_text), p.status, white(&p.full_name), green(desc));
    let _ = writeln!(
        w,
        "{:padding$}{} | {} ({} / {})",
        "",
        cyan(&p.repo),
        magenta(&p.maintainer),
        yellow(format_bytes(p.size_download)),
        magenta(format_bytes(p.size_installed))
    );
}

// get_installed_packages: conjunto dos pkgver instalados.
fn get_installed_packages() -> HashSet<String> {
    let out = capture("xbps-query", ["-l"]);
    if let Some(e) = out.err {
        eprintln!("{} {}: {e}", yellow("[!]"), white("falha ao listar pacotes instalados (xbps-query -l)"));
        return HashSet::new();
    }
    out.stdout
        .lines()
        .filter_map(|l| l.split_whitespace().nth(1))
        .map(String::from)
        .collect()
}

// get_installed_pkgver_by_name retorna nome-do-pacote -> pkgver instalado
// (ex.: "firefox" -> "firefox-128.0_1").
fn get_installed_pkgver_by_name() -> HashMap<String, String> {
    let out = capture("xbps-query", ["-l"]);
    if let Some(e) = out.err {
        eprintln!("{} {}: {e}", yellow("[!]"), white("falha ao listar pacotes instalados (xbps-query -l)"));
        return HashMap::new();
    }
    out.stdout
        .lines()
        .filter_map(|l| l.split_whitespace().nth(1))
        .map(|pkgver| (clean_version(pkgver).to_string(), pkgver.to_string()))
        .collect()
}

// removed_packages compara dois snapshots (antes e depois de um
// xbps-remove) e retorna os pkgver que sumiram: a contagem real do que foi
// removido, e não uma previsão feita antes.
fn removed_packages(before: &HashMap<String, String>, after: &HashMap<String, String>) -> Vec<String> {
    let mut removed: Vec<String> =
        before.iter().filter(|(name, _)| !after.contains_key(*name)).map(|(_, v)| v.clone()).collect();
    removed.sort();
    removed
}

// print_removal_summary mostra quantos pacotes saíram num -X/-XR e quantos
// deles eram órfãos (removidos sem terem sido pedidos como alvo).
fn print_removal_summary(removed: &[String], targets: &[String]) {
    if removed.is_empty() {
        return;
    }
    let asked: HashSet<&str> = targets.iter().map(String::as_str).collect();
    let orphans = removed.iter().filter(|p| !asked.contains(p.as_str()) && !asked.contains(clean_version(p))).count();
    println!("\n{} {} {}", yellow("[!]"), white("Pacotes removidos:"), cyan(removed.len()));
    println!("{} {} {}", yellow("[!]"), white("Órfãos removidos:"), cyan(orphans));
}

// is_new_version usa o comparador oficial do xbps (xbps-uhelper cmpver)
// para confirmar que req_ver é mais recente que inst_ver, e não só
// "diferente" (evita listar downgrade como atualização).
// Exit status: 255 = instver < reqver, 0 = iguais, 1 = instver > reqver.
fn is_newer_version(inst_ver: &str, req_ver: &str) -> bool {
    let st = Command::new("xbps-uhelper")
        .args(["cmpver", inst_ver, req_ver])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    match st {
        Ok(st) => st.code() == Some(255),
        Err(e) => {
            eprintln!("{} {}: {e}", yellow("[!]"), white("falha ao comparar versões via xbps-uhelper"));
            false // não conseguiu confirmar; por segurança, não lista como upgrade
        }
    }
}

// extract_pkgver_from_cache_file: "firefox-128.0_1.x86_64.xbps" ->
// "firefox-128.0_1" (padrão "<pkgver>.<arch>.xbps").
fn extract_pkgver_from_cache_file(name: &str) -> &str {
    let base = name.strip_suffix(".xbps").unwrap_or(name);
    match base.rfind('.') {
        Some(i) => &base[..i],
        None => "",
    }
}

// get_active_repos: diretório em /var/db/xbps -> URL do repositório.
fn get_active_repos() -> HashMap<String, String> {
    let out = capture("xbps-query", ["-L"]);
    if let Some(e) = out.err {
        eprintln!("{} {}: {e}", yellow("[!]"), white("falha ao listar repositórios (xbps-query -L)"));
        return HashMap::new();
    }
    out.stdout
        .lines()
        .filter_map(|l| l.split_whitespace().nth(1))
        .map(|url| (url.replace([':', '/', '.'], "_"), url.to_string()))
        .collect()
}

// repodata_arch: sufixo de arquitetura do índice (x86_64-repodata,
// x86_64-musl-repodata, aarch64-repodata), em vez de "x86_64" fixo.
fn repodata_arch() -> String {
    let out = capture("xbps-query", ["-p", "architecture", "xbps"]);
    let mut arch = out.stdout.trim().to_string();
    if out.err.is_some() || arch.is_empty() {
        let o2 = capture("uname", ["-m"]);
        if o2.err.is_none() {
            arch = o2.stdout.trim().to_string();
        }
    }
    if arch.is_empty() {
        arch = "x86_64".into();
    }
    arch
}

// repodata_paths lista (arquivo de índice, URL) de cada repositório ativo,
// com fallback para x86_64-repodata (ex.: repo multilib).
fn repodata_paths(repo_map: &HashMap<String, String>, arch: &str) -> Vec<(PathBuf, String)> {
    let base = Path::new("/var/db/xbps");
    repo_map
        .iter()
        .filter_map(|(dir, url)| {
            let p = base.join(dir).join(format!("{arch}-repodata"));
            if !not_exist(&p) {
                return Some((p, url.clone()));
            }
            let p = base.join(dir).join("x86_64-repodata");
            (!not_exist(&p)).then(|| (p, url.clone()))
        })
        .collect()
}

// read_repodata_plist descomprime o repodata (zstd) e extrai o
// "index.plist" do tar embutido (o crate tar trata headers PAX).
fn read_repodata_plist(path: &Path) -> Result<Vec<u8>, String> {
    let file = fs::File::open(path).map_err(|e| e.to_string())?;
    let zr = zstd::stream::read::Decoder::new(file).map_err(|e| e.to_string())?;
    let mut archive = tar::Archive::new(zr);
    for entry in archive.entries().map_err(|e| e.to_string())? {
        let mut entry = entry.map_err(|e| e.to_string())?;
        let is_index = entry.path().map(|p| p.as_os_str() == "index.plist").unwrap_or(false);
        if is_index {
            let mut buf = Vec::new();
            entry.read_to_end(&mut buf).map_err(|e| e.to_string())?;
            return Ok(buf);
        }
    }
    Err("index.plist não encontrado no repodata".into())
}

// load_index lê e decodifica o índice; verbose avisa os erros no stderr.
fn load_index(path: &Path, url: &str, verbose: bool) -> Option<plist::Dictionary> {
    let data = match read_repodata_plist(path) {
        Ok(d) => d,
        Err(e) => {
            if verbose {
                eprintln!("{} {} ({url}): {e}", yellow("[!]"), white("falha ao ler repodata"));
            }
            return None;
        }
    };
    match plist::Value::from_reader(Cursor::new(data)).map(|v| v.into_dictionary()) {
        Ok(Some(d)) => Some(d),
        Ok(None) => {
            if verbose {
                eprintln!("{} {} ({url}): não é um dicionário", yellow("[!]"), white("falha ao decodificar plist"));
            }
            None
        }
        Err(e) => {
            if verbose {
                eprintln!("{} {} ({url}): {e}", yellow("[!]"), white("falha ao decodificar plist"));
            }
            None
        }
    }
}

fn pv_str(d: &plist::Dictionary, key: &str) -> String {
    match d.get(key) {
        Some(plist::Value::String(s)) => s.clone(),
        _ => String::new(),
    }
}

fn pv_int(d: &plist::Dictionary, key: &str) -> i64 {
    match d.get(key) {
        Some(plist::Value::Integer(i)) => i.as_signed().or_else(|| i.as_unsigned().map(|u| u as i64)).unwrap_or(0),
        Some(plist::Value::Real(f)) => *f as i64,
        _ => 0,
    }
}

// remote_search_detailed é o -Sss: busca no texto completo e mostra no
// formato detalhado. Sem resultado, procura o nome corrigido de cada termo
// ("fiiglet" -> "figlet") e mostra a busca detalhada dele.
fn remote_search_detailed(queries: &[String]) {
    let pkgs = search_detailed(queries);
    if !pkgs.is_empty() {
        print_detailed(&pkgs, "Resultados encontrados nos repositórios:");
        return;
    }
    println!("{} {}", red("[!]"), white("Nenhum pacote encontrado."));
    for q in queries {
        for name in corrected_terms(q) {
            let found = search_detailed(std::slice::from_ref(&name));
            if !found.is_empty() {
                print_detailed(&found, &format!("Você quis dizer '{name}' ?"));
            }
        }
    }
    exit_with(1);
}

// print_detailed mostra pacotes no formato do -Sss (duas linhas por pacote).
fn print_detailed(pkgs: &[Package], title: &str) {
    let width = terminal_width();
    let mut w = BufWriter::new(io::stdout().lock());
    let _ = writeln!(w, "\n{}", cyan(title));
    let _ = writeln!(w, "{}", separator(width));
    for (i, p) in pkgs.iter().enumerate() {
        print_package(&mut w, i, p, width);
    }
    let _ = writeln!(w, "{}", separator(width));
}

enum Matcher {
    Re(Regex),
    Plain(String),
}

// search_detailed procura os termos no texto completo dos pacotes (nome,
// versão, descrição, mantenedor, repositório) e devolve os que casam com
// qualquer um, ordenados. Um thread por repositório.
fn search_detailed(queries: &[String]) -> Vec<Package> {
    let repo_map = get_active_repos();
    let installed = get_installed_packages();
    let arch = repodata_arch();

    // termo com cara de regex casa sobre o texto (sem diferenciar
    // maiúsculas/minúsculas); senão, busca simples sem espaços
    let matchers: Vec<Matcher> = queries
        .iter()
        .map(|q| {
            if looks_like_regex(q) {
                Matcher::Re(Regex::new(&format!("(?i){q}")).expect("regex já validada"))
            } else {
                Matcher::Plain(q.to_lowercase().replace(' ', ""))
            }
        })
        .collect();

    let paths = repodata_paths(&repo_map, &arch);
    let mut pkgs: Vec<Package> = thread::scope(|s| {
        let handles: Vec<_> = paths
            .iter()
            .map(|(path, url)| {
                let (matchers, installed) = (&matchers, &installed);
                s.spawn(move || {
                    let Some(index) = load_index(path, url, true) else { return Vec::new() };
                    let mut local = Vec::new();
                    for pkg in index.values().filter_map(|v| v.as_dictionary()) {
                        let pkgver = pv_str(pkg, "pkgver");
                        let desc = pv_str(pkg, "short_desc");
                        let maint = pv_str(pkg, "maintainer");
                        let full = format!("{pkgver} {desc} {maint} {url}");
                        let full_clean = full.replace(' ', "").to_lowercase();
                        let matched = matchers.iter().any(|m| match m {
                            Matcher::Re(re) => re.is_match(&full),
                            Matcher::Plain(c) => full_clean.contains(c.as_str()),
                        });
                        if !matched {
                            continue;
                        }
                        let status = if installed.contains(&pkgver) { green("[✔]") } else { red("[-]") };
                        local.push(Package {
                            status,
                            full_name: pkgver,
                            description: desc,
                            maintainer: maint,
                            repo: url.clone(),
                            size_download: pv_int(pkg, "filename-size"),
                            size_installed: pv_int(pkg, "installed_size"),
                        });
                    }
                    local
                })
            })
            .collect();
        handles.into_iter().flat_map(|h| h.join().unwrap_or_default()).collect()
    });
    sort_by_name(&mut pkgs);
    unique_packages_exact(pkgs)
}

// sync_and_check_updates sincroniza os índices e lista o que mudaria, sem
// instalar nada:
//   "sy":  sync normal + só atualizações reais (repo mais novo)
//   "syy": sync FORÇADO (--force) + só atualizações reais
//   "sf":  sync FORÇADO + TODAS as diferenças, incluindo downgrades
fn sync_and_check_updates(flavor: &str) {
    let force_sync = flavor == "syy" || flavor == "sf";
    let show_downgrades = flavor == "sf";

    println!("{} {}", cyan("[vinstall]"), white("Sincronizando índices dos repositórios..."));
    let mut sync_flags = vec!["-S".to_string()];
    if force_sync {
        sync_flags.push("--force".into());
    }
    if !run_binary("xbps-install", &sync_flags, &[]) {
        println!("{} {}", red("[!]"), white("Falha ao sincronizar índices."));
        fail_exit();
    }
    println!();

    let repo_map = get_active_repos();
    let installed = get_installed_pkgver_by_name();
    let arch = repodata_arch();
    let paths = repodata_paths(&repo_map, &arch);

    let updates: Vec<Package> = thread::scope(|s| {
        let handles: Vec<_> = paths
            .iter()
            .map(|(path, url)| {
                let installed = &installed;
                s.spawn(move || {
                    let Some(index) = load_index(path, url, true) else { return Vec::new() };
                    let mut local = Vec::new();
                    for pkg in index.values().filter_map(|v| v.as_dictionary()) {
                        let pkgver = pv_str(pkg, "pkgver");
                        let Some(inst_ver) = installed.get(clean_version(&pkgver)) else { continue };
                        if *inst_ver == pkgver {
                            continue; // já na versão mais recente
                        }
                        let is_upgrade = is_newer_version(inst_ver, &pkgver);
                        if !is_upgrade && !show_downgrades {
                            // versão diferente, mas não mais nova (mirror
                            // desatualizado, por exemplo); só no -Sf
                            continue;
                        }
                        local.push(Package {
                            status: if is_upgrade { "upgrade" } else { "downgrade" }.into(),
                            full_name: pkgver,
                            description: inst_ver.clone(), // guarda a versão instalada
                            maintainer: pv_str(pkg, "maintainer"),
                            repo: url.clone(),
                            size_download: pv_int(pkg, "filename-size"),
                            size_installed: pv_int(pkg, "installed_size"),
                        });
                    }
                    local
                })
            })
            .collect();
        handles.into_iter().flat_map(|h| h.join().unwrap_or_default()).collect()
    });

    if updates.is_empty() {
        println!("{} {}", green("[✔]"), white("Sistema já está atualizado."));
        return;
    }

    // Pacotes em mais de um repositório (principal + multilib) repetem o
    // mesmo pkgver de destino: dedup por full_name.
    let mut updates = unique_packages_exact(updates);
    sort_by_name(&mut updates);

    let downgrade_count = updates.iter().filter(|p| p.status == "downgrade").count();
    let upgrade_count = updates.len() - downgrade_count;

    let width = terminal_width();
    if show_downgrades && downgrade_count > 0 {
        println!(
            "{} ({} {}, {} {}):",
            cyan("Pacotes com diferença de versão"),
            yellow(upgrade_count),
            green("atualizações"),
            yellow(downgrade_count),
            red("downgrades")
        );
    } else {
        println!("{} ({}):", cyan("Pacotes com atualização disponível"), yellow(updates.len()));
    }
    println!("{}", separator(width));

    // Largura de cada coluna medida no texto PURO e cor aplicada depois do
    // padding: colorir antes faz os bytes de escape contarem no tamanho.
    struct Row<'a> {
        name: &'a str,
        old_ver: &'a str,
        new_ver: &'a str,
        size: String,
        repo: &'a str,
        is_upgrade: bool,
        download: i64,
    }
    let rows: Vec<Row> = updates
        .iter()
        .map(|p| Row {
            name: clean_version(&p.full_name),
            old_ver: extract_version(&p.description),
            new_ver: extract_version(&p.full_name),
            size: format_bytes(p.size_download),
            repo: &p.repo,
            is_upgrade: p.status != "downgrade",
            download: p.size_download,
        })
        .collect();
    let max_name = rows.iter().map(|r| r.name.len()).max().unwrap_or(0).min(40);
    let max_old = rows.iter().map(|r| r.old_ver.len()).max().unwrap_or(0);
    let max_new = rows.iter().map(|r| r.new_ver.len()).max().unwrap_or(0);
    let max_size = rows.iter().map(|r| r.size.len()).max().unwrap_or(0);

    let mut total: i64 = 0;
    {
        let mut w = BufWriter::new(io::stdout().lock());
        for (i, r) in rows.iter().enumerate() {
            let idx = yellow(format!("[{:2}]", i + 1));
            let new_padded = format!("{:<max_new$}", r.new_ver);
            let (new_colored, tag) = if r.is_upgrade {
                (green(&new_padded), String::new())
            } else {
                (red(&new_padded), format!("  {}", red("[DOWNGRADE]")))
            };
            let _ = writeln!(
                w,
                "{} {} {} → {}  ({})  {}{}",
                idx,
                white(format!("{:<max_name$}", r.name)),
                yellow(format!("{:>max_old$}", r.old_ver)),
                new_colored,
                magenta(format!("{:>max_size$}", r.size)),
                cyan(truncate(r.repo, 45)),
                tag
            );
            total += r.download;
        }
    }

    println!("{}", separator(width));
    println!("{} {}\n", white("Total a baixar:"), cyan(format_bytes(total)));
    if downgrade_count > 0 {
        println!(
            "{} {}",
            yellow("[!]"),
            white("Itens marcados como [DOWNGRADE] não serão instalados por 'vinstall -Syu'.")
        );
        println!("{} {}", yellow("[!]"), white("Void permite downgrade, mas exige --force/-f. Para aplicar um específico:"));
        println!("    {}", cyan("vinstall -f <pacote>"));
    }
    println!("{} {}", yellow("[!]"), white("Rode 'vinstall -Syu' para aplicar as atualizações."));
}

// --- FUNÇÕES DE BUSCA RÁPIDA E OUTROS ---

// looks_like_regex: o termo do -Ss/-Sss é tratado como regex se tiver
// algum de ^ $ | ( ) [ ] { } \ * ? ou ".+", e compilar. "+" e "." sozinhos
// ficam como texto (gtk+3, libstdc++, python3.12).
fn looks_like_regex(term: &str) -> bool {
    if !term.chars().any(|c| "^$|()[]{}\\*?".contains(c)) && !term.contains(".+") {
        return false;
    }
    Regex::new(term).is_ok()
}

// quote_meta escapa os mesmos caracteres que o regexp.QuoteMeta do Go
// (seguro para o ERE do git grep usado pelo xlocate).
fn quote_meta(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for c in s.chars() {
        if "\\.+*?()|[]{}^$".contains(c) {
            out.push('\\');
        }
        out.push(c);
    }
    out
}

// levenshtein: quantas letras trocar, acrescentar ou tirar para a virar b.
fn levenshtein(a: &str, b: &str) -> usize {
    let (ra, rb): (Vec<char>, Vec<char>) = (a.chars().collect(), b.chars().collect());
    let mut prev: Vec<usize> = (0..=rb.len()).collect();
    let mut cur = vec![0; rb.len() + 1];
    for i in 1..=ra.len() {
        cur[0] = i;
        for j in 1..=rb.len() {
            let cost = usize::from(ra[i - 1] != rb[j - 1]);
            cur[j] = (prev[j] + 1).min(cur[j - 1] + 1).min(prev[j - 1] + cost);
        }
        std::mem::swap(&mut prev, &mut cur);
    }
    prev[rb.len()]
}

// name_distance compara o termo com o nome inteiro e também com o começo
// do nome, do tamanho do termo ("telegran" -> "telegram-desktop").
fn name_distance(term: &str, name: &str) -> usize {
    let mut d = levenshtein(term, name);
    let tn = term.chars().count();
    if name.chars().count() > tn {
        let prefix: String = name.chars().take(tn).collect();
        d = d.min(levenshtein(term, &prefix) + 1);
    }
    d
}

// repo_packages lê todos os pacotes dos índices locais dos repositórios,
// marcando os instalados com status "*".
fn repo_packages() -> Vec<Package> {
    let installed = get_installed_packages();
    let arch = repodata_arch();
    let mut pkgs = Vec::new();
    for (path, url) in repodata_paths(&get_active_repos(), &arch) {
        let Some(index) = load_index(&path, &url, false) else { continue };
        for pkg in index.values().filter_map(|v| v.as_dictionary()) {
            let pkgver = pv_str(pkg, "pkgver");
            pkgs.push(Package {
                status: if installed.contains(&pkgver) { "*".into() } else { String::new() },
                description: pv_str(pkg, "short_desc"),
                full_name: pkgver,
                ..Default::default()
            });
        }
    }
    pkgs
}

// similar_packages devolve até limit pacotes com nome parecido com algum
// dos termos ("fiirefox" -> "firefox"), do mais parecido para o menos.
// Aceita até ~1 letra errada a cada 4 do termo. Regex não entra.
fn similar_packages(terms: &[String], limit: usize) -> Vec<Package> {
    let mut all: Option<Vec<Package>> = None;
    let mut best: Vec<(Package, usize)> = Vec::new();
    let mut seen = HashSet::new();
    for term in terms {
        if looks_like_regex(term) {
            continue;
        }
        let all = all.get_or_insert_with(repo_packages);
        let t = term.to_lowercase();
        let max_dist = (t.chars().count() / 4).max(1);
        for p in all.iter() {
            let name = clean_version(&p.full_name);
            if seen.contains(&p.full_name) || name.to_lowercase() == t {
                continue;
            }
            let d = name_distance(&t, &name.to_lowercase());
            if d <= max_dist {
                seen.insert(p.full_name.clone());
                best.push((p.clone(), d));
            }
        }
    }
    best.sort_by(|a, b| a.1.cmp(&b.1).then_with(|| a.0.full_name.cmp(&b.0.full_name)));
    best.into_iter().take(limit).map(|(p, _)| p).collect()
}

// corrected_terms devolve os nomes de pacote mais parecidos com o termo
// (os de menor distância, até 3): "fiirefox" -> ["firefox"].
fn corrected_terms(term: &str) -> Vec<String> {
    let similar = similar_packages(&[term.to_string()], 50);
    let Some(first) = similar.first() else { return Vec::new() };
    let t = term.to_lowercase();
    let best = name_distance(&t, &clean_version(&first.full_name).to_lowercase());
    let mut names: Vec<String> = Vec::new();
    for p in &similar {
        let name = clean_version(&p.full_name);
        if names.iter().any(|n| n == name) || name_distance(&t, &name.to_lowercase()) != best {
            continue;
        }
        names.push(name.to_string());
        if names.len() == 3 {
            break;
        }
    }
    names
}

// show_did_you_mean: sem resultado, procura o nome corrigido de cada termo
// e mostra a busca completa dele ("Você quis dizer 'firefox' ?").
fn show_did_you_mean(terms: &[String], filter: &str) {
    for term in terms {
        for name in corrected_terms(term) {
            let mut pkgs = unique_packages_exact(fetch_suggestions(&name));
            sort_by_name(&mut pkgs);
            if !filter.is_empty() {
                pkgs = filter_packages(pkgs, filter);
            }
            if !pkgs.is_empty() {
                display_search_list(&pkgs, &format!("Você quis dizer '{name}' ?"), false);
            }
        }
    }
}

fn fetch_suggestions(query: &str) -> Vec<Package> {
    let mut cmd = Command::new("xbps-query");
    if looks_like_regex(query) {
        cmd.args(["--regex", "-Rs", query]); // ERE do próprio xbps-query
    } else {
        cmd.args(["-Rs", query]);
    }
    let (out, _) = output_timeout(cmd, EXEC_TIMEOUT);
    let mut pkgs = Vec::new();
    for line in out.split('\n') {
        let (Some(status), Some(rest)) = (line.get(..4), line.get(4..)) else { continue };
        let rest = rest.trim();
        let (name, desc) = rest.split_once(' ').unwrap_or((rest, ""));
        if !name.is_empty() {
            pkgs.push(Package {
                status: status.trim().into(),
                full_name: name.into(),
                description: desc.trim().into(),
                ..Default::default()
            });
        }
    }
    pkgs
}

fn display_search(pkgs: &[Package], title: &str) {
    display_search_list(pkgs, title, true);
}

// display_search_list mostra a tabela de pacotes; numbered=false tira a
// coluna "[ n]" (lista só informativa, sem menu).
fn display_search_list(pkgs: &[Package], title: &str, numbered: bool) {
    if pkgs.is_empty() {
        return;
    }
    let width = terminal_width();
    let max_name = pkgs.iter().map(|p| p.full_name.len()).max().unwrap_or(0).min(50);
    let mut w = BufWriter::new(io::stdout().lock());
    if !title.is_empty() {
        let _ = writeln!(w, "\n{}", cyan(title));
    }
    let _ = writeln!(w, "{}", separator(width));
    for (i, p) in pkgs.iter().enumerate() {
        let status = if p.status.contains('*') { green_bold("[✓] ") } else { red("[-] ") };
        if numbered {
            let _ = write!(w, "{} ", yellow(format!("[{:2}]", i + 1)));
        }
        let _ =
            writeln!(w, "{} {}  {}", status, white(format!("{:<max_name$}", p.full_name)), green(&p.description));
    }
    let _ = writeln!(w, "{}", separator(width));
}

fn read_line() -> String {
    flush();
    let mut s = String::new();
    let _ = io::stdin().read_line(&mut s);
    s
}

// select_from_menu mostra as sugestões e devolve os nomes escolhidos
// (aceita "1 3 5" ou "1,3,5"; repetidos e inválidos são ignorados, os
// inválidos com aviso).
fn select_from_menu(pkgs: &[Package], term: &str) -> Vec<String> {
    if pkgs.is_empty() {
        return Vec::new();
    }
    display_search(pkgs, &format!("Sugestões para '{term}':"));
    print!("{}", yellow("Selecione o(s) número(s) para instalar (ex.: 1 3 5) ou 'q' para sair: "));
    let input = read_line();
    let input = input.trim();
    if input.is_empty() || input == "q" {
        return Vec::new();
    }
    let mut names = Vec::new();
    let mut seen = HashSet::new();
    for field in input.split([' ', ',']).filter(|f| !f.is_empty()) {
        match field.parse::<usize>() {
            Ok(n) if (1..=pkgs.len()).contains(&n) => {
                if seen.insert(n) {
                    names.push(clean_version(&pkgs[n - 1].full_name).to_string());
                }
            }
            _ => println!("{} {} '{}'", yellow("[!]"), white("opção inválida ignorada:"), field),
        }
    }
    names
}

fn sort_by_name(pkgs: &mut [Package]) {
    pkgs.sort_by(|a, b| a.full_name.cmp(&b.full_name));
}

fn unique_packages_exact(pkgs: Vec<Package>) -> Vec<Package> {
    let mut keys = HashSet::new();
    pkgs.into_iter().filter(|p| keys.insert(p.full_name.clone())).collect()
}

fn filter_packages(pkgs: Vec<Package>, mode: &str) -> Vec<Package> {
    pkgs.into_iter()
        .filter(|p| {
            let installed = p.status.contains('*');
            (mode == "installed" && installed) || (mode == "missing" && !installed)
        })
        .collect()
}

// --- FUNÇÕES DE SISTEMA, LIMPEZA E FIND ---

// FileQuery é o termo do -F/-FR, no estilo do "pacman -F". Modos:
//   - "exact" (-F/-FR): o termo em qualquer parte do NOME do arquivo, sem
//     olhar os diretórios. Com "/" no termo, vale o caminho todo;
//   - "any" (-Fa/-FRa): o termo em QUALQUER parte do caminho;
//   - "regex" (-Fx/-FRx): expressão regular aplicada ao caminho completo.
struct FileQuery {
    term: String,
    mode: String,
    re: Option<Regex>,
}

impl FileQuery {
    fn new(term: &str, mode: &str) -> Result<Self, regex::Error> {
        let re = if mode == "regex" { Some(Regex::new(term)?) } else { None };
        Ok(FileQuery { term: term.into(), mode: mode.into(), re })
    }

    fn is_regex(&self) -> bool {
        self.re.is_some()
    }

    // has_dir: o termo tem "/", ou seja, fala de diretório também.
    fn has_dir(&self) -> bool {
        self.term.contains('/')
    }

    fn is_match(&self, path: &str) -> bool {
        if let Some(re) = &self.re {
            return re.is_match(path);
        }
        if self.mode == "any" || self.has_dir() {
            return path.contains(&self.term);
        }
        base_name(path).contains(&self.term)
    }

    // owned_args: argumentos do xbps-query -o (fnmatch no caminho
    // completo). "*termo*" pega também diretórios; no modo exato,
    // find_local filtra depois pelo nome do arquivo.
    fn owned_args(&self) -> Vec<String> {
        if self.is_regex() {
            return vec!["--regex".into(), "-o".into(), self.term.clone()];
        }
        vec!["-o".into(), format!("*{}*", self.term)]
    }

    // xlocate_args: argumentos do xlocate (git grep sobre os caminhos).
    // Modo exato: "termo[^/]*$" (termo no nome do arquivo); modo "any" ou
    // termo com "/": texto fixo (-F).
    fn xlocate_args(&self) -> Vec<String> {
        if self.is_regex() {
            return vec!["-E".into(), self.term.clone()];
        }
        if self.mode == "any" || self.has_dir() {
            return vec!["-F".into(), self.term.clone()];
        }
        vec!["-E".into(), format!("{}[^/]*$", quote_meta(&self.term))]
    }
}

// base_name: último segmento do caminho (como filepath.Base).
fn base_name(path: &str) -> &str {
    let p = path.trim_end_matches('/');
    if p.is_empty() {
        return if path.is_empty() { "." } else { "/" };
    }
    p.rsplit('/').next().unwrap_or(p)
}

// find_provides: -F busca só nos instalados; -FR só nos repositórios.
// Retorna true se achou algum arquivo.
fn find_provides(term: &str, search_remote: bool, mode: &str) -> bool {
    let q = match FileQuery::new(term, mode) {
        Ok(q) => q,
        Err(e) => {
            println!("{} {}: {e}", red("[!]"), white("expressão regular inválida"));
            return false;
        }
    };
    if search_remote {
        find_remote(&q)
    } else {
        find_local(&q)
    }
}

// owned_line_path extrai o caminho de uma linha do xbps-query -o
// ("pkgver: /caminho (pkgver)" ou "pkgver: /link -> /alvo (pkgver)").
fn owned_line_path(line: &str) -> &str {
    let Some((_, mut rest)) = line.split_once(": ") else { return "" };
    if let Some(i) = rest.rfind(" (") {
        rest = &rest[..i];
    }
    if let Some(i) = rest.find(" -> ") {
        rest = &rest[..i];
    }
    rest
}

// FileRow: uma linha do resultado do -F/-FR.
struct FileRow {
    pkg: String,
    path: String,
    installed: bool,
}

// add_file_row acrescenta a linha se o par pacote+caminho ainda não
// apareceu (plist e xbps-query -o, ou dois índices xlocate, repetem).
fn add_file_row(rows: &mut Vec<FileRow>, seen: &mut HashSet<String>, r: FileRow) {
    if r.pkg.is_empty() || r.path.is_empty() {
        return;
    }
    if seen.insert(format!("{}\0{}", r.pkg, r.path)) {
        rows.push(r);
    }
}

// print_file_rows mostra o resultado do -F/-FR numa tabela única, no estilo
// do "pacman -F": [✓]/[-]  pacote  caminho.
fn print_file_rows(rows: &mut [FileRow]) {
    rows.sort_by(|a, b| a.pkg.cmp(&b.pkg).then_with(|| a.path.cmp(&b.path)));
    let max_pkg = rows.iter().map(|r| r.pkg.len()).max().unwrap_or(0).min(50);
    let pkgs: HashSet<&str> = rows.iter().map(|r| r.pkg.as_str()).collect();
    let width = terminal_width();
    let mut w = BufWriter::new(io::stdout().lock());
    let _ = writeln!(w, "{}", separator(width));
    for r in rows.iter() {
        let status = if r.installed { green_bold("[✓]") } else { red("[-]") };
        let _ = writeln!(w, "{} {}  {}", status, white(format!("{:<max_pkg$}", r.pkg)), green(&r.path));
    }
    let _ = writeln!(w, "{}", separator(width));
    let _ = writeln!(
        w,
        "{} {} {} {} {}",
        yellow("[!]"),
        cyan(rows.len()),
        white("arquivo(s) em"),
        cyan(pkgs.len()),
        white("pacote(s)")
    );
}

// -F: só pacotes instalados — plist local (/var/db/xbps/.*-files.plist) + xbps-query -o
fn find_local(q: &FileQuery) -> bool {
    println!("{} {} '{}' {}", cyan("[vinstall]"), white("Procurando"), yellow(&q.term), white("nos pacotes instalados:"));

    let mut rows = Vec::new();
    let mut seen = HashSet::new();

    // o plist dá só o nome do pacote; usa a versão instalada para casar
    // com as linhas do xbps-query -o
    let pkgver = get_installed_pkgver_by_name();
    println!("{} {} {}", cyan(">>>"), cyan("grep local /var/db/xbps/.*-files.plist"), yellow(&q.term));
    for mut r in search_in_local_plist(q) {
        if let Some(v) = pkgver.get(&r.pkg) {
            r.pkg = v.clone();
        }
        add_file_row(&mut rows, &mut seen, r);
    }

    let args = q.owned_args();
    println!("{} {} {}", cyan(">>>"), cyan("xbps-query"), yellow(args.join(" ")));
    flush();
    let mut cmd = Command::new("xbps-query");
    cmd.args(&args);
    let (out, _) = output_timeout(cmd, EXEC_TIMEOUT);
    for line in out.trim().split('\n') {
        let Some((pkg, _)) = line.split_once(": ") else { continue };
        let path = owned_line_path(line);
        if path.is_empty() {
            continue;
        }
        // no modo exato descarta o que casou só no diretório; no modo
        // regex, vale o --regex do próprio xbps
        if !q.is_regex() && !q.is_match(path) {
            continue;
        }
        add_file_row(&mut rows, &mut seen, FileRow { pkg: pkg.into(), path: path.into(), installed: true });
    }

    if rows.is_empty() {
        println!(
            "{} {}",
            red("[!]"),
            white("Nenhum pacote instalado contém esse arquivo. Use -FR para buscar nos repositórios.")
        );
        return false;
    }
    print_file_rows(&mut rows);
    true
}

// XlocateIndex descreve um índice de arquivos do xlocate. Com mais de um
// repo, os seguintes são reserva: o xlocate -S tenta um por vez, na ordem.
struct XlocateIndex {
    name: String,       // nome exibido
    repos: Vec<String>, // XLOCATE_REPO, em ordem de preferência
    git: PathBuf,       // XLOCATE_GIT (cópia local)
}

impl XlocateIndex {
    // command monta o xlocate apontando para este índice e, se repo não
    // for vazio, para essa origem.
    fn command(&self, x_path: &Path, repo: &str) -> Command {
        let mut cmd = Command::new(x_path);
        cmd.env("XLOCATE_GIT", &self.git);
        if !repo.is_empty() {
            cmd.env("XLOCATE_REPO", repo);
        }
        cmd
    }
}

// XLOCATE_DEFAULT_REPO é a origem padrão do xlocate (índice oficial do Void).
const XLOCATE_DEFAULT_REPO: &str = "https://repo-default.voidlinux.org/xlocate/xlocate.git";

// user_cache_dir retorna ${XDG_CACHE_HOME:-~/.cache}.
fn user_cache_dir() -> PathBuf {
    match std::env::var("XDG_CACHE_HOME") {
        Ok(d) if !d.is_empty() => PathBuf::from(d),
        _ => PathBuf::from(std::env::var("HOME").unwrap_or_default()).join(".cache"),
    }
}

// XlocateRemote: índice xlocate encontrado num servidor (URL final, depois
// de redirects, e o commit do master).
#[derive(Clone)]
struct XlocateRemote {
    url: String,
    hash: String,
}

// xlocate_indexes descobre os índices a partir dos repositórios do xbps.d.
// Convenção: o índice de um repositório fica DENTRO dele, em
// <repo>/xlocate/xlocate.git (onde o voidbr-xlocate-index grava).
// Servidores com o MESMO índice (mesmo commit: um mirror do outro) viram
// um índice só, com as URLs como origens alternativas no xlocate -S. O
// índice oficial do Void (~/.cache/xlocate.git) entra sempre.
fn xlocate_indexes() -> Vec<XlocateIndex> {
    let cache_dir = user_cache_dir();
    let remotes = discover_xlocate_remotes(&cache_dir);

    let mut indexes = vec![XlocateIndex {
        name: "Void".into(),
        git: cache_dir.join("xlocate.git"),
        repos: vec![XLOCATE_DEFAULT_REPO.into()],
    }];
    let mut by_hash: HashMap<String, usize> = HashMap::new();
    if let Some(r) = remotes.iter().find(|r| r.url == XLOCATE_DEFAULT_REPO) {
        by_hash.insert(r.hash.clone(), 0);
    }
    for r in remotes.iter().filter(|r| r.url != XLOCATE_DEFAULT_REPO) {
        if let Some(&i) = by_hash.get(&r.hash) {
            indexes[i].repos.push(r.url.clone());
            if i != 0 {
                indexes[i].name += &format!(" | {}", xlocate_host(&r.url));
            }
            continue;
        }
        by_hash.insert(r.hash.clone(), indexes.len());
        indexes.push(XlocateIndex {
            name: xlocate_host(&r.url),
            repos: vec![r.url.clone()],
            git: cache_dir.join(format!("xlocate-{}.git", xlocate_cache_name(&r.url))),
        });
    }
    indexes
}

// url_host: host[:porta] de uma URL (como u.Host do Go).
fn url_host(u: &url::Url) -> String {
    match (u.host_str(), u.port()) {
        (Some(h), Some(p)) => format!("{h}:{p}"),
        (Some(h), None) => h.to_string(),
        _ => String::new(),
    }
}

// xlocate_host extrai o host de uma URL, para exibição. Para repositório
// local, retorna o próprio caminho.
fn xlocate_host(raw: &str) -> String {
    match url::Url::parse(raw) {
        Ok(u) if u.host_str().is_some() => url_host(&u),
        _ => raw.to_string(),
    }
}

// xlocate_cache_name gera o nome da cópia local a partir de host+caminho:
// https://void.voidbr.org/voidlinux/extra/xlocate/xlocate.git ->
// void_voidbr_org_voidlinux_extra
fn xlocate_cache_name(raw: &str) -> String {
    let name = match url::Url::parse(raw) {
        Ok(u) if u.host_str().is_some() => format!("{}{}", url_host(&u), u.path()),
        _ => raw.to_string(),
    };
    let name = name.strip_suffix("/xlocate/xlocate.git").unwrap_or(&name);
    name.trim_matches('/').replace(['.', ':', '/'], "_")
}

// is_void_official_repo: repositórios oficiais do Void (e mirrors) usam o
// layout <base>/current[/...]; o índice deles é o oficial, que já entra.
fn is_void_official_repo(path: &str) -> bool {
    format!("/{}/", path.trim_matches('/')).contains("/current/")
}

// discover_xlocate_remotes lista os índices xlocate dos repositórios do
// xbps.d. O resultado fica em ~/.cache/vinstall/xlocate-indexes por 24h;
// se a lista de repositórios mudar, descobre de novo. Sem rede, reaproveita
// a última lista salva.
fn discover_xlocate_remotes(cache_dir: &Path) -> Vec<XlocateRemote> {
    let cache_file = cache_dir.join("vinstall").join("xlocate-indexes");

    let repos: Vec<String> = capture("xbps-query", ["-L"])
        .stdout
        .lines()
        .filter_map(|l| l.split_whitespace().nth(1))
        .map(String::from)
        .collect();
    let signature = format!("repos\t{}", repos.join(" "));

    if younger_than(&cache_file, Duration::from_secs(24 * 3600)) {
        let (sig, remotes) = read_xlocate_remotes(&cache_file);
        if sig == signature {
            return remotes;
        }
    }

    println!("{} {}", cyan(">>>"), cyan("procurando índices xlocate nos repositórios do xbps.d"));
    flush();

    // candidatos http(s), na ordem dos repositórios; o oficial primeiro
    let mut candidates: Vec<Vec<String>> = vec![vec![XLOCATE_DEFAULT_REPO.into()]];
    let mut local = Vec::new();
    for repo in &repos {
        if repo.starts_with('/') {
            // repositório local: índice direto do disco
            if let Some(r) = probe_xlocate_local(&Path::new(repo).join("xlocate").join("xlocate.git")) {
                local.push(r);
            }
            continue;
        }
        let Ok(u) = url::Url::parse(repo) else { continue };
        if (u.scheme() != "http" && u.scheme() != "https") || is_void_official_repo(u.path()) {
            continue;
        }
        // sobe pelo caminho: host/a/b/xlocate, host/a/xlocate, host/xlocate
        let mut list = Vec::new();
        let mut path = u.path().trim_matches('/').to_string();
        loop {
            let mut base = format!("{}://{}", u.scheme(), url_host(&u));
            if !path.is_empty() {
                base += &format!("/{path}");
            }
            list.push(format!("{base}/xlocate/xlocate.git"));
            if path.is_empty() {
                break;
            }
            path = match path.rfind('/') {
                Some(i) => path[..i].to_string(),
                None => String::new(),
            };
        }
        candidates.push(list);
    }

    let agent = ureq::AgentBuilder::new().timeout(EXEC_TIMEOUT).try_proxy_from_env(true).build();
    let results: Vec<Option<XlocateRemote>> = thread::scope(|s| {
        let handles: Vec<_> = candidates
            .iter()
            .map(|list| {
                let agent = &agent;
                s.spawn(move || list.iter().find_map(|cand| probe_xlocate_remote(agent, cand)))
            })
            .collect();
        handles.into_iter().map(|h| h.join().unwrap_or(None)).collect()
    });

    let mut remotes: Vec<XlocateRemote> = Vec::new();
    let mut seen = HashSet::new();
    for r in results.into_iter().flatten().chain(local) {
        if seen.insert(r.url.clone()) {
            remotes.push(r);
        }
    }

    if remotes.is_empty() {
        return read_xlocate_remotes(&cache_file).1; // sem rede: última lista salva
    }
    let mut buf = format!("{signature}\n");
    for r in &remotes {
        buf += &format!("{}\t{}\n", r.hash, r.url);
    }
    if let Some(dir) = cache_file.parent() {
        if fs::create_dir_all(dir).is_ok() {
            let _ = fs::write(&cache_file, buf);
        }
    }
    remotes
}

// probe_xlocate_local confere um índice xlocate num diretório local (repo
// git bare) e retorna o commit do master.
fn probe_xlocate_local(git_dir: &Path) -> Option<XlocateRemote> {
    let mut arg = std::ffi::OsString::from("--git-dir=");
    arg.push(git_dir);
    let out = Command::new("git")
        .arg(arg)
        .args(["rev-parse", "--verify", "-q", "refs/heads/master"])
        .stdin(Stdio::null())
        .stderr(Stdio::null())
        .output()
        .ok()?;
    let hash = String::from_utf8_lossy(&out.stdout).trim().to_string();
    (out.status.success() && hash.len() == 40)
        .then(|| XlocateRemote { url: git_dir.to_string_lossy().into_owned(), hash })
}

// read_xlocate_remotes lê a lista salva: primeira linha "repos<TAB>..."
// com os repositórios usados na descoberta, depois hash<TAB>url por linha.
fn read_xlocate_remotes(cache_file: &Path) -> (String, Vec<XlocateRemote>) {
    let Ok(data) = fs::read_to_string(cache_file) else { return (String::new(), Vec::new()) };
    let mut signature = String::new();
    let mut remotes = Vec::new();
    for line in data.split('\n') {
        if line.starts_with("repos\t") {
            signature = line.to_string();
            continue;
        }
        if let Some((hash, u)) = line.split_once('\t') {
            if !hash.is_empty() && !u.is_empty() {
                remotes.push(XlocateRemote { url: u.into(), hash: hash.into() });
            }
        }
    }
    (signature, remotes)
}

// probe_xlocate_remote confere se existe um índice xlocate (repositório git
// servido por HTTP) em cand, lendo cand/info/refs. Retorna a URL final,
// depois de redirects (ex.: voidbr.org -> www.voidbr.org), e o commit.
fn probe_xlocate_remote(agent: &ureq::Agent, cand: &str) -> Option<XlocateRemote> {
    let resp = agent.get(&format!("{cand}/info/refs")).call().ok()?;
    if resp.status() != 200 {
        return None;
    }
    let final_url = resp.get_url().to_string();
    let mut body = Vec::new();
    resp.into_reader().take(64 * 1024).read_to_end(&mut body).ok()?;
    let body = String::from_utf8_lossy(&body);
    for line in body.split('\n') {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() == 2
            && fields[1] == "refs/heads/master"
            && fields[0].len() == 40
            && fields[0].chars().all(|c| c.is_ascii_digit() || ('a'..='f').contains(&c))
        {
            let url = final_url.strip_suffix("/info/refs").unwrap_or(&final_url).to_string();
            return Some(XlocateRemote { url, hash: fields[0].into() });
        }
    }
    None
}

// -FR: repositórios, via xlocate, nos índices do Void e do VoidBR. O
// xbps-query -Ro NÃO é usado: em modo repositório ele baixa a lista de
// arquivos de cada pacote pela rede, um por um. Sem resultado no xlocate,
// cai no xbps-query -Rs, que procura pelo NOME do pacote.
fn find_remote(q: &FileQuery) -> bool {
    println!("{} {} '{}' {}", cyan("[vinstall]"), white("Procurando"), yellow(&q.term), white("nos repositórios remotos:"));

    let mut rows = Vec::new();
    let mut seen = HashSet::new();
    let installed = get_installed_pkgver_by_name();
    if let Some(x_path) = look_path("xlocate") {
        for idx in xlocate_indexes() {
            ensure_xlocate_index(&x_path, &idx);
            let args = q.xlocate_args();
            println!(
                "{} {} {} {}",
                cyan(">>>"),
                cyan("xlocate"),
                yellow(args.join(" ")),
                cyan(format!("({})", idx.name))
            );
            flush();
            let mut cmd = idx.command(&x_path, "");
            cmd.args(&args);
            let (out, _) = output_timeout(cmd, REMOTE_TIMEOUT);
            // saída do xlocate: "pkgver  caminho" (colunas separadas por espaços)
            for line in out.trim().split('\n') {
                let mut fields = line.split_whitespace();
                let (Some(pkg), Some(_)) = (fields.next(), fields.next()) else { continue };
                let path = line.strip_prefix(pkg).unwrap_or(line).trim();
                let is_inst = installed.contains_key(clean_version(pkg));
                add_file_row(&mut rows, &mut seen, FileRow { pkg: pkg.into(), path: path.into(), installed: is_inst });
            }
        }
    } else {
        println!(
            "{} {}",
            yellow("[!]"),
            white("xlocate não encontrado (pacote xtools); busca por arquivo nos repositórios indisponível.")
        );
    }

    if !rows.is_empty() {
        print_file_rows(&mut rows);
        return true;
    }
    if q.is_regex() {
        println!("{} {}", red("[!]"), white("Nenhum arquivo encontrado nos repositórios."));
        return false;
    }

    println!("{} {} {} {}", cyan(">>>"), cyan("xbps-query"), cyan("-Rs"), yellow(&q.term));
    let mut pkgs = unique_packages_exact(fetch_suggestions(&q.term));
    if pkgs.is_empty() {
        println!("{} {}", red("[!]"), white("Nenhum pacote encontrado nos repositórios."));
        return false;
    }
    sort_by_name(&mut pkgs);
    display_search(&pkgs, "Nenhum arquivo encontrado; pacotes com esse nome:");
    false // achou pacotes pelo nome, mas nenhum arquivo
}

// LocalPkg: pacote instalado, lido do "xbps-query -l"
// ("<estado> <pkgver> <descrição>").
struct LocalPkg {
    state: String,
    name: String,
    version: String,
    desc: String,
}

fn installed_packages() -> Result<Vec<LocalPkg>, String> {
    let out = capture("xbps-query", ["-l"]);
    if let Some(e) = out.err {
        return Err(e);
    }
    Ok(out
        .stdout
        .lines()
        .filter_map(|line| {
            let fields: Vec<&str> = line.split_whitespace().collect();
            (fields.len() >= 2).then(|| LocalPkg {
                state: fields[0].into(),
                name: clean_version(fields[1]).into(),
                version: extract_version(fields[1]).into(),
                desc: fields[2..].join(" "),
            })
        })
        .collect())
}

// list_local lista pacotes instalados (-Li), órfãos (-Lo) ou instalados que
// contêm um termo (-Qs) numa tabela alinhada: estado, nome, versão e
// descrição.
fn list_local(mode: &str, queries: &[String]) {
    match mode {
        "installed" => println!("{} {}", cyan("[vinstall]"), white("Listando pacotes instalados:")),
        "orphans" => println!("{} {}", cyan("[vinstall]"), white("Listando pacotes órfãos:")),
        "search" => println!(
            "{} {} '{}'...",
            cyan("[vinstall]"),
            white("Buscando localmente por:"),
            yellow(queries.join("', '"))
        ),
        _ => {}
    }

    let all = match installed_packages() {
        Ok(a) => a,
        Err(e) => {
            eprintln!("{} {}: {e}", yellow("[!]"), white("falha ao executar xbps-query"));
            return;
        }
    };

    let mut pkgs: Vec<LocalPkg> = match mode {
        "orphans" => {
            let out = capture("xbps-query", ["-O"]);
            if let Some(e) = out.err {
                eprintln!("{} {}: {e}", yellow("[!]"), white("falha ao executar xbps-query -O"));
                return;
            }
            let orphan: HashSet<&str> = out.stdout.split_whitespace().collect();
            all.into_iter().filter(|p| orphan.contains(format!("{}-{}", p.name, p.version).as_str())).collect()
        }
        "search" => {
            // vários termos: entra o pacote que contiver QUALQUER um deles
            let lowered: Vec<String> = queries.iter().map(|q| q.to_lowercase()).collect();
            all.into_iter()
                .filter(|p| {
                    let text = format!("{}-{} {}", p.name, p.version, p.desc).to_lowercase();
                    lowered.iter().any(|q| text.contains(q.as_str()))
                })
                .collect()
        }
        _ => all,
    };
    pkgs.sort_by(|a, b| a.name.cmp(&b.name));

    if pkgs.is_empty() {
        println!("{} {}", yellow("[!]"), white("Nenhum pacote encontrado."));
        return;
    }

    let max_name = pkgs.iter().map(|p| p.name.len()).max().unwrap_or(0).min(40);
    let max_ver = pkgs.iter().map(|p| p.version.len()).max().unwrap_or(0).min(25);

    let width = terminal_width();
    let desc_width = width as isize - (4 + 1 + max_name + 2 + max_ver + 2) as isize;
    let mut w = BufWriter::new(io::stdout().lock());
    let _ = writeln!(w, "{}", separator(width));
    for p in &pkgs {
        // largura fixa de 4 (em caracteres), para "[✓]" e "[uu]" alinharem
        let status = if p.state == "ii" {
            green_bold(format!("{:<4}", "[✓]"))
        } else {
            yellow(format!("{:<4}", format!("[{}]", p.state))) // ex.: uu = desempacotado, não configurado
        };
        let _ = writeln!(
            w,
            "{} {}  {}  {}",
            status,
            white(format!("{:<max_name$}", p.name)),
            yellow(format!("{:<max_ver$}", p.version)),
            green(truncate(&p.desc, desc_width))
        );
    }
    let _ = writeln!(w, "{}", separator(width));
    let _ = writeln!(w, "{} {} {}", yellow("[!]"), white("Total:"), cyan(pkgs.len()));
}

// is_root: o vinstall está rodando como root?
fn is_root() -> bool {
    unsafe { libc::geteuid() == 0 }
}

// reexec_with_sudo roda o próprio vinstall de novo, via sudo, com a opção.
fn reexec_with_sudo(opt: &str) {
    println!("{} {}", yellow("[vinstall]"), white("A limpeza do cache requer privilégios de root."));
    flush();
    let argv0 = std::env::args().next().unwrap_or_else(|| "vinstall".into());
    let _ = Command::new("sudo").arg(argv0).arg(opt).status();
}

// cache_files lista os arquivos .xbps e .sig2 de /var/cache/xbps.
fn cache_files(cache_path: &Path) -> Option<Vec<(String, u64)>> {
    let entries = fs::read_dir(cache_path).ok()?;
    let mut files = Vec::new();
    for e in entries.flatten() {
        if e.file_type().map(|t| t.is_dir()).unwrap_or(true) {
            continue;
        }
        let name = e.file_name().to_string_lossy().into_owned();
        if !name.ends_with(".xbps") && !name.ends_with(".sig2") {
            continue;
        }
        let Ok(md) = e.metadata() else { continue };
        files.push((name, md.len()));
    }
    Some(files)
}

// -Scc: esvazia o cache e remove os órfãos, sem perguntar.
fn clean_xbps_cache() {
    let cache_path = Path::new("/var/cache/xbps");
    if !is_root() {
        reexec_with_sudo("-Scc");
        return;
    }
    let Some(files) = cache_files(cache_path) else { return };

    let installed = get_installed_packages(); // pkgver
    let installed_by_name = get_installed_pkgver_by_name(); // nome -> pkgver

    let (mut pkg_count, mut not_installed, mut old_count) = (0, 0, 0);
    let mut total: i64 = 0;
    println!("{} {}", cyan("[vinstall]"), white("Iniciando limpeza do cache..."));

    for (name, size) in files {
        let is_pkg = name.ends_with(".xbps");
        if fs::remove_file(cache_path.join(&name)).is_err() {
            continue;
        }
        total += size as i64;
        if !is_pkg {
            continue;
        }
        pkg_count += 1;
        // Classifica pelo NOME do pacote, não pelo pkgver exato: um pacote
        // instalado em outra versão é "versão antiga", não "não instalado".
        let pkgver = extract_pkgver_from_cache_file(&name);
        if pkgver.is_empty() || installed.contains(pkgver) {
            continue;
        }
        if installed_by_name.contains_key(clean_version(pkgver)) {
            old_count += 1;
        } else {
            not_installed += 1;
        }
    }

    // Órfãos removidos ANTES do resumo; a contagem vem da diferença real
    // entre os instalados antes e depois do xbps-remove. -y: o -Scc promete
    // limpeza sem perguntar.
    let mut orphan_count = 0;
    if !capture("xbps-query", ["-O"]).stdout.trim().is_empty() {
        let before = get_installed_pkgver_by_name();
        run_binary("xbps-remove", &["-o".into(), "-y".into()], &[]);
        orphan_count = removed_packages(&before, &get_installed_pkgver_by_name()).len();
    }

    println!("\n{} {}", green("[✔]"), white("Limpeza concluída!"));
    println!("{} {} {}", yellow("[!]"), white("Removidos:"), cyan(pkg_count));
    println!("{} {} {}", yellow("[!]"), white("Não instalados removidos:"), cyan(not_installed));
    println!("{} {} {}", yellow("[!]"), white("Versões antigas removidas:"), cyan(old_count));
    println!("{} {} {}", yellow("[!]"), white("Órfãos removidos:"), cyan(orphan_count));
    println!("{} {} {}", yellow("[!]"), white("Espaço liberado:"), green(format_bytes(total)));
}

// -Sc: limpeza leve do cache, no estilo do "pacman -Sc": mantém os
// arquivos da versão instalada de cada pacote e remove o resto (não
// instalados e versões antigas). Não mexe em órfãos.
fn clean_xbps_cache_light() {
    let cache_path = Path::new("/var/cache/xbps");
    if !is_root() {
        reexec_with_sudo("-Sc");
        return;
    }
    let Some(files) = cache_files(cache_path) else { return };

    let installed = get_installed_packages();
    let installed_by_name = get_installed_pkgver_by_name();

    let (mut pkg_count, mut not_installed, mut old_count, mut kept) = (0, 0, 0, 0);
    let mut total: i64 = 0;
    println!("{} {}", cyan("[vinstall]"), white("Iniciando limpeza leve do cache..."));

    for (name, size) in files {
        let is_pkg = name.ends_with(".xbps");
        // A assinatura (.xbps.sig2) segue o mesmo destino do pacote.
        let pkgver = extract_pkgver_from_cache_file(name.strip_suffix(".sig2").unwrap_or(&name)).to_string();
        if pkgver.is_empty() || installed.contains(&pkgver) {
            if is_pkg && !pkgver.is_empty() {
                kept += 1;
            }
            continue;
        }
        if fs::remove_file(cache_path.join(&name)).is_err() {
            continue;
        }
        total += size as i64;
        if is_pkg {
            pkg_count += 1;
            if installed_by_name.contains_key(clean_version(&pkgver)) {
                old_count += 1;
            } else {
                not_installed += 1;
            }
        }
    }

    println!("\n{} {}", green("[✔]"), white("Limpeza concluída!"));
    println!("{} {} {}", yellow("[!]"), white("Removidos:"), cyan(pkg_count));
    println!("{} {} {}", yellow("[!]"), white("Não instalados removidos:"), cyan(not_installed));
    println!("{} {} {}", yellow("[!]"), white("Versões antigas removidas:"), cyan(old_count));
    println!("{} {} {}", yellow("[!]"), white("Mantidos (instalados):"), cyan(kept));
    println!("{} {} {}", yellow("[!]"), white("Espaço liberado:"), green(format_bytes(total)));
}

// package_services descobre os serviços runit que um pacote instalou, pelos
// arquivos dele em /etc/sv/<serviço>/ (xbps-query -f). Acha serviços com
// nome diferente do pacote (openssh -> sshd). Inclui /etc/sv/<pacote>.
fn package_services(pkg_name: &str) -> Vec<String> {
    let mut services: Vec<String> = Vec::new();
    for line in capture("xbps-query", ["-f", pkg_name]).stdout.lines() {
        let mut path = line.trim();
        if let Some(i) = path.find(" -> ") {
            path = &path[..i]; // link simbólico: "caminho -> alvo"
        }
        if let Some(rest) = path.strip_prefix("/etc/sv/") {
            let svc = rest.split('/').next().unwrap_or("");
            if !svc.is_empty() && !services.iter().any(|s| s == svc) {
                services.push(svc.into());
            }
        }
    }
    if Path::new("/etc/sv").join(pkg_name).is_dir() && !services.iter().any(|s| s == pkg_name) {
        services.push(pkg_name.into());
    }
    services.sort();
    services
}

// check_and_enable_service oferece ativar (link em /var/service) cada
// serviço runit instalado pelo pacote que ainda não esteja ativo.
fn check_and_enable_service(pkg_name: &str) {
    for svc in package_services(pkg_name) {
        let service_path = Path::new("/etc/sv").join(&svc);
        let target_path = Path::new("/var/service").join(&svc);
        if !service_path.is_dir() {
            continue;
        }
        match fs::symlink_metadata(&target_path) {
            Err(e) if e.kind() == io::ErrorKind::NotFound => {}
            _ => continue, // já ativo
        }
        let sp = service_path.display();
        if !stdin_is_terminal() {
            // sem terminal não pergunta: só avisa que o serviço existe
            println!(
                "{} {} '{}' ({} {}), {}",
                yellow("[!]"),
                white("Serviço disponível:"),
                cyan(&svc),
                white("pacote"),
                cyan(pkg_name),
                white(format!("não ativado. Para ativar: sudo ln -s {sp} /var/service/"))
            );
            continue;
        }

        print!(
            "\n{} {} '{}' ({} {}). Ativar? [s/N]: ",
            yellow("[!]"),
            white("Serviço disponível:"),
            cyan(&svc),
            white("pacote"),
            cyan(pkg_name)
        );
        let answer = read_line().trim().to_lowercase();
        if answer != "s" && answer != "sim" {
            continue;
        }

        let mut cmd = if is_root() { Command::new("ln") } else { Command::new("sudo") };
        if !is_root() {
            cmd.arg("ln");
        }
        let status = cmd.arg("-s").arg(&service_path).arg(&target_path).stdout(Stdio::null()).status();
        match status {
            Ok(st) if st.success() => println!("{} {} '{}' {}", green("[✔]"), white("Serviço"), cyan(&svc), white("ativado.")),
            Ok(st) => println!("{} {} '{}': {}", red("[!]"), white("falha ao ativar o serviço"), cyan(&svc), status_desc(st)),
            Err(e) => println!("{} {} '{}': {e}", red("[!]"), white("falha ao ativar o serviço"), cyan(&svc)),
        }
    }
}

// show_history mostra o histórico de transações do xbps, gravado pelo
// socklog em /var/log/socklog/xbps/current. Com termos ("vinstall
// --history firefox"), mostra só as linhas que contêm algum deles.
fn show_history(filters: &[String]) {
    let log_path = "/var/log/socklog/xbps/current";
    let file = match fs::File::open(log_path) {
        Ok(f) => f,
        Err(e) if e.kind() == io::ErrorKind::PermissionDenied => {
            // o log só é legível por root e pelo grupo socklog
            let argv0 = std::env::args().next().unwrap_or_else(|| "vinstall".into());
            run_binary(&argv0, &["--history".into()], filters);
            return;
        }
        Err(_) => {
            println!("{} {} {}", red("[!]"), white("Histórico indisponível:"), cyan(log_path));
            println!("{} {}", yellow("[!]"), white("O histórico vem do socklog. Para ativar:"));
            println!("    {}", cyan("sudo xbps-install -S socklog-void"));
            println!("    {}", cyan("sudo ln -s /etc/sv/socklog-unix /etc/sv/nanoklogd /var/service/"));
            println!("{} {}", yellow("[!]"), white("Só as transações feitas depois disso entram no histórico."));
            println!("{} {}", yellow("[!]"), white("Para ler sem sudo: sudo usermod -aG socklog $USER (e entrar de novo)."));
            return;
        }
    };

    let lowered: Vec<String> = filters.iter().map(|f| f.to_lowercase()).collect();
    let matches = |line: &str| {
        if lowered.is_empty() {
            return true;
        }
        let l = line.to_lowercase();
        lowered.iter().any(|f| l.contains(f.as_str()))
    };

    let title =
        if filters.is_empty() { "Histórico:".to_string() } else { format!("Histórico de {}:", filters.join(", ")) };
    println!("\n{} {}", cyan("[vinstall]"), white(title));
    let width = terminal_width();
    println!("{}", separator(width));
    let mut count = 0;
    let mut reader = BufReader::new(file);
    let mut raw = Vec::new();
    let mut w = BufWriter::new(io::stdout().lock());
    loop {
        raw.clear();
        match reader.read_until(b'\n', &mut raw) {
            Ok(0) | Err(_) => break,
            Ok(_) => {}
        }
        let full = String::from_utf8_lossy(&raw);
        let full = full.trim_end_matches(['\n', '\r']);
        // tira o carimbo de tempo do socklog (25 primeiros bytes)
        let line = if full.len() > 25 { full.get(25..).unwrap_or(full) } else { full };
        if !matches(line) {
            continue;
        }
        count += 1;
        let colored = if line.contains("installed") {
            green(line)
        } else if line.contains("removed") {
            red(line)
        } else {
            white(line)
        };
        let _ = writeln!(w, "{colored}");
    }
    let _ = writeln!(w, "{}", separator(width));
    if !filters.is_empty() && count == 0 {
        let _ = writeln!(w, "{} {}", yellow("[!]"), white("Nenhuma transação encontrada para esse filtro."));
    }
}

// search_in_local_plist procura nos arquivos /var/db/xbps/.<pkg>-files.plist
// dos pacotes instalados e devolve um FileRow (nome do pacote + caminho)
// para cada caminho que casa com o termo.
fn search_in_local_plist(q: &FileQuery) -> Vec<FileRow> {
    let db_path = Path::new("/var/db/xbps");
    let Ok(entries) = fs::read_dir(db_path) else { return Vec::new() };
    let mut rows = Vec::new();
    for e in entries.flatten() {
        let fname = e.file_name().to_string_lossy().into_owned();
        let Some(stem) = fname.strip_suffix("-files.plist") else { continue };
        let Ok(content) = fs::read(e.path()) else { continue };
        let content = String::from_utf8_lossy(&content);
        let pkg_name = stem.strip_prefix('.').unwrap_or(stem);
        for line in content.split('\n') {
            let line = line.trim();
            let Some(path) = line.strip_prefix("<string>").and_then(|l| l.strip_suffix("</string>")) else { continue };
            if path.starts_with('/') && q.is_match(path) {
                rows.push(FileRow { pkg: pkg_name.into(), path: path.into(), installed: true });
            }
        }
    }
    rows
}

// ensure_xlocate_index atualiza um índice do xlocate (xlocate -S) quando ele
// não existe ou tem mais de 24h (o xlocate -S grava FETCH_HEAD a cada
// sync). Tenta as origens na ordem; se todas falharem, a busca segue com a
// cópia local que existir.
fn ensure_xlocate_index(x_path: &Path, idx: &XlocateIndex) {
    if younger_than(&idx.git.join("FETCH_HEAD"), Duration::from_secs(24 * 3600)) {
        return;
    }
    let default = vec![String::new()]; // origem padrão do xlocate
    let repos = if idx.repos.is_empty() { &default } else { &idx.repos };
    for (i, repo) in repos.iter().enumerate() {
        let label = if repo.is_empty() { idx.name.clone() } else { format!("{}: {repo}", idx.name) };
        println!("{} {} {}", cyan(">>>"), cyan("xlocate -S"), cyan(format!("({label})")));
        flush();
        let mut cmd = idx.command(x_path, repo);
        cmd.arg("-S");
        match run_timeout(&mut cmd, REMOTE_TIMEOUT) {
            Ok(()) => return,
            Err(e) => {
                println!("{} {} ({label}): {e}", yellow("[!]"), white("falha ao atualizar o índice do xlocate"));
                if i < repos.len() - 1 {
                    println!("{} {}", yellow("[!]"), white("Tentando a próxima origem..."));
                }
            }
        }
    }
}

// --- AJUDA ---

// USAGE_COL_W é a largura da coluna das opções no --help.
const USAGE_COL_W: usize = 28;

// usage_opt imprime "  opção   descrição" alinhado. O preenchimento é
// calculado no texto puro, antes de colorir. Opção maior que a coluna
// desce a descrição para a linha de baixo.
fn usage_opt(opt: &str, desc: &str) {
    let n = opt.chars().count();
    if n >= USAGE_COL_W {
        println!("  {}\n  {}{}", green(opt), " ".repeat(USAGE_COL_W), white(desc));
        return;
    }
    println!("  {}{}{}", green(opt), " ".repeat(USAGE_COL_W - n), white(desc));
}

// usage_more continua a descrição da linha anterior.
fn usage_more(desc: &str) {
    println!("  {}{}", " ".repeat(USAGE_COL_W), white(desc));
}

fn usage_section(title: &str) {
    println!("\n{}", yellow(title));
}

fn print_usage() {
    println!(
        "{} {} - {}",
        white("vinstall"),
        cyan(format!("v{VERSION}")),
        white("wrapper para xbps-install, xbps-query e xbps-remove")
    );
    println!("{}\n", cyan(COPYRIGHT_TEAM));
    println!("{} vinstall [opções] [pacote...]", yellow("Uso:"));
    println!("     {}", white("==> O vinstall aceita todas as flags nativas do xbps-install"));
    println!("     {}", white("Opções que o vinstall não conhece são repassadas ao xbps-install."));

    usage_section("Instalar e atualizar:");
    usage_opt("<pacote...>", "Instala; se o nome não existir, sugere pacotes");
    usage_opt("-Syu", "Sincroniza e atualiza o sistema");
    usage_opt("-f <pacote>", "Força reinstalação ou downgrade, sem pedir confirmação");
    usage_opt("-y", "Responde sim a tudo");
    usage_opt("-n", "Simula: mostra o que seria feito, sem instalar ou remover");
    usage_opt("--parallel-downloads <n>", "Downloads simultâneos (1-16); aceita também =<n>");

    usage_section("Sincronizar:");
    usage_opt("-Sy", "Sincroniza índices e lista as atualizações");
    usage_opt("-Syy", "Igual ao -Sy, forçando o resync");
    usage_opt("-Sf", "Igual ao -Syy, listando também downgrades");
    usage_more("(downgrade: vinstall -f pacote-versao_revisao)");

    usage_section("Buscar:");
    usage_opt("-Ss <termo>", "Nos repositórios (rápido)");
    usage_opt("-Sss <termo>", "Nos repositórios, detalhado (texto completo)");
    usage_more("(aceitam regex: '^fire', 'fire.*fox', 'qt[56]-base', 'joystick|gamepad')");
    usage_more("(-Ss/-Sss/-Qs aceitam vários termos: mostram o que casar com qualquer um)");
    usage_opt("-Ssi <termo>", "Nos repositórios, só os instalados");
    usage_opt("-Ssu <termo>", "Nos repositórios, só os NÃO instalados");
    usage_opt("-Qs <termo>", "Nos pacotes instalados");
    usage_opt("-F <arquivo...>", "Qual pacote instalado tem o arquivo (só no nome)");
    usage_opt("-FR <arquivo...>", "Qual pacote dos repositórios tem o arquivo (só no nome)");
    usage_opt("-Fa <termo>", "Igual ao -F, termo em qualquer parte do caminho");
    usage_opt("-FRa <termo>", "Igual ao -FR, termo em qualquer parte do caminho");
    usage_opt("-Fx <regex>", "Igual ao -F, com expressão regular no caminho");
    usage_opt("-FRx <regex>", "Igual ao -FR, com expressão regular no caminho");
    usage_more("(aceitam vários termos; saem com 1 se algum não for encontrado)");
    usage_opt("-Si <pacote...>", "Informações do pacote no repositório");
    usage_opt("-Qi <pacote...>", "Informações do pacote instalado");
    usage_opt("-Qf <pacote>", "Lista os arquivos de um pacote instalado");
    usage_opt("-QRf <pacote>", "Lista os arquivos de um pacote do repositório");
    usage_opt("-Q<opção> [pacote]", "Repassa ao xbps-query (ex.: -Qx pacote = -x)");

    usage_section("Listar:");
    usage_opt("-Li", "Pacotes instalados");
    usage_opt("-Lo", "Pacotes órfãos");

    usage_section("Remover:");
    usage_opt("-X <pacote>", "Remove o pacote");
    usage_opt("-XR <pacote>", "Remove o pacote e as dependências órfãs");
    usage_opt("-X<flags> <pacote>", "Flags após -X vão ao xbps-remove (-Xy = -y)");

    usage_section("Manutenção:");
    usage_opt("-Sc", "Limpa do cache só não instalados e versões antigas");
    usage_opt("-Scc", "Esvazia o cache e remove os órfãos, sem perguntar");
    usage_opt("--history [termo...]", "Histórico de transações (só as linhas com o termo)");

    usage_section("Outras:");
    usage_opt("-h, --help", "Esta ajuda");
    usage_opt("-v, --version", "Versão do vinstall");

    usage_section("Variáveis de ambiente:");
    usage_opt("XBPS_PARALLEL_DOWNLOADS=<n>", "Igual a --parallel-downloads <n>");
    usage_more("Sem ela nem a opção, vale o /etc/xbps.d/xbps.conf");

    usage_section("Exemplos:");
    usage_opt("vinstall telegram", "Instala o telegram");
    usage_opt("vinstall -Syu", "Atualiza o sistema");
    usage_opt("vinstall -XR telegram", "Remove o telegram e as dependências");
    usage_opt("vinstall -F ifconfig", "Descobre o pacote do ifconfig");
    println!();
}

/*
   CHANGELOG

   [1.6.45] - 2026-11-06
   Added:
     - Porte do vinstall-v1.6.45.go para Rust, com as mesmas opções,
       mensagens e códigos de saída. Dependências: zstd + tar (repodata),
       plist (index.plist), regex, ureq (descoberta dos índices xlocate)
       e libc (isatty/ioctl/geteuid).
     - SIGPIPE volta ao padrão do Unix: "vinstall -Li | head" termina em
       silêncio, como na versão Go.
*/
