package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConfig = `
anoInicio: 2014
anoFim: 2024
estados:
  - AC: Acre
  - CE: Ceará
codigosCid10:
  Queimaduras:
    - X00
    - X09
relatorios:
  - id: DENSIDADE_MUNICIPAL_POR_PERIODO
    nome: Densidade municipal por período
    path: /relatorios/queimaduras/densidade-municipal-por-periodo
    multiplos_estados: false
    campos_data:
      - data_inicio
      - data_fim
    parametros:
      - estado
`

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(testConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPreservesOrder(t *testing.T) {
	cfg, err := Load(writeTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Estados) != 2 || cfg.Estados[0].Code != "AC" || cfg.Estados[1].Code != "CE" {
		t.Fatalf("estados order not preserved: %+v", cfg.Estados)
	}
	if len(cfg.Codigos) != 1 || cfg.Codigos[0].Name != "Queimaduras" ||
		len(cfg.Codigos[0].Codigos) != 2 || cfg.Codigos[0].Codigos[0] != "X00" {
		t.Fatalf("codigos order not preserved: %+v", cfg.Codigos)
	}
	if len(cfg.Relatorios) != 1 || cfg.Relatorios[0].ID != "DENSIDADE_MUNICIPAL_POR_PERIODO" {
		t.Fatalf("relatorios not parsed: %+v", cfg.Relatorios)
	}
}

func TestAnosDisponiveisRange(t *testing.T) {
	cfg, err := Load(writeTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	anos := cfg.AnosDisponiveis(filepath.Join(t.TempDir(), "missing.txt"))
	if len(anos) != 11 || anos[0] != 2014 || anos[10] != 2024 {
		t.Fatalf("unexpected range: %v", anos)
	}
}

func TestAnosDisponiveisMaxYearOverride(t *testing.T) {
	cfg, err := Load(writeTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	maxPath := filepath.Join(t.TempDir(), "datasus-max-year.txt")
	if err := os.WriteFile(maxPath, []byte("2025\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	anos := cfg.AnosDisponiveis(maxPath)
	if len(anos) != 12 || anos[len(anos)-1] != 2025 {
		t.Fatalf("max year not applied: %v", anos)
	}
}

func TestEstadoObjectsJSONShape(t *testing.T) {
	cfg, err := Load(writeTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(cfg.EstadoObjects())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `{"AC":"Acre"}`) {
		t.Fatalf("unexpected estado objects JSON: %s", out)
	}
}

func TestCodigoObjectsJSONOrder(t *testing.T) {
	cfg, err := Load(writeTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(cfg.CodigoObjects())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `{"Queimaduras":["X00","X09"]}`) {
		t.Fatalf("unexpected codigos JSON: %s", out)
	}
}

func TestRelatorioByID(t *testing.T) {
	cfg, err := Load(writeTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	relatorio, err := cfg.RelatorioByID("DENSIDADE_MUNICIPAL_POR_PERIODO")
	if err != nil || relatorio.Nome != "Densidade municipal por período" {
		t.Fatalf("lookup failed: %+v, %v", relatorio, err)
	}
	if _, err := cfg.RelatorioByID("NOPE"); err == nil {
		t.Fatal("expected error for unknown id")
	}
}
