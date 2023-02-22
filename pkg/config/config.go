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
func New(configPath string) ConfigMap {
	conf := make(ConfigMap)
	jsonData := readFiles(configPath)
	conf.PopulateConfig(jsonData)
	return conf
}

// readFiles loads in a file at the designated config path.
func readFiles(configPath string) []byte {

	jsonFile, err := os.ReadFile(configPath)
	if err != nil {
		log.Println(err)
	}
	return jsonFile
}

// PopulateConfig adds the JSON command metadata into the ConfigMap struct.
func (cm ConfigMap) PopulateConfig(jsonData []byte) {
	json.Unmarshal(jsonData, &cm)
}
