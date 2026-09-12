// Command enceladus-fetch-sim-archives is a Go port of the former
// scripts/fetch_sim_archives.py: download official OpenDataSUS SIM archives
// and retain only the requested states.
package main

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var archiveURLs = map[int]string{
	2019: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/Mortalidade_Geral_2019_csv.zip",
	2020: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/Mortalidade_Geral_2020_csv.zip",
	2021: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/Mortalidade_Geral_2021_csv.zip",
	2022: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/json/Mortalidade_Geral_2022_json.zip",
	2023: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/Mortalidade_Geral_2023_csv.zip",
	2024: "https://s3.sa-east-1.amazonaws.com/ckan.saude.gov.br/SIM/csv/DO24OPEN_csv.zip",
}

var stateCodes = map[string]string{
	"RO": "11", "AC": "12", "AM": "13", "RR": "14", "PA": "15", "AP": "16",
	"TO": "17", "MA": "21", "PI": "22", "CE": "23", "RN": "24", "PB": "25",
	"PE": "26", "AL": "27", "SE": "28", "BA": "29", "MG": "31", "ES": "32",
	"RJ": "33", "SP": "35", "PR": "41", "SC": "42", "RS": "43", "MS": "50",
	"MT": "51", "GO": "52", "DF": "53",
}

const userAgent = "enceladus-open-datasus/1.0"

type options struct {
	yearStart int
	yearEnd   int
	states    string
	cacheDir  string
	output    string
}

func download(url, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".*.part")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		cleanup()
		return err
	}
	request.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: 120 * 1_000_000_000}
	response, err := client.Do(request)
	if err != nil {
		cleanup()
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		cleanup()
		return fmt.Errorf("download of %s failed with HTTP %d", url, response.StatusCode)
	}
	if _, err := io.Copy(temporary, response.Body); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryName)
		return err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		_ = os.Remove(temporaryName)
		return err
	}
	return nil
}

func cachedSource(year int, cacheDirectory string) (string, error) {
	url := archiveURLs[year]
	suffix := ".zip"
	if !strings.HasSuffix(url, ".zip") {
		suffix = ".csv"
	}
	destination := filepath.Join(cacheDirectory, "archives", fmt.Sprintf("sim-%d%s", year, suffix))
	info, err := os.Stat(destination)
	if err == nil && info.Size() > 0 {
		return destination, nil
	}
	if err := download(url, destination); err != nil {
		return "", err
	}
	return destination, nil
}

// record holds the ordered keys and values of one DataSUS record.
type record struct {
	keys   []string
	values map[string]string
}

// recordIterator streams records on demand, mirroring Python generators.
type recordIterator struct {
	next    func() (record, error)
	cleanup func()
}

func (it *recordIterator) Close() {
	if it.cleanup != nil {
		it.cleanup()
		it.cleanup = nil
	}
}

// bomReader strips a UTF-8 BOM exactly like Python's utf-8-sig codec.
type bomReader struct {
	reader io.ReadCloser
	done   bool
}

func (r *bomReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		head := make([]byte, 3)
		n, err := io.ReadFull(r.reader, head)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return n, err
		}
		if n >= 3 && head[0] == 0xEF && head[1] == 0xBB && head[2] == 0xBF {
			return r.reader.Read(p)
		}
		if len(p) >= n {
			copy(p, head[:n])
			return n, nil
		}
		return 0, io.ErrShortBuffer
	}
	return r.reader.Read(p)
}

func (r *bomReader) Close() error { return r.reader.Close() }

// csvIterator streams a semicolon-separated CSV file.
func csvIterator(file io.ReadCloser) *recordIterator {
	reader := csv.NewReader(&bomReader{reader: file})
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	headerErr := err
	return &recordIterator{
		next: func() (record, error) {
			if headerErr != nil {
				return record{}, io.EOF
			}
			row, err := reader.Read()
			if errors.Is(err, io.EOF) {
				return record{}, io.EOF
			}
			if err != nil {
				return record{}, err
			}
			values := make(map[string]string, len(header))
			for i, value := range row {
				if i < len(header) {
					values[header[i]] = value
				}
			}
			return record{keys: header, values: values}, nil
		},
		cleanup: func() { _ = file.Close() },
	}
}

// decodeOrdered decodes the next JSON object preserving key order, mirroring
// Python's dict insertion order.
func decodeOrdered(dec *json.Decoder) (record, error) {
	token, err := dec.Token()
	if err != nil {
		return record{}, err
	}
	if token != json.Delim('{') {
		return record{}, errors.New("expected a JSON object")
	}
	out := record{values: map[string]string{}}
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return record{}, err
		}
		key := keyToken.(string)
		var value any
		if err := dec.Decode(&value); err != nil {
			return record{}, err
		}
		out.keys = append(out.keys, key)
		out.values[key] = stringify(value)
	}
	if _, err := dec.Token(); err != nil {
		return record{}, err
	}
	return out, nil
}

func stringify(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case nil:
		return ""
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(typed)
	}
}

// zipIterator streams records from the CSV or JSON members of a SIM archive.
func zipIterator(archivePath string) (*recordIterator, error) {
	zipFile, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	var csvMembers, jsonMembers []string
	for _, file := range zipFile.File {
		lower := strings.ToLower(file.Name)
		if strings.HasSuffix(lower, ".csv") {
			csvMembers = append(csvMembers, file.Name)
		}
		if strings.HasSuffix(lower, ".json") {
			jsonMembers = append(jsonMembers, file.Name)
		}
	}

	if len(csvMembers) == 1 {
		member, err := zipFile.Open(csvMembers[0])
		if err != nil {
			zipFile.Close()
			return nil, err
		}
		it := csvIterator(member)
		it.cleanup = func() {
			_ = member.Close()
			_ = zipFile.Close()
		}
		return it, nil
	}

	if len(jsonMembers) > 0 {
		sort.Strings(jsonMembers)
		state := struct {
			reader   io.ReadCloser
			decoder  *json.Decoder
			member   int
			started  bool
			finished bool
		}{}
		it := &recordIterator{}
		it.next = func() (record, error) {
			for {
				if state.finished {
					return record{}, io.EOF
				}
				if state.decoder == nil {
					if state.member >= len(jsonMembers) {
						state.finished = true
						return record{}, io.EOF
					}
					opened, err := zipFile.Open(jsonMembers[state.member])
					if err != nil {
						return record{}, err
					}
					state.reader = &bomReader{reader: opened}
					state.decoder = json.NewDecoder(state.reader)
					state.decoder.UseNumber()
					state.started = false
				}
				if !state.started {
					token, err := state.decoder.Token()
					if err != nil {
						if errors.Is(err, io.EOF) {
							_ = state.reader.Close()
							state.reader = nil
							state.decoder = nil
							state.member++
							continue
						}
						return record{}, err
					}
					if token != json.Delim('[') {
						return record{}, errors.New("JSON resource is not an array")
					}
					state.started = true
					if !state.decoder.More() {
						if _, err := state.decoder.Token(); err == nil {
							_ = state.reader.Close()
							state.reader = nil
							state.decoder = nil
							state.member++
							continue
						}
					}
				}
				if state.decoder.More() {
					return decodeOrdered(state.decoder)
				}
				// Consume the closing bracket and advance to the next member.
				if _, err := state.decoder.Token(); err != nil {
					return record{}, err
				}
				_ = state.reader.Close()
				state.reader = nil
				state.decoder = nil
				state.member++
			}
		}
		it.cleanup = func() {
			if state.reader != nil {
				_ = state.reader.Close()
			}
			_ = zipFile.Close()
		}
		return it, nil
	}

	zipFile.Close()
	return nil, errors.New("no supported CSV or JSON resources")
}

// openSource returns a streaming iterator for a plain CSV or zipped archive.
func openSource(source string) (*recordIterator, error) {
	if strings.HasSuffix(strings.ToLower(source), ".zip") {
		return zipIterator(source)
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	return csvIterator(file), nil
}

func sourceFieldnames(source string) ([]string, error) {
	it, err := openSource(source)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	first, err := it.next()
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	found := false
	for _, field := range first.keys {
		if field == "CODMUNRES" {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%s has no CODMUNRES column", source)
	}
	return first.keys, nil
}

func filterYear(source string, writer *csv.Writer, stateCodesSet map[string]bool, writeHeader bool, fieldnames []string) (int, error) {
	it, err := openSource(source)
	if err != nil {
		return 0, err
	}
	defer it.Close()

	if writeHeader {
		if err := writer.Write(fieldnames); err != nil {
			return 0, err
		}
	}
	count := 0
	for {
		item, err := it.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		municipality := item.values["CODMUNRES"]
		if len(municipality) >= 2 && stateCodesSet[municipality[:2]] {
			out := make([]string, len(fieldnames))
			for i, field := range fieldnames {
				out[i] = item.values[field]
			}
			if err := writer.Write(out); err != nil {
				return 0, err
			}
			count++
		}
	}
	return count, nil
}

func run(options options) error {
	years := make([]int, 0, options.yearEnd-options.yearStart+1)
	for year := options.yearStart; year <= options.yearEnd; year++ {
		years = append(years, year)
	}
	var unsupported []int
	for _, year := range years {
		if _, ok := archiveURLs[year]; !ok {
			unsupported = append(unsupported, year)
		}
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("no official SIM archive configured for years: %v", unsupported)
	}

	statesSet := map[string]bool{}
	var unknown []string
	for _, raw := range strings.Split(options.states, ",") {
		state := strings.ToUpper(strings.TrimSpace(raw))
		if state == "" {
			continue
		}
		if _, ok := stateCodes[state]; !ok {
			unknown = append(unknown, state)
		}
		statesSet[state] = true
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown state abbreviations: %v", unknown)
	}

	if err := os.MkdirAll(filepath.Dir(options.output), 0o755); err != nil {
		return err
	}
	sources := make([]string, 0, len(years))
	for _, year := range years {
		source, err := cachedSource(year, options.cacheDir)
		if err != nil {
			return err
		}
		sources = append(sources, source)
	}

	seenFields := map[string]bool{}
	var fieldnames []string
	for _, source := range sources {
		sourceFields, err := sourceFieldnames(source)
		if err != nil {
			return err
		}
		for _, field := range sourceFields {
			if !seenFields[field] {
				seenFields[field] = true
				fieldnames = append(fieldnames, field)
			}
		}
	}

	stateCodesSet := map[string]bool{}
	for state := range statesSet {
		stateCodesSet[stateCodes[state]] = true
	}

	output, err := os.Create(options.output)
	if err != nil {
		return err
	}
	writer := csv.NewWriter(output)
	writer.Comma = ';'
	writer.UseCRLF = false

	rowCount := 0
	for index, source := range sources {
		count, err := filterYear(source, writer, stateCodesSet, index == 0, fieldnames)
		if err != nil {
			output.Close()
			return err
		}
		rowCount += count
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}

	var sortedStates []string
	for state := range statesSet {
		sortedStates = append(sortedStates, state)
	}
	sort.Strings(sortedStates)
	fmt.Fprintf(os.Stderr, "Extracted %d SIM records for %s\n", rowCount, strings.Join(sortedStates, ","))
	return nil
}

func main() {
	var options options
	flag.IntVar(&options.yearStart, "year-start", 0, "first year to download")
	flag.IntVar(&options.yearEnd, "year-end", 0, "last year to download")
	flag.StringVar(&options.states, "states", "", "comma-separated state abbreviations")
	flag.StringVar(&options.cacheDir, "cache-dir", "", "DataSUS cache directory")
	flag.StringVar(&options.output, "output", "", "output CSV path")
	flag.Parse()

	switch {
	case options.yearStart == 0:
		fmt.Fprintln(os.Stderr, "error: --year-start is required")
		os.Exit(2)
	case options.yearEnd == 0:
		fmt.Fprintln(os.Stderr, "error: --year-end is required")
		os.Exit(2)
	case options.states == "":
		fmt.Fprintln(os.Stderr, "error: --states is required")
		os.Exit(2)
	case options.cacheDir == "":
		fmt.Fprintln(os.Stderr, "error: --cache-dir is required")
		os.Exit(2)
	case options.output == "":
		fmt.Fprintln(os.Stderr, "error: --output is required")
		os.Exit(2)
	}

	if err := run(options); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
