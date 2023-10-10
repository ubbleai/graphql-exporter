package config

import (
	"encoding/json"
	"log"
	"os"
)

type Cfg struct {
	GraphqlURL      string
	GraphqlAPIToken string
	Queries         []Query
}

type Query struct {
	Query   string
	Metrics []Metric
}

type Metric struct {
	Description string
	Placeholder string
	Labels      []string
	Value       string
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
	err = json.Unmarshal(content, &Config)
	if err != nil {
		return err
	}
	log.Printf("Finished reading config.")
	return nil
}
