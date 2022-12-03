package config

type ConfigMap map[string]config
type config struct {
	Name        string
	Description string
	Filepaths   []string
}

// TODO: Should take in filepath of config file
func New() *ConfigMap {
	conf := make(ConfigMap)
	conf.PopulateConfig()
	return &conf
}

// TODO: Read in files
func ReadFiles() {

}

func (cm *ConfigMap) PopulateConfig() {

	(*cm)["sfw"] = config{
		Name:        "sfw",
		Description: "cleanse the chat",
		Filepaths: []string{
			"static/sfw-sasuke-crop1.png",
			"static/sfw-sasuke-crop2.png",
			"static/sfw-sasuke-crop3.png",
			"static/sfw-sasuke-crop4.png",
		},
	}
	(*cm)["razzle"] = config{
		Name:        "razzle",
		Description: "hit 'em with the ol' razzle dazzle",
		Filepaths: []string{
			"static/razzle.png",
		},
	}
}
