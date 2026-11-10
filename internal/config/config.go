package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rig-avanade/genesys-copilot-studio/internal/cloud"
)

func Required(name string) (string, error) {
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func URL(name string) (string, error) {
	value, err := Required(name)
	if err != nil {
		return "", err
	}
	if err := cloud.HTTPS(value); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return cloud.Base(value), nil
}

func ResourceName(name string) (string, error) {
	value, err := Required(name)
	if err != nil {
		return "", err
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`).MatchString(value) || strings.Contains(value, "--") {
		return "", fmt.Errorf("%s must be a lowercase Azure resource name of 3-63 characters", name)
	}
	return value, nil
}

func Secret(name string) (string, error) {
	value, err := Required(name)
	if err != nil {
		return "", err
	}
	if len(value) < 32 {
		return "", fmt.Errorf("%s must contain at least 32 characters", name)
	}
	if strings.HasPrefix(value, "@Microsoft.KeyVault(") || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%s must be a resolved, single-line secret", name)
	}
	return value, nil
}

type Server struct {
	Port, GenesysKey, KnowledgeKey       string
	IntegrationID, BotID, BotVersion     string
	DirectLineEndpoint, DirectLineSecret string
	StorageEndpoint, SessionContainer    string
	SearchEndpoint, SearchIndex          string
	TurnTimeout                          time.Duration
}

func LoadServer() (Server, error) {
	var c Server
	var errs []error
	for _, field := range []struct {
		name string
		dest *string
		load func(string) (string, error)
	}{
		{"GENESYS_API_KEY", &c.GenesysKey, Secret},
		{"KNOWLEDGE_API_KEY", &c.KnowledgeKey, Secret},
		{"GENESYS_INTEGRATION_ID", &c.IntegrationID, Required},
		{"GENESYS_BOT_ID", &c.BotID, Required},
		{"GENESYS_BOT_VERSION", &c.BotVersion, Required},
		{"DIRECT_LINE_ENDPOINT", &c.DirectLineEndpoint, URL},
		{"DIRECT_LINE_SECRET", &c.DirectLineSecret, Secret},
		{"STORAGE_ENDPOINT", &c.StorageEndpoint, URL},
		{"SESSION_CONTAINER", &c.SessionContainer, ResourceName},
		{"SEARCH_ENDPOINT", &c.SearchEndpoint, URL},
		{"SEARCH_INDEX", &c.SearchIndex, ResourceName},
	} {
		value, err := field.load(field.name)
		*field.dest = value
		errs = append(errs, err)
	}
	if c.GenesysKey != "" && c.GenesysKey == c.KnowledgeKey {
		errs = append(errs, errors.New("GENESYS_API_KEY and KNOWLEDGE_API_KEY must be different"))
	}
	c.Port = os.Getenv("PORT")
	if c.Port == "" {
		c.Port = "8080"
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		errs = append(errs, errors.New("PORT must be between 1 and 65535"))
	}
	c.TurnTimeout = 20 * time.Second
	if raw := os.Getenv("TURN_TIMEOUT"); raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil || duration < time.Second || duration > 45*time.Second {
			errs = append(errs, errors.New("TURN_TIMEOUT must be between 1s and 45s"))
		} else {
			c.TurnTimeout = duration
		}
	}
	return c, errors.Join(errs...)
}
