// Package config defines the service configuration, loaded exclusively from the
// environment / service secrets. No secrets are stored in the repo; the repo is
// treated as publicly readable, so every credential comes in via env.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Logger describes structured-log settings.
type Logger struct {
	Level string `json:"level"`
}

// NexudusConfig holds the connection + credential for the Nexudus API.
type NexudusConfig struct {
	BaseURL  string
	Username string // basic-auth username (service key)
	Password string // basic-auth password / api key
	ReadOnly bool   // disable all Nexudus mutations (default true until write is verified)
}

// DoorflowConfig holds both Doorflow hosts (reads vs writes) and the auth scheme.
type DoorflowConfig struct {
	BaseURL  string        // api.doorflow.com (writes / credential CRUD)
	AdminURL string        // admin.doorflow.com (reads, e.g. person lookup)
	AuthType string        // "basic" | "bearer"
	APIKey   string        // basic username or bearer token
	APIKey2  string        // basic password (empty for bearer)
	Timeout  time.Duration // per-request client timeout
}

// MappingConfig locates the team→group mapping source.
type MappingConfig struct {
	SourcePath    string // path to team_group_map.csv (nexudus_id,doorflow_id)
	BasicMemberID string // the required BasicMember doorflow group id (e.g. 4482)
}

// ReconcileConfig controls the reconciliation sweeps.
type ReconcileConfig struct {
	FullSweepInterval time.Duration // interval between full-membership sweeps
	FirstFullSweepAt  time.Duration // delay before the first sweep after start
	PageSize          int           // page size for paged listing (no 50-cap)
	Concurrency       int           // max concurrent syncOneCoworker workers in a sweep
	Clock             Clock         // clock used for the sweep scheduler (testable)
}

// EventsConfig configures the JetStream consumer.
type EventsConfig struct {
	NatsURL    string
	Stream     string
	Subject    string // webhook subject pattern published by the http-nats-proxy
	Consumer   string
	AckWait    time.Duration
	MaxAckWait time.Duration
	MaxDeliver int // bounded max-redeliveries before dead-letter
	PullBatch  int
}

// StoreConfig locates the durable pending-back-write ledger.
type StoreConfig struct {
	Path string // file path for the durable store (empty = in-memory only)
}

// Config is the top-level service configuration.
type Config struct {
	Logger     Logger
	Nexudus    NexudusConfig
	Doorflow   DoorflowConfig
	Mapping    MappingConfig
	Reconcile  ReconcileConfig
	Events     EventsConfig
	Store      StoreConfig
	Writes     bool   `json:"writes_enabled"`
	NotifyPIN  bool   `json:"pin_notification_enabled"`
	AuthVerify bool   // when true, events bearing a signature MUST verify
	HealthHost string // host:port for /healthz /readyz ("" disables)
}

// Clock is the subset of time used by scheduling logic, so tests can control it.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) *time.Ticker
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

// RealClock is the production Clock implementation.
func RealClock() Clock { return realClock{} }

// EnvFunc abstracts environment access for testability.
type EnvFunc func(string) (string, bool)

// osEnv reads from the real process environment.
func osEnv(key string) (string, bool) { return os.LookupEnv(key) }

// LoadFromEnv builds a Config from the given EnvFunc, applying safe defaults.
// Required values that are missing produce an error so the service fails fast at
// boot rather than silently running misconfigured.
func LoadFromEnv(env EnvFunc) (*Config, error) {
	env = orDefault(env, osEnv)
	cfg := &Config{}

	cfg.Logger.Level = orDefaultStr(env, "LOG_LEVEL", "info")
	cfg.Writes = envBool(env, "WRITES_ENABLED", false)
	cfg.NotifyPIN = envBool(env, "NOTIFY_PIN", true)
	cfg.AuthVerify = envBool(env, "ENFORCE_EVENT_AUTH", true)

	cfg.Nexudus.BaseURL = require(env, "NEXUDUS_BASE_URL")
	cfg.Nexudus.Username = envStr(env, "NEXUDUS_USERNAME", "")
	cfg.Nexudus.Password = envStr(env, "NEXUDUS_PASSWORD", "")
	cfg.Nexudus.ReadOnly = !cfg.Writes // single master kill-switch for all writes (task 7.2)

	d, err := time.ParseDuration(orDefaultStr(env, "DOORFLOW_TIMEOUT", "30s"))
	if err != nil {
		d = 30 * time.Second
	}
	cfg.Doorflow.BaseURL = require(env, "DOORFLOW_BASE_URL")
	cfg.Doorflow.AdminURL = orDefaultStr(env, "DOORFLOW_ADMIN_URL", cfg.Doorflow.BaseURL)
	cfg.Doorflow.AuthType = orDefaultStr(env, "DOORFLOW_AUTH_TYPE", "basic")
	cfg.Doorflow.APIKey = envStr(env, "DOORFLOW_API_KEY", "")
	cfg.Doorflow.APIKey2 = envStr(env, "DOORFLOW_API_KEY2", "")
	cfg.Doorflow.Timeout = d

	cfg.Mapping.SourcePath = require(env, "TEAM_GROUP_MAP")
	cfg.Mapping.BasicMemberID = require(env, "TEAM_GROUP_MAP_BASIC_MEMBER")

	ri, err := time.ParseDuration(orDefaultStr(env, "RECONCILE_FULL_SWEEP_INTERVAL", "6h"))
	if err != nil {
		ri = 6 * time.Hour
	}
	fw, err := time.ParseDuration(orDefaultStr(env, "RECONCILE_FIRST_SWEEP_AT", "60s"))
	if err != nil {
		fw = 60 * time.Second
	}
	cfg.Reconcile.FullSweepInterval = ri
	cfg.Reconcile.FirstFullSweepAt = fw
	cfg.Reconcile.PageSize = envInt(env, "RECONCILE_PAGE_SIZE", 200)
	cfg.Reconcile.Concurrency = envInt(env, "RECONCILE_CONCURRENCY", 4)
	cfg.Reconcile.Clock = RealClock()

	cfg.Events.NatsURL = require(env, "NATS_URL")
	cfg.Events.Stream = orDefaultStr(env, "NATS_STREAM", "NEXUDUS_WEBHOOKS")
	cfg.Events.Subject = orDefaultStr(env, "NATS_SUBJECT", "nexudus.webhook.*")
	cfg.Events.Consumer = orDefaultStr(env, "NATS_CONSUMER", "doorflow-sync")
	aw, _ := time.ParseDuration(orDefaultStr(env, "NATS_ACK_WAIT", "30s"))
	cfg.Events.AckWait = aw
	mw, _ := time.ParseDuration(orDefaultStr(env, "NATS_MAX_ACK_WAIT", "10m"))
	cfg.Events.MaxAckWait = mw
	cfg.Events.MaxDeliver = envInt(env, "NATS_MAX_DELIVER", 5)
	cfg.Events.PullBatch = envInt(env, "NATS_PULL_BATCH", 10)

	cfg.Store.Path = envStr(env, "STORE_PATH", "")
	cfg.HealthHost = orDefaultStr(env, "HEALTH_HOST", ":9464")

	// Invariants that must fail fast.
	if cfg.Doorflow.BaseURL == "" {
		return nil, fmt.Errorf("DOORFLOW_BASE_URL is required")
	}
	if cfg.Nexudus.BaseURL == "" {
		return nil, fmt.Errorf("NEXUDUS_BASE_URL is required")
	}
	if cfg.Events.NatsURL == "" {
		return nil, fmt.Errorf("NATS_URL is required")
	}
	return cfg, nil
}

func orDefault(env EnvFunc, fallback EnvFunc) EnvFunc {
	if env == nil {
		return fallback
	}
	return env
}

func envStr(env EnvFunc, key, def string) string {
	if v, ok := env(key); ok && v != "" {
		return v
	}
	return def
}

func orDefaultStr(env EnvFunc, key, def string) string {
	if v, ok := env(key); ok && v != "" {
		return v
	}
	return def
}

func require(env EnvFunc, key string) string {
	v, ok := env(key)
	if !ok || v == "" {
		return ""
	}
	return v
}

func envBool(env EnvFunc, key string, def bool) bool {
	v, ok := env(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(env EnvFunc, key string, def int) int {
	v, ok := env(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
