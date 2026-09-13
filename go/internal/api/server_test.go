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
	"github.com/rodrinac/enceladus/go/internal/reportstore"
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

func newTestEnv(t *testing.T) (*Server, *fakeStore, *reportstore.Memory) {
	t.Helper()
	reportsDir := t.TempDir()
	store := &fakeStore{dates: map[string]string{}}
	mem := reportstore.NewMemory()

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
				Parametros:       []string{"estado", "data_inicio", "data_fim"},
			},
			{
				ID:               reportTypeDensity,
				Nome:             "Densidade municipal por período",
				Path:             "/relatorios/queimaduras/densidade-municipal-por-periodo",
				MultiplosEstados: false,
				CamposData:       []string{"dd", "MM", "yyyy"},
				Parametros:       []string{"estado", "data_inicio", "data_fim"},
			},
			{
				ID:               reportTypeCasos,
				Nome:             "Casos mensais por município por estado",
				Path:             "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado",
				MultiplosEstados: true,
				ColunasMensais:   true,
				CamposData:       []string{"yyyy"},
				Parametros:       []string{"estado", "ano_inicio", "ano_fim"},
			},
		},
	}

	st := settings.Settings{ReportsDir: reportsDir, CORSOrigins: []string{"*"}, CorsWildcard: true}

	scriptPath := writeFakeScript(t)
	runner := &rreport.Runner{SourceRoot: reportsDir, RscriptsDir: reportsDir, ScriptBin: scriptPath}
	worker := reports.NewWorker(cfg, reportsDir, store, mem, runner)
	registry := jobstatus.NewRegistry()

	return NewServer(cfg, st, store, mem, registry, worker), store, mem
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
	server, store, mem := newTestEnv(t)
	target := "/relatorios/queimaduras/densidade-municipal-por-periodo-geral" +
		"?estado=AC&estado=CE&data_inicio=2022-01-01&data_fim=2022-12-31"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d, body %s", recorder.Code, recorder.Body.String())
	}
	var submit map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &submit); err != nil {
		t.Fatal(err)
	}
	idReq, _ := submit["id_requisicao"].(string)
	if idReq == "" {
		t.Fatalf("missing id_requisicao: %+v", submit)
	}
	if submit["reutilizado"] != false {
		t.Fatalf("expected reutilizado=false, got %+v", submit)
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

	if exists, _ := mem.Exists(t.Context(), "queimaduras/densidade-municipal-por-periodo-geral/"+fileName); !exists {
		t.Fatal("expected PDF in report store")
	}
}

func TestSubmitCasos(t *testing.T) {
	server, _, mem := newTestEnv(t)
	target := "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado" +
		"?estado=AC&ano_inicio=2021&ano_fim=2021"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d, body %s", recorder.Code, recorder.Body.String())
	}
	waitFor(t, 3*time.Second, func() bool { return len(server.Registry.List()) == 0 })

	if exists, _ := mem.Exists(t.Context(), "queimaduras/casos-mensais-por-municipio-por-estado/AC.2021.2021.pdf"); !exists {
		t.Fatal("expected PDF in report store")
	}
}

func TestSubmitQueuesWithoutContact(t *testing.T) {
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
	if submit["id_requisicao"] == nil || submit["id_requisicao"] == "" {
		t.Fatalf("expected id_requisicao, got %+v", submit)
	}
	if _, ok := submit["destino"]; ok {
		t.Fatalf("email field should be gone, got %+v", submit)
	}
	waitFor(t, 3*time.Second, func() bool { return len(server.Registry.List()) == 0 })
}

func TestSubmitReusesStoredReport(t *testing.T) {
	server, _, mem := newTestEnv(t)
	if err := mem.Put(t.Context(), "queimaduras/densidade-municipal-por-periodo/CE.2021-01-01.2022-12-31.pdf", []byte("%PDF-1.5"), "application/pdf"); err != nil {
		t.Fatal(err)
	}
	target := "/relatorios/queimaduras/densidade-municipal-por-periodo" +
		"?estado=CE&data_inicio=2021-01-01&data_fim=2022-12-31"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 reuse, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var submit map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &submit); err != nil {
		t.Fatal(err)
	}
	if submit["reutilizado"] != true {
		t.Fatalf("expected reutilizado=true, got %+v", submit)
	}
	if submit["uri"] == nil || submit["uri"] == "" {
		t.Fatalf("expected uri, got %+v", submit)
	}
	if len(server.Registry.List()) != 0 {
		t.Fatal("reuse should not queue a job")
	}
}

func TestEventosStreamsSSE(t *testing.T) {
	server, _, _ := newTestEnv(t)
	recorder := server.request(t, "GET", "/relatorios/eventos?snapshot=1", nil)
	if ct := recorder.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("unexpected content type: %q", ct)
	}
	if !strings.Contains(recorder.Body.String(), "event: relatorios") {
		t.Fatalf("expected SSE relatorios event, got %q", recorder.Body.String())
	}
}

func TestEventosStreamEndsBeforeProxyTimeout(t *testing.T) {
	server, _, _ := newTestEnv(t)
	previous := sseMaxStreamDuration
	sseMaxStreamDuration = 150 * time.Millisecond
	defer func() { sseMaxStreamDuration = previous }()

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/relatorios/eventos", nil))
		done <- recorder
	}()

	select {
	case recorder := <-done:
		if recorder.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), "event: relatorios") {
			t.Fatalf("expected SSE relatorios event, got %q", recorder.Body.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("event stream did not terminate before the proxy timeout")
	}
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
		"?estado=CE&data_inicio=2021-01-01&data_fim=2022-12-31"
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
	server, _, mem := newTestEnv(t)
	if err := mem.Put(t.Context(), "queimaduras/densidade-municipal-por-periodo/CE.2020.2022.pdf", []byte("%PDF-1.5"), "application/pdf"); err != nil {
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
	server, store, mem := newTestEnv(t)
	target := "/relatorios/queimaduras/densidade-municipal-por-periodo-geral" +
		"?estado=TODOS&data_inicio=2022-01-01&data_fim=2022-12-31"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	waitFor(t, 3*time.Second, func() bool { return len(server.Registry.List()) == 0 })

	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.dates["dataProcessamento.DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL.AC-CE.2022-01-01.2022-12-31.pdf"]; !ok {
		t.Fatalf("expected expansion to all states: %v", store.dates)
	}
	if exists, _ := mem.Exists(t.Context(), "queimaduras/densidade-municipal-por-periodo-geral/AC-CE.2022-01-01.2022-12-31.pdf"); !exists {
		t.Fatal("expected expanded PDF in report store")
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
		"?estado=CE&data_inicio=2021-01-01&data_fim=2023-12-31"
	recorder := server.request(t, "POST", target, nil)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d, body %s", recorder.Code, recorder.Body.String())
	}
	waitFor(t, 3*time.Second, func() bool { return len(server.Registry.List()) == 0 })
}
