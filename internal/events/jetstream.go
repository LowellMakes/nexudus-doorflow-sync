package events

import (
	"context"
	"log/slog"
	"time"

	nats "github.com/nats-io/nats.go"
)

// JetStreamOpts configure the concrete durable consumer (spec: event-ingestion).
type JetStreamOpts struct {
	URL           string
	Subject       string
	Consumer      string        // durable consumer name
	AckWait       time.Duration // redelivery wait; <=0 -> 30s
	MaxDeliver    int           // bounded attempts; <=0 -> 5
	MaxAckPending int           // <=0 -> 256
	Timeout       time.Duration // next-message wait; <=0 -> 10s
	ConnectOpts   []nats.Option
	Logger        *slog.Logger
}

// NewJetStreamConsumer builds the concrete durable JetStream consumer. This is
// the at-least-once transport: a durable consumer replays unacked messages on
// restart (spec: replay across downtime), and bounded MaxDeliver + the processor
// loop provide the dead-letter path. Verified by a local run (tasks 4.1, 4.4).
func NewJetStreamConsumer(o JetStreamOpts) (Consumer, error) {
	if o.AckWait <= 0 {
		o.AckWait = 30 * time.Second
	}
	if o.MaxDeliver <= 0 {
		o.MaxDeliver = 5
	}
	if o.MaxAckPending <= 0 {
		o.MaxAckPending = 256
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.Consumer == "" {
		o.Consumer = "nexudus-doorflow-sync"
	}
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	nc, err := nats.Connect(o.URL, o.ConnectOpts...)
	if err != nil {
		return nil, err
	}
	jsc, err := nc.JetStream()
	if err != nil {
		return nil, err
	}
	sub, err := jsc.SubscribeSync(o.Subject,
		nats.Durable(o.Consumer),
		nats.ManualAck(),
		nats.AckWait(o.AckWait),
		nats.MaxDeliver(o.MaxDeliver),
		nats.MaxAckPending(o.MaxAckPending),
	)
	if err != nil {
		return nil, err
	}
	return &jsConsumer{sub: sub, log: log, timeout: o.Timeout}, nil
}

// jsConsumer is the concrete events.Consumer (synchronous pull durable).
type jsConsumer struct {
	sub     *nats.Subscription
	log     *slog.Logger
	timeout time.Duration
	cur     *nats.Msg
}

func (c *jsConsumer) Next(ctx context.Context) (Message, error) {
	m, err := c.sub.NextMsg(c.timeout)
	if err != nil {
		return Message{}, err
	}
	_ = ctx
	c.cur = m
	hdrs := map[string]string{}
	for k, vs := range m.Header {
		if len(vs) > 0 {
			hdrs[k] = vs[0]
		}
	}
	return Message{Subject: m.Subject, Headers: hdrs, Data: m.Data}, nil
}

func (c *jsConsumer) Ack(Message, uint64) error {
	if c.cur != nil {
		return c.cur.Ack()
	}
	return nil
}

func (c *jsConsumer) Term(Message, uint64) error {
	if c.cur != nil {
		return c.cur.Term()
	}
	return nil
}
