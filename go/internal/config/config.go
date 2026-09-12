// Package config mirrors src/default_config.py.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Estado struct {
	Code string
	Name string
}

type CodigoGroup struct {
	Name    string
	Codigos []string
}

type Relatorio struct {
	ID               string   `yaml:"id" json:"id"`
	Nome             string   `yaml:"nome" json:"nome"`
	Path             string   `yaml:"path" json:"path"`
	MultiplosEstados bool     `yaml:"multiplos_estados" json:"multiplos_estados"`
	CamposData       []string `yaml:"campos_data" json:"campos_data"`
	ColunasMensais   bool     `yaml:"colunas_mensais" json:"colunas_mensais"`
	Parametros       []string `yaml:"parametros" json:"parametros"`
}

type Config struct {
	Anos          []int
	AnosFromRange bool
	AnoInicio     int
	AnoFim        int
	Estados       []Estado
	Codigos       []CodigoGroup
	Relatorios    []Relatorio
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	cfg := &Config{}
	if len(root.Content) == 0 {
		return cfg, nil
	}
	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return cfg, nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		value := mapping.Content[i+1]
		switch key {
		case "anoInicio":
			cfg.AnoInicio, _ = strconv.Atoi(value.Value)
			cfg.AnosFromRange = true
		case "anoFim":
			cfg.AnoFim, _ = strconv.Atoi(value.Value)
		case "anos":
			cfg.Anos = scalarInts(value)
		case "estados":
			cfg.Estados = parseEstados(value)
		case "codigosCid10":
			cfg.Codigos = parseCodigos(value)
		case "relatorios":
			cfg.Relatorios = parseRelatorios(value)
		}
	}
	return cfg, nil
}

func scalarInts(node *yaml.Node) []int {
	out := []int{}
	if node.Kind != yaml.SequenceNode {
		return out
	}
	for _, child := range node.Content {
		if value, err := strconv.Atoi(child.Value); err == nil {
			out = append(out, value)
		}
	}
	return out
}

func parseEstados(node *yaml.Node) []Estado {
	estados := []Estado{}
	if node.Kind != yaml.SequenceNode {
		return estados
	}
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode || len(item.Content) < 2 {
			continue
		}
		estados = append(estados, Estado{
			Code: item.Content[0].Value,
			Name: item.Content[1].Value,
		})
	}
	return estados
}

func parseCodigos(node *yaml.Node) []CodigoGroup {
	groups := []CodigoGroup{}
	if node.Kind != yaml.MappingNode {
		return groups
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		group := CodigoGroup{Name: node.Content[i].Value}
		values := node.Content[i+1]
		if values.Kind == yaml.SequenceNode {
			for _, child := range values.Content {
				group.Codigos = append(group.Codigos, child.Value)
			}
		}
		groups = append(groups, group)
	}
	return groups
}

func parseRelatorios(node *yaml.Node) []Relatorio {
	relatorios := []Relatorio{}
	if node.Kind != yaml.SequenceNode {
		return relatorios
	}
	for _, item := range node.Content {
		var relatorio Relatorio
		if err := item.Decode(&relatorio); err != nil {
			continue
		}
		relatorios = append(relatorios, relatorio)
	}
	return relatorios
}

// AnosDisponiveis mirrors default_config.anos_disponiveis: an inclusive range
// whose upper bound can be extended by the discovered DataSUS maximum year.
func (c *Config) AnosDisponiveis(maxYearPath string) []int {
	if c.AnosFromRange {
		end := c.AnoFim
		if raw, err := os.ReadFile(maxYearPath); err == nil {
			if discoveredYear, convErr := strconv.Atoi(string(trimSpace(string(raw)))); convErr == nil &&
				discoveredYear >= c.AnoInicio {
				end = discoveredYear
			}
		}
		out := make([]int, 0, end-c.AnoInicio+1)
		for year := c.AnoInicio; year <= end; year++ {
			out = append(out, year)
		}
		return out
	}
	return c.Anos
}

func trimSpace(value string) string {
	out := ""
	for _, r := range value {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		out += string(r)
	}
	return out
}

// EstadoObjects returns the estados as a list of single-key maps, matching the
// JSON shape the Python API exposed (key order preserved).
func (c *Config) EstadoObjects() []map[string]string {
	out := make([]map[string]string, 0, len(c.Estados))
	for _, estado := range c.Estados {
		out = append(out, map[string]string{estado.Code: estado.Name})
	}
	return out
}

// AllStateCodes returns the state UF codes in the order declared in config.
func (c *Config) AllStateCodes() []string {
	out := make([]string, 0, len(c.Estados))
	for _, estado := range c.Estados {
		out = append(out, estado.Code)
	}
	return out
}

// CodigoObjects is an ordered map of group name to codes; it marshals with
// insertion order preserved so the JSON matches what the Python API produced.
type CodigoObjects struct {
	Keys []string
	Vals map[string][]string
}

func (c *Config) CodigoObjects() CodigoObjects {
	keys := make([]string, 0, len(c.Codigos))
	vals := make(map[string][]string, len(c.Codigos))
	for _, group := range c.Codigos {
		keys = append(keys, group.Name)
		vals[group.Name] = group.Codigos
	}
	return CodigoObjects{Keys: keys, Vals: vals}
}

func (o CodigoObjects) MarshalJSON() ([]byte, error) {
	var buffer []byte
	buffer = append(buffer, '{')
	for i, key := range o.Keys {
		if i > 0 {
			buffer = append(buffer, ',')
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buffer = append(buffer, encoded...)
		buffer = append(buffer, ':')
		values, err := json.Marshal(o.Vals[key])
		if err != nil {
			return nil, err
		}
		buffer = append(buffer, values...)
	}
	buffer = append(buffer, '}')
	return buffer, nil
}

// UsesYears reports whether the report is requested with year-only parameters
// (ano_inicio/ano_fim) instead of full dates.
func (r Relatorio) UsesYears() bool {
	for _, parametro := range r.Parametros {
		if parametro == "ano_inicio" {
			return true
		}
	}
	return false
}

// RelatorioByID returns the report definition for the given id, mirroring
// _nome_relatorio.
func (c *Config) RelatorioByID(id string) (Relatorio, error) {
	for _, relatorio := range c.Relatorios {
		if relatorio.ID == id {
			return relatorio, nil
		}
	}
	return Relatorio{}, errors.New("unknown report id")
}
