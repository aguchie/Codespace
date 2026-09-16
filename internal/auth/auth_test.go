package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"workspace-manager/internal/config"
)

func setupTestAuthenticator(t *testing.T) (*Authenticator, *config.ConfigManager) {
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

	auth, err := NewAuthenticator(cm)
	if err != nil {
		t.Fatalf("authenticator error: %v", err)
	}

	return auth, cm
}

func TestSignAndVerifyCookie(t *testing.T) {
	auth, _ := setupTestAuthenticator(t)

	payload := SessionPayload{
		Username:  "admin",
		Role:      RoleAdmin,
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	cookieVal, err := auth.signPayload(payload)
	if err != nil {
		t.Fatalf("signPayload failed: %v", err)
	}

	verified, err := auth.VerifyCookie(cookieVal)
	if err != nil {
		t.Fatalf("verifyCookie failed: %v", err)
	}

	if verified.Username != "admin" || verified.Role != RoleAdmin {
		t.Errorf("unexpected verified payload: %+v", verified)
	}

	tampered := cookieVal + "corrupt"
	if _, err := auth.VerifyCookie(tampered); err == nil {
		t.Errorf("expected error on tampered cookie, got nil")
	}
}

func TestLoginHandlerAdminSuccess(t *testing.T) {
	auth, _ := setupTestAuthenticator(t)

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "secret123")
	form.Set("redirect", "/editor")

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.PostForm = form
	w := httptest.NewRecorder()

	auth.LoginHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected status 302, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location != "/editor" {
		t.Errorf("expected redirect to /editor, got %s", location)
	}

	cookies := resp.Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == SessionCookieName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected session cookie to be set")
	}
}

func TestLoginHandlerFailure(t *testing.T) {
	auth, _ := setupTestAuthenticator(t)

	form := url.Values{}
	form.Set("username", "wrong")
	form.Set("password", "wrong")

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.PostForm = form
	w := httptest.NewRecorder()

	auth.LoginHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 Unauthorized, got %d", resp.StatusCode)
	}
}
