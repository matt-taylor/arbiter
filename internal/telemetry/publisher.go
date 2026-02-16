package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/domostack/arbiter/internal/config"
)

// Publisher is the interface for telemetry event emission.
// Implementations must be safe for concurrent use.
type Publisher interface {
	// Emit enqueues a telemetry event. Must be non-blocking.
	// If the internal buffer is full the event is silently dropped.
	Emit(e Event)

	// Close signals the background worker to stop and releases resources.
	// It does NOT drain buffered events (best-effort semantics).
	Close()
}

// RedisStreamer abstracts the subset of *redis.Client needed by the publisher.
// This enables testing with a fake implementation.
type RedisStreamer interface {
	XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd
	Close() error
}

// ---------------------------------------------------------------------------
// NoopPublisher
// ---------------------------------------------------------------------------

// NoopPublisher discards all events. Returned when telemetry is disabled or
// initialization fails. All methods are safe to call on a nil *NoopPublisher.
type NoopPublisher struct{}

func (NoopPublisher) Emit(Event) {}
func (NoopPublisher) Close()     {}

// ---------------------------------------------------------------------------
// RedisPublisher
// ---------------------------------------------------------------------------

// RedisPublisher publishes telemetry events to a Redis Stream via a buffered
// channel and single background worker goroutine.
type RedisPublisher struct {
	events    chan Event
	stop      chan struct{}
	client    RedisStreamer
	streamKey string
	timeoutMs int
	logger    zerolog.Logger
	dropped   atomic.Int64
}

// NewPublisher creates a Publisher based on the supplied configuration.
//
// If telemetry is disabled (cfg.Enabled == false) a NoopPublisher is returned.
// If the Redis URL is invalid, uses rediss://, or the initial PING fails,
// a warning is logged and a NoopPublisher is returned.
// Telemetry misconfiguration NEVER prevents Arbiter from starting.
func NewPublisher(cfg config.TelemetryConfig, logger zerolog.Logger) Publisher {
	if !cfg.Enabled {
		logger.Info().Msg("telemetry disabled")
		return NoopPublisher{}
	}

	// Reject rediss:// (TLS) for Phase 1
	if strings.HasPrefix(cfg.RedisURL, "rediss://") {
		logger.Warn().Str("url", cfg.RedisURL).
			Msg("telemetry: rediss:// (TLS) not supported in Phase 1; falling back to noop")
		return NoopPublisher{}
	}

	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Warn().Err(err).Str("url", cfg.RedisURL).
			Msg("telemetry: failed to parse Redis URL; falling back to noop")
		return NoopPublisher{}
	}

	client := redis.NewClient(opts)

	// Best-effort PING — if it fails we degrade gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		logger.Warn().Err(err).
			Msg("telemetry: Redis PING failed at startup; falling back to noop")
		_ = client.Close()
		return NoopPublisher{}
	}

	return newRedisPublisher(client, cfg, logger)
}

// newRedisPublisher constructs a RedisPublisher from an already-validated
// RedisStreamer. Exported only for testing via the internal package.
func newRedisPublisher(client RedisStreamer, cfg config.TelemetryConfig, logger zerolog.Logger) *RedisPublisher {
	bufSize := cfg.BufferSize
	if bufSize <= 0 {
		bufSize = 8192
	}

	p := &RedisPublisher{
		events:    make(chan Event, bufSize),
		stop:      make(chan struct{}),
		client:    client,
		streamKey: cfg.StreamKey,
		timeoutMs: cfg.TimeoutMs,
		logger:    logger,
	}

	go p.run()

	logger.Info().
		Str("stream", cfg.StreamKey).
		Int("buffer", bufSize).
		Int("timeout_ms", cfg.TimeoutMs).
		Msg("telemetry publisher started")

	return p
}

// Emit performs a non-blocking send. If the buffer is full the event is dropped.
func (p *RedisPublisher) Emit(e Event) {
	select {
	case p.events <- e:
	default:
		count := p.dropped.Add(1)
		// Log every 1000 drops to avoid log spam
		if count%1000 == 1 {
			p.logger.Warn().Int64("total_dropped", count).
				Msg("telemetry: event channel full, dropping events")
		}
	}
}

// Close signals the worker to stop and closes the Redis client.
// Does NOT close the events channel. Does NOT drain buffered events.
func (p *RedisPublisher) Close() {
	close(p.stop)
	if err := p.client.Close(); err != nil {
		p.logger.Warn().Err(err).Msg("telemetry: error closing Redis client")
	}
	if dropped := p.dropped.Load(); dropped > 0 {
		p.logger.Warn().Int64("total_dropped", dropped).
			Msg("telemetry: total events dropped during lifetime")
	}
}

// run is the background worker loop. It selects on stop vs events.
// When stop is closed, the worker returns immediately (no drain).
func (p *RedisPublisher) run() {
	for {
		select {
		case <-p.stop:
			return
		case ev := <-p.events:
			p.process(ev)
		}
	}
}

// process normalizes, JSON-encodes, and XADDs a single event.
func (p *RedisPublisher) process(ev Event) {
	wire := wireEvent{
		V:              1,
		TsMs:           ev.Time.UnixMilli(),
		IP:             ev.IP,
		Host:           NormalizeHost(ev.HostRaw),
		HostRaw:        ev.HostRaw,
		Method:         ev.Method,
		Path:           NormalizePath(ev.PathRaw),
		PathRaw:        ev.PathRaw,
		Status:         ev.Status,
		Decision:       mapDecision(ev.EngineDecision),
		EngineDecision: ev.EngineDecision,
	}

	data, err := json.Marshal(wire)
	if err != nil {
		p.logger.Error().Err(err).Msg("telemetry: failed to marshal event")
		return
	}

	timeout := time.Duration(p.timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 25 * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.streamKey,
		ID:     "*",
		Values: map[string]interface{}{
			"event": string(data),
		},
	}).Err(); err != nil {
		p.logger.Warn().Err(err).Msg("telemetry: XADD failed")
	}
}
