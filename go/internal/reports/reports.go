// Package reports mirrors the src/relatorios/* modules: it builds each report
// by invoking the R scripts, persists the PDF in the report store (S3 bucket
// when configured, local filesystem otherwise) and records processing dates.
// Repeat submissions for an already stored report reuse the file instead of
// re-running R.
package reports

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rodrinac/enceladus/go/internal/config"
	"github.com/rodrinac/enceladus/go/internal/reportstore"
	"github.com/rodrinac/enceladus/go/internal/rreport"
	"github.com/rodrinac/enceladus/go/internal/store"
)

type Request struct {
	States    []string
	Start     string
	End       string
	RequestID string
}

type Worker struct {
	Cfg        *config.Config
	ReportsDir string
	Store      store.DataStore
	Reports    reportstore.Storage
	Runner     *rreport.Runner
}

func NewWorker(cfg *config.Config, reportsDir string, dataStore store.DataStore,
	storage reportstore.Storage, runner *rreport.Runner) *Worker {
	return &Worker{Cfg: cfg, ReportsDir: reportsDir, Store: dataStore, Reports: storage, Runner: runner}
}

func formatIntervalo(inicio, fim string) string {
	if inicio == fim {
		return "em " + inicio
	}
	return "entre " + inicio + " e " + fim
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// reportHome returns the report subfolder for the given config-declared path.
func reportHome(reportsDir, reportPath string) string {
	subdir := strings.TrimPrefix(strings.TrimPrefix(reportPath, "/"), "relatorios/")
	return filepath.Join(reportsDir, subdir)
}

// storageKey returns the persistence key plus the public download URI.
func storageKey(reportPath, fileName string) (string, string) {
	return reportstore.KeyForReport(reportPath, fileName), reportstore.PublicURI(reportPath, fileName)
}

func (w *Worker) expandTodos(states []string) []string {
	if contains(states, "TODOS") {
		return w.Cfg.AllStateCodes()
	}
	return states
}

// Exists reports whether the finished PDF is already persisted.
func (w *Worker) Exists(ctx context.Context, reportPath, fileName string) bool {
	key, _ := storageKey(reportPath, fileName)
	exists, err := w.Reports.Exists(ctx, key)
	if err != nil {
		slog.Warn("falha ao verificar relatório existente", "chave", key, "error", err)
		return false
	}
	return exists
}

// safeFileName mirrors the deterministic names built from validated states
// and dates (e.g. AC-CE.2022-01-01.2022-12-31.pdf or AC.2021.2021.pdf). It is
// the second layer (behind validateSubmission) ensuring the local R working
// files only ever use expected names.
var safeFileName = regexp.MustCompile(`^[A-Z]{2}(-[A-Z]{2})*\.\d{4}(-\d{2}-\d{2})?\.\d{4}(-\d{2}-\d{2})?\.pdf$`)

// checkLocalPath rejects unexpected file names and paths escaping ReportsDir
// before the R working files are touched.
func (w *Worker) checkLocalPath(filePath string) error {
	if !safeFileName.MatchString(filepath.Base(filePath)) {
		return fmt.Errorf("nome de relatório inválido")
	}
	rel, err := filepath.Rel(w.ReportsDir, filePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("nome de relatório inválido")
	}
	return nil
}

// generate runs the R script unless the PDF is already stored. It returns
// (generated, reused, exitCode, output): reused is true when the file already
// existed and R was skipped.
func (w *Worker) generate(ctx context.Context, key, filePath, workingPath, script string, args []string) (bool, bool, int, string, error) {
	if exists, err := w.Reports.Exists(ctx, key); err == nil && exists {
		return false, true, 0, "", nil
	}
	if err := w.checkLocalPath(filePath); err != nil {
		return false, false, 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil { // lgtm[go/path-injection] filePath is allowlisted by checkLocalPath and contained in ReportsDir
		return false, false, 0, "", err
	}
	if err := os.MkdirAll(workingPath, 0o755); err != nil {
		return false, false, 0, "", err
	}
	code, output := w.Runner.Run(script, args)
	slog.Debug("Retorno da execução R", "script", script, "output", output)
	slog.Info("Executou comando R", "script", script, "status", code)
	if code != 0 {
		return true, false, code, output, nil
	}
	data, err := os.ReadFile(filePath) // lgtm[go/path-injection] filePath is allowlisted by checkLocalPath and contained in ReportsDir
	if err != nil {
		return true, false, code, output, err
	}
	if err := w.Reports.Put(ctx, key, data, "application/pdf"); err != nil {
		return true, false, code, output, err
	}
	return true, false, code, output, nil
}

// DensidadeGeral mirrors relatorios.densidade_municipal_por_periodo_geral.
func (w *Worker) DensidadeGeral(req Request) bool {
	const idRelatorio = "DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL"
	const reportPath = "/relatorios/queimaduras/densidade-municipal-por-periodo-geral"
	states := w.expandTodos(req.States)

	slog.Info("Obtendo registros de queimaduras",
		"estados", fmt.Sprint(states),
		"intervalo", formatIntervalo(req.Start, req.End))

	folder := reportHome(w.ReportsDir, reportPath)
	fileName := strings.Join(states, "-") + "." + req.Start + "." + req.End + ".pdf"
	key, _ := storageKey(reportPath, fileName)
	filePath := filepath.Join(folder, fileName)
	workingPath := filepath.Join(folder, req.RequestID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	generated, reused, code, output, err := w.generate(ctx, key, filePath, workingPath,
		"densidade_municipal_por_periodo_geral.R",
		[]string{strings.Join(states, ","), req.Start, req.End, filePath, workingPath})
	if err != nil {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "error", err)
		return false
	}
	if generated && code != 0 {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "saida", output)
		return false
	}
	if reused {
		slog.Info("Relatório reutilizado do armazenamento", "relatorio", idRelatorio, "chave", key)
	}

	return w.deliver(ctx, idRelatorio, fileName, workingPath, req)
}

// Densidade mirrors relatorios.densidade_municipal_por_periodo (single state).
func (w *Worker) Densidade(req Request) bool {
	const idRelatorio = "DENSIDADE_MUNICIPAL_POR_PERIODO"
	const reportPath = "/relatorios/queimaduras/densidade-municipal-por-periodo"
	estado := ""
	if len(req.States) > 0 {
		estado = req.States[0]
	}

	slog.Info("Obtendo registros de queimaduras",
		"estado", estado,
		"intervalo", formatIntervalo(req.Start, req.End))

	folder := reportHome(w.ReportsDir, reportPath)
	fileName := estado + "." + req.Start + "." + req.End + ".pdf"
	key, _ := storageKey(reportPath, fileName)
	filePath := filepath.Join(folder, fileName)
	workingPath := filepath.Join(folder, req.RequestID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	generated, reused, code, output, err := w.generate(ctx, key, filePath, workingPath, "densidade_municipal_por_periodo.R",
		[]string{estado, req.Start, req.End, filePath, workingPath})
	if err != nil {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "error", err)
		return false
	}
	if generated && code != 0 {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "saida", output)
		return false
	}
	if reused {
		slog.Info("Relatório reutilizado do armazenamento", "relatorio", idRelatorio, "chave", key)
	}

	return w.deliver(ctx, idRelatorio, fileName, workingPath, req)
}

// CasosMensais mirrors relatorios.casos_mensais_por_municipio_por_estado.
func (w *Worker) CasosMensais(req Request) bool {
	const idRelatorio = "CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO"
	const reportPath = "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado"

	slog.Info("Obtendo registros de queimaduras",
		"estados", strings.Join(req.States, ", "),
		"intervalo", formatIntervalo(req.Start, req.End))

	folder := reportHome(w.ReportsDir, reportPath)
	fileName := strings.Join(req.States, "-") + "." + req.Start + "." + req.End + ".pdf"
	key, _ := storageKey(reportPath, fileName)
	filePath := filepath.Join(folder, fileName)
	workingPath := filepath.Join(folder, req.RequestID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	generated, reused, code, output, err := w.generate(ctx, key, filePath, workingPath, "casos_mensais_por_municipio_por_estado.R",
		[]string{strings.Join(req.States, ","), req.Start, req.End, filePath, workingPath})
	if err != nil {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "error", err)
		return false
	}
	if generated && code != 0 {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "saida", output)
		return false
	}
	if reused {
		slog.Info("Relatório reutilizado do armazenamento", "relatorio", idRelatorio, "chave", key)
	}

	return w.deliver(ctx, idRelatorio, fileName, workingPath, req)
}

// deliver records the processing date and cleans the working directory. The
// PDF itself is already persisted in the report store by generate; clients are
// notified through polling/SSE on /relatorios/processados and /relatorios/eventos.
func (w *Worker) deliver(ctx context.Context, idRelatorio, fileName, workingPath string, req Request) bool {
	_ = ctx
	if err := w.Store.Save(idRelatorio, fileName); err != nil {
		slog.Error("falha ao gravar data de processamento", "error", err)
		return false
	}
	if workingPath != "" {
		if err := os.RemoveAll(workingPath); err != nil {
			slog.Warn("falha ao limpar diretório de trabalho", "path", workingPath, "error", err)
		}
	}
	return true
}
