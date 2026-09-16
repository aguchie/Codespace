// Package auth provides session management, user authentication handlers, and access control middlewares.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"workspace-manager/internal/config"
	"workspace-manager/web"
)

const (
	// SessionCookieName is the name of the HTTP cookie holding the session token.
	SessionCookieName = "codespace_session"
	// RoleAdmin indicates administrative privileges including editor and logs access.
	RoleAdmin = "admin"
	// RoleSubUser indicates restricted client privileges limited to user application preview.
	RoleSubUser = "sub_user"
)

// SessionPayload represents the serialized payload inside the session cookie.
type SessionPayload struct {
	Username  string `json:"u"`
	Role      string `json:"r"`
	ExpiresAt int64  `json:"exp"`
}

// Authenticator handles login, session validation, and auth middlewares.
type Authenticator struct {
	cfgManager *config.ConfigManager
	secret     []byte
	loginTmpl  *template.Template
}

// NewAuthenticator creates a new Authenticator instance.
func NewAuthenticator(cm *config.ConfigManager) (*Authenticator, error) {
	tmplContent, err := web.ReadStaticFile("login.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read login.html: %w", err)
	}

	tmpl, err := template.New("login").Parse(string(tmplContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse login template: %w", err)
	}

	return &Authenticator{
		cfgManager: cm,
		secret:     []byte(cm.GetSystem().CookieSecret),
		loginTmpl:  tmpl,
	}, nil
}

// signPayload generates an HMAC-SHA256 signature for the given payload.
func (a *Authenticator) signPayload(payload SessionPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, a.secret)
	mac.Write(data)
	sig := mac.Sum(nil)

	val := base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(sig)
	return val, nil
}

// VerifyCookie decodes and validates a session cookie string.
func (a *Authenticator) VerifyCookie(cookieVal string) (*SessionPayload, error) {
	parts := strings.Split(cookieVal, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cookie format")
	}

	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding: %w", err)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding: %w", err)
	}

	mac := hmac.New(sha256.New, a.secret)
	mac.Write(data)
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(sig, expectedSig) {
		return nil, fmt.Errorf("signature verification failed")
	}

	var payload SessionPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("failed to decode json payload: %w", err)
	}

	if time.Now().Unix() > payload.ExpiresAt {
		return nil, fmt.Errorf("session expired")
	}

	return &payload, nil
}

// SetSessionCookie sets an authenticated session cookie on the response.
func (a *Authenticator) SetSessionCookie(w http.ResponseWriter, username, role string) error {
	payload := SessionPayload{
		Username:  username,
		Role:      role,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix(),
	}

	signed, err := a.signPayload(payload)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(payload.ExpiresAt, 0),
	})

	return nil
}

// ClearSessionCookie clears the session cookie.
func (a *Authenticator) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

// GetCurrentSession returns the authenticated session from the request, or nil if unauthenticated.
func (a *Authenticator) GetCurrentSession(r *http.Request) *SessionPayload {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil
	}

	payload, err := a.VerifyCookie(cookie.Value)
	if err != nil {
		return nil
	}

	return payload
}

// LoginHandler handles GET and POST requests for /login.
func (a *Authenticator) LoginHandler(w http.ResponseWriter, r *http.Request) {
	redirectURL := r.URL.Query().Get("redirect")
	if redirectURL == "" {
		redirectURL = r.FormValue("redirect")
	}
	if !strings.HasPrefix(redirectURL, "/") || strings.HasPrefix(redirectURL, "//") {
		redirectURL = "/"
	}

	if r.Method == http.MethodGet {
		if sess := a.GetCurrentSession(r); sess != nil {
			if sess.Role == RoleAdmin || (sess.Role == RoleSubUser && !strings.HasPrefix(redirectURL, "/editor") && !strings.HasPrefix(redirectURL, "/logs")) {
				http.Redirect(w, r, redirectURL, http.StatusFound)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = a.loginTmpl.Execute(w, map[string]interface{}{
			"Error":    "",
			"Redirect": redirectURL,
		})
		return
	}

	if r.Method == http.MethodPost {
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")

		sysCfg := a.cfgManager.GetSystem()
		wsCfg := a.cfgManager.GetWorkspace()

		var matchedRole string

		if username == sysCfg.AdminUser && password == sysCfg.AdminPass {
			matchedRole = RoleAdmin
		} else {
			for _, sub := range wsCfg.SubUsers {
				if sub.ID == username && sub.Password == password {
					matchedRole = RoleSubUser
					break
				}
			}
		}

		if matchedRole == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = a.loginTmpl.Execute(w, map[string]interface{}{
				"Error":    "ユーザー名またはパスワードが無効です。",
				"Redirect": redirectURL,
			})
			return
		}

		if matchedRole == RoleSubUser && (strings.HasPrefix(redirectURL, "/editor") || strings.HasPrefix(redirectURL, "/logs")) {
			redirectURL = "/"
		}

		if err := a.SetSessionCookie(w, username, matchedRole); err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, redirectURL, http.StatusFound)
	}
}

// LogoutHandler handles requests for /logout.
func (a *Authenticator) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	a.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// RequireAdmin ensures that the request is authenticated with admin privileges.
func (a *Authenticator) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := a.GetCurrentSession(r)
		if sess == nil || sess.Role != RoleAdmin {
			redir := "/login?redirect=" + url.QueryEscape(r.RequestURI)
			http.Redirect(w, r, redir, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CheckAppAccess verifies if access to the user application is permitted.
func (a *Authenticator) CheckAppAccess(r *http.Request) bool {
	wsCfg := a.cfgManager.GetWorkspace()
	if wsCfg.App.Visibility == "public" {
		return true
	}

	sess := a.GetCurrentSession(r)
	if sess == nil {
		return false
	}
	return sess.Role == RoleAdmin || sess.Role == RoleSubUser
}
