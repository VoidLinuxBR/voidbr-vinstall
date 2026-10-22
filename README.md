# voidbr-vinstall

Wrapper para o Void xbps-install, xbps-query e xbps-remove

# vinstall 📦

[![Version](https://img.shields.io/badge/version-1.6.10-cyan.svg)](https://github.com/voidlinuxbr/voidbr-vinstall)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Void Linux](https://img.shields.io/badge/platform-Void_Linux-blueviolet.svg)](https://voidlinux.org)
[![Language](https://img.shields.io/badge/Made%20with-Go-00ADD8.svg?style=flat&logo=go)](https://go.dev/)

**vinstall** junta `xbps-install`, `xbps-query` e `xbps-remove` num único
comando, com atalhos no estilo pacman. Quando o nome do pacote está errado ou
incompleto, ele busca nos repositórios e oferece um menu para escolher.

Faz parte da base do **VoidBR** e é mantido pela comunidade Void Linux Brasil.
Foi feito para usar junto com o
[voidbr-xbps](https://github.com/voidlinuxbr/voidbr-xbps) (o xbps com hooks e
downloads paralelos), mas funciona com o xbps original.

---

## ✨ Funcionalidades

* 🚀 **Repassa as flags:** tudo o que o `xbps-install` aceita funciona igual no
  vinstall; o que ele não conhece vai direto para o xbps.
* 🔍 **Sugere pacotes:** se o nome não existe nos repositórios, busca
  (`xbps-query -Rs`) e mostra um menu numerado para escolher.
* 🗑️ **Remove e busca no mesmo comando:** `-X` para remover, `-Ss`/`-Qs` para
  buscar, `-F` para descobrir qual pacote tem um arquivo.
* 🔄 **Mostra as atualizações:** `-Sy` sincroniza e lista o que vai mudar,
  sem instalar nada.
* 🧹 **Limpeza em um comando:** `-Sc` tira do cache só os pacotes não
  instalados e as versões antigas; `-Scc` esvazia o cache e remove os órfãos,
  sem perguntar. Ambos mostram quanto foi removido.
* ⚙️ **Ativa serviços:** depois de instalar um pacote que tem serviço runit
  (`/etc/sv/<pacote>`), pergunta se você quer ativá-lo.
* ⚡ **Downloads paralelos:** aceita `--parallel-downloads <n>` e a variável
  `XBPS_PARALLEL_DOWNLOADS` (com o voidbr-xbps).
* 🛡️ **sudo só quando precisa:** roda como usuário comum e pede `sudo` apenas
  na hora de alterar o sistema.

---

## 🛠 Instalação

### Via repositório (recomendado)

No VoidBR o vinstall já vem instalado. Em outro Void com o repositório
voidlinuxbr:

```bash
sudo xbps-install -S voidbr-vinstall
```

### Via código-fonte

É preciso ter o Go instalado:

```bash
sudo xbps-install -S go
git clone https://github.com/voidlinuxbr/voidbr-vinstall.git
cd voidbr-vinstall
go build -o vinstall vinstall-v1.6.10.go
sudo install -m755 vinstall /usr/local/bin/vinstall
```

O fonte da versão atual fica na raiz (`vinstall-v<versão>.go`); as versões
anteriores ficam em `src/`.

---

## 🚀 Como usar

```bash
vinstall [opções] [pacote...]
```

### Instalar e atualizar

```bash
vinstall telegram                       # instala (ou sugere, se o nome não existir)
vinstall -Syu                           # atualiza o sistema
vinstall -f yasm                        # força reinstalação ou downgrade
vinstall -y telegram                    # instala sem perguntar
vinstall --parallel-downloads 8 -Syu    # 8 downloads simultâneos
```

### Sincronizar

```bash
vinstall -Sy      # sincroniza e lista as atualizações disponíveis
vinstall -Syy     # igual, forçando o resync dos índices
vinstall -Sf      # igual ao -Syy, listando também os downgrades
```

### Buscar

```bash
vinstall -Ss termo      # nos repositórios (rápido)
vinstall -Sss termo     # nos repositórios, detalhado
vinstall -Ssi termo     # nos repositórios, só os instalados
vinstall -Ssu termo     # nos repositórios, só os não instalados
vinstall -Qs termo      # nos pacotes instalados
vinstall -Si pacote     # informações do pacote no repositório
vinstall -Qi pacote     # informações do pacote instalado
vinstall -F ifconfig    # qual pacote instalado tem esse arquivo
vinstall -FR ifconfig   # qual pacote dos repositórios tem esse arquivo
vinstall -Qx pacote     # qualquer -Q<opção> vai para o xbps-query (aqui, -x)
```

O `-Ss`/`-Ssi`/`-Ssu` e o `-Sss` aceitam **expressão regular**, detectada
sozinha: um termo com `^ $ | ( ) [ ] { } \ * ?` ou `.+` é tratado como regex
(sem diferenciar maiúsculas de minúsculas no `-Sss`). `+` e `.` sozinhos
contam como texto, para nomes como `gtk+3`, `libstdc++` e `python3.12`.
Use aspas, para o shell não mexer no termo.

```bash
vinstall -Ss '^fire'              # nome começando com "fire"
vinstall -Ss 'fire.*fox'          # "fire", qualquer coisa, "fox"
vinstall -Ss 'qt[56]-base'        # qt5-base ou qt6-base
vinstall -Sss 'joystick|gamepad'  # um ou outro, no texto completo
```

### Buscar arquivos (`-F`)

No estilo do `pacman -F`: diz qual pacote tem um arquivo.

```bash
vinstall -F ifconfig            # instalados: "ifconfig" no NOME do arquivo
vinstall -FR ifconfig           # repositórios: idem
vinstall -Fa ifconfig           # "ifconfig" em qualquer parte do caminho
vinstall -FRa ifconfig
vinstall -Fx 'ifconfig\.[0-9]$' # expressão regular no caminho completo
vinstall -FRx 'ifconfig\.[0-9]$'
```

O caminho inverso — listar os arquivos de um pacote:

```bash
vinstall -Qf pacote             # pacote instalado (xbps-query -f)
vinstall -QRf pacote            # pacote do repositório (xbps-query -Rf)
```

| Modo | Casa |
|---|---|
| `-F` / `-FR` | o termo no **nome** do arquivo, não nos diretórios (com `/` no termo, vale o caminho) |
| `-Fa` / `-FRa` | o termo em **qualquer parte** do caminho |
| `-Fx` / `-FRx` | **expressão regular** no caminho completo |

O resultado sai numa tabela `[✓]/[-]  pacote  caminho`; `[✓]` = instalado.

Aceita vários termos (`vinstall -F ifconfig route`) e sai com código `1`
se algum não for encontrado, como o `pacman -F` — dá para usar em script:

```bash
vinstall -F ifconfig >/dev/null || echo "ifconfig não está instalado"
```

**Como o `-FR` funciona:** usa o [`xlocate`](https://man.voidlinux.org/xlocate.1)
(pacote `xtools`), que busca num índice de arquivos baixado para
`~/.cache`, sem consultar a rede a cada busca. O vinstall:

- descobre os índices a partir dos repositórios do `xbps.d`, procurando
  `<repo>/xlocate/xlocate.git` em cada um (e repositórios locais no disco);
- sempre inclui o índice oficial do Void;
- junta servidores com o mesmo índice (mirrors) num só, um servindo de
  reserva do outro;
- atualiza cada índice sozinho (`xlocate -S`) quando tem mais de 24h.

Sem resultado por arquivo, o `-FR` mostra os pacotes com esse nome
(`xbps-query -Rs`).

### Índice de arquivos do repositório VoidBR (servidor)

O índice do repositório VoidBR é gerado no servidor pelo
[`voidbr-xlocate-index`](usr/local/bin/voidbr-xlocate-index), instalado
junto com o pacote em `/usr/local/bin/`, e fica
**dentro do repositório**, em `<repo>/xlocate/xlocate.git` — assim ele
acompanha o repositório em qualquer mirror.

```bash
sudo install -Dm644 server/voidbr-xlocate-index.cron /etc/cron.d/voidbr-xlocate-index
sudo voidbr-xlocate-index -f    # primeira geração
```

O cron roda a cada hora e só regera quando o repositório mudou.

### Listar

```bash
vinstall -Li      # pacotes instalados
vinstall -Lo      # pacotes órfãos
```

### Remover

```bash
vinstall -X pacote      # remove
vinstall -XR pacote     # remove junto as dependências que ficaram órfãs (mostra a contagem)
vinstall -Xy pacote     # o que vem depois do -X vai para o xbps-remove (aqui, -y)
```

### Manutenção

```bash
vinstall -Sc          # limpa do cache só não instalados e versões antigas
vinstall -Scc         # esvazia o cache e remove os órfãos
vinstall --history    # histórico de transações
```

A lista completa está em `vinstall -h`.

### Downloads paralelos

Com o [voidbr-xbps](https://github.com/voidlinuxbr/voidbr-xbps), o valor
padrão vem do `paralleldownloads=` em `/etc/xbps.d/xbps.conf`. Para trocar
numa execução, qualquer uma das formas funciona:

```bash
vinstall --parallel-downloads 8 -Syu
vinstall --parallel-downloads=8 -Syu
XBPS_PARALLEL_DOWNLOADS=8 vinstall -Syu
```

O `sudo` limpa as variáveis de ambiente, então o vinstall converte a
`XBPS_PARALLEL_DOWNLOADS` em `--parallel-downloads=<n>` antes de chamar o
`xbps-install`.

---

### Autocompletar

O pacote instala o autocompletar para **bash** e **zsh**: as opções do
vinstall e, conforme a opção, os nomes de pacote — instalados depois de
`-X`/`-XR`/`-Qf`, do repositório na instalação e no `-QRf`. Abra um
terminal novo depois de instalar.

---

## 🧰 Outras ferramentas do pacote

O pacote `voidbr-vinstall` também instala, em `/usr/local/bin/`:

| Ferramenta | O que faz |
|---|---|
| `voidbr-vkpurge` | remove com segurança os kernels que não estão mais em uso |
| `voidbr-dmesg` | monitor de logs do sistema |
| `voidbr-vpm` | wrapper para o `xbps-query` |
| `voidbr-xlocate-index` | gera o índice `xlocate` de um repositório (uso no servidor) |

---

## 🤝 Contribuição

Contribuições são muito bem-vindas! Sinta-se à vontade para abrir Issues ou
enviar um Pull Request.

1. Fork o projeto
2. Crie sua Feature Branch (`git checkout -b feature/NovaFeature`)
3. Commit suas mudanças (`git commit -m 'Adiciona nova feature'`)
4. Push para a Branch (`git push origin feature/NovaFeature`)
5. Abra um Pull Request

---

## 📜 Créditos

* Criado por: Vilmar Catafesta <vcatafesta@gmail.com>
* Comunidade: Void Linux Brasil ([https://github.com/voidlinuxbr](https://github.com/voidlinuxbr))
* Distribuição: VoidBR ([https://voidbr.org](https://voidbr.org))
* Distribuição: Chili Linux ([https://chililinux.com](https://chililinux.com))

---

## ⚖️ Disclaimer (Aviso Legal)

ESTE SOFTWARE É FORNECIDO "COMO ESTÁ", SEM ABSOLUTAMENTE NENHUMA GARANTIA DE QUALQUER TIPO, EXPRESSA OU IMPLÍCITA, INCLUINDO, MAS NÃO SE LIMITANDO A, GARANTIAS DE COMERCIALIZAÇÃO OU ADEQUAÇÃO A UM PROPÓSITO ESPECÍFICO. O USO DESTE WRAPPER É DE TOTAL RESPONSABILIDADE DO USUÁRIO. EM NENHUM MOMENTO O AUTOR OU OS CONTRIBUIDORES SERÃO RESPONSÁVEIS POR QUALQUER DANO, PERDA DE DADOS OU FALHAS NO SISTEMA DECORRENTES DO USO DESTE PROGRAMA.

---

Copyright (C) 2019-2026 Vilmar Catafesta
Copyright (c) 2026-2026, VoidBR Development Team <https://voidbr.org>
