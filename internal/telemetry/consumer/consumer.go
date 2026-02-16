package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// wireEvent (re-declared here to avoid import cycle with internal/telemetry)
// ---------------------------------------------------------------------------

// WireEvent is the JSON payload published to the Redis Stream by Phase 1.
// Matches the schema in internal/telemetry/event.go.
type WireEvent struct {
	V              int    `json:"v"`
	TsMs           int64  `json:"ts_ms"`
	IP             string `json:"ip"`
	Host           string `json:"host"`
	HostRaw        string `json:"host_raw"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	PathRaw        string `json:"path_raw"`
	Status         int    `json:"status"`
	Decision       string `json:"decision"`
	EngineDecision string `json:"engine_decision"`
}

// ---------------------------------------------------------------------------
// Flush thresholds
// ---------------------------------------------------------------------------

const (
	flushPendingIDThreshold = 10_000
	flushMapSizeThreshold   = 50_000
	ackChunkSize            = 1000
	shutdownFlushTimeout    = 2 * time.Second
)

// ---------------------------------------------------------------------------
// Consumer
// ---------------------------------------------------------------------------

// Consumer reads from a Redis Stream consumer group, aggregates events into
// rollup buckets, and flushes them to MariaDB.
type Consumer struct {
	cfg      TelemetryConsumerConfig
	rdb      *redis.Client
	rollupDB *RollupDB
	agg      *Aggregator
	logger   zerolog.Logger

	stopOnce sync.Once
	stopCh   chan struct{}

	// Rate limiter for malformed event logging (1 per 10s).
	lastMalformedLog time.Time
}

// NewConsumer creates a Consumer. Call Run() to start consuming.
func NewConsumer(cfg TelemetryConsumerConfig, rdb *redis.Client, rollupDB *RollupDB, logger zerolog.Logger) *Consumer {
	return &Consumer{
		cfg:      cfg,
		rdb:      rdb,
		rollupDB: rollupDB,
		agg:      NewAggregator(cfg.PathCap),
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
}

// ---------------------------------------------------------------------------
// Consumer group setup
// ---------------------------------------------------------------------------

// EnsureGroup creates the consumer group if it does not exist.
// Uses MKSTREAM to create the stream if it is also absent.
func (c *Consumer) EnsureGroup(ctx context.Context) error {
	err := c.rdb.XGroupCreateMkStream(ctx, c.cfg.StreamKey, c.cfg.ConsumerGroup, c.cfg.GroupStartID).Err()
	if err != nil {
		// BUSYGROUP means the group already exists — not an error.
		if strings.Contains(err.Error(), "BUSYGROUP") {
			c.logger.Info().
				Str("group", c.cfg.ConsumerGroup).
				Msg("telemetry_consumer.group_created: already exists")
			return nil
		}
		return fmt.Errorf("XGROUP CREATE: %w", err)
	}
	c.logger.Info().
		Str("group", c.cfg.ConsumerGroup).
		Str("start_id", c.cfg.GroupStartID).
		Msg("telemetry_consumer.group_created: new group")
	return nil
}

// ---------------------------------------------------------------------------
// PEL claim (optional)
// ---------------------------------------------------------------------------

// ClaimPending uses XAUTOCLAIM to reclaim stale messages from the PEL.
// Only called if PELClaimEnabled is true.
func (c *Consumer) ClaimPending(ctx context.Context) error {
	if !c.cfg.PELClaimEnabled {
		return nil
	}

	idleMs := time.Duration(c.cfg.PELIdleMs) * time.Millisecond

	c.logger.Info().
		Dur("idle_threshold", idleMs).
		Int("count", c.cfg.PELClaimCount).
		Msg("telemetry_consumer: claiming stale PEL entries")

	msgs, _, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   c.cfg.StreamKey,
		Group:    c.cfg.ConsumerGroup,
		Consumer: c.cfg.ConsumerName,
		MinIdle:  idleMs,
		Start:    "0-0",
		Count:    int64(c.cfg.PELClaimCount),
	}).Result()
	if err != nil {
		return fmt.Errorf("XAUTOCLAIM: %w", err)
	}

	if len(msgs) > 0 {
		c.logger.Info().Int("claimed", len(msgs)).Msg("telemetry_consumer: processing claimed PEL entries")
		c.processMessages(msgs)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Main loop
// ---------------------------------------------------------------------------

// Run enters the main consumer loop. It blocks until Shutdown() is called
// or the context is cancelled.
func (c *Consumer) Run(ctx context.Context) {
	c.logger.Info().
		Str("stream", c.cfg.StreamKey).
		Str("group", c.cfg.ConsumerGroup).
		Str("consumer", c.cfg.ConsumerName).
		Int("read_count", c.cfg.ReadCount).
		Int("block_ms", c.cfg.BlockMs).
		Int("flush_ms", c.cfg.FlushMs).
		Int("path_cap", c.cfg.PathCap).
		Msg("telemetry_consumer.started")

	flushInterval := time.Duration(c.cfg.FlushMs) * time.Millisecond
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	blockDur := time.Duration(c.cfg.BlockMs) * time.Millisecond

	for {
		select {
		case <-c.stopCh:
			c.finalFlush()
			return
		case <-ctx.Done():
			c.finalFlush()
			return
		default:
		}

		// XREADGROUP — blocking read with short timeout
		streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.cfg.ConsumerGroup,
			Consumer: c.cfg.ConsumerName,
			Streams:  []string{c.cfg.StreamKey, ">"},
			Count:    int64(c.cfg.ReadCount),
			Block:    blockDur,
		}).Result()

		if err != nil && !errors.Is(err, redis.Nil) {
			// redis.Nil means timeout with no messages — normal
			if ctx.Err() != nil {
				c.finalFlush()
				return
			}
			c.logger.Warn().Err(err).Msg("telemetry_consumer: XREADGROUP error")
			time.Sleep(500 * time.Millisecond) // back off briefly
			continue
		}

		// Process received messages
		for _, stream := range streams {
			c.processMessages(stream.Messages)
		}

		// Check flush triggers
		if c.shouldFlush(ticker) {
			c.flush(ctx)
		}
	}
}

// shouldFlush returns true if any flush trigger is met.
// It also drains the ticker channel if it has fired.
func (c *Consumer) shouldFlush(ticker *time.Ticker) bool {
	// Size-based triggers
	if c.agg.PendingCount() >= flushPendingIDThreshold {
		return true
	}
	if c.agg.Size() >= flushMapSizeThreshold {
		return true
	}
	// Time-based trigger
	select {
	case <-ticker.C:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Event parsing
// ---------------------------------------------------------------------------

// ParseStreamEvent extracts and validates the wire event from a Redis stream message.
// Returns (event, nil) on success or (zero, error) for malformed messages.
func ParseStreamEvent(values map[string]interface{}) (WireEvent, error) {
	rawEvent, ok := values["event"]
	if !ok {
		return WireEvent{}, fmt.Errorf("missing 'event' field")
	}

	var eventStr string
	switch v := rawEvent.(type) {
	case string:
		eventStr = v
	case []byte:
		eventStr = string(v)
	default:
		return WireEvent{}, fmt.Errorf("'event' field has unexpected type %T", rawEvent)
	}

	var evt WireEvent
	if err := json.Unmarshal([]byte(eventStr), &evt); err != nil {
		return WireEvent{}, fmt.Errorf("JSON unmarshal: %w", err)
	}

	if evt.V != 1 {
		return WireEvent{}, fmt.Errorf("unsupported event version %d (want 1)", evt.V)
	}

	return evt, nil
}

// processMessages parses and aggregates a batch of stream messages.
// Malformed messages are ACKed immediately and counted.
func (c *Consumer) processMessages(msgs []redis.XMessage) {
	var malformedIDs []string

	for _, msg := range msgs {
		evt, err := ParseStreamEvent(msg.Values)
		if err != nil {
			c.agg.DroppedMalformed++
			malformedIDs = append(malformedIDs, msg.ID)

			// Rate-limited sample log (1 per 10s)
			now := time.Now()
			if now.Sub(c.lastMalformedLog) >= 10*time.Second {
				c.lastMalformedLog = now
				c.logger.Warn().
					Err(err).
					Str("stream_id", msg.ID).
					Int64("total_malformed", c.agg.DroppedMalformed).
					Msg("telemetry_consumer: malformed event (sample)")
			}
			continue
		}

		bucket := BucketStart(evt.TsMs)
		c.agg.Add(bucket, evt.Host, evt.IP, evt.Method, evt.Path, evt.Status)
		c.agg.TrackID(msg.ID)
	}

	// ACK malformed messages immediately to prevent poison-pill loops
	if len(malformedIDs) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.rdb.XAck(ctx, c.cfg.StreamKey, c.cfg.ConsumerGroup, malformedIDs...).Err(); err != nil {
			c.logger.Warn().Err(err).Int("count", len(malformedIDs)).
				Msg("telemetry_consumer: failed to ACK malformed messages")
		}
	}
}

// ---------------------------------------------------------------------------
// Flush + ACK
// ---------------------------------------------------------------------------

func (c *Consumer) flush(ctx context.Context) {
	snap := c.agg.Snapshot()
	if len(snap.HostIP) == 0 && len(snap.HostIPPath) == 0 {
		return
	}

	start := time.Now()

	if err := c.rollupDB.Flush(ctx, snap); err != nil {
		c.logger.Error().Err(err).
			Int("host_ip_rows", len(snap.HostIP)).
			Int("path_rows", len(snap.HostIPPath)).
			Msg("telemetry_consumer.flush_error")

		// Put the data back — we'll retry next cycle.
		// In practice this means we lose this snapshot's data if it keeps failing,
		// but at-least-once semantics means the messages remain pending in PEL.
		time.Sleep(1 * time.Second)
		return
	}

	flushDur := time.Since(start)

	c.logger.Info().
		Int("host_ip_rows", len(snap.HostIP)).
		Int("path_rows", len(snap.HostIPPath)).
		Dur("duration", flushDur).
		Int64("dropped_paths", snap.DroppedPaths).
		Int64("dropped_malformed", snap.DroppedMalformed).
		Msg("telemetry_consumer.flush")

	// ACK in chunks
	c.ackIDs(ctx, snap.PendingIDs)
}

func (c *Consumer) ackIDs(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}

	chunks := 0
	for i := 0; i < len(ids); i += ackChunkSize {
		end := i + ackChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]

		if err := c.rdb.XAck(ctx, c.cfg.StreamKey, c.cfg.ConsumerGroup, chunk...).Err(); err != nil {
			c.logger.Warn().Err(err).Int("chunk_size", len(chunk)).
				Msg("telemetry_consumer: XACK error")
		}
		chunks++
	}

	c.logger.Debug().
		Int("total", len(ids)).
		Int("chunks", chunks).
		Msg("telemetry_consumer.ack")
}

// ---------------------------------------------------------------------------
// Shutdown
// ---------------------------------------------------------------------------

// Shutdown signals the consumer to stop and performs a final flush.
func (c *Consumer) Shutdown() {
	c.stopOnce.Do(func() {
		c.logger.Info().Msg("telemetry_consumer.shutdown: initiated")
		close(c.stopCh)
	})
}

// finalFlush attempts one last flush with a hard timeout.
func (c *Consumer) finalFlush() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
	defer cancel()

	snap := c.agg.Snapshot()
	if len(snap.HostIP) == 0 && len(snap.HostIPPath) == 0 {
		c.logger.Info().Msg("telemetry_consumer.shutdown: no buffered data to flush")
		return
	}

	c.logger.Info().
		Int("host_ip_rows", len(snap.HostIP)).
		Int("path_rows", len(snap.HostIPPath)).
		Int("pending_ids", len(snap.PendingIDs)).
		Msg("telemetry_consumer.shutdown: final flush attempt")

	if err := c.rollupDB.Flush(ctx, snap); err != nil {
		c.logger.Error().Err(err).Msg("telemetry_consumer.shutdown: final flush failed")
		return
	}

	c.ackIDs(ctx, snap.PendingIDs)
	c.logger.Info().Msg("telemetry_consumer.shutdown: final flush complete")
}
