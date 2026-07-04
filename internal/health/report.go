package health

import (
	"net/http"
	"os"
)

// Report is the Phase 1 producer-side GET /healthz JSON contract.
type Report struct {
	Status      string            `json:"status"`
	Service     string            `json:"service"`
	GitRevision string            `json:"git_revision"`
	Checks      map[string]string `json:"checks"`
}

// BuildReport constructs the health contract from runtime env and an optional database check.
// When dbCheck is nil, checks.database is "skipped".
func BuildReport(dbCheck func() error) (httpStatus int, report Report) {
	env := os.Environ()
	envMap := make(map[string]string, len(env))
	for _, entry := range env {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				envMap[entry[:i]] = entry[i+1:]
				break
			}
		}
	}

	return BuildReportFromEnv(envMap, dbCheck)
}

// BuildReportFromEnv is exported for unit tests with explicit env maps.
func BuildReportFromEnv(env map[string]string, dbCheck func() error) (httpStatus int, report Report) {
	serviceName := env["SERVICE_NAME"]
	gitRevision := env["GIT_REVISION"]

	serviceValue := serviceName
	if serviceValue == "" {
		serviceValue = "unknown"
	}
	gitValue := gitRevision
	if gitValue == "" {
		gitValue = "unknown"
	}

	checks := map[string]string{
		"service_name": "healthy",
		"git_revision": "healthy",
	}

	if serviceName == "" {
		checks["service_name"] = "degraded"
	}
	if gitRevision == "" {
		checks["git_revision"] = "degraded"
	}

	if dbCheck == nil {
		checks["database"] = "skipped"
	} else if err := dbCheck(); err != nil {
		checks["database"] = "unhealthy"
	} else {
		checks["database"] = "healthy"
	}

	overall := computeOverallStatus(checks)

	report = Report{
		Status:      overall,
		Service:     serviceValue,
		GitRevision: gitValue,
		Checks:      checks,
	}

	if overall == "unhealthy" {
		return http.StatusServiceUnavailable, report
	}
	return http.StatusOK, report
}

func computeOverallStatus(checks map[string]string) string {
	if checks["database"] == "unhealthy" {
		return "unhealthy"
	}
	for _, status := range checks {
		if status == "degraded" {
			return "degraded"
		}
	}
	return "healthy"
}
