package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigmccaskill/posthorn/config"
)

// --- buildTransport ---

func TestBuildTransport_Postmark(t *testing.T) {
	tp, err := buildTransport(config.TransportConfig{
		Type:     "postmark",
		Settings: map[string]any{"api_key": "test-key"},
	})
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	if tp == nil {
		t.Fatal("nil transport with nil error")
	}
}

func TestBuildTransport_UnknownType(t *testing.T) {
	_, err := buildTransport(config.TransportConfig{Type: "smtp"})
	if err == nil {
		t.Fatal("expected error for unknown transport type")
	}
	if !strings.Contains(err.Error(), "unknown transport type") {
		t.Errorf("error: %v", err)
	}
}

func TestBuildTransport_PostmarkMissingAPIKey(t *testing.T) {
	_, err := buildTransport(config.TransportConfig{
		Type:     "postmark",
		Settings: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error for missing api_key")
	}
}

// --- buildLogger ---

func TestBuildLogger_DefaultLevel(t *testing.T) {
	logger := buildLogger(config.LoggingConfig{})
	if logger == nil {
		t.Fatal("nil logger")
	}
}

func TestBuildLogger_LevelAccepted(t *testing.T) {
	for _, lvl := range []string{"debug", "info", "warn", "error", ""} {
		t.Run(lvl, func(t *testing.T) {
			logger := buildLogger(config.LoggingConfig{Level: lvl})
			if logger == nil {
				t.Fatal("nil logger")
			}
		})
	}
}

// --- runValidate ---

const validTOML = `
[[endpoints]]
path = "/api/contact"
to = ["craig@example.com"]
from = "noreply@example.com"
subject = "Contact"
body = "Body"

[endpoints.transport]
type = "postmark"

[endpoints.transport.settings]
api_key = "test-key"
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestRunValidate_Valid(t *testing.T) {
	path := writeConfig(t, validTOML)
	if err := runValidate([]string{"--config", path}); err != nil {
		t.Errorf("runValidate: %v", err)
	}
}

func TestRunValidate_FileNotFound(t *testing.T) {
	err := runValidate([]string{"--config", "/no/such/file.toml"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRunValidate_InvalidConfig(t *testing.T) {
	// Missing required field "from" should surface as a config error.
	bad := strings.Replace(validTOML, `from = "noreply@example.com"`, "", 1)
	path := writeConfig(t, bad)
	err := runValidate([]string{"--config", path})
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestRunValidate_TemplateParseError(t *testing.T) {
	// Body with unclosed action — config.Load passes (it doesn't parse
	// templates), but gateway.New surfaces the template parse error.
	bad := strings.Replace(validTOML, `body = "Body"`, `body = "Bad: {{.x"`, 1)
	path := writeConfig(t, bad)
	err := runValidate([]string{"--config", path})
	if err == nil {
		t.Fatal("expected error for unparseable template")
	}
}

// --- buildMux ---

func TestBuildMux_RoutesEndpointsCorrectly(t *testing.T) {
	cfg := &config.Config{
		Endpoints: []config.EndpointConfig{
			{
				Path:    "/api/contact",
				To:      []string{"to@example.com"},
				From:    "from@example.com",
				Subject: "S",
				Body:    "B",
				Transport: config.TransportConfig{
					Type:     "postmark",
					Settings: map[string]any{"api_key": "k"},
				},
			},
		},
	}
	logger := buildLogger(config.LoggingConfig{})
	mux, err := buildMux(cfg, logger)
	if err != nil {
		t.Fatalf("buildMux: %v", err)
	}

	// Configured path is reachable.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/contact", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(rec, req)
	// The transport is real (Postmark client pointing at the public API), so
	// a synchronous Send will fail (test-key is invalid). But the mux at
	// least routed the request — we assert we got SOMETHING back, not 404.
	// Not 404 = mux routed correctly.
	if rec.Code == http.StatusNotFound {
		t.Errorf("configured path /api/contact returned 404; mux did not route")
	}
}

func TestBuildMux_UnconfiguredPath_404(t *testing.T) {
	cfg := &config.Config{
		Endpoints: []config.EndpointConfig{
			{
				Path:    "/api/contact",
				To:      []string{"to@example.com"},
				From:    "from@example.com",
				Subject: "S",
				Body:    "B",
				Transport: config.TransportConfig{
					Type:     "postmark",
					Settings: map[string]any{"api_key": "k"},
				},
			},
		},
	}
	mux, err := buildMux(cfg, buildLogger(config.LoggingConfig{}))
	if err != nil {
		t.Fatalf("buildMux: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/unconfigured", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for unconfigured path", rec.Code)
	}
}

func TestBuildMux_BadTransport_PropagatesError(t *testing.T) {
	cfg := &config.Config{
		Endpoints: []config.EndpointConfig{
			{
				Path:    "/api/x",
				To:      []string{"to@example.com"},
				From:    "from@example.com",
				Subject: "S",
				Body:    "B",
				Transport: config.TransportConfig{
					Type: "nonexistent",
				},
			},
		},
	}
	_, err := buildMux(cfg, buildLogger(config.LoggingConfig{}))
	if err == nil {
		t.Fatal("expected error for bad transport")
	}
}
