package config

import (
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

type Cfg struct {
	MetricsPrefix       string  `yaml:"metricsPrefix"`
	GraphqlURL          string  `yaml:"graphqlURL"`
	GraphqlAPIToken     string  `yaml:"graphqlAPIToken"`
	GraphqlCustomHeader string  `default:"Authorization" yaml:"customAPIHeader"`
	LabelPathSeparator  string  `default:"," yaml:"labelPathSeparator"`
	CacheExpire         int64   `yaml:"cacheExpire"`
	QueryTimeout        int64   `yaml:"queryTimeout"`
	FailFast            bool    `yaml:"failFast"`
	ExtendCacheOnError  bool    `yaml:"extendCacheOnError"`
	Queries             []Query `yaml:"queries"`
	DisableTimestamp    bool    `yaml:"disableTimestamp"`
}

type Query struct {
	Query     string `yaml:"query"`
	Subsystem string `yaml:"subsystem"`
	Metrics   []Metric
}

type Label struct {
	Path  string `yaml:"path"`
	Alias string `yaml:"alias"`
}

type Metric struct {
	Description      string    `yaml:"description"`
	MetricType       string    `yaml:"metricType"`
	HistogramBuckets []float64 `yaml:"histogramBuckets"`
	Placeholder      string    `yaml:"placeholder"`
	Labels           []Label   `yaml:"labels"`
	Value            string    `yaml:"value"`
	Name             string    `yaml:"name"`
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
