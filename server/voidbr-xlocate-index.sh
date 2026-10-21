#!/usr/bin/env bash
# voidbr-xlocate-index — gera o índice xlocate do repositório VoidBR
#
# Mesmo formato do índice oficial (função xupdategit do xlocate/xtools):
#   - um arquivo por pacote, com nome = pkgver (ex.: conectux-1.0_1)
#   - conteúdo = lista ordenada dos caminhos que o pacote instala
#   - um único commit no branch master
#
# Roda no SERVIDOR onde ficam os .xbps: o xbps-query lê os pacotes do disco
# (rápido), em vez de baixar cada um pela rede como o -Ro remoto.
#
# Só regera quando o repositório mudou (x86_64-repodata mais novo que o
# índice). Assim o commit do índice só muda quando há pacote novo, e os
# mirrors ficam com o mesmo commit — o vinstall agrupa os iguais.
#
# Uso: voidbr-xlocate-index [-f|--force] [dir-do-repo] [destino-do-indice.git]
#   -f, --force  regera mesmo sem mudança no repositório

set -euo pipefail

force=0
if [[ "${1:-}" == "-f" || "${1:-}" == "--force" ]]; then
	force=1
	shift
fi

repo_dir="$(realpath -e "${1:-/vg/void-mirror/extra}")"
index_git="${2:-$repo_dir/xlocate/xlocate.git}"
arch="${XBPS_ARCH:-x86_64}"

msg() {
	echo "voidbr-xlocate-index: $(date '+%F %T') $*"
}

die() {
	echo "voidbr-xlocate-index: $(date '+%F %T') ERRO: $*" >&2
	exit 1
}

for cmd in xbps-query git awk sort; do
	command -v "$cmd" >/dev/null || die "'$cmd' não encontrado."
done
[[ -f "$repo_dir/$arch-repodata" ]] || die "$arch-repodata não encontrado em $repo_dir"

if ((!force)) && [[ -f "$index_git/info/refs" && ! "$repo_dir/$arch-repodata" -nt "$index_git/info/refs" ]]; then
	msg "sem mudanças em $repo_dir desde o último índice; nada a fazer (use -f para forçar)"
	exit 0
fi

msg "início — repositório: $repo_dir ($arch)"
msg "destino: $index_git"

work="$(mktemp -d -t voidbr-xlocate.XXXXXX)"
trap 'rm -rf "$work"' EXIT

git init -q -b master "$work/tree"
cd "$work/tree"

msg "listando os arquivos dos pacotes (xbps-query -Ro)..."
# -i: ignora os repositórios do xbps.conf; consulta SÓ o repo VoidBR local.
# Saída do xbps-query -Ro: "pkgver: /caminho (pkgver)". O awk grava cada
# caminho no arquivo do seu pkgver, igual ao xupdategit.
XBPS_ARCH="$arch" xbps-query -i --repository="$repo_dir" -Ro '*' | awk '
	$0 ~ ": " {
		s = index($0, ": ")
		pkg = substr($0, 1, s-1)
		file = substr($0, s+2)
		sub(" *\\([^)]*\\)$", "", file)
		print file >>pkg
		close(pkg)
	}'

shopt -s nullglob
files=(./*)
((${#files[@]})) || die "nenhum pacote encontrado em $repo_dir; índice NÃO publicado."

msg "${#files[@]} pacotes lidos; ordenando as listas..."
for f in "${files[@]}"; do
	LC_ALL=C sort -o "$f" "$f"
done

msg "gravando o commit do índice..."
git add -A
git -c user.name=voidbr-xlocate -c user.email=voidbr-xlocate@none \
	commit -q -m "voidbr-xlocate $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Monta o repositório bare novo ao lado e troca de uma vez, para que
# nenhum cliente baixe um índice pela metade.
msg "montando o repositório bare e publicando..."
git clone -q --bare "$work/tree" "$work/new.git"
git -C "$work/new.git" repack -adq
git -C "$work/new.git" update-server-info # permite clone/fetch por HTTP simples

mkdir -p "${index_git%/*}"
[[ -d "$index_git" ]] && mv "$index_git" "$work/old.git"
mv "$work/new.git" "$index_git"

msg "concluído: ${#files[@]} pacotes indexados em $index_git"
