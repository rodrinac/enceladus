// Package api mirrors src/main.py: the HTTP surface of the Enceladus API.
//
// Progress push works without email: clients subscribe to
// GET /relatorios/eventos (Server-Sent Events) and fall back to polling
// GET /relatorios/processados. Finished PDFs are served from the report
// store (S3 bucket when configured), so repeat submissions reuse the file.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rodrinac/enceladus/go/internal/config"
	"github.com/rodrinac/enceladus/go/internal/jobstatus"
	"github.com/rodrinac/enceladus/go/internal/processed"
	"github.com/rodrinac/enceladus/go/internal/reports"
	"github.com/rodrinac/enceladus/go/internal/reportstore"
	"github.com/rodrinac/enceladus/go/internal/settings"
	"github.com/rodrinac/enceladus/go/internal/store"
)

const (
	reportTypeGeral   = "DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL"
	reportTypeDensity = "DENSIDADE_MUNICIPAL_POR_PERIODO"
	reportTypeCasos   = "CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO"
)

var (
	anoRegex      = regexp.MustCompile(`^\d{4}$`)
	dataRegex     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	downloadRegex = regexp.MustCompile(`^[A-Z]{2}(-[A-Z]{2})*\.\d{4}(-\d{2}-\d{2})?\.\d{4}(-\d{2}-\d{2})?\.pdf$`)
)

// sseMaxStreamDuration bounds an event stream so it always terminates before
// the managed proxy chain gives up: the Lambda proxy buffers the whole
// response and times out after 29s, so an endless stream would surface in the
// browser as a 500 instead of push updates. EventSource reconnects on close
// and receives a fresh snapshot, so a bounded stream behaves like push.
var sseMaxStreamDuration = 20 * time.Second

type Server struct {
	Cfg      *config.Config
	Settings settings.Settings
	Store    store.DataStore
	Reports  reportstore.Storage
	Registry *jobstatus.Registry
	Worker   *reports.Worker
	MaxYear  string
}

func NewServer(cfg *config.Config, st settings.Settings, dataStore store.DataStore,
	storage reportstore.Storage, registry *jobstatus.Registry, worker *reports.Worker) *Server {
	return &Server{Cfg: cfg, Settings: st, Store: dataStore, Reports: storage, Registry: registry, Worker: worker}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.root)
	mux.HandleFunc("GET /health", s.health)

	mux.HandleFunc("GET /config/anos", s.anosDisponiveis)
	mux.HandleFunc("GET /config/estados", s.estadosDisponiveis)
	mux.HandleFunc("GET /config/codigoscid10", s.codigosCid10)
	mux.HandleFunc("GET /config/relatorios", s.relatoriosDisponiveis)

	mux.HandleFunc("GET /relatorios/processados", s.processados)
	mux.HandleFunc("GET /relatorios/eventos", s.eventos)

	mux.HandleFunc("GET /relatorios/queimaduras/densidade-municipal-por-periodo-geral/{path...}", s.downloadPDF(reportTypeGeral))
	mux.HandleFunc("POST /relatorios/queimaduras/densidade-municipal-por-periodo-geral", s.postGeral)
	mux.HandleFunc("GET /relatorios/queimaduras/densidade-municipal-por-periodo/{path...}", s.downloadPDF(reportTypeDensity))
	mux.HandleFunc("POST /relatorios/queimaduras/densidade-municipal-por-periodo", s.postDensity)
	mux.HandleFunc("GET /relatorios/queimaduras/casos-mensais-por-municipio-por-estado/{path...}", s.downloadPDF(reportTypeCasos))
	mux.HandleFunc("POST /relatorios/queimaduras/casos-mensais-por-municipio-por-estado", s.postCasos)

	return s.cors(s.rejectTraversal(mux))
}

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"service": "enceladus-api", "status": "healthy"})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Server) anosDisponiveis(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Cfg.AnosDisponiveis(s.Settings.DatasusMaxYearPath))
}

func (s *Server) estadosDisponiveis(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Cfg.EstadoObjects())
}

func (s *Server) codigosCid10(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Cfg.CodigoObjects())
}

func (s *Server) relatoriosDisponiveis(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Cfg.Relatorios)
}

func (s *Server) processados(w http.ResponseWriter, r *http.Request) {
	items, err := processed.List(s.Cfg, s.Reports, s.Store, s.Registry)
	if err != nil {
		slog.Error("falha ao listar relatórios processados", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"mensagem": "Não foi possível listar os relatórios."})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// eventos streams the processed list over Server-Sent Events so browsers get
// push updates without email. Clients should fall back to polling
// /relatorios/processados when EventSource is unavailable.
func (s *Server) eventos(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if r.URL.Query().Get("snapshot") == "1" {
		items, err := processed.List(s.Cfg, s.Reports, s.Store, s.Registry)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"mensagem": "Não foi possível listar os relatórios."})
			return
		}
		raw, _ := json.Marshal(items)
		w.Write([]byte("event: relatorios\n"))
		w.Write([]byte("data: "))
		w.Write(raw)
		w.Write([]byte("\n\n"))
		return
	}

	fl, canFlush := w.(http.Flusher)
	send := func(payload string) bool {
		if _, err := fmt.Fprintf(w, "event: relatorios\ndata: %s\n\n", payload); err != nil {
			return false
		}
		if canFlush {
			fl.Flush()
		}
		return true
	}

	// last tracks the newest payload already pushed, seeded with the
	// initial snapshot so an unchanged state holds the stream open
	// instead of echoing the snapshot on the first tick.
	last := ""

	// Initial snapshot so subscribers render immediately.
	if items, err := processed.List(s.Cfg, s.Reports, s.Store, s.Registry); err == nil {
		if raw, err := json.Marshal(items); err == nil {
			last = string(raw)
			if !send(last) {
				return
			}
		}
	} else {
		last = "[]"
		if !send(last) {
			return
		}
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	streamDeadline := time.NewTimer(sseMaxStreamDuration)
	defer streamDeadline.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-streamDeadline.C:
			// Close the stream before the proxy's timeout so the client
			// reconnects for a fresh snapshot instead of seeing a 500.
			return
		case <-ticker.C:
			items, err := processed.List(s.Cfg, s.Reports, s.Store, s.Registry)
			if err != nil {
				continue
			}
			raw, err := json.Marshal(items)
			if err != nil {
				continue
			}
			if string(raw) == last {
				continue
			}
			last = string(raw)
			if !send(last) {
				return
			}
			// End the stream after the first change: the buffering proxy
			// only delivers the body once the handler returns, so holding
			// the stream open would delay this update by up to the
			// deadline above. The client reconnects immediately.
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			if canFlush {
				fl.Flush()
			}
		}
	}
}

func (s *Server) reportName(reportID string) string {
	report, err := s.Cfg.RelatorioByID(reportID)
	if err != nil {
		return reportID
	}
	return report.Nome
}

// validateSubmission rejects submissions whose states or interval would escape
// the configured universe: the same values feed file paths and R argv, so they
// are constrained to known states, well-formed dates/years, available years and
// (for monthly-column reports) a single calendar year.
func (s *Server) validateSubmission(reportID string, states []string, start, end string) error {
	report, err := s.Cfg.RelatorioByID(reportID)
	if err != nil {
		return errors.New("tipo de relatório desconhecido")
	}

	normalized := make([]string, 0, len(states))
	for _, estado := range states {
		if strings.TrimSpace(estado) != "" {
			normalized = append(normalized, estado)
		}
	}
	if len(normalized) == 0 {
		return errors.New("informe pelo menos um estado")
	}
	allowed := s.Cfg.AllStateCodes()
	for _, estado := range normalized {
		if estado != "TODOS" && !containsValue(allowed, estado) {
			return fmt.Errorf("estado desconhecido: %s", estado)
		}
	}

	startYear, endYear := start, end
	if report.UsesYears() {
		if !anoRegex.MatchString(start) || !anoRegex.MatchString(end) {
			return errors.New("ano_inicio e ano_fim devem ser anos válidos (AAAA)")
		}
		if start > end {
			return errors.New("ano inicial depois do ano final")
		}
	} else {
		startTime, errStart := time.Parse("2006-01-02", start)
		endTime, errEnd := time.Parse("2006-01-02", end)
		if errStart != nil || errEnd != nil {
			return errors.New("data_inicio e data_fim devem estar no formato AAAA-MM-DD")
		}
		if startTime.After(endTime) {
			return errors.New("data inicial depois da data final")
		}
		startYear, endYear = startTime.Format("2006"), endTime.Format("2006")
	}

	available := s.Cfg.AnosDisponiveis(s.Settings.DatasusMaxYearPath)
	if len(available) == 0 {
		return errors.New("nenhum ano disponível")
	}
	if !containsYear(available, startYear) || !containsYear(available, endYear) {
		return fmt.Errorf("período fora do intervalo de anos disponíveis (de %d a %d)",
			available[0], available[len(available)-1])
	}

	if report.ColunasMensais && startYear != endYear {
		return errors.New("relatórios com colunas mensais aceitam no máximo um ano de período")
	}
	return nil
}

func containsValue(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func containsYear(years []int, value string) bool {
	for _, year := range years {
		if strconv.Itoa(year) == value {
			return true
		}
	}
	return false
}

// expectedOutput computes the deterministic file name, storage key and public
// URI for a submission, so repeats hit the same object.
func (s *Server) expectedOutput(reportID string, states []string, start, end string) (fileName, key, uri string) {
	report, err := s.Cfg.RelatorioByID(reportID)
	if err != nil {
		return "", "", ""
	}
	expanded := states
	for _, estado := range states {
		if estado == "TODOS" {
			expanded = s.Cfg.AllStateCodes()
			break
		}
	}
	fileName = strings.Join(expanded, "-") + "." + start + "." + end + ".pdf"
	key = reportstore.KeyForReport(report.Path, fileName)
	uri = reportstore.PublicURI(report.Path, fileName)
	return fileName, key, uri
}

func (s *Server) submit(w http.ResponseWriter, r *http.Request, reportID string, states []string, start, end string) {
	if err := s.validateSubmission(reportID, states, start, end); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"mensagem": err.Error()})
		return
	}
	_, key, uri := s.expectedOutput(reportID, states, start, end)
	if key != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		exists, err := s.Reports.Exists(ctx, key)
		cancel()
		if err == nil && exists {
			// Already processed: reuse the stored file instead of
			// re-running R. Clients learn about it via SSE/polling too.
			writeJSON(w, http.StatusOK, map[string]any{
				"id_requisicao": nil,
				"status":        "succeeded",
				"uri":           uri,
				"reutilizado":   true,
			})
			return
		}
	}
	idReq := uuid.New().String()
	s.Registry.Register(idReq, s.reportName(reportID), states, start, end)

	go s.processReport(idReq, reportID, reports.Request{
		States: states, Start: start, End: end, RequestID: idReq,
	})

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id_requisicao": idReq,
		"status":        "queued",
		"uri":           nil,
		"reutilizado":   false,
	})
}

func (s *Server) processReport(idReq, reportID string, req reports.Request) {
	s.Registry.MarkRunning(idReq)
	success := false
	switch reportID {
	case reportTypeGeral:
		success = s.Worker.DensidadeGeral(req)
	case reportTypeDensity:
		success = s.Worker.Densidade(req)
	case reportTypeCasos:
		success = s.Worker.CasosMensais(req)
	}
	if success {
		s.Registry.MarkSucceeded(idReq)
	} else {
		slog.Error("falha ao processar requisição", "requisicao", idReq)
		s.Registry.MarkFailed(idReq)
	}
}

func (s *Server) postGeral(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	s.submit(w, r, reportTypeGeral, multiParam(query, "estado"),
		query.Get("data_inicio"), query.Get("data_fim"))
}

func (s *Server) postDensity(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	estado := query.Get("estado")
	s.submit(w, r, reportTypeDensity, []string{estado},
		query.Get("data_inicio"), query.Get("data_fim"))
}

func (s *Server) postCasos(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	s.submit(w, r, reportTypeCasos, multiParam(query, "estado"),
		query.Get("ano_inicio"), query.Get("ano_fim"))
}

// downloadPDF serves a previously generated PDF from the report store,
// rejecting path traversal.
func (s *Server) downloadPDF(reportID string) http.HandlerFunc {
	report, err := s.Cfg.RelatorioByID(reportID)
	reportPath := ""
	if err == nil {
		reportPath = report.Path
	} else {
		reportPath = reportsHomeFallback(s.Settings.ReportsDir, reportID)
	}
	_ = reportPath
	return func(w http.ResponseWriter, r *http.Request) {
		pathTail := r.PathValue("path")
		// Only a single allowlisted PDF base name may vary; sub-paths are
		// rejected to keep storage keys inside the report folder.
		if !downloadRegex.MatchString(pathTail) || !isSafePath(pathTail) ||
			strings.Contains(filepath.ToSlash(pathTail), "/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		key := reportstore.KeyForReport(reportPath, filepath.ToSlash(pathTail))
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		data, err := s.Reports.Get(ctx, key)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename=\""+pathTail+"\"")
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		_, _ = w.Write(data)
	}
}

// rejectTraversal returns 404 for requests whose path contains "." or ".."
// segments before the ServeMux cleans them (mirrors src/main.py).
func (s *Server) rejectTraversal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, segment := range strings.Split(r.URL.Path, "/") {
			if segment == "." || segment == ".." {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func reportsHome(reportsDir, reportID string) string {
	switch reportID {
	case reportTypeGeral:
		return filepath.Join(reportsDir, "queimaduras", "densidade-municipal-por-periodo-geral")
	case reportTypeDensity:
		return filepath.Join(reportsDir, "queimaduras", "densidade-municipal-por-periodo")
	case reportTypeCasos:
		return filepath.Join(reportsDir, "queimaduras", "casos-mensais-por-municipio-por-estado")
	}
	return reportsDir
}

// reportsHomeFallback is only used when the report ID is unknown; known IDs
// resolve through the config path so storage keys stay canonical.
func reportsHomeFallback(reportsDir, reportID string) string {
	return reportsHome(reportsDir, reportID)
}

func isSafePath(value string) bool {
	if value == "" || strings.Contains(value, "..") || strings.Contains(value, "\\") {
		return false
	}
	return true
}

func multiParam(query url.Values, key string) []string {
	return query[key]
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("falha ao serializar resposta", "error", err)
	}
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := s.originAllowed(origin)

		if r.Method == http.MethodOptions && origin != "" {
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", s.originHeader(origin))
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if origin != "" && allowed {
			w.Header().Set("Access-Control-Allow-Origin", s.originHeader(origin))
			w.Header().Add("Vary", "Origin")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	if s.Settings.CorsWildcard {
		return true
	}
	for _, allowed := range s.Settings.CORSOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}

func (s *Server) originHeader(origin string) string {
	if s.Settings.CorsWildcard {
		return "*"
	}
	return origin
}
