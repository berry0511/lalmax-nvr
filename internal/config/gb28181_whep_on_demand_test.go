package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGB28181WHEPOnDemandIsOptIn(t *testing.T) {
	var cfg Config
	cfg.ApplyDefaults()
	if cfg.GB28181.WHEPOnDemandEnabled {
		t.Fatal("external WHEP must not auto-INVITE GB28181 by default")
	}
	if err := yaml.Unmarshal([]byte("gb28181:\n  whep_on_demand_enabled: true\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.GB28181.WHEPOnDemandEnabled {
		t.Fatal("opt-in YAML did not enable GB28181 WHEP on demand")
	}
}
