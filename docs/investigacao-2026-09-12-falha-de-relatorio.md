# Investigação: falha do relatório e2295dd6 (12/09/2026)

Estado da investigação do relatório **DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL**
que falhou na madrugada de 12/09/2026, e das falhas subsequentes de geração
descobertas durante a validação. Hospedeiro de produção: instância
`i-02f9549e59466b7ef`, conta `118462784293`, região `eu-west-1`, reload
Nix/systemd (migrado de Docker).

## Linha do tempo (UTC)

| Horário | Evento |
| --- | --- |
| 2026-09-11 23:57 | Deploy Nix via Actions; api reiniciada na closure `bba04ki…-enceladus-api`. `nix-collect-garbage` (deploy.sh) remove as closures recém-criadas; processos seguem vivos por inode. |
| 23:59:54 | Requisição `e2295dd6-ae3c-11f1-985b-06919963ffb9` registrada `queued` — estados CE, BA, ES, GO, RN, RS, PI; 2021-01-01 → 2022-12-30; sem e-mail. |
| 00:00–00:10 | Pipeline de dados R roda com sucesso: 7 estados gravados como `.rds` no cache, ZIPs `sim-2021/2022.zip`; knit completo. |
| 00:10:41 | R encerra com status 1: **pandoc aborta** — `The 'HOME' environment variable must be set before running Pandoc.` → job marcado `failed` na UI. |
| 00:30 | Durante correção (restart da unit), a api **não sobe**: status 127. As closures `current-api`/`current-population` tinham sido apagadas pelo GC do deploy; eram `Missing` no store. |
| 00:34–00:42 | Rebuilds e retestes sequenciais: cada reteste avança mais uma camada do pipeline (ver causas). |
| 00:5x | PR #25 mesclado; deploy oficial via CI (run `34662939752`) verde; hospedeiro saudável. |
| 01:5x | Reteste single-state (RS) — também falha na renderização LaTeX (causa 4, aberta). |

## Evidências coletadas

- Unit `enceladus-api.service`: `EnvironmentFile=/etc/enceladus/runtime.env`;
  `ExecStart=/bin/sh -c 'exec /opt/enceladus/current-api/bin/enceladus-api'`;
  **sem `User=`**, sem `Environment=`.
- `runtime.env` original: sem `HOME`. `deploy/deploy.sh:10` e
  `deploy/bootstrap.sh:9` exportam `HOME` apenas para o próprio script do
  deploy; o heredoc que gera `runtime.env` nunca escreveu `HOME=`.
- Processo vivo (pid `1167498`): `environ` contém apenas `PATH`; `whoami=root`,
  `$HOME` vazio até num shell SSM.
- Deploy: `deploy/deploy.sh:128 nix-collect-garbage --quiet`. Os out-links
  `current-*` criados por `nix build … --out-link` **não são GC roots**; o GC
  apagou api e population. Journal: `/bin/sh: line 1: …/enceladus-api: No such
  file or directory` + `status=127`.
- Journal do reteste com texlive pequeno:
  `! LaTeX Error: File `multirow.sty' not found.` + `Emergency stop.`
- Journal do reteste com texliveFull:
  `! Dimension too large. \LT@max@sel …` em `\end{longtabu}`.

## Causas raiz

### 1. `HOME` ausente derruba pandoc/rmarkdown (corrigida)
`rreport` → R → `with_pandoc_safe_environment` exige `HOME`. O unit roda como
root e o systemd não define `HOME` para root; o envfile não o carregava.
Correção (commits em `main`):
- heredoc de `runtime.env` em `deploy.sh` e `bootstrap.sh` grava `HOME=/root`;
- units de api/population/redis recebem `Environment=HOME=/root`;
- wrappers do flake exportam `HOME=${HOME:-/root}` (vale também para Go).

### 2. `nix-collect-garbage` apaga as releases vivas (corrigida)
Todo deploy deletava as closures recém-construídas; symlinks `current-*`
ficavam quebrados e qualquer restart rebootava para `127` (a unit `population`,
que usaria a closure no timer mensal de 01/10, morreria também).
Correção: `pin_current_release()` em `deploy.sh` cria raízes de GC
`/nix/var/nix/gcroots/enceladus/{api,population,redis}` apontando **diretamente
para o store path** (`readlink -f`, um salto); re-pin no rollback de health
check; `bootstrap.sh` pina `current-redis` na primeira execução.

### 3. `texliveSmall` não tem `multirow.sty` (corrigida)
O image Docker antigo trazia texlive completo; o Nix usava texliveSmall.
Correção: `flake.nix` agora usa `pkgs.texliveFull`
(`texlive-2025-r78234-final-env`). Durante o caminho tentou-se
`texlive.combine` (deprecado em 26.11pre — removido) e
`texliveSmall.withPackages` (nomes de pacotes filtrados pelo base). `texliveFull`
resolveu.

### 4. `longtabu`/kableExtra — `Dimension too large` (ABERTA)
Com tudo acima resolvido, pdflatex morre com
`! Dimension too large`/`\LT@max@sel` em `\end{longtabu}` na tabela de 27
colunas X (meses + total + desvio por estado) gerada por kableExtra. Ocorre
também com **um único estado** (RS), portanto não é escala dos 7 estados.
O Rmd não muda desde `69bb3cf` (2021-12-10); os PDFs do Docker (09–10/09)
renderizaram o mesmo template. Hipóteses:
- drift de versões de R/kableExtra/`tabu`/`longtable` entre a closure Nix e o
  image Docker (o `longtabu` é comportamento do kableExtra com
  `repeat_header`);
- dado de entrada mudou (commits de pipeline 2026: `9b1308c`, `a7a84ca`);
- o diretório de relatórios mostra requisições Docker (09–11/09, uid 10001)
  que **também não deixaram PDF**, sugerindo que a regressão pode ser
  pré-existente à migração.

## Correções aplicadas (estado atual do `main`)

- PR #25 (`fix/home-env`), squash `0c2b057`:
  - `deploy/deploy.sh` + `deploy/bootstrap.sh` — HOME no envfile, `Environment=`,
    pin de GC roots;
  - `flake.nix` — wrappers com `HOME`, `latex = pkgs.texliveFull`.
- Deploy via CI `34662939752` concluído com sucesso. Verificado no host:
  `APP_REF=main`, `HOME=/root` em `runtime.env`, raízes de GC resolvendo para
  store paths atuais, api `active`/`healthy` (pid `1177683`, `HOME` presente no
  `environ`).
- Artefatos dos retestes falhos removidos do diretório de relatórios
  (workdirs `a632f2b0…`, `e079b134…`, `a175fbf4…`, `d79f142c…`, CSVs e `.tex`
  órfãos, `e2295dd6…`).

## Pendências

1. **Geração de PDF ainda falha** com qualquer requisição (causa 4). Próximo
   passo natural: ajustar o código do relatório
   (`src/rscripts/densidade_municipal_por_periodo_geral.Rmd` — opções de
   `kableExtra`: `longtable`/`latex_options("repeat_header")`/fonte) ou
   investigar a versão dos pacotes R da closure vs. Docker. Verificar também os
   outros dois templates (`densidade_municipal_por_periodo.Rmd`,
   `casos_mensais_por_municipio_por_estado.Rmd`).
2. `curl /health` só prova que o processo HTTP responde; não cobre
   OpenDataSUS, SIDRA, Redis, renderização R nem SES.
3. Avisos de ambiente no R: `TZ` ausente (`/etc/localtime` não é symlink,
   `timedatectl` reporta `n/a`) — baixa prioridade.
4. A migração para Go (runner, fetchers e API) deve incorporar as mesmas
   garantias de `HOME` e timeouts — ver `docs/go-migration.md`.