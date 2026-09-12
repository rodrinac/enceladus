// Package reports mirrors the src/relatorios/* modules: it builds each report
// by invoking the R scripts, records processing dates and emails the PDF.
package reports

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/rodrinac/enceladus/go/internal/config"
	"github.com/rodrinac/enceladus/go/internal/rreport"
	"github.com/rodrinac/enceladus/go/internal/store"
)

type Request struct {
	States    []string
	Start     string
	End       string
	Email     string
	RequestID string
}

type Worker struct {
	Cfg        *config.Config
	ReportsDir string
	Store      store.DataStore
	Runner     *rreport.Runner
	Send       func(to, subject, fileName string, attachment []byte) error
}

func NewWorker(cfg *config.Config, reportsDir string, dataStore store.DataStore,
	runner *rreport.Runner, send func(to, subject, fileName string, attachment []byte) error) *Worker {
	return &Worker{Cfg: cfg, ReportsDir: reportsDir, Store: dataStore, Runner: runner, Send: send}
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

func (w *Worker) expandTodos(states []string) []string {
	if contains(states, "TODOS") {
		return w.Cfg.AllStateCodes()
	}
	return states
}

// generate returns (generated, exitCode, output). generated is false when the
// PDF already exists; exitCode is meaningful only when generated is true.
func (w *Worker) generate(filePath, workingPath, script string, args []string) (bool, int, string, error) {
	if _, err := os.Stat(filePath); err == nil {
		return false, 0, "", nil
	}
	if err := os.MkdirAll(workingPath, 0o755); err != nil {
		return false, 0, "", err
	}
	code, output := w.Runner.Run(script, args)
	slog.Debug("Retorno da execução R", "script", script, "output", output)
	slog.Info("Executou comando R", "script", script, "status", code)
	return true, code, output, nil
}

// DensidadeGeral mirrors relatorios.densidade_municipal_por_periodo_geral.
func (w *Worker) DensidadeGeral(req Request) bool {
	const idRelatorio = "DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL"
	states := w.expandTodos(req.States)

	slog.Info("Obtendo registros de queimaduras",
		"estados", fmt.Sprint(states),
		"intervalo", formatIntervalo(req.Start, req.End))

	folder := reportHome(w.ReportsDir, "/relatorios/queimaduras/densidade-municipal-por-periodo-geral")
	fileName := strings.Join(states, "-") + "." + req.Start + "." + req.End + ".pdf"
	filePath := filepath.Join(folder, fileName)
	workingPath := filepath.Join(folder, req.RequestID)

	generated, code, output, err := w.generate(filePath, workingPath,
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

	subject := fmt.Sprintf("Relatório de densidade municipal por período - %s, %s",
		strings.Join(states, ", "), formatIntervalo(req.Start, req.End))
	return w.deliver(idRelatorio, fileName, filePath, workingPath, req, subject, "relatorio_densidade_municipal.pdf")
}

// Densidade mirrors relatorios.densidade_municipal_por_periodo (single state).
func (w *Worker) Densidade(req Request) bool {
	const idRelatorio = "DENSIDADE_MUNICIPAL_POR_PERIODO"
	estado := ""
	if len(req.States) > 0 {
		estado = req.States[0]
	}

	slog.Info("Obtendo registros de queimaduras",
		"estado", estado,
		"intervalo", formatIntervalo(req.Start, req.End))

	folder := reportHome(w.ReportsDir, "/relatorios/queimaduras/densidade-municipal-por-periodo")
	fileName := estado + "." + req.Start + "." + req.End + ".pdf"
	filePath := filepath.Join(folder, fileName)
	workingPath := filepath.Join(folder, req.RequestID)

	generated, code, output, err := w.generate(filePath, workingPath, "densidade_municipal_por_periodo.R",
		[]string{estado, req.Start, req.End, filePath, workingPath})
	if err != nil {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "error", err)
		return false
	}
	if generated && code != 0 {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "saida", output)
		return false
	}

	subject := fmt.Sprintf("Relatório de densidade municipal por período - %s, %s",
		estado, formatIntervalo(req.Start, req.End))
	return w.deliver(idRelatorio, fileName, filePath, workingPath, req, subject, "relatorio_densidade_municipal.pdf")
}

// CasosMensais mirrors relatorios.casos_mensais_por_municipio_por_estado.
func (w *Worker) CasosMensais(req Request) bool {
	const idRelatorio = "CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO"

	slog.Info("Obtendo registros de queimaduras",
		"estados", strings.Join(req.States, ", "),
		"intervalo", formatIntervalo(req.Start, req.End))

	folder := reportHome(w.ReportsDir, "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado")
	fileName := strings.Join(req.States, "-") + "." + req.Start + "." + req.End + ".pdf"
	filePath := filepath.Join(folder, fileName)
	workingPath := filepath.Join(folder, req.RequestID)

	generated, code, output, err := w.generate(filePath, workingPath, "casos_mensais_por_municipio_por_estado.R",
		[]string{strings.Join(req.States, ","), req.Start, req.End, filePath, workingPath})
	if err != nil {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "error", err)
		return false
	}
	if generated && code != 0 {
		slog.Error("falha ao gerar relatório", "relatorio", idRelatorio, "requisicao", req.RequestID, "saida", output)
		return false
	}

	subject := fmt.Sprintf("Diagrama de distribuição do local de falecimento para %s, %s",
		strings.Join(req.States, ", "), formatIntervalo(req.Start, req.End))
	return w.deliver(idRelatorio, fileName, filePath, workingPath, req, subject, "relatorio.pdf")
}

// deliver mirrors the tail of the Python workers: record the processing date,
// remove the working directory, then attach and email the PDF.
func (w *Worker) deliver(idRelatorio, fileName, filePath, workingPath string, req Request, subject, attachmentName string) bool {
	if err := w.Store.Save(idRelatorio, fileName); err != nil {
		slog.Error("falha ao gravar data de processamento", "error", err)
		return false
	}
	if err := os.RemoveAll(workingPath); err != nil {
		slog.Warn("falha ao limpar diretório de trabalho", "path", workingPath, "error", err)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		slog.Error("falha ao ler relatório para envio", "arquivo", filePath, "error", err)
		return false
	}
	if w.Send == nil {
		// Nothing will email the report; treat as failure to stay observable.
		slog.Error("remetente de e-mail não configurado", "destinatario", req.Email)
		return false
	}
	if err := w.Send(req.Email, subject, attachmentName, data); err != nil {
		slog.Error("falha ao enviar relatório", "destinatario", req.Email, "error", err)
		return false
	}
	return true
}
