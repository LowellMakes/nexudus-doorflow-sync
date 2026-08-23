package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LowellMakes/nexudus-doorflow-sync/internal/events"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/health"
	"github.com/LowellMakes/nexudus-doorflow-sync/internal/reconcile"
)

// dispatchFunc adapts a plain function to the events.Dispatcher interface.
type dispatchFunc func(ctx context.Context, coworkerID int) error

func (f dispatchFunc) Dispatch(ctx context.Context, coworkerID int) error { return f(ctx, coworkerID) }

func dispatchFor(c *components) events.Dispatcher {
	return dispatchFunc(func(ctx context.Context, id int) error {
		rep := c.Eng.SyncOne(ctx, id)
		c.log.Info("event processed", "member", id, "mutated", rep.Mutated, "remediated", rep.Remediated, "err", rep.Err)
		return rep.Err
	})
}

// authFor returns the event-auth verifier. Fail-closed by default (RejectAll);
// a StaticSecret is used when event verification is enabled and a secret is set.
func authFor(c *components) events.AuthVerifier {
	if c.cfg.AuthVerify {
		if sec := os.Getenv("NEXUDUS_WEBHOOK_SECRET"); sec != "" {
			return events.StaticSecret{Secret: sec}
		}
	}
	return events.RejectAll{}
}

// cmdRun is the live service: startup reconcile, periodic sweeps, health, and the
// durable JetStream event consumer (blocks until SIGINT/SIGTERM).
func cmdRun(c *components) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := c.D.ResolveTypes(ctx); err != nil {
		c.log.Error("doorflow credential type discovery failed; aborting (check auth/keys)", "err", err)
		os.Exit(1)
	}
	c.log.Info("engine ready", "writes", !c.Eng.DryRun)

	// Health endpoint.
	var st *health.State
	if c.cfg.HealthHost != "" {
		st = health.NewState()
		go serveHealth(c, st, c.cfg.HealthHost)
	}

	rec := newReconciler(c)

	// Startup reconciliation (task 5.2) — converge the whole membership once.
	go func() {
		p, m := rec.Full(ctx)
		c.log.Info("startup full reconcile done", "processed", p, "mutated", m)
		if st != nil {
			st.MarkConsumerConnected("startup-reconcile")
		}
	}()

	// Scheduled full sweeps (task 5.3).
	go sweepLoop(c, rec, ctx)

	// Event consumer (spec: event-ingestion). Reconcile-only if unconfigured.
	if c.cfg.Events.NatsURL == "" || c.cfg.Events.Subject == "" {
		c.log.Warn("no NATS event stream configured; running reconcile-only mode")
		<-ctx.Done()
		return
	}
	consumer, err := events.NewJetStreamConsumer(events.JetStreamOpts{
		URL: c.cfg.Events.NatsURL, Subject: c.cfg.Events.Subject,
		Consumer: c.cfg.Events.Consumer, AckWait: c.cfg.Events.AckWait, MaxDeliver: c.cfg.Events.MaxDeliver,
		Logger: c.log,
	})
	if err != nil {
		c.log.Error("failed to subscribe to JetStream", "err", err)
		<-ctx.Done()
		return
	}
	if st != nil {
		st.MarkConsumerConnected(c.cfg.Events.Consumer)
	}
	c.log.Info("event consumer connected", "subject", c.cfg.Events.Subject)

	if err := events.New(dispatchFor(c), authFor(c), c.log).Run(ctx, consumer); err != nil {
		c.log.Error("event consumer loop ended", "err", err)
	}
}

func newReconciler(c *components) *reconcile.Reconciler {
	return reconcile.New(
		c.N,
		func(ctx context.Context, id int) (bool, error) { return c.Eng.ProcessMember(ctx, id), nil },
		c.Eng.Commit,
		c.cfg.Reconcile.PageSize, c.cfg.Reconcile.Concurrency, c.log,
	)
}

// sweepLoop runs a full membership sweep on the configured interval (task 5.3).
func sweepLoop(c *components, rec *reconcile.Reconciler, ctx context.Context) {
	interval := c.cfg.Reconcile.FullSweepInterval
	if interval <= 0 {
		return
	}
	first := c.cfg.Reconcile.FirstFullSweepAt
	if first <= 0 {
		first = 1 * time.Minute
	}
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			runSweep(c, rec)
			t.Reset(interval)
		}
	}
}

func runSweep(c *components, rec *reconcile.Reconciler) {
	t := c.cfg.Reconcile.FullSweepInterval
	if t <= 0 {
		t = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), t)
	defer cancel()
	p, m := rec.Full(ctx)
	c.log.Info("full reconcile sweep done", "processed", p, "mutated", m)
}

func serveHealth(c *components, st *health.State, host string) {
	srv := &http.Server{Addr: host, Handler: st, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		c.log.Error("health server", "err", err)
	}
}
