# Enceladus — repo guidance

Stack: Go API (`go/`), Next.js static web app (`web/`), R report scripts + shared config (`src/`), EC2/CloudFormation infra (`infra/`, `deploy/`). The Go API replaces the legacy Python `src/main.py`; only `src/config.yml` and `src/rscripts/*.R/.Rmd` remain live in `src/`.

## Commands

- Go API — run exactly what CI (`go/` job) runs:
  `cd go && gofmt -l . && go vet ./... && go test ./...`
  Go tooling comes from the Nix dev shell; `go` may not be on `PATH` otherwise.
- Web lint/typecheck/export — `cd web && npm run lint && npm run typecheck && npm run build`.
  `npm run build` invokes `next build --webpack` (non-default flag; keep it).
- E2E — `npm run build && npx playwright test` (alias `npm run test:e2e`). The Playwright webServer serves the static export dir `web/out/` via `python3 -m http.server`; if the build is stale, tests silently exercise old code.
- Full R + rmarkdown + LaTeX toolchain only inside `nix develop`; build the api app with `nix build .#api`.
- R report input validation (the CI `nix` job):
  `nix develop . --command bash -c 'cd src && Rscript ../tests/test_report_years.R && Rscript ../tests/test_datasus_cache.R'`
- Before first use of the reports run `nix run .#population` once to write `data/population.csv` and `data/datasus-max-year.txt` (needs IBGE SIDRA access).
- The API needs Redis on `localhost:6379` by default (`REDIS_HOST`/`REDIS_PORT`/`REDIS_DB`); the Nix dev shell bundles it on Linux only.

## Hard-earned context

- `src/config.yml` is the single source of truth for states, CID-10 codes, available years, and report definitions; the Go API loads it from `ENCELADUS_SOURCE_ROOT`. Report fields are cross-boundary — keep Go and web in sync:
  - `colunas_mensais: true` → `validateSubmission` in `go/internal/api/server.go` rejects ranges spanning more than one calendar year, and the web clamps its date inputs to a single year.
  - `campos_data` tokens drive web input granularity via `reportDateGranularity()` in `web/lib/api.ts` (`dd` → day, `MM` → month, else single year). `casos` uses `['yyyy']` and is sent as `ano_inicio`/`ano_fim`; other reports use `data_inicio`/`data_fim` (`YYYY-MM-DD`). Go mirrors this with `config.Relatorio.UsesYears()`.
- Do not weaken server-side validation: `validateSubmission` rejects unknown states (config codes or `TODOS`), malformed dates/years, years outside the available range, reversed ranges, and >1-year ranges for monthly-column reports. User input eventually becomes file paths and R argv; state/date validation is what closes the path-injection findings.
- Playwright: filling a controlled React input with the value it already shows does NOT fire `onChange` (React value-tracker dedup). In e2e tests, fill distinct values when the assertion depends on state changes.
- `web/AGENTS.md` holds an auto-maintained Next.js 16 notice: this Next.js differs from training data, so read `node_modules/next/dist/docs/` before writing web code. The block is re-added by `next dev`; committing it keeps the tree clean.
- Report PDFs render through R/rmarkdown/LaTeX. If rmarkdown's own `pdflatex` attempt fails in the dev shell on a missing `.sty` (e.g. `multirow`), the emitted `.tex` can still be compiled directly with `pdflatex` to validate layout changes (e.g. pagination/longtable).
- The web is a static export deployed to GitHub Pages (`output: "export"`; `basePath` auto-derives from `GITHUB_REPOSITORY`); the API is published to a single EC2 instance via GitHub OIDC + Systems Manager (see `deploy/README.md`).
- UI copy and API error messages are Brazilian Portuguese; commit messages follow conventional commits (e.g. `fix(report): …`, `ci(deploy): …`). PRs target `main` and CI runs on PRs.

## Codex delegation

When Codex subagent/model-routing capabilities are available:

- keep the current high-reasoning model as the parent;
- delegate MECHANICAL tasks to a cheaper/faster model;
- delegate SCOPED tasks to a cheaper model unless complexity warrants escalation;
- keep ARCHITECTURAL tasks in the parent;
- spawn children with fresh/minimal context where possible;
- never copy the complete parent history into an executor;
- dispatch only the task envelope;
- review child changes before accepting them.

Prefer one-level delegation:

orchestrator
└── executor

Executors should not recursively create additional implementation agents unless
the orchestrator explicitly requests it.

## References

- `README.md` — local run, IBGE population, production topology.
- `docs/go-migration.md`, `docs/data-processing-pipeline.md` — deeper architecture.
- `deploy/README.md` — EC2/GitHub Pages provisioning and releases.