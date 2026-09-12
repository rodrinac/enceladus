# Migração da API para Go

Especificação da reescrita da aplicação do Enceladus da pilha Python
(Quart/Hypercorn) para Go. O **R continua sendo o motor de relatórios**; a
migração cobre a API HTTP, o runner de relatórios e os dois fetchers de dados.

## Objetivo e justificativa

- Eliminar a dependência do runtime Python no fechamento da API (closure Nix
  sem `python`), mantendo R/pandoc/LaTeX como runtime de geração.
- Execução de downloads e streaming em Go: memória previsível, timeouts claros
  e concorrência explícita, sem os gargalos do executor em thread do Quart.
- Testabilidade unitária (sem servidor nem Docker).
- Paridade de comportamento observável com a implementação Python atual
  (`src/main.py`, `src/relatorios/*.py`, `src/scripts/fetch_sim_archives.py`,
  `scripts/fetch_population.py`); mudanças internas são livres.

## Estrutura do módulo

Módulo: `github.com/rodrinac/enceladus/go` (raiz em `go/`).

```text
go/
  cmd/
    enceladus-api/        main.go          # wiring settings/aws/redis/runner/api
    fetch-sim-archives/   main.go          # CLI de extração dos ZIPs anuais SIM
    fetch-population/     main.go          # CLI de população IBGE/SIDRA
  internal/
    api/       server.go  server_test.go   # rotas HTTP, CORS, download de PDF
    config/    config.go  config_test.go   # src/config.yml (ordem preservada)
    email/     email.go                    # SES SendRawEmail, MIME multipart
    jobstatus/ jobstatus.go jobstatus_test.go
    processed/ processed.go processed_test.go
    reports/   reports.go                  # 3 tipos + pool de workers
    rreport/   runner.go  runner_test.go   # runner R script
    settings/  settings.go                 # env vars + defaults
    store/     store.go                    # Redis (metadados)
```

## Contrato de paridade (HTTP)

Todas as rotas e semânticas abaixo mapeiam 1:1 a implementação Python.

| Item | Comportamento |
| --- | --- |
| Rotas GET | `/` , `/health`, `/config/anos`, `/config/estados`, `/config/codigoscid10`, `/config/relatorios`, `/relatorios/processados` |
| Rotas POST | `/relatorios/queimaduras/densidade-municipal-por-periodo-geral`, `/relatorios/queimaduras/densidade-municipal-por-periodo`, `/relatorios/queimaduras/casos-mensais-por-municipio-por-estado` (parâmetros em query string) |
| GET PDF | `/relatorios/queimaduras/<tipo>/<path:path>` lê do volume persistente, `Content-Type: application/pdf` (com guarda contra path traversal) |
| CORS | Middleware espelhando `CORS_ORIGINS`; preflight responde `200` |
| Submissão | `POST` lê `estado` (repetível), `data_inicio`, `data_fim`, `email`; gera `id_requisicao` via `uuid.newV1()` (equivalente a `uuid.uuid1()`); registra `queued` em memória; dispara goroutine; responde `202 {"destino": <email|null>, "id_requisicao": <id>}` |
| Estado | Registro só em memória (`jobstatus`): `queued` → `running` → `succeeded` (remove o trabalho; o PDF vira a fonte de verdade) ou `failed` com `mensagem: "Não foi possível gerar este relatório."` |
| Nome do PDF | `<estados>.​<inicio>.​<fim>.pdf`, com estados unidos por `-` (ex.: `CE-BA-ES-GO-RN-RS-PI.2021-01-01.2022-12-30.pdf`) |
| Workdir | Diretório por `id_requisicao`; reuso do PDF se já existir; em falha o diretório é mantido (rastreio), em sucesso `store` (Redis) → remove workdir → SES |
| Timeout R | 900 s padrão; em estouro, rc `124` e mensagem `Rscript excedeu o timeout de 900s.` |
| Intervalo na UI/e-mail | `em {x}` quando início=fim; `entre {a} e {b}` caso contrário |
| Assuntos/atachments | `Relatório de densidade municipal por período - …`; `Diagrama de distribuição do local de falecimento para …` (casos mensais) |
| `TODOS` | Expandido para todas as UFs nos relatórios gerais |
| Ordenação `processados` | Mesma comparação por data (desc) no merge de PDFs + jobs ativos |
| Config | `src/config.yml` preservando a ordem dos campos (parsing via `yaml.Node`) |

## Contrato de variáveis de ambiente

`internal/settings` segue os mesmos defaults de `src/settings.py`:

- `ENCELADUS_HOME` (pasta de trabalho/`.enceladus` local)
- `ENCELADUS_REPORTS_DIR`, `ENCELADUS_DATASUS_CACHE_PATH`, `ENCELADUS_DATASUS_CACHE_MAX_BYTES` (default 5 GiB), `ENCELADUS_DATASUS_MAX_YEAR_PATH`
- `ENCELADUS_POPULATION_DATA_PATH`, `IBGE_POPULATION_PERIOD`
- `REDIS_HOST`, `REDIS_PASSWORD`, `REDIS_SECRET_ARN`
- `SES_SENDER`, `SES_CONFIGURATION_SET`, região SES por `AWS_DEFAULT_REGION` (default `eu-west-1`) — via `envOr` (uso de valores vazios cai no default)
- `CORS_ORIGINS`

## Fetchers (CLI)

### `fetch-sim-archives`

Caminho no R: `src/rscripts/fetch_datasus_cached.R` já foi editado para chamar
`system2(Sys.getenv("ENCELADUS_FETCH_SIM_BIN","enceladus-fetch-sim-archives"), …)`
sem argumento de caminho de script.

- Args: `--year-start/--year-end/--states/--cache-dir/--output`.
- Mapa oficial de arquivos: 2019–2021 e 2023 = `Mortalidade_Geral_<ano>_csv.zip`;
  2022 = ZIP JSON (o CSV não está acessível); 2024 = `DO24OPEN_csv.zip`.
- Download com timeout de 120 s, `--part` + renomeação atômica.
- Leitura streaming (CSV `;` ou array JSON percorrido incrementalmente, sem
  carregar o JSON inteiro); remove BOM; unifica colunas entre anos; valida
  `CODMUNRES`; filtra municípios pelo prefixo das UFs solicitadas.
- Saída por stderr: `Extracted N SIM records for <states>`. Anos fora do mapa
  (>=2019 e <=2024) encerram com erro explícito.

### `fetch-population`

- Busca SIDRA 6579 (com HTTP e regex em `__NEXT_DATA__`), valida shape,
  `MINIMUM_MUNICIPALITIES = 5500`, 3 tentativas com backoff.
- Grava `population.csv` atomicamente; `hasValidCache` preserva CSV anterior
  válido quando o SIDRA falha; sem dados válidos a inicialização falha.
- Descobre o maior ano do SIM no OpenDataSUS (limitado a 2024) e grava
  `datasus-max-year.txt`; em falha mantém o anterior ou
  `DATASUS_MAX_YEAR_FALLBACK` (atualmente 2024).

## Testes

Escritos para `internal/config`, `internal/jobstatus`, `internal/rreport`
(timeout rc 124 com `sh -c`), `internal/processed` (sort) e `internal/api`
(server com Rscript fake). Também para `cmd/fetch-sim-archives`
(fixtures CSV/JSON zip) e `cmd/fetch-population` (normalização/`hasValidCache`).

## Empacotamento e CI

- `flake.nix`: trocar o app `api` Python por `buildGoModule` (com
  `vendorHash`, `subPackages` para as 3 bins), wrapper exportando
  `HOME=${HOME:-/root}` e `ENCELADUS_FETCH_SIM_BIN`, mantendo R/texlive/pandoc
  no runtime; drop do `python` da closure.
- `.github/workflows/ci.yml`: job `go` (lint, `go vet`, `go test ./...`).
- `.github/workflows/deploy-api.yml`: **adicionar `go/**` às paths** para que o
  merge do PR de migração dispare o deploy.

## Estados atuais

- [x] Packages Go (`cmd/*` + `internal/*`) escrevidos; `go build ./...` e
  `go vet ./...` passam (toolchain via `nix shell nixpkgs#go`).
- [x] Testes de `config`, `jobstatus`, `rreport`, `processed`, `api` escritos.
- [x] Rodar `go test ./...` e ajustar falhas.
- [x] Testes de `cmd/fetch-sim-archives` e `cmd/fetch-population`.
- [x] `flake.nix` com `buildGoModule`, `vendorHash`, wrappers; drop do python.
- [x] Job `go` no CI; `go/**` nas paths do workflow de deploy.
- [ ] PR de migração revisado e validado; deploy verde; revalidação do R.

## Riscos e notas

- HOME no runtime já é garantido pela fix de infra (unit `Environment=HOME=/root`
  + `HOME=` em `runtime.env` + wrappers); o runner Go herda do processo.
- `vendorHash` precisa ser descoberto por build (fakeHash → hash real).
- Paridade do rc 124 exige que o runner transmita o sinal do `Rscript`
  (timeout real matando a árvore, não apenas o binário).
- LaTeX ainda depende de `texliveFull` (issue de `multirow.sty` do runtime
  Nix); houve regressão de renderização — ver
  `docs/investigacao-2026-09-12-falha-de-relatorio.md`.