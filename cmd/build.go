package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/config"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/doctor"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/domain"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/doorflow"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/engine"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/mapping"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/nexudusapi"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/remediation"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/store"
)

// components bundles the fully-wired dependencies for the service.
type components struct {
	cfg *config.Config
	log *slog.Logger
	N   *nexudusapi.Client // Nexudus (source of truth)
	D   *doorflow.Client   // Doorflow (target)
	M   *mapping.Mapping   // team -> group
	S   store.Store        // durable PIN back-write guard
	Not domain.Notifier    // member-facing PIN notifications
	Rem *remediation.Remediator
	Eng *engine.Engine
	Doc *doctor.Doctor
}

// build wires the concrete Nexudus + Doorflow clients, mapping, store,
// remediator, engine, and doctor from configuration. It exits on fatal
// configuration errors so the service fails fast at boot.
func build(cfg *config.Config, log *slog.Logger) *components {
	n := nexudusapi.New(cfg.Nexudus.BaseURL, cfg.Nexudus.Username, cfg.Nexudus.Password, log, 30*time.Second)
	d := doorflow.New(cfg.Doorflow.BaseURL, cfg.Doorflow.AuthType, cfg.Doorflow.APIKey, cfg.Doorflow.APIKey2, log, cfg.Doorflow.Timeout)

	m, err := mapping.LoadFromCSV(cfg.Mapping.SourcePath, cfg.Mapping.BasicMemberID)
	if err != nil {
		log.Error("failed to load team-group mapping", "err", err)
		os.Exit(3)
	}

	var s store.Store = store.NewMemory()
	if cfg.Store.Path != "" {
		fs, err := store.NewFile(cfg.Store.Path)
		if err != nil {
			log.Error("failed to open durable store", "path", cfg.Store.Path, "err", err)
			os.Exit(3)
		}
		s = fs
	}

	not := notifier{N: n, log: log, enabled: cfg.NotifyPIN}
	rem := remediation.New(n, d, s, not, log)
	eng := engine.New(n, d, m, rem, log)
	eng.DryRun = !cfg.Writes // write flag off -> zero mutations (7.2)
	doc := doctor.New(n, d, m)

	return &components{cfg: cfg, log: log, N: n, D: d, M: m, S: s, Not: not, Rem: rem, Eng: eng, Doc: doc}
}

// notifier delivers member-facing PIN notifications (log + optional Nexudus
// reminder). It is best-effort and never blocks sync.
type notifier struct {
	N       domain.Nexudus
	log     *slog.Logger
	enabled bool
}

func (nt notifier) NotifyPinChanged(ctx context.Context, coworkerID int, email string) error {
	nt.log.Info("PIN changed/regenerated", "member", coworkerID, "email", email)
	if nt.enabled && nt.N != nil {
		if err := nt.N.SendPinReminder(ctx, coworkerID); err != nil {
			nt.log.Warn("pin reminder failed", "member", coworkerID, "err", err)
		}
	}
	return nil
}

var _ domain.Notifier = notifier{}
