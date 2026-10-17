# voidbr-vinstall

Wrapper para o xbps-install, xbps-query e xbps-remove do Void Linux.

# vinstall 📦

[![Version](https://img.shields.io/badge/version-1.6.9-cyan.svg)](https://github.com/voidlinuxbr/voidbr-vinstall)
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
* 🧹 **Limpeza em um comando:** `-Scc` esvazia o cache e remove os órfãos,
  sem perguntar.
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
go build -o vinstall vinstall-v1.6.9.go
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
vinstall -F ifconfig    # qual pacote instalado tem esse arquivo
vinstall -FR ifconfig   # qual pacote dos repositórios tem esse arquivo
vinstall -Qx pacote     # qualquer -Q<opção> vai para o xbps-query (aqui, -x)
```

### Listar

```bash
vinstall -Li      # pacotes instalados
vinstall -Lo      # pacotes órfãos
```

### Remover

```bash
vinstall -X pacote      # remove
vinstall -XR pacote     # remove junto as dependências que ficaram órfãs
vinstall -Xy pacote     # o que vem depois do -X vai para o xbps-remove (aqui, -y)
```

### Manutenção

```bash
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

## 🧰 Outras ferramentas do pacote

O pacote `voidbr-vinstall` também instala, em `/usr/local/bin/`:

| Ferramenta | O que faz |
|---|---|
| `voidbr-vkpurge` | remove com segurança os kernels que não estão mais em uso |
| `voidbr-dmesg` | monitor de logs do sistema |
| `voidbr-vpm` | wrapper para o `xbps-query` |

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
