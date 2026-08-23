package config

import (
	"testing"
)

type envmap map[string]string

func (e envmap) get(k string) (string, bool) {
	v, ok := e[k]
	return v, ok
}

func baseEnv() envmap {
	return envmap{
		"LOG_LEVEL":                   "debug",
		"WRITES_ENABLED":              "true",
		"NEXUDUS_BASE_URL":            "https://spaces.nexudus.com",
		"NEXUDUS_USERNAME":            "svc",
		"NEXUDUS_PASSWORD":            "secret",
		"DOORFLOW_BASE_URL":           "https://api.doorflow.com",
		"DOORFLOW_ADMIN_URL":          "https://admin.doorflow.com",
		"DOORFLOW_AUTH_TYPE":          "basic",
		"DOORFLOW_API_KEY":            "abc",
		"DOORFLOW_API_KEY2":           "def",
		"TEAM_GROUP_MAP":              "data/team_group_map.csv",
		"TEAM_GROUP_MAP_BASIC_MEMBER": "4482",
		"RECONCILE_PAGE_SIZE":         "500",
		"RECONCILE_CONCURRENCY":       "8",
		"NATS_URL":                    "nats://localhost:4222",
		"NATS_CONSUMER":               "doorflow-sync",
		"NATS_MAX_DELIVER":            "7",
	}
}

func TestLoadFromEnvReadsValues(t *testing.T) {
	cfg, err := LoadFromEnv(baseEnv().get)
	if err != nil {
		t.Fatalf("LoadFromEnv: unexpected error: %v", err)
	}
	if cfg.Logger.Level != "debug" {
		t.Errorf("Logger.Level = %q, want debug", cfg.Logger.Level)
	}
	if !cfg.Writes {
		t.Errorf("Writes = %v, want true", cfg.Writes)
	}
	if cfg.Nexudus.ReadOnly {
		t.Errorf("Nexudus.ReadOnly = %v, want false (NEXUDUS_WRITE_ENABLED default is off)", cfg.Nexudus.ReadOnly)
	}
	if cfg.Doorflow.BaseURL != "https://api.doorflow.com" {
		t.Errorf("Doorflow.BaseURL = %q", cfg.Doorflow.BaseURL)
	}
	if cfg.Doorflow.AdminURL != "https://admin.doorflow.com" {
		t.Errorf("Doorflow.AdminURL = %q", cfg.Doorflow.AdminURL)
	}
	if cfg.Reconcile.PageSize != 500 || cfg.Reconcile.Concurrency != 8 {
		t.Errorf("Reconcile.PageSize=%d Concurrency=%d", cfg.Reconcile.PageSize, cfg.Reconcile.Concurrency)
	}
	if cfg.Events.Consumer != "doorflow-sync" || cfg.Events.MaxDeliver != 7 {
		t.Errorf("Events.Consumer=%s MaxDeliver=%d", cfg.Events.Consumer, cfg.Events.MaxDeliver)
	}
	if cfg.Mapping.BasicMemberID != "4482" {
		t.Errorf("Mapping.BasicMemberID = %q", cfg.Mapping.BasicMemberID)
	}
}

func TestLoadFromEnvDefaults(t *testing.T) {
	env := baseEnv()
	delete(env, "LOG_LEVEL")
	delete(env, "WRITES_ENABLED")
	delete(env, "DOORFLOW_AUTH_TYPE")
	env["DOORFLOW_TIMEOUT"] = "5s"
	cfg, err := LoadFromEnv(env.get)
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Logger.Level != "info" {
		t.Errorf("Logger.Level default = %q, want info", cfg.Logger.Level)
	}
	if cfg.Writes {
		t.Errorf("Writes default = %v, want false (fail safe)", cfg.Writes)
	}
	if cfg.Doorflow.AuthType != "basic" {
		t.Errorf("Doorflow.AuthType default = %q, want basic", cfg.Doorflow.AuthType)
	}
	if cfg.Doorflow.Timeout.Seconds() != 5 {
		t.Errorf("Doorflow.Timeout = %v, want 5s", cfg.Doorflow.Timeout)
	}
}

func TestLoadFromEnvFailsFastOnMissingRequired(t *testing.T) {
	env := baseEnv()
	delete(env, "DOORFLOW_BASE_URL")
	if _, err := LoadFromEnv(env.get); err == nil {
		t.Fatalf("expected error for missing DOORFLOW_BASE_URL")
	}
}
