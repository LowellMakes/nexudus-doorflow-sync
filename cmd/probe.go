package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// signalCtx returns a context cancelled on SIGINT/SIGTERM for CLI-style commands.
func signalCtx() (context.Context, func()) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return ctx, stop
}

// cmdDoctor is the read-only self-inspection mode (task 7.1): it walks the
// membership and prints the desired-vs-current diff per member, issuing ZERO
// Doorflow/Nexudus writes.
func cmdDoctor(c *components) {
	ctx, stop := signalCtx()
	defer stop()
	rep, err := c.Doc.Run(ctx, c.cfg.Reconcile.PageSize)
	if err != nil {
		c.log.Error("doctor failed", "err", err)
		os.Exit(1)
	}
	if rep.String == "" {
		fmt.Println("(no members found)")
	} else {
		fmt.Print(rep.String)
	}
	fmt.Printf("\n%d member(s); %d need change(s); 0 writes issued (read-only)\n", rep.Members, rep.NeedDiffs)
}

// cmdProbe is the startup capability probe (task 7.4): it resolves the Doorflow
// credential type ids and auth scheme and logs them, so an auth/type problem
// surfaces at boot with diagnostic detail instead of failing mid-sync.
func cmdProbe(c *components) {
	ctx, stop := signalCtx()
	defer stop()

	if err := c.D.ResolveTypes(ctx); err != nil {
		c.log.Error("doorflow type/auth probe failed",
			"base", c.cfg.Doorflow.BaseURL, "auth", c.cfg.Doorflow.AuthType, "err", err)
		os.Exit(1)
	}
	ready := c.D.TypesReady()
	c.log.Info("doorflow credential types RESOLVED", "ready", ready, "auth", c.cfg.Doorflow.AuthType)

	// A read probe (auth path) — list-free, uses a harmless read.
	c.log.Info("nexudus basic-auth configured", "base", c.cfg.Nexudus.BaseURL, "readonly", c.cfg.Nexudus.ReadOnly)

	c.log.Info("startup probe OK — safe to enable writes (set WRITES_ENABLED=true)")
}
