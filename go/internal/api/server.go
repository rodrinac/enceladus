// Package api mirrors src/main.py: the HTTP surface of the Enceladus API.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
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
	"github.com/rodrinac/enceladus/go/internal/settings"
	"github.com/rodrinac/enceladus/go/internal/store"
)

const (
	reportTypeGeral   = "DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL"
	reportTypeDensity = "DENSIDADE_MUNICIPAL_POR_PERIODO"
	reportTypeCasos   = "CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO"
)

var (
	anoRegex = regexp.MustCompile(`^\d{4}$`)
	dataRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

type Server struct {
	Cfg      *config.Config
	Settings settings.Settings
	Store    store.DataStore
	Registry *jobstatus.Registry
	Worker   *reports.Worker
	MaxYear  string
}

func NewServer(cfg *config.Config, st settings.Settings, dataStore store.DataStore,
	registry *jobstatus.Registry, worker *reports.Worker) *Server {
	return &Server{Cfg: cfg, Settings: st, Store: dataStore, Registry: registry, Worker: worker}
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
	items, err := processed.List(s.Cfg, s.Settings.ReportsDir, s.Store, s.Registry)
	if err != nil {
		slog.Error("falha ao listar relatórios processados", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"mensagem": "Não foi possível listar os relatórios."})
		return
	}
	writeJSON(w, http.StatusOK, items)
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

func (s *Server) submit(w http.ResponseWriter, reportID string, states []string, start, end, email string) {
	if err := s.validateSubmission(reportID, states, start, end); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"mensagem": err.Error()})
		return
	}
	idReq := uuid.New().String()
	s.Registry.Register(idReq, s.reportName(reportID), states, start, end)

	go s.processReport(idReq, reportID, reports.Request{
		States: states, Start: start, End: end, Email: email, RequestID: idReq,
	})

	writeJSON(w, http.StatusAccepted, map[string]any{
		"destino":       nullableString(email),
		"id_requisicao": idReq,
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
	s.submit(w, reportTypeGeral, multiParam(query, "estado"),
		query.Get("data_inicio"), query.Get("data_fim"), query.Get("email"))
}

func (s *Server) postDensity(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	estado := query.Get("estado")
	s.submit(w, reportTypeDensity, []string{estado},
		query.Get("data_inicio"), query.Get("data_fim"), query.Get("email"))
}

func (s *Server) postCasos(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	s.submit(w, reportTypeCasos, multiParam(query, "estado"),
		query.Get("ano_inicio"), query.Get("ano_fim"), query.Get("email"))
}

// downloadPDF serves a previously generated PDF, rejecting path traversal.
func (s *Server) downloadPDF(reportID string) http.HandlerFunc {
	folder := reportsHome(s.Settings.ReportsDir, reportID)
	return func(w http.ResponseWriter, r *http.Request) {
		pathTail := r.PathValue("path")
		if !isSafePath(pathTail) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		filePath := filepath.Join(folder, filepath.FromSlash(pathTail))
		data, err := os.ReadFile(filePath)
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

func isSafePath(value string) bool {
	if value == "" || strings.Contains(value, "..") || strings.Contains(value, "\\") {
		return false
	}
	return true
}

func multiParam(query url.Values, key string) []string {
	return query[key]
}

func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
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
