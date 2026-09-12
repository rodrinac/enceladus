// Package processed mirrors src/relatorios/relatorios.py: the list of
// processed / in-flight reports shown by the frontend.
package processed

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rodrinac/enceladus/go/internal/config"
	"github.com/rodrinac/enceladus/go/internal/jobstatus"
	"github.com/rodrinac/enceladus/go/internal/store"
)

var (
	redisDateFormat    = "02/01/2006 15:04:05"
	isoOnlyFormat      = "2006-01-02T15:04:05"
	fullISOPassiveDate = "2006-01-02T15:04:05Z07:00"
)

// List assembles processed PDFs plus live jobs, sorted newest first.
func List(cfg *config.Config, reportsDir string, dataStore store.DataStore, registry *jobstatus.Registry) ([]map[string]interface{}, error) {
	dates, err := dataStore.Dates()
	if err != nil {
		return nil, err
	}

	relatorios := []map[string]interface{}{}
	for _, report := range cfg.Relatorios {
		subdir := strings.TrimPrefix(strings.TrimPrefix(report.Path, "/"), "relatorios/")
		folder := filepath.Join(reportsDir, subdir)

		matches, globErr := filepath.Glob(filepath.Join(folder, "*.pdf"))
		if globErr != nil {
			continue
		}
		for _, match := range matches {
			nomeBase := filepath.Base(match)
			partes := strings.Split(nomeBase, ".")
			if len(partes) < 3 {
				continue
			}
			redisKey := fmt.Sprintf("dataProcessamento.%s.%s", report.ID, nomeBase)
			valor := dates[redisKey]

			relatorios = append(relatorios, map[string]interface{}{
				"tipo":               report.Nome,
				"estado":             partes[0],
				"data_inicio":        partes[1],
				"data_fim":           partes[2],
				"uri":                report.Path + "/" + nomeBase,
				"data_processamento": nullable(valor),
				"id_requisicao":      nil,
				"mensagem":           nil,
				"status":             "succeeded",
				"criado_em":          nullable(valor),
			})
		}
	}

	for _, job := range registry.List() {
		relatorios = append(relatorios, map[string]interface{}{
			"criado_em":     job.CriadoEm,
			"data_fim":      job.DataFim,
			"data_inicio":   job.DataInicio,
			"estado":        job.Estado,
			"id_requisicao": job.IDRequisicao,
			"mensagem":      job.Mensagem,
			"status":        string(job.Status),
			"tipo":          job.Tipo,
		})
	}

	sort.SliceStable(relatorios, func(i, j int) bool {
		return parseDate(relatorios[i]).After(parseDate(relatorios[j]))
	})
	return relatorios, nil
}

func nullable(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

// parseDate mirrors _ordenar_por_data: ISO first, then %d/%m/%Y %H:%M:%S.
func parseDate(entry map[string]interface{}) time.Time {
	value := firstString(entry, "criado_em", "data_processamento")
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(isoOnlyFormat, value); err == nil {
		return parsed
	}
	if parsed, err := time.Parse(fullISOPassiveDate, value); err == nil {
		return parsed
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed
	}
	if parsed, err := time.Parse(redisDateFormat, value); err == nil {
		return parsed
	}
	return time.Time{}
}

func firstString(entry map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := entry[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}
