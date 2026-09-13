// Package processed mirrors src/relatorios/relatorios.py: the list of
// processed / in-flight reports shown by the frontend. Finished PDFs are
// listed from the report store (S3 bucket when configured), so the history
// survives restarts.
package processed

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rodrinac/enceladus/go/internal/config"
	"github.com/rodrinac/enceladus/go/internal/jobstatus"
	"github.com/rodrinac/enceladus/go/internal/reportstore"
	"github.com/rodrinac/enceladus/go/internal/store"
)

var (
	redisDateFormat     = "02/01/2006 15:04:05"
	processedDateFormat = "2006-01-02T15:04:05-07:00"
	isoOnlyFormat       = "2006-01-02T15:04:05"
	fullISOPassiveDate  = "2006-01-02T15:04:05Z07:00"
)

// List assembles stored PDFs plus live jobs, sorted newest first.
func List(cfg *config.Config, reports reportstore.Storage, dataStore store.DataStore, registry *jobstatus.Registry) ([]map[string]interface{}, error) {
	dates, err := dataStore.Dates()
	if err != nil {
		return nil, err
	}

	produced := map[string]bool{}
	relatorios := []map[string]interface{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, report := range cfg.Relatorios {
		subdir := strings.TrimPrefix(strings.TrimPrefix(report.Path, "/"), "relatorios/")
		keys, listErr := reports.List(ctx, subdir)
		if listErr != nil {
			continue
		}
		for _, key := range keys {
			segments := strings.Split(key, "/")
			nomeBase := segments[len(segments)-1]
			partes := strings.Split(nomeBase, ".")
			if len(partes) < 3 {
				continue
			}
			redisKey := fmt.Sprintf("dataProcessamento.%s.%s", report.ID, nomeBase)
			valor := dates[redisKey]
			processadoEm := withOffset(valor)
			estado := estadoDisplay(partes[0])

			relatorios = append(relatorios, map[string]interface{}{
				"tipo":               report.Nome,
				"estado":             estado,
				"data_inicio":        partes[1],
				"data_fim":           partes[2],
				"uri":                report.Path + "/" + nomeBase,
				"data_processamento": nullable(processadoEm),
				"id_requisicao":      nil,
				"mensagem":           nil,
				"status":             "succeeded",
				"criado_em":          nullable(processadoEm),
			})
			produced[reportKey(report.Nome, estado, partes[1], partes[2])] = true
		}
	}

	for _, job := range registry.List() {
		if produced[reportKey(job.Tipo, estadoDisplay(job.Estado), job.DataInicio, job.DataFim)] {
			continue
		}
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

// estadoDisplay renders a PDF filename state segment ("DF-SP") with the same
// separator shown for live jobs ("DF · SP").
func estadoDisplay(value string) string {
	return strings.Join(strings.Split(value, "-"), " · ")
}

// reportKey identifies a report by type, states, and date range, so that a
// still-registered job does not shadow its already-generated PDF.
func reportKey(tipo, estado, dataInicio, dataFim string) string {
	return strings.Join([]string{tipo, estado, dataInicio, dataFim}, "|")
}

// withOffset normalizes a stored processing timestamp to include the UTC
// offset, so clients do not have to guess the timezone of naive values.
func withOffset(value string) string {
	if parsed := parseDateTime(value); !parsed.IsZero() {
		return parsed.Format(processedDateFormat)
	}
	return value
}

// parseDate mirrors _ordenar_por_data: ISO first, then %d/%m/%Y %H:%M:%S.
func parseDate(entry map[string]interface{}) time.Time {
	return parseDateTime(firstString(entry, "criado_em", "data_processamento"))
}

func parseDateTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{processedDateFormat, isoOnlyFormat, fullISOPassiveDate, time.RFC3339, redisDateFormat} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
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
