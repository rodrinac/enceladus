package main

import (
	"archive/zip"
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZipFixture(t *testing.T, path string, members [][2]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	for _, member := range members {
		writer, err := archive.Create(member[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(member[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestRunFiltersByStateAcrossCSVAndJSONZip runs the real pipeline against a
// CSV archive (2021, UTF-8 BOM) and a JSON archive (2022), asserting the
// merged header, ";":-separated rows and state filtering (CE=23, BA=29).
func TestRunFiltersByStateAcrossCSVAndJSONZip(t *testing.T) {
	cacheDir := t.TempDir()
	// 2021: single CSV member with a UTF-8 BOM; MT (51) must be dropped.
	writeZipFixture(t, filepath.Join(cacheDir, "archives", "sim-2021.zip"),
		[][2]string{{"DO21OPEN.csv", "\xEF\xBB\xBFCODMUNRES;SEXO;CAUSABAS\n" +
			"2300111;M;X00\n2900111;F;X09\n5100111;M;X01\n2312345;F;X02\n"}})
	// 2022: official archive is JSON; SP (35) must be dropped.
	writeZipFixture(t, filepath.Join(cacheDir, "archives", "sim-2022.zip"),
		[][2]string{{"2022.json", `[{"CODMUNRES":"2300222","NOME":"A","CAUSABAS_O":"X00"},` +
			`{"CODMUNRES":"3500222","NOME":"B","CAUSABAS_O":"X01"}]`}})

	output := filepath.Join(t.TempDir(), "sim.csv")
	err := run(options{
		yearStart: 2021, yearEnd: 2022, states: "ce,BA",
		cacheDir: cacheDir, output: output,
	})
	if err != nil {
		t.Fatal(err)
	}

	source, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	reader := csv.NewReader(source)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 { // header + 4 CE/BA records
		t.Fatalf("expected 5 rows, got %d: %v", len(rows), rows)
	}
	header := rows[0]
	if !strings.Contains(strings.Join(header, ";"), "CODMUNRES") {
		t.Fatalf("missing CODMUNRES in header: %v", header)
	}
	if strings.HasPrefix(header[0], "\xEF\xBB\xBF") {
		t.Fatalf("BOM was not stripped from header: %q", header[0])
	}

	wantCodes := map[string]bool{"2300111": true, "2900111": true, "2300222": true, "2312345": true}
	gotCodes := map[string]bool{}
	for _, row := range rows[1:] {
		gotCodes[row[0]] = true
	}
	for code := range wantCodes {
		if !gotCodes[code] {
			t.Fatalf("missing record for municipality %s; got %v", code, gotCodes)
		}
	}
	if len(gotCodes) != 4 {
		t.Fatalf("expected only CE/BA records, got %v", gotCodes)
	}
}

func TestRunRejectsUnsupportedYear(t *testing.T) {
	err := run(options{yearStart: 2018, yearEnd: 2018, states: "CE", cacheDir: t.TempDir(), output: filepath.Join(t.TempDir(), "o.csv")})
	if err == nil || !strings.Contains(err.Error(), "no official SIM archive") {
		t.Fatalf("expected unsupported year error, got %v", err)
	}
}

func TestRunRejectsUnknownState(t *testing.T) {
	err := run(options{yearStart: 2019, yearEnd: 2019, states: "XY", cacheDir: t.TempDir(), output: filepath.Join(t.TempDir(), "o.csv")})
	if err == nil || !strings.Contains(err.Error(), "unknown state") {
		t.Fatalf("expected unknown state error, got %v", err)
	}
}

func TestZipIteratorRejectsUnsupportedMember(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "sim-2019.zip")
	writeZipFixture(t, archivePath, [][2]string{{"README.txt", "hello"}})
	if _, err := zipIterator(archivePath); err == nil ||
		!strings.Contains(err.Error(), "no supported CSV or JSON") {
		t.Fatalf("expected unsupported member error, got %v", err)
	}
}

// TestZipIteratorJSONStreamsMultipleMembers verifies the JSON iterator walks
// every array member and preserves record order and values across them.
func TestZipIteratorJSONStreamsMultipleMembers(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "sim-2022.zip")
	writeZipFixture(t, archivePath, [][2]string{
		{"a.json", `[{"CODMUNRES":"2300111","NOME":"first"}]`},
		{"b.json", `[{"CODMUNRES":"2300112","NOME":"second"},{"CODMUNRES":"2300113","NOME":"third"}]`},
	})

	it, err := zipIterator(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()

	var codes, names []string
	for {
		item, err := it.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, item.values["CODMUNRES"])
		names = append(names, item.values["NOME"])
	}
	if strings.Join(codes, ",") != "2300111,2300112,2300113" {
		t.Fatalf("unexpected records: codes=%v names=%v", codes, names)
	}
}

func TestSourceFieldnamesRejectsMissingCODMUNRES(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.csv")
	if err := os.WriteFile(path, []byte("SEXO;CURSO\nM;A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceFieldnames(path); err == nil ||
		!strings.Contains(err.Error(), "CODMUNRES") {
		t.Fatalf("expected missing CODMUNRES error, got %v", err)
	}
}
