package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rodrinac/enceladus/go/internal/config"
	"github.com/rodrinac/enceladus/go/internal/jobstatus"
	"github.com/rodrinac/enceladus/go/internal/reports"
	"github.com/rodrinac/enceladus/go/internal/rreport"
	"github.com/rodrinac/enceladus/go/internal/settings"
)

type fakeStore struct {
	mu    sync.Mutex
	dates map[string]string
}

func (f *fakeStore) Save(id, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dates["dataProcessamento."+id+"."+name] = time.Now().Format("02/01/2006 15:04:05")
	return nil
}

func (f *fakeStore) Dates() (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]string, len(f.dates))
	for k, v := range f.dates {
		out[k] = v
	}
	return out, nil
}

type emailLog struct {
	mu               sync.Mutex
	to, subject      string
	fileName         string
	attachmentLength int
}

func (e *emailLog) send(to, subject, fileName string, attachment []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.to = to
	e.subject = subject
	e.fileName = fileName
	e.attachmentLength = len(attachment)
	return nil
}

func newTestEnv(t *testing.T) (*Server, *fakeStore, *emailLog) {
	t.Helper()
	reportsDir := t.TempDir()
	store := &fakeStore{dates: map[string]string{}}
	emailLog := &emailLog{}

	cfg := &config.Config{
		AnosFromRange: true,
		AnoInicio:     2014,
		AnoFim:        2024,
		Estados:       []config.Estado{{Code: "AC", Name: "Acre"}, {Code: "CE", Name: "Ceará"}},
		Codigos: []config.CodigoGroup{
			{Name: "Queimaduras", Codigos: []string{"X00", "X09"}},
		},
		Relatorios: []config.Relatorio{
			{
				ID:               reportTypeGeral,
				Nome:             "Densidade municipal por período",
				Path:             "/relatorios/queimaduras/densidade-municipal-por-periodo-geral",
				MultiplosEstados: true,
				ColunasMensais:   true,
				CamposData:       []string{"MM", "yyyy"},
				Parametros:       []string{"estado", "data_inicio", "data_fim", "email"},
			},
			{
				ID:               reportTypeDensity,
				Nome:             "Densidade municipal por período",
				Path:             "/relatorios/queimaduras/densidade-municipal-por-periodo",
				MultiplosEstados: false,
				CamposData:       []string{"dd", "MM", "yyyy"},
				Parametros:       []string{"estado", "data_inicio", "data_fim", "email"},
			},
			{
				ID:               reportTypeCasos,
				Nome:             "Casos mensais por município por estado",
				Path:             "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado",
				MultiplosEstados: true,
				ColunasMensais:   true,
				CamposData:       []string{"yyyy"},
				Parametros:       []string{"estado", "ano_inicio", "ano_fim", "email"},
			},
		},
	}

	st := settings.Settings{ReportsDir: reportsDir, CORSOrigins: []string{"*"}, CorsWildcard: true}

	scriptPath := writeFakeScript(t)
	runner := &rreport.Runner{SourceRoot: reportsDir, RscriptsDir: reportsDir, ScriptBin: scriptPath}
	worker := reports.NewWorker(cfg, reportsDir, store, runner, emailLog.send)
	registry := jobstatus.NewRegistry()

	return NewServer(cfg, st, store, registry, worker), store, emailLog
}

func writeFakeScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-rscript")
	// Stands in for `Rscript <script> estados inicio fim <pdf> <workdir>`:
	// $1 is the script path, so the pdf lands in $5 and workdir in $6.
	script := "#!/bin/sh\nmkdir -p \"$6\"\necho '%PDF-1.5' > \"$5\"\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func (s *Server) request(t *testing.T, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, req)
	return recorder
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

func TestHealth(t *testing.T) {
	server, _, _ := newTestEnv(t)
	recorder := server.request(t, "GET", "/health", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"healthy"`) {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestConfigEndpoints(t *testing.T) {
	server, _, _ := newTestEnv(t)

	anos := server.request(t, "GET", "/config/anos", nil)
	if anos.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", anos.Code)
	}
	var anosList []int
	if err := json.Unmarshal(anos.Body.Bytes(), &anosList); err != nil {
		t.Fatal(err)
	}
	if len(anosList) != 11 || anosList[0] != 2014 || anosList[10] != 2024 {
		t.Fatalf("unexpected years: %v", anosList)
	}

	estados := server.request(t, "GET", "/config/estados", nil)
	if !strings.Contains(estados.Body.String(), `{"AC":"Acre"}`) {
		t.Fatalf("unexpected estados body: %s", estados.Body.String())
	}

	codigos := server.request(t, "GET", "/config/codigoscid10", nil)
	if !strings.Contains(codigos.Body.String(), `{"Queimaduras":["X00","X09"]}`) {
		t.Fatalf("unexpected codigos body: %s", codigos.Body.String())
	}

	relatorios := server.request(t, "GET", "/config/relatorios", nil)
	var relatoriosList []map[string]any
	if err := json.Unmarshal(relatorios.Body.Bytes(), &relatoriosList); err != nil {
		t.Fatal(err)
	}
	if len(relatoriosList) != 3 {
		t.Fatalf("unexpected relatorios: %v", relatoriosList)
	}
}

func TestProcessadosEmpty(t *testing.T) {
	server, _, _ := newTestEnv(t)
	recorder := server.request(t, "GET", "/relatorios/processados", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if strings.TrimSpace(recorder.Body.String()) != "[]\n" && strings.TrimSpace(recorder.Body.String()) != "[]" {
		t.Fatalf("expected empty list, got %s", recorder.Body.String())
	}
}

func TestSubmitGeral(t *testing.T) {
	server, store, mail := newTestEnv(t)
	target := "/relatorios/queimaduras/densidade-municipal-por-periodo-geral" +
		"?estado=AC&estado=CE&data_inicio=2022-01-01&data_fim=2022-12-31&email=a@b.co"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d, body %s", recorder.Code, recorder.Body.String())
	}
	var submit map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &submit); err != nil {
		t.Fatal(err)
	}
	if submit["destino"] != "a@b.co" {
		t.Fatalf("unexpected destino: %+v", submit)
	}
	idReq, _ := submit["id_requisicao"].(string)
	if idReq == "" {
		t.Fatalf("missing id_requisicao: %+v", submit)
	}

	waitFor(t, 3*time.Second, func() bool {
		return len(server.Registry.List()) == 0
	})

	fileName := "AC-CE.2022-01-01.2022-12-31.pdf"
	store.mu.Lock()
	_, saved := store.dates["dataProcessamento.DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL."+fileName]
	store.mu.Unlock()
	if !saved {
		t.Fatal("processing date was not saved to store")
	}

	mail.mu.Lock()
	defer mail.mu.Unlock()
	if mail.to != "a@b.co" || mail.fileName != "relatorio_densidade_municipal.pdf" {
		t.Fatalf("unexpected email: %+v", mail)
	}
	if mail.subject != "Relatório de densidade municipal por período - AC, CE, entre 2022-01-01 e 2022-12-31" {
		t.Fatalf("unexpected subject: %q", mail.subject)
	}
	if mail.attachmentLength == 0 {
		t.Fatal("expected a non-empty attachment")
	}
}

func TestSubmitCasos(t *testing.T) {
	server, _, mail := newTestEnv(t)
	target := "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado" +
		"?estado=AC&ano_inicio=2021&ano_fim=2021&email=a@b.co"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d, body %s", recorder.Code, recorder.Body.String())
	}
	waitFor(t, 3*time.Second, func() bool { return len(server.Registry.List()) == 0 })

	mail.mu.Lock()
	defer mail.mu.Unlock()
	if mail.subject != "Diagrama de distribuição do local de falecimento para AC, em 2021" {
		t.Fatalf("unexpected subject: %q", mail.subject)
	}
	if mail.fileName != "relatorio.pdf" {
		t.Fatalf("unexpected attachment: %q", mail.fileName)
	}
}

func TestSubmitWithoutEmailReturnsNullDestino(t *testing.T) {
	server, _, _ := newTestEnv(t)
	target := "/relatorios/queimaduras/densidade-municipal-por-periodo" +
		"?estado=CE&data_inicio=2021-01-01&data_fim=2022-12-31"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var submit map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &submit); err != nil {
		t.Fatal(err)
	}
	if submit["destino"] != nil {
		t.Fatalf("expected null destino, got %+v", submit["destino"])
	}
	waitFor(t, 3*time.Second, func() bool { return len(server.Registry.List()) == 0 })
}

func TestFailedReportMarksJob(t *testing.T) {
	server, _, _ := newTestEnv(t)
	// Point the runner at a script that always fails.
	failureScript := filepath.Join(t.TempDir(), "fail-rscript")
	if err := os.WriteFile(failureScript, []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	server.Worker.Runner.ScriptBin = failureScript

	target := "/relatorios/queimaduras/densidade-municipal-por-periodo" +
		"?estado=CE&data_inicio=2021-01-01&data_fim=2022-12-31&email=a@b.co"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	waitFor(t, 3*time.Second, func() bool {
		jobs := server.Registry.List()
		return len(jobs) == 1 && string(jobs[0].Status) == "failed"
	})
	jobs := server.Registry.List()
	if jobs[0].Mensagem == nil || *jobs[0].Mensagem != "Não foi possível gerar este relatório." {
		t.Fatalf("unexpected failed job: %+v", jobs[0])
	}
}

func TestCORS(t *testing.T) {
	server, _, _ := newTestEnv(t)

	preflight := server.request(t, "OPTIONS", "/relatorios/processados",
		map[string]string{"Origin": "https://rodrinac.github.io"})
	if preflight.Code != http.StatusOK {
		t.Fatalf("unexpected preflight status: %d", preflight.Code)
	}
	if preflight.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing wildcard ACAO: %q", preflight.Header().Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(preflight.Header().Get("Access-Control-Allow-Methods"), "POST") {
		t.Fatalf("missing methods: %q", preflight.Header().Get("Access-Control-Allow-Methods"))
	}

	get := server.request(t, "GET", "/health", map[string]string{"Origin": "https://rodrinac.github.io"})
	if get.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing ACAO on GET: %q", get.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSRestrictedOrigin(t *testing.T) {
	server, _, _ := newTestEnv(t)
	server.Settings = settings.Settings{
		ReportsDir:  server.Settings.ReportsDir,
		CORSOrigins: []string{"https://rodrinac.github.io"},
	}

	allowed := server.request(t, "OPTIONS", "/health", map[string]string{"Origin": "https://rodrinac.github.io"})
	if allowed.Header().Get("Access-Control-Allow-Origin") != "https://rodrinac.github.io" {
		t.Fatalf("expected echoed origin: %q", allowed.Header().Get("Access-Control-Allow-Origin"))
	}

	blocked := server.request(t, "OPTIONS", "/health", map[string]string{"Origin": "https://evil.example"})
	if blocked.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin leaked ACAO: %q", blocked.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestDownloadPDF(t *testing.T) {
	server, _, _ := newTestEnv(t)
	pdfPath := filepath.Join(server.Settings.ReportsDir, "queimaduras", "densidade-municipal-por-periodo")
	if err := os.MkdirAll(pdfPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdfPath, "CE.2020.2022.pdf"), []byte("%PDF-1.5"), 0o644); err != nil {
		t.Fatal(err)
	}

	recorder := server.request(t, "GET",
		"/relatorios/queimaduras/densidade-municipal-por-periodo/CE.2020.2022.pdf", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if recorder.Body.String() != "%PDF-1.5" {
		t.Fatalf("unexpected body: %q", recorder.Body.String())
	}
	if recorder.Header().Get("Content-Disposition") != `attachment; filename="CE.2020.2022.pdf"` {
		t.Fatalf("unexpected disposition: %q", recorder.Header().Get("Content-Disposition"))
	}
}

func TestDownloadPDFTraversalRejected(t *testing.T) {
	server, _, _ := newTestEnv(t)
	for _, target := range []string{
		"/relatorios/queimaduras/densidade-municipal-por-periodo/..%2F..%2Fetc%2Fpasswd",
		"/relatorios/queimaduras/densidade-municipal-por-periodo/../config.yml",
	} {
		recorder := server.request(t, "GET", target, nil)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", target, recorder.Code)
		}
	}
	recorder := server.request(t, "GET", "/relatorios/queimaduras/densidade-municipal-por-periodo/missing.pdf", nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing file, got %d", recorder.Code)
	}
}

func TestRunningJobVisibleInProcessados(t *testing.T) {
	server, _, _ := newTestEnv(t)
	server.Registry.Register("req-9", "Densidade municipal por período", []string{"SP"}, "2020", "2022")

	recorder := server.request(t, "GET", "/relatorios/processados", nil)
	if !strings.Contains(recorder.Body.String(), `"id_requisicao":"req-9"`) {
		t.Fatalf("missing running job: %s", recorder.Body.String())
	}
}

func TestGeralTODOSExpandsToAllStates(t *testing.T) {
	server, store, mail := newTestEnv(t)
	target := "/relatorios/queimaduras/densidade-municipal-por-periodo-geral" +
		"?estado=TODOS&data_inicio=2022-01-01&data_fim=2022-12-31&email=a@b.co"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	waitFor(t, 3*time.Second, func() bool { return len(server.Registry.List()) == 0 })

	mail.mu.Lock()
	defer mail.mu.Unlock()
	if mail.subject != "Relatório de densidade municipal por período - AC, CE, entre 2022-01-01 e 2022-12-31" {
		t.Fatalf("unexpected subject: %q", mail.subject)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.dates["dataProcessamento.DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL.AC-CE.2022-01-01.2022-12-31.pdf"]; !ok {
		t.Fatalf("expected expansion to all states: %v", store.dates)
	}
}

func TestSubmitRejectsInvalidPayload(t *testing.T) {
	server, _, _ := newTestEnv(t)
	cases := []struct {
		name   string
		target string
	}{
		{
			name:   "estado fora da lista de configuração",
			target: "/relatorios/queimaduras/densidade-municipal-por-periodo?estado=../../etc&data_inicio=2022-01-01&data_fim=2022-12-31",
		},
		{
			name:   "sem estados",
			target: "/relatorios/queimaduras/densidade-municipal-por-periodo?data_inicio=2022-01-01&data_fim=2022-12-31",
		},
		{
			name:   "data com formato inválido",
			target: "/relatorios/queimaduras/densidade-municipal-por-periodo?estado=CE&data_inicio=2022&data_fim=2022-12-31",
		},
		{
			name:   "data inexistente",
			target: "/relatorios/queimaduras/densidade-municipal-por-periodo?estado=CE&data_inicio=2022-13-45&data_fim=2022-12-31",
		},
		{
			name:   "data inicial depois da final",
			target: "/relatorios/queimaduras/densidade-municipal-por-periodo?estado=CE&data_inicio=2022-12-31&data_fim=2022-01-01",
		},
		{
			name:   "ano fora do intervalo disponível",
			target: "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado?estado=AC&ano_inicio=1999&ano_fim=1999",
		},
		{
			name:   "período acima de um ano para colunas mensais (geral)",
			target: "/relatorios/queimaduras/densidade-municipal-por-periodo-geral?estado=AC&data_inicio=2022-01-01&data_fim=2023-12-31",
		},
		{
			name:   "período acima de um ano para colunas mensais (casos)",
			target: "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado?estado=AC&ano_inicio=2022&ano_fim=2023",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := server.request(t, "POST", tc.target, nil)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "mensagem") {
				t.Fatalf("expected a mensagem body, got %s", recorder.Body.String())
			}
		})
	}
}

func TestSubmitRejectsTwoYearRangeForMonthlyColumns(t *testing.T) {
	server, _, _ := newTestEnv(t)
	target := "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado" +
		"?estado=AC&ano_inicio=2022&ano_fim=2023"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "colunas mensais") {
		t.Fatalf("expected monthly-columns message, got %s", recorder.Body.String())
	}
}

func TestSubmitAcceptsMultiYearForDayGranularity(t *testing.T) {
	server, _, _ := newTestEnv(t)
	target := "/relatorios/queimaduras/densidade-municipal-por-periodo" +
		"?estado=CE&data_inicio=2021-01-01&data_fim=2023-12-31&email=a@b.co"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d, body %s", recorder.Code, recorder.Body.String())
	}
	waitFor(t, 3*time.Second, func() bool { return len(server.Registry.List()) == 0 })
}
