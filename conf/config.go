package conf

import (
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
	"os"
)

type RetunerConfig struct {
	WebServerPort         int      `ini:"WebServerPort"`
	InterceptDNs          []string `ini:"InterceptDNs"`
	DNSAdvertiseIP        string   `ini:"DNSAdvertiseIP"`
	DNSRestrictForwarding bool     `ini:"DNSRestrictForwarding"`
	CommonAVRini          bool     `ini:"CommonAVRini"`
}

func LoadRetunerConfig(path string) (*RetunerConfig, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}

	retunerCfg := &RetunerConfig{
		WebServerPort:         80,
		CommonAVRini:          true,
	}

	section := cfg.Section("Retuner")
	retunerCfg.WebServerPort = section.Key("WebServerPort").MustInt(80)
	retunerCfg.InterceptDNs = section.Key("InterceptDNs").Strings(",")
	retunerCfg.DNSAdvertiseIP = section.Key("DNSAdvertiseIP").String()
	retunerCfg.DNSRestrictForwarding = section.Key("RestrictForwarding").MustBool(false)
	retunerCfg.CommonAVRini = section.Key("CommonAVRini").MustBool(true)

	return retunerCfg, nil
}

type Station struct {
	URL  string
	Logo string
}

type StationsConfig map[string]map[string]Station

type YAMLStation string
type YAMLStations map[string]map[string]YAMLStation

func LoadYAMLStations(path string) (StationsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var yamlCfg YAMLStations
	if err := yaml.Unmarshal(data, &yamlCfg); err != nil {
		return nil, err
	}

	cfg := make(StationsConfig)
	for catName, cat := range yamlCfg {
		cfg[catName] = make(map[string]Station)
		for statName, statData := range cat {
			parts := []rune(statData)
			url := ""
			logo := ""
			pipeIdx := -1
			for i, r := range parts {
				if r == '|' {
					pipeIdx = i
					break
				}
			}
			if pipeIdx != -1 {
				url = string(parts[:pipeIdx])
				logo = string(parts[pipeIdx+1:])
			} else {
				url = string(statData)
			}
			cfg[catName][statName] = Station{URL: url, Logo: logo}
		}
	}
	return cfg, nil
}
