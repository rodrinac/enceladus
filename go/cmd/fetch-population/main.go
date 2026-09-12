// Command enceladus-fetch-population is a Go port of the former
// scripts/fetch_population.py: refresh IBGE municipal population estimates and
// discover the newest available DataSUS SIM year.
package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	sidraURL = "https://apisidra.ibge.gov.br/values/t/6579/n6/all/v/9324/" +
		"p/{period}?formato=json"
	openDatasusCatalogURL = "https://dadosabertos.saude.gov.br/dataset/sim"
	minimumMunicipalities = 5_500
)

var expectedFields = map[string]bool{"D1C": true, "D1N": true, "V": true, "D3C": true}

var nextDataPattern = regexp.MustCompile(
	`<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
var mortalidadePattern = regexp.MustCompile(`Mortalidade Geral (\d{4})`)

func dataDir() string {
	home := os.Getenv("ENCELADUS_HOME")
	if home == "" {
		home = "/var/lib/enceladus"
	}
	resolved, err := filepath.Abs(home)
	if err != nil {
		resolved = home
	}
	return filepath.Join(resolved, "data")
}

func outputPath() string {
	if value := os.Getenv("ENCELADUS_POPULATION_DATA_PATH"); value != "" {
		return value
	}
	return filepath.Join(dataDir(), "population.csv")
}

func datasusMaxYearPath() string {
	if value := os.Getenv("ENCELADUS_DATASUS_MAX_YEAR_PATH"); value != "" {
		return value
	}
	return filepath.Join(dataDir(), "datasus-max-year.txt")
}

func httpGet(url, userAgent string, timeout time.Duration) (io.ReadCloser, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("GET %s returned HTTP %d", url, response.StatusCode)
	}
	return response.Body, nil
}

func discoverDatasusMaxYear() (int, error) {
	body, err := httpGet(openDatasusCatalogURL, "enceladus-population-fetcher/1.0", 20*time.Second)
	if err != nil {
		return 0, err
	}
	defer body.Close()
	catalog, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}

	match := nextDataPattern.FindSubmatch(catalog)
	if match == nil {
		return 0, errors.New("OpenDataSUS catalog metadata is missing")
	}
	var payload struct {
		Props struct {
			PageProps struct {
				Resources []struct {
					Format string `json:"format"`
					Name   string `json:"name"`
				} `json:"resources"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(match[1], &payload); err != nil {
		return 0, err
	}
	var years []int
	for _, resource := range payload.Props.PageProps.Resources {
		if resource.Format != "CSV" {
			continue
		}
		submatch := mortalidadePattern.FindStringSubmatch(resource.Name)
		if submatch == nil {
			continue
		}
		year, convErr := strconv.Atoi(submatch[1])
		if convErr != nil {
			continue
		}
		years = append(years, year)
	}
	if len(years) == 0 {
		return 0, errors.New("OpenDataSUS catalog returned no final SIM CSV archives")
	}
	maxYear := years[0]
	for _, year := range years[1:] {
		if year > maxYear {
			maxYear = year
		}
	}
	if maxYear > 2024 {
		maxYear = 2024
	}
	return maxYear, nil
}

func writeTextAtomically(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	cleaned := false
	defer func() {
		if !cleaned {
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err := temporary.WriteString(value); err != nil {
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return err
	}
	cleaned = true
	return nil
}

func refreshDatasusMaxYear() error {
	fallback := 2024
	if raw := os.Getenv("DATASUS_MAX_YEAR_FALLBACK"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			fallback = parsed
		}
	}
	maxYear, err := discoverDatasusMaxYear()
	if err != nil {
		if _, statErr := os.Stat(datasusMaxYearPath()); statErr == nil {
			fmt.Printf("OpenDataSUS discovery unavailable; retaining cached maximum year: %v\n", err)
			return nil
		}
		if writeErr := writeTextAtomically(datasusMaxYearPath(), fmt.Sprintf("%d\n", fallback)); writeErr != nil {
			return writeErr
		}
		fmt.Printf("OpenDataSUS discovery unavailable; using fallback year %d: %v\n", fallback, err)
		return nil
	}
	if err := writeTextAtomically(datasusMaxYearPath(), fmt.Sprintf("%d\n", maxYear)); err != nil {
		return err
	}
	fmt.Printf("Discovered OpenDataSUS SIM data through %d\n", maxYear)
	return nil
}

type record struct {
	keys   []string
	values map[string]string
}

func decodeJSONArray(body io.Reader) ([]map[string]any, error) {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var out []map[string]any
	for decoder.More() {
		var item map[string]any
		if err := decoder.Decode(&item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func stringize(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func fetchRecords(period string) ([]record, error) {
	body, err := httpGet(strings.ReplaceAll(sidraURL, "{period}", period),
		"enceladus-population-fetcher/1.0", 60*time.Second)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	payload, err := decodeJSONArray(body)
	if err != nil {
		return nil, err
	}
	if len(payload) < 2 {
		return nil, errors.New("SIDRA returned no municipal population records")
	}

	records := make([]record, 0, len(payload)-1)
	for _, raw := range payload[1:] {
		item := record{values: map[string]string{}}
		// Key order is not preserved by map decoding, which only matters for
		// the validation below and the CSV columns (fixed order is used).
		for key, value := range raw {
			item.keys = append(item.keys, key)
			item.values[key] = stringize(value)
		}
		records = append(records, item)
	}

	for field := range expectedFields {
		if _, ok := records[0].values[field]; !ok {
			return nil, errors.New("SIDRA response does not contain the expected fields")
		}
	}
	return records, nil
}

type municipality struct {
	state               string
	stateIBGECode       string
	cityIBGECode        string
	city                string
	estimatedPopulation string
	referenceYear       string
}

func normalize(records []record) ([]municipality, error) {
	municipalities := []municipality{}
	seenCodes := map[string]bool{}

	for _, item := range records {
		row := item.values
		cityCode := row["D1C"]
		population := strings.ReplaceAll(row["V"], " ", "")
		cityAndState := strings.SplitN(row["D1N"], " - ", 2)

		if len(cityAndState) != 2 ||
			len(cityCode) != 7 ||
			!isDigits(cityCode) ||
			!isDigits(population) ||
			toInt(population) <= 0 ||
			seenCodes[cityCode] {
			return nil, fmt.Errorf("invalid SIDRA municipality record: %v", row)
		}

		seenCodes[cityCode] = true
		municipalities = append(municipalities, municipality{
			state:               cityAndState[1],
			stateIBGECode:       cityCode[:2],
			cityIBGECode:        cityCode,
			city:                cityAndState[0],
			estimatedPopulation: population,
			referenceYear:       row["D3C"],
		})
	}

	if len(municipalities) < minimumMunicipalities {
		return nil, fmt.Errorf("SIDRA returned only %d municipalities; refusing partial data", len(municipalities))
	}
	return municipalities, nil
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func toInt(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

var populationColumns = []string{
	"state", "state_ibge_code", "city_ibge_code", "city",
	"estimated_population", "reference_year",
}

func writeAtomically(rows []municipality) error {
	if err := os.MkdirAll(filepath.Dir(outputPath()), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath()), "population-*.csv")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	cleaned := false
	defer func() {
		if !cleaned {
			_ = os.Remove(temporaryName)
		}
	}()

	writer := csv.NewWriter(temporary)
	if err := writer.Write(populationColumns); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writer.Write([]string{
			row.state, row.stateIBGECode, row.cityIBGECode, row.city,
			row.estimatedPopulation, row.referenceYear,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, outputPath()); err != nil {
		return err
	}
	cleaned = true
	return nil
}

func hasValidCache() bool {
	source, err := os.Open(outputPath())
	if err != nil {
		return false
	}
	defer source.Close()
	reader := csv.NewReader(source)
	header, err := reader.Read()
	if err != nil {
		return false
	}
	required := map[string]bool{
		"state": true, "state_ibge_code": true, "city_ibge_code": true,
		"city": true, "estimated_population": true, "reference_year": true,
	}
	for _, column := range header {
		delete(required, column)
	}
	if len(required) > 0 {
		return false
	}

	rows := 0
	seenCodes := map[string]bool{}
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false
		}
		rows++
		if len(row) < 6 {
			return false
		}
		code := row[2]
		population := row[4]
		state := row[0]
		if len(code) != 7 || !isDigits(code) || seenCodes[code] ||
			!isDigits(population) || toInt(population) <= 0 || len(state) != 2 {
			return false
		}
		seenCodes[code] = true
	}
	return rows >= minimumMunicipalities
}

func main() {
	period := os.Getenv("IBGE_POPULATION_PERIOD")
	if period == "" {
		period = "last%201"
	}
	var lastError error

	for attempt := 1; attempt <= 3; attempt++ {
		records, err := fetchRecords(period)
		var rows []municipality
		if err == nil {
			rows, err = normalize(records)
		}
		if err == nil {
			err = writeAtomically(rows)
		}
		if err == nil {
			fmt.Printf("Saved %d IBGE municipality estimates for %s to %s\n",
				len(rows), rows[0].referenceYear, outputPath())
			_ = refreshDatasusMaxYear()
			return
		}
		lastError = err
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}

	if hasValidCache() {
		fmt.Printf("IBGE is unavailable; retaining cached population data: %v\n", lastError)
		_ = refreshDatasusMaxYear()
		return
	}

	fmt.Fprintf(os.Stderr, "Unable to fetch IBGE population data and no cache exists: %v\n", lastError)
	os.Exit(1)
}
