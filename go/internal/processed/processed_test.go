package processed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rodrinac/enceladus/go/internal/config"
	"github.com/rodrinac/enceladus/go/internal/jobstatus"
)

type fakeStore struct{ dates map[string]string }

func (f *fakeStore) Save(id, name string) error {
	f.dates["dataProcessamento."+id+"."+name] = "10/10/2024 12:00:33"
	return nil
}

func (f *fakeStore) Dates() (map[string]string, error) { return f.dates, nil }

func testConfig() *config.Config {
	return &config.Config{
		Relatorios: []config.Relatorio{
			{
				ID:   "DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL",
				Nome: "Densidade municipal por período",
				Path: "/relatorios/queimaduras/densidade-municipal-por-periodo-geral",
			},
			{
				ID:   "CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO",
				Nome: "Casos mensais",
				Path: "/relatorios/queimaduras/casos-mensais-por-municipio-por-estado",
			},
		},
	}
}

func TestList(t *testing.T) {
	reportsDir := t.TempDir()
	geral := filepath.Join(reportsDir, "queimaduras", "densidade-municipal-por-periodo-geral")
	casos := filepath.Join(reportsDir, "queimaduras", "casos-mensais-por-municipio-por-estado")
	if err := os.MkdirAll(geral, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(casos, 0o755); err != nil {
		t.Fatal(err)
	}

	pdfPaths := []string{
		filepath.Join(geral, "CE.2020.2022.pdf"),
		filepath.Join(geral, "CE-BA.2020.2022.pdf"),
		filepath.Join(casos, "RN.2021.2021.pdf"),
	}
	for _, path := range pdfPaths {
		if err := os.WriteFile(path, []byte("%PDF"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	store := &fakeStore{dates: map[string]string{
		"dataProcessamento.DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL.CE.2020.2022.pdf":  "2024-10-10T12:00:01",
		"dataProcessamento.CASOS_MENSAIS_POR_MUNICIPIO_POR_ESTADO.RN.2021.2021.pdf": "10/10/2024 12:00:33",
	}}
	registry := jobstatus.NewRegistry()
	registry.Register("req-1", "Densidade municipal por período", []string{"RN"}, "2020", "2022")

	items, err := List(testConfig(), reportsDir, store, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}

	first := items[0]
	if first["id_requisicao"] != "req-1" {
		t.Fatalf("expected newest job first, got %+v", first)
	}
	if first["status"] != "queued" {
		t.Fatalf("unexpected job status: %+v", first)
	}

	var pdfEntry map[string]interface{}
	for _, item := range items {
		if item["status"] == "succeeded" && item["data_fim"] == "2022" {
			pdfEntry = item
			break
		}
	}
	if pdfEntry == nil {
		t.Fatalf("missing processed pdf entry: %+v", items)
	}
	if pdfEntry["uri"] != "/relatorios/queimaduras/densidade-municipal-por-periodo-geral/CE.2020.2022.pdf" {
		t.Fatalf("unexpected uri: %+v", pdfEntry)
	}
	if pdfEntry["data_processamento"] != "2024-10-10T12:00:01+00:00" {
		t.Fatalf("unexpected processing date: %+v", pdfEntry)
	}
}

func TestListSortsNewestFirst(t *testing.T) {
	reportsDir := t.TempDir()
	folder := filepath.Join(reportsDir, "queimaduras", "densidade-municipal-por-periodo-geral")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"CE.2020.2021.pdf", "CE.2020.2022.pdf"} {
		if err := os.WriteFile(filepath.Join(folder, base), []byte("%PDF"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := &fakeStore{dates: map[string]string{
		"dataProcessamento.DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL.CE.2020.2021.pdf": "10/10/2024 12:00:01",
		"dataProcessamento.DENSIDADE_MUNICIPAL_POR_PERIODO_GERAL.CE.2020.2022.pdf": "10/10/2025 12:00:01",
	}}

	items, err := List(testConfig(), reportsDir, store, jobstatus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0]["data_fim"] != "2022" {
		t.Fatalf("expected 2022 first, got %+v", items[0])
	}
}

func TestParseDate(t *testing.T) {
	iso := map[string]interface{}{"criado_em": "2024-10-10T12:00:01"}
	if got := parseDate(iso); got.IsZero() {
		t.Fatal("iso date parse failed")
	}
	br := map[string]interface{}{"criado_em": "10/10/2024 12:00:33"}
	if got := parseDate(br); got.IsZero() {
		t.Fatal("br date parse failed")
	}
	if got := parseDate(map[string]interface{}{}); !got.IsZero() {
		t.Fatal("empty date should be zero time")
	}
}
