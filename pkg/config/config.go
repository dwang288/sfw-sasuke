package config

import (
	"encoding/json"
	"os"
)

type ConfigMap map[string][]filesConfig
type filesConfig struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Filenames   []string `json:"filenames"`
}

// New creates a new ConfigMap struct populated with file metadata.
func New(configPath string) (ConfigMap, error) {
	conf := make(ConfigMap)
	jsonData, err := readFiles(configPath)
	if err != nil {
		return nil, err
	}
	if err := conf.PopulateConfig(jsonData); err != nil {
		return nil, err
	}
	return conf, nil
}

// readFiles loads in a file at the designated config path.
func readFiles(configPath string) ([]byte, error) {
	return os.ReadFile(configPath)
}

// PopulateConfig adds the JSON command metadata into the ConfigMap struct.
func (cm ConfigMap) PopulateConfig(jsonData []byte) error {
	return json.Unmarshal(jsonData, &cm)
}
