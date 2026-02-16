package telemetry

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/domostack/arbiter/internal/config"
)

// ---------------------------------------------------------------------------
// Fake RedisStreamer
// ---------------------------------------------------------------------------

type fakeRedisStreamer struct {
	mu     sync.Mutex
	calls  []fakeXAddCall
	closed bool
}

type fakeXAddCall struct {
	Stream string
	Values interface{}
}

func (f *fakeRedisStreamer) XAdd(_ context.Context, a *redis.XAddArgs) *redis.StringCmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeXAddCall{
		Stream: a.Stream,
		Values: a.Values,
	})
	cmd := redis.NewStringCmd(context.Background())
	cmd.SetVal("1234567890-0")
	return cmd
}

func (f *fakeRedisStreamer) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeRedisStreamer) getCalls() []fakeXAddCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]fakeXAddCall, len(f.calls))
	copy(cp, f.calls)
	return cp
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestNoopPublisher_DoesNotPanic(t *testing.T) {
	var p NoopPublisher
	p.Emit(Event{})
	p.Close()
	// If we get here, no panic.
}

func TestRedisPublisher_EventFlowsToXADD(t *testing.T) {
	fake := &fakeRedisStreamer{}
	logger := zerolog.Nop()
	cfg := config.TelemetryConfig{
		Enabled:    true,
		StreamKey:  "test:events",
		TimeoutMs:  100,
		BufferSize: 16,
	}

	pub := newRedisPublisher(fake, cfg, logger)

	ev := Event{
		IP:             "1.2.3.4",
		HostRaw:        "WWW.Example.COM",
		Method:         "GET",
		PathRaw:        "/api/v1/users/42",
		Status:         200,
		EngineDecision: "allow",
		Time:           time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
	}

	pub.Emit(ev)

	// Wait for worker to process
	time.Sleep(100 * time.Millisecond)

	calls := fake.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 XADD call, got %d", len(calls))
	}

	call := calls[0]
	if call.Stream != "test:events" {
		t.Errorf("stream = %q, want %q", call.Stream, "test:events")
	}

	valuesMap, ok := call.Values.(map[string]interface{})
	if !ok {
		t.Fatalf("expected Values to be map[string]interface{}, got %T", call.Values)
	}
	rawEvent, ok := valuesMap["event"].(string)
	if !ok {
		t.Fatalf("expected event value to be string, got %T", valuesMap["event"])
	}

	var wire wireEvent
	if err := json.Unmarshal([]byte(rawEvent), &wire); err != nil {
		t.Fatalf("failed to unmarshal event JSON: %v", err)
	}

	// Verify fields
	if wire.V != 1 {
		t.Errorf("v = %d, want 1", wire.V)
	}
	if wire.IP != "1.2.3.4" {
		t.Errorf("ip = %q, want %q", wire.IP, "1.2.3.4")
	}
	if wire.Host != "example.com" {
		t.Errorf("host = %q, want %q", wire.Host, "example.com")
	}
	if wire.HostRaw != "WWW.Example.COM" {
		t.Errorf("host_raw = %q, want %q", wire.HostRaw, "WWW.Example.COM")
	}
	if wire.Method != "GET" {
		t.Errorf("method = %q, want %q", wire.Method, "GET")
	}
	if wire.Path != "/api/v1/users/:id" {
		t.Errorf("path = %q, want %q", wire.Path, "/api/v1/users/:id")
	}
	if wire.PathRaw != "/api/v1/users/42" {
		t.Errorf("path_raw = %q, want %q", wire.PathRaw, "/api/v1/users/42")
	}
	if wire.Status != 200 {
		t.Errorf("status = %d, want 200", wire.Status)
	}
	if wire.Decision != "allow" {
		t.Errorf("decision = %q, want %q", wire.Decision, "allow")
	}
	if wire.EngineDecision != "allow" {
		t.Errorf("engine_decision = %q, want %q", wire.EngineDecision, "allow")
	}
	if wire.TsMs != ev.Time.UnixMilli() {
		t.Errorf("ts_ms = %d, want %d", wire.TsMs, ev.Time.UnixMilli())
	}

	pub.Close()
}

func TestRedisPublisher_DecisionMapping(t *testing.T) {
	tests := []struct {
		engine  string
		wantDec string
		wantEng string
	}{
		{"allow", "allow", "allow"},
		{"unauth", "deny", "unauth"},
		{"forbid", "deny", "forbid"},
		{"killswitch", "deny", "killswitch"},
		{"error", "error", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.engine, func(t *testing.T) {
			fake := &fakeRedisStreamer{}
			logger := zerolog.Nop()
			cfg := config.TelemetryConfig{
				Enabled:    true,
				StreamKey:  "test:events",
				TimeoutMs:  100,
				BufferSize: 16,
			}

			pub := newRedisPublisher(fake, cfg, logger)

			pub.Emit(Event{
				HostRaw:        "example.com",
				Method:         "GET",
				PathRaw:        "/",
				Status:         200,
				EngineDecision: tt.engine,
				Time:           time.Now(),
			})

			time.Sleep(100 * time.Millisecond)

			calls := fake.getCalls()
			if len(calls) != 1 {
				t.Fatalf("expected 1 XADD call, got %d", len(calls))
			}

			rawEvent := calls[0].Values.(map[string]interface{})["event"].(string)
			var wire wireEvent
			json.Unmarshal([]byte(rawEvent), &wire)

			if wire.Decision != tt.wantDec {
				t.Errorf("decision = %q, want %q", wire.Decision, tt.wantDec)
			}
			if wire.EngineDecision != tt.wantEng {
				t.Errorf("engine_decision = %q, want %q", wire.EngineDecision, tt.wantEng)
			}

			pub.Close()
		})
	}
}

func TestRedisPublisher_ChannelFull_DropsWithoutBlocking(t *testing.T) {
	fake := &fakeRedisStreamer{}
	logger := zerolog.Nop()
	cfg := config.TelemetryConfig{
		Enabled:    true,
		StreamKey:  "test:events",
		TimeoutMs:  100,
		BufferSize: 2, // Tiny buffer
	}

	pub := newRedisPublisher(fake, cfg, logger)

	// Stop the worker immediately so events pile up in the channel
	close(pub.stop)
	time.Sleep(50 * time.Millisecond)

	// Fill the buffer (2) + emit extra that should be dropped
	for i := 0; i < 10; i++ {
		pub.Emit(Event{
			HostRaw:        "example.com",
			Method:         "GET",
			PathRaw:        "/",
			Status:         200,
			EngineDecision: "allow",
			Time:           time.Now(),
		})
	}

	// Should not block. If we got here, non-blocking send works.
	dropped := pub.dropped.Load()
	if dropped == 0 {
		t.Error("expected some events to be dropped, got 0")
	}
	if dropped < 8 {
		// Buffer is 2, so at least 8 of 10 should be dropped
		t.Errorf("expected at least 8 dropped, got %d", dropped)
	}

	// Clean up (Close on an already-stopped publisher should not panic)
	_ = pub.client.Close()
}

func TestRedisPublisher_Close_StopsWorker_NoDrain(t *testing.T) {
	fake := &fakeRedisStreamer{}
	logger := zerolog.Nop()
	cfg := config.TelemetryConfig{
		Enabled:    true,
		StreamKey:  "test:events",
		TimeoutMs:  100,
		BufferSize: 1024,
	}

	pub := newRedisPublisher(fake, cfg, logger)

	// Emit many events
	for i := 0; i < 100; i++ {
		pub.Emit(Event{
			HostRaw:        "example.com",
			Method:         "GET",
			PathRaw:        "/",
			Status:         200,
			EngineDecision: "allow",
			Time:           time.Now(),
		})
	}

	// Close immediately — should NOT drain all 100 events
	pub.Close()

	// Give a tiny bit of time for worker to have exited
	time.Sleep(50 * time.Millisecond)

	calls := fake.getCalls()
	// The worker may have processed some events before Close(), but
	// it should NOT have processed all 100 (no drain guarantee).
	// We can't assert an exact count, but if the worker drained everything
	// that would indicate a bug.
	if len(calls) == 100 {
		t.Error("worker appears to have drained all events; expected no-drain behavior")
	}

	// Verify Redis client was closed
	fake.mu.Lock()
	closed := fake.closed
	fake.mu.Unlock()
	if !closed {
		t.Error("expected Redis client to be closed after Close()")
	}
}

func TestMapDecision(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"allow", "allow"},
		{"unauth", "deny"},
		{"forbid", "deny"},
		{"killswitch", "deny"},
		{"error", "error"},
		{"unknown", "error"},
		{"", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := mapDecision(tt.in)
			if got != tt.want {
				t.Errorf("mapDecision(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
