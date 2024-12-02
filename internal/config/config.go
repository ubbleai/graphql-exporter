package config

import (
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

type Cfg struct {
	MetricsPrefix      string  `yaml:"metricsPrefix"`
	GraphqlURL         string  `yaml:"graphqlURL"`
	GraphqlAPIToken    string  `yaml:"graphqlAPIToken"`
	CacheExpire        int64   `yaml:"cacheExpire"`
	QueryTimeout       int64   `yaml:"queryTimeout"`
	FailFast           bool    `yaml:"failFast"`
	ExtendCacheOnError bool    `yaml:"extendCacheOnError"`
	Queries            []Query `yaml:"queries"`
}

type Query struct {
	Query     string `yaml:"query"`
	Subsystem string `yaml:"subsystem"`
	Metrics   []Metric
}

type Metric struct {
	Description string   `yaml:"description"`
	MetricType  string   `yaml:"metricType"`
	Placeholder string   `yaml:"placeholder"`
	Labels      []string `yaml:"labels"`
	Value       string   `yaml:"value"`
	Name        string   `yaml:"name"`
}

var (
	Config     *Cfg
	ConfigPath string
)

func Init(configPath string) error {
	ConfigPath = configPath
	content := []byte(`{}`)
	_, err := os.Stat(ConfigPath)
	if !os.IsNotExist(err) {
		content, err = os.ReadFile(ConfigPath)
		if err != nil {
			return err
		}
	}

	if len(content) == 0 {
		content = []byte(`{}`)
	}

	err = yaml.Unmarshal(content, &Config)
	if err != nil {
		return err
	}
	val, isSet := os.LookupEnv("GRAPHQLAPITOKEN")
	if isSet {
		Config.GraphqlAPIToken = val
	}

	if Config.QueryTimeout == 0 {
		Config.QueryTimeout = 60
	}

	slog.Info(fmt.Sprintf("Finished reading config from %s", configPath))
	return nil
}
