# voidbr-vinstall

Wrapper para o Void xbps-query e xbps-install

# vinstall 📦

[![Version](https://img.shields.io/badge/version-1.2.4--20260203-cyan.svg)](https://github.com/voidlinuxbr/voidbr-vinstall)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Void Linux](https://img.shields.io/badge/platform-Void_Linux-blueviolet.svg)](https://voidlinux.org)

**vinstall** é um wrapper inteligente e elegante para o gerenciador de pacotes xbps do Void Linux. Desenvolvido para tornar a experiência no terminal mais fluida, ele combina a robustez do xbps-install com uma interface interativa que ajuda você a encontrar o pacote certo quando ocorre um erro de digitação ou o nome é incerto.

Este projeto faz parte do ecossistema Chili Linux e é mantido pela comunidade Void Linux Brasil.

---

## ✨ Funcionalidades

* 🚀 Wrapper Direto: Repassa comandos e flags para o xbps-install de forma transparente.
* 🔍 Sugestões Inteligentes: Se um pacote não for encontrado, o vinstall realiza uma busca automática nos repositórios remotos (xbps-query -Rs).
* 🎨 Interface Moderna: Menu interativo com cores, índices alinhados e separadores que se ajustam automaticamente à largura do seu terminal.
* ✅ Fidelidade Total: Exibe o status do pacote ([*] instalado, [-] disponível) e a versão exata, mantendo a compatibilidade visual do XBPS.
* 🛡️ Privilégio Inteligente: Roda como usuário comum e solicita sudo apenas no momento da execução do comando de escrita.

---

## 🛠 Instalação

### Via Repositório (Recomendado)

Se você já utiliza o repositório voidlinuxbr ou está no Chili Linux, instale diretamente via xbps:
```bash
sudo xbps-install -S voidbr-vinstall
```

### Via Código Fonte (Compilação)

Certifique-se de ter o Go instalado:

```bash
sudo xbps-install -S go
```

1. Clone o repositório:
```bash
git clone https://github.com/voidlinuxbr/voidbr-vinstall.git
cd voidbr-vinstall
```

2. Compile o binário:
```bash
go build -o vinstall vinstall-v1.2.4.go
```

3. Mova para seu PATH:
```bash
sudo mv vinstall /usr/local/bin/
```

---

## 🚀 Como usar

O vinstall aceita as mesmas flags que o xbps-install.

Uso básico:
```bash
vinstall telegram
```

Atualizar o sistema:
```bash
vinstall -Syu
```

Forçar reinstalação:
```bash
vinstall -f yasm
```

Ajuda do vinstall:
```bash
vinstall -h
```

---

## 🤝 Contribuição

Contribuições são muito bem-vindas! Sinta-se à vontade para abrir Issues ou enviar um Pull Request.

1. Fork o projeto
2. Crie sua Feature Branch (git checkout -b feature/NovaFeature)
3. Commit suas mudanças (git commit -m 'Adiciona nova feature')
4. Push para a Branch (git push origin feature/NovaFeature)
5. Abra um Pull Request

---

## 📜 Créditos

* Criado por: Vilmar Catafesta <vcatafesta@gmail.com>
* Comunidade: Void Linux Brasil ([https://github.com/voidlinuxbr](https://github.com/voidlinuxbr))
* Distribuição: Chili Linux ([https://chililinux.com](https://chililinux.com))

---

## ⚖️ Disclaimer (Aviso Legal)

ESTE SOFTWARE É FORNECIDO "COMO ESTÁ", SEM ABSOLUTAMENTE NENHUMA GARANTIA DE QUALQUER TIPO, EXPRESSA OU IMPLÍCITA, INCLUINDO, MAS NÃO SE LIMITANDO A, GARANTIAS DE COMERCIALIZAÇÃO OU ADEQUAÇÃO A UM PROPÓSITO ESPECÍFICO. O USO DESTE WRAPPER É DE TOTAL RESPONSABILIDADE DO USUÁRIO. EM NENHUM MOMENTO O AUTOR OU OS CONTRIBUIDORES SERÃO RESPONSÁVEIS POR QUALQUER DANO, PERDA DE DADOS OU FALHAS NO SISTEMA DECORRENTES DO USO DESTE PROGRAMA.

---

Copyright (C) 2019-2026 Vilmar Catafesta
