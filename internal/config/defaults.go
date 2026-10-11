package config

import (
	_ "embed"

	"gopkg.in/yaml.v3"
)

// defaultConfigYAML is the baseline configuration written to disk the first
// time the application runs without an existing config file: example duty
// groups, Finnish and English translations, flag days pre-filled for 2026
// and 2027, and one admin account (username "admin", password "change-me"
// -- change it). See default-config.yaml for details and sources.
//
//go:embed default-config.yaml
var defaultConfigYAML []byte

// Default returns a fresh configuration parsed from the embedded
// default-config.yaml.
func Default() *Config {
	cfg := &Config{}
	if err := yaml.Unmarshal(defaultConfigYAML, cfg); err != nil {
		// The embedded file is part of the binary and controlled by us, so a
		// parse failure here indicates a build-time bug, not a runtime
		// condition callers can recover from.
		panic("config: embedded default-config.yaml is invalid: " + err.Error())
	}
	normalize(cfg)
	return cfg
}
