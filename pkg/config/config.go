package config

import (
	"encoding/json"
	"log"
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
		return conf, err
	}
	conf.PopulateConfig(jsonData)
	return conf, nil
}

// readFiles loads in a file at the designated config path.
func readFiles(configPath string) ([]byte, error) {
	return os.ReadFile(configPath)
}

// PopulateConfig adds the JSON command metadata into the ConfigMap struct.
func (cm ConfigMap) PopulateConfig(jsonData []byte) {
	if err := json.Unmarshal(jsonData, &cm); err != nil {
		log.Println(err)
	}
}
