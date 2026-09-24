package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workspace-manager/internal/auth"
	"workspace-manager/internal/config"
)

func setupTestRouter(t *testing.T) (*GatewayRouter, *config.ConfigManager, *auth.Authenticator) {
	tempDir := t.TempDir()
	sysCfg := config.SystemConfig{
		AdminUser:    "admin",
		AdminPass:    "secret123",
		WorkspaceDir: tempDir,
		CookieSecret: "test-secret-key",
	}

	cm, err := config.NewConfigManager(sysCfg, nil)
	if err != nil {
		t.Fatalf("config error: %v", err)
	}

	a, err := auth.NewAuthenticator(cm)
	if err != nil {
		t.Fatalf("auth error: %v", err)
	}

	router, err := NewGatewayRouter(cm, a)
	if err != nil {
		t.Fatalf("router error: %v", err)
	}

	return router, cm, a
}

func TestRobotsTxtDisallow(t *testing.T) {
	router, _, _ := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	w := httptest.NewRecorder()

	router.RobotsHandler(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Disallow: /") {
		t.Errorf("expected Disallow: / in robots.txt, got: %s", body)
	}
}

func TestUnauthenticatedEditorRedirect(t *testing.T) {
	router, _, _ := setupTestRouter(t)
	codeServerProxy := router.BuildCodeServerProxy()

	req := httptest.NewRequest(http.MethodGet, "/editor", nil)
	w := httptest.NewRecorder()

	codeServerProxy.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 Found, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "/login?redirect=") {
		t.Errorf("expected redirect to login, got %s", location)
	}
}

func TestEditorTrailingSlashRedirect(t *testing.T) {
	router, _, _ := setupTestRouter(t)
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := router.RouteTraffic(dummy, dummy, dummy, dummy)

	req := httptest.NewRequest(http.MethodGet, "/editor", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 Found, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/editor/" {
		t.Errorf("expected redirect to /editor/, got %s", resp.Header.Get("Location"))
	}
}

func TestCodeServerAssetRouting(t *testing.T) {
	router, _, _ := setupTestRouter(t)
	codeServerVisited := false
	userAppVisited := false

	codeServerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		codeServerVisited = true
	})
	userAppHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAppVisited = true
	})
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	handler := router.RouteTraffic(userAppHandler, codeServerHandler, dummy, dummy)

	// Test /_static/ should route to code-server
	req := httptest.NewRequest(http.MethodGet, "/_static/src/browser/pages/vscode.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !codeServerVisited || userAppVisited {
		t.Errorf("expected /_static/ to route to codeServerHandler, codeServerVisited=%v, userAppVisited=%v", codeServerVisited, userAppVisited)
	}
}

