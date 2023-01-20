package config

type ConfigMap map[string]config
type config struct {
	Name        string
	Description string
	Filepaths   []string
}

// TODO: Should take in filepath of config file
func New() ConfigMap {
	conf := make(ConfigMap)
	conf.PopulateConfig()
	return conf
}

// TODO: Read in files
func ReadFiles() {

}

func (cm ConfigMap) PopulateConfig() {

	cm["sfw"] = config{
		Name:        "sfw",
		Description: "cleanse the chat",
		Filepaths: []string{
			"static/sfw-sasuke-crop1.png",
			"static/sfw-sasuke-crop2.png",
			"static/sfw-sasuke-crop3.png",
			"static/sfw-sasuke-crop4.png",
		},
	}
	cm["razzle"] = config{
		Name:        "razzle",
		Description: "hit 'em with the ol' razzle dazzle",
		Filepaths: []string{
			"static/razzle.png",
		},
	}
	cm["paulruddimok"] = config{
		Name:        "paulruddimok",
		Description: "‼️...i'm ok",
		Filepaths: []string{
			"static/celerymanimok.gif",
		},
	}
	cm["elmorise"] = config{
		Name:        "elmorise",
		Description: "elmo...rise!!",
		Filepaths: []string{
			"static/elmorise.gif",
		},
	}
	cm["gaku"] = config{
		Name:        "gaku",
		Description: "gaku dance",
		Filepaths: []string{
			"static/gaku.gif",
		},
	}
	cm["godiwishthatwereme"] = config{
		Name:        "godiwishthatwereme",
		Description: "god i wish that were me",
		Filepaths: []string{
			"static/godiwishthatwereme.jpeg",
		},
	}
	cm["qiqifallen"] = config{
		Name:        "qiqifallen",
		Description: "qiqi...fallen",
		Filepaths: []string{
			"static/qiqifallen.png",
		},
	}
	cm["whyareyoubooing"] = config{
		Name:        "whyareyoubooing",
		Description: "why are you booing me? i'm right.",
		Filepaths: []string{
			"static/whyareyoubooing.jpg",
		},
	}
	cm["yesyesyesyes"] = config{
		Name:        "yesyesyesyes",
		Description: "yes! yes! yes! yes!",
		Filepaths: []string{
			"static/yesyesyesyes.gif",
		},
	}
}
