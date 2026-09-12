// Package settings mirrors src/settings.py: reads runtime configuration from
// the environment used by the systemd units and the deploy scripts.
package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Settings struct {
	AppHome              string
	SourceRoot           string
	CORSOrigins          []string
	CorsWildcard         bool
	DatasusMaxYearPath   string
	PopulationDataPath   string
	ReportsDir           string
	RscriptsDir          string
	RedisDB              int
	RedisHost            string
	RedisPassword        string
	RedisPort            int
	SESConfigurationSet  string
	SESRegion            string
	SESSender            string
	Bind                 string
	DatasusCacheMaxBytes uint64
	DatasusCachePath     string
}

const defaultAppHome = "/var/lib/enceladus"
const defaultSESSender = "Enceladus Big Data <enceladus.bigdata@hotmail.com>"
const defaultRegion = "eu-west-1"

func getenvInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed := 0
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func getenvUint64(name string, fallback uint64) uint64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed := uint64(0)
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

// FromEnvironment builds settings the same way Settings.from_environment did.
func FromEnvironment() Settings {
	appHome := os.Getenv("ENCELADUS_HOME")
	if appHome == "" {
		appHome = defaultAppHome
	}
	appHome, err := filepath.Abs(appHome)
	if err != nil {
		appHome = defaultAppHome
	}

	sourceRoot := os.Getenv("ENCELADUS_SOURCE_ROOT")
	if sourceRoot == "" {
		sourceRoot = "."
	}

	return Settings{
		AppHome:              appHome,
		SourceRoot:           sourceRoot,
		CORSOrigins:          parseOrigins(os.Getenv("CORS_ORIGINS")),
		DatasusMaxYearPath:   envPath("ENCELADUS_DATASUS_MAX_YEAR_PATH", filepath.Join(appHome, "data", "datasus-max-year.txt")),
		PopulationDataPath:   envPath("ENCELADUS_POPULATION_DATA_PATH", filepath.Join(appHome, "data", "population.csv")),
		ReportsDir:           filepath.Join(appHome, "relatorios"),
		RscriptsDir:          filepath.Join(sourceRoot, "rscripts"),
		RedisDB:              getenvInt("REDIS_DB", 0),
		RedisHost:            os.Getenv("REDIS_HOST"),
		RedisPassword:        os.Getenv("REDIS_PASSWORD"),
		RedisPort:            getenvInt("REDIS_PORT", 6379),
		SESConfigurationSet:  envOr("SES_CONFIGURATION_SET", "Default"),
		SESRegion:            envOr("AWS_DEFAULT_REGION", defaultRegion),
		SESSender:            envOr("SES_SENDER", defaultSESSender),
		Bind:                 os.Getenv("ENCELADUS_BIND"),
		DatasusCacheMaxBytes: getenvUint64("ENCELADUS_DATASUS_CACHE_MAX_BYTES", 5_368_709_120),
		DatasusCachePath:     envPath("ENCELADUS_DATASUS_CACHE_PATH", filepath.Join(appHome, "relatorios", ".cache", "datasus")),
	}
}

func (s Settings) WithCORS() Settings {
	if len(s.CORSOrigins) == 1 && s.CORSOrigins[0] == "*" {
		s.CorsWildcard = true
	}
	return s
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envPath(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	expanded, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return expanded
}

// parseOrigins mirrors _get_origins: "*" is kept as a single wildcard token.
func parseOrigins(value string) []string {
	if strings.TrimSpace(value) == "*" {
		return []string{"*"}
	}
	origins := []string{}
	for _, raw := range strings.Split(value, ",") {
		origin := strings.TrimSpace(raw)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	if len(origins) == 0 {
		return []string{"*"}
	}
	return origins
}
