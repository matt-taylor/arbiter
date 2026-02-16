package telemetry

import "time"

// Event is the lightweight struct enqueued by the request handler.
// It carries raw values; normalization and JSON encoding happen in the worker.
type Event struct {
	IP             string    // client IP (extracted by ClientIP helper)
	HostRaw        string    // X-Original-Host, as-is
	Method         string    // X-Original-Method
	PathRaw        string    // X-Original-Uri with query params already stripped by caller
	Status         int       // exact HTTP status written to NGINX
	EngineDecision string    // raw engine decision (allow/unauth/forbid/killswitch/error)
	Time           time.Time // captured at emit time
}

// wireEvent is the JSON payload published to the Redis Stream.
type wireEvent struct {
	V              int    `json:"v"`
	TsMs           int64  `json:"ts_ms"`
	IP             string `json:"ip"`
	Host           string `json:"host"`
	HostRaw        string `json:"host_raw"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	PathRaw        string `json:"path_raw"`
	Status         int    `json:"status"`
	Decision       string `json:"decision"`        // collapsed: allow / deny / error
	EngineDecision string `json:"engine_decision"` // raw: allow / unauth / forbid / killswitch / error
}

// mapDecision collapses engine decision strings into the telemetry
// decision vocabulary: allow, deny, error.
func mapDecision(engineDecision string) string {
	switch engineDecision {
	case "allow":
		return "allow"
	case "unauth", "forbid", "killswitch":
		return "deny"
	case "error":
		return "error"
	default:
		return "error"
	}
}
