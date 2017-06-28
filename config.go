package koi

import (
	"io/ioutil"

	yaml "gopkg.in/yaml.v2"
)

// Label struct for yaml
type Label struct {
	ApptimeLabel string `yaml:"apptime_label"`
	ReqtimeLabel string `yaml:"reqtime_label"`
	StatusLabel  string `yaml:"status_label"`
	SizeLabel    string `yaml:"size_label"`
	MethodLabel  string `yaml:"method_label"`
	URILabel     string `yaml:"uri_label"`
	TimeLabel    string `yaml:"time_label"`
}

func loadYAML(filename string) (conf Label, err error) {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return conf, err
	}
	err = yaml.Unmarshal(buf, &conf)

	return conf, err
}
