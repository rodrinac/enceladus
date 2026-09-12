package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleRecords(count int) []record {
	records := make([]record, 0, count)
	for i := 1; i <= count; i++ {
		records = append(records, record{
			keys: []string{"D1C", "D1N", "V", "D3C"},
			values: map[string]string{
				"D1C": fmt.Sprintf("11%05d", i),
				"D1N": fmt.Sprintf("Example City %d - AC", i),
				"V":   "1000",
				"D3C": "2022",
			},
		})
	}
	return records
}

func TestNormalize(t *testing.T) {
	municipalities, err := normalize(sampleRecords(minimumMunicipalities))
	if err != nil {
		t.Fatal(err)
	}
	if len(municipalities) != minimumMunicipalities {
		t.Fatalf("expected %d municipalities, got %d", minimumMunicipalities, len(municipalities))
	}
	first := municipalities[0]
	if first.cityIBGECode != "1100001" || first.stateIBGECode != "11" ||
		first.state != "AC" || first.city != "Example City 1" ||
		first.estimatedPopulation != "1000" || first.referenceYear != "2022" {
		t.Fatalf("unexpected first municipality: %+v", first)
	}
}

func TestNormalizeRejectsInvalidRecords(t *testing.T) {
	cases := []struct {
		name   string
		record func() []record
	}{
		{"duplicate code", func() []record { return append(sampleRecords(10), sampleRecords(1)[0]) }},
		{"non-digit population", func() []record {
			rows := sampleRecords(10)
			rows[0].values["V"] = "abc"
			return rows
		}},
		{"zero population", func() []record {
			rows := sampleRecords(10)
			rows[0].values["V"] = "0"
			return rows
		}},
		{"short code", func() []record {
			rows := sampleRecords(10)
			rows[0].values["D1C"] = "123456"
			return rows
		}},
		{"missing name separator", func() []record {
			rows := sampleRecords(10)
			rows[0].values["D1N"] = "NoSeparator"
			return rows
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalize(tc.record()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNormalizeRefusesPartialData(t *testing.T) {
	if _, err := normalize(sampleRecords(100)); err == nil ||
		!strings.Contains(err.Error(), "refusing partial data") {
		t.Fatalf("expected partial data error, got %v", err)
	}
}

func writePopulationCSV(t *testing.T, path string, rows int, valid bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var builder strings.Builder
	builder.WriteString("state,state_ibge_code,city_ibge_code,city,estimated_population,reference_year\n")
	for i := 1; i <= rows; i++ {
		if valid {
			fmt.Fprintf(&builder, "AC,11,11%05d,Example City %d,1000,2022\n", i, i)
		} else {
			// Duplicate municipal code (i and i-1 collide) invalidates the cache.
			fmt.Fprintf(&builder, "AC,11,11%05d,Example City %d,1000,2022\n", (i+1)/2, i)
		}
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHasValidCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "population.csv")
	t.Setenv("ENCELADUS_POPULATION_DATA_PATH", cachePath)

	if hasValidCache() {
		t.Fatal("missing cache should be invalid")
	}

	writePopulationCSV(t, cachePath, minimumMunicipalities, true)
	if !hasValidCache() {
		t.Fatal("valid cache should be accepted")
	}

	writePopulationCSV(t, cachePath, 100, true)
	if hasValidCache() {
		t.Fatal("short cache should be rejected")
	}

	writePopulationCSV(t, cachePath, minimumMunicipalities, false)
	if hasValidCache() {
		t.Fatal("cache with duplicate municipal codes should be rejected")
	}

	if err := os.WriteFile(cachePath, []byte("state,state_ibge_code,city_ibge_code,city,estimated_population\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hasValidCache() {
		t.Fatal("cache missing reference_year column should be rejected")
	}
}

func sampleMunicipalities(count int) []municipality {
	out := make([]municipality, 0, count)
	for i := 1; i <= count; i++ {
		out = append(out, municipality{
			state:               "AC",
			stateIBGECode:       "11",
			cityIBGECode:        fmt.Sprintf("11%05d", i),
			city:                fmt.Sprintf("Example City %d", i),
			estimatedPopulation: "1000",
			referenceYear:       "2022",
		})
	}
	return out
}

func TestWriteAtomically(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "population.csv")
	t.Setenv("ENCELADUS_POPULATION_DATA_PATH", cachePath)

	if err := writeAtomically(sampleMunicipalities(10)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "state,state_ibge_code,city_ibge_code,city,estimated_population,reference_year\n") {
		t.Fatalf("unexpected header: %q", content)
	}
	if !strings.Contains(content, "1100010,Example City 10,1000,2022") {
		t.Fatalf("unexpected row content: %q", content)
	}
}

func TestDecodeJSONArrayAndStringize(t *testing.T) {
	items, err := decodeJSONArray(strings.NewReader(`[{"meta":"x"},{"a":123,"b":"str","c":null,"e":1.5}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	row := items[1]
	if stringize(row["a"]) != "123" || stringize(row["b"]) != "str" ||
		stringize(row["c"]) != "" || stringize(row["e"]) != "1.5" {
		t.Fatalf("unexpected stringize results: %v", row)
	}
	if _, ok := row["a"].(json.Number); !ok {
		t.Fatalf("expected json.Number preserved, got %T", row["a"])
	}
}
