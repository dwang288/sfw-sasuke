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
	Filepaths   []string `json:"filepaths"`
}

// TODO: Should take in filepath of config file
func New() ConfigMap {
	conf := make(ConfigMap)
	jsonData := readFiles("env/files-metadata.json")
	conf.PopulateConfig(jsonData)
	return conf
}

// TODO: Read in files
func readFiles(configPath string) []byte {

	jsonFile, err := os.ReadFile(configPath)
	if err != nil {
		log.Println(err)
	}
	// log.Printf("jsonFile contents: %v", jsonFile)
	return jsonFile
}

func (cm ConfigMap) PopulateConfig(jsonData []byte) {
	json.Unmarshal(jsonData, &cm)
}
