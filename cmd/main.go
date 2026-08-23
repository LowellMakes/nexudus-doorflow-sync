// Command nexudus-doorflow-sync keeps Nexudus membership/credential state in
// sync with Doorflow door access.
//
// Subcommands:
//
//	run    (default) — the live service: durable JetStream event consumer +
//	         scheduled full reconciles + a health/readiness endpoint.
//	doctor — read-only desired-vs-current diff report across the membership
//	         (issues ZERO Doorflow/Nexudus writes).
//	probe  — startup capability probe: resolves the Doorflow credential type
//	         ids and auth and logs them so auth/type issues surface at boot.
package main

import (
	"fmt"
	"os"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/config"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/logging"
)

func main() {
	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	cfg, err := config.LoadFromEnv(os.LookupEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	log := logging.New(os.Stderr, cfg.Logger.Level)
	c := build(cfg, log)
	log.Info("nexudus-doorflow-sync", "cmd", cmd, "writes", cfg.Writes, "dryrun", !c.Eng.DryRun)

	switch cmd {
	case "run", "":
		cmdRun(c)
	case "doctor":
		cmdDoctor(c)
	case "probe":
		cmdProbe(c)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `nexudus-doorflow-sync <cmd>

commands:
  run     the live service (events + reconciles + health)        [default]
  doctor  read-only desired-vs-current diff report, zero writes
  probe   resolve/log Doorflow credential types + auth at boot

configuration is via environment variables (see internal/config).`)
}
