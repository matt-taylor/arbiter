package health

import (
	"net/http"
	"testing"
)

func TestBuildReportFromEnv_healthy(t *testing.T) {
	status, report := BuildReportFromEnv(map[string]string{
		"SERVICE_NAME":  "arbiter",
		"GIT_REVISION":  "abc1234def5678901234567890abcdef12345678",
	}, func() error { return nil })

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if report.Status != "healthy" {
		t.Fatalf("expected healthy, got %q", report.Status)
	}
	if report.Service != "arbiter" {
		t.Fatalf("expected service arbiter, got %q", report.Service)
	}
	if report.Checks["database"] != "healthy" {
		t.Fatalf("expected database healthy, got %q", report.Checks["database"])
	}
}

func TestBuildReportFromEnv_missingServiceName(t *testing.T) {
	status, report := BuildReportFromEnv(map[string]string{
		"GIT_REVISION": "abc1234def5678901234567890abcdef12345678",
	}, func() error { return nil })

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if report.Status != "degraded" {
		t.Fatalf("expected degraded, got %q", report.Status)
	}
	if report.Service != "unknown" {
		t.Fatalf("expected unknown service, got %q", report.Service)
	}
	if report.Checks["service_name"] != "degraded" {
		t.Fatalf("expected service_name degraded, got %q", report.Checks["service_name"])
	}
}

func TestBuildReportFromEnv_missingGitRevision(t *testing.T) {
	status, report := BuildReportFromEnv(map[string]string{
		"SERVICE_NAME": "arbiter",
	}, func() error { return nil })

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if report.Status != "degraded" {
		t.Fatalf("expected degraded, got %q", report.Status)
	}
	if report.GitRevision != "unknown" {
		t.Fatalf("expected unknown git revision, got %q", report.GitRevision)
	}
}

func TestBuildReportFromEnv_databaseFailure(t *testing.T) {
	status, report := BuildReportFromEnv(map[string]string{
		"SERVICE_NAME": "arbiter",
		"GIT_REVISION": "abc1234def5678901234567890abcdef12345678",
	}, func() error { return errDatabaseDown })

	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", status)
	}
	if report.Status != "unhealthy" {
		t.Fatalf("expected unhealthy, got %q", report.Status)
	}
	if report.Checks["database"] != "unhealthy" {
		t.Fatalf("expected database unhealthy, got %q", report.Checks["database"])
	}
}

var errDatabaseDown = &testError{msg: "database down"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
