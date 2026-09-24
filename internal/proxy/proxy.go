// Package proxy provides HTTP and WebSocket reverse proxying, crawler control, and traffic routing.
package proxy

import (
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"workspace-manager/internal/auth"
	"workspace-manager/internal/config"
	"workspace-manager/web"
)

// GatewayRouter handles routing, authentication middleware, and reverse proxying.
type GatewayRouter struct {
	cfgManager *config.ConfigManager
	auth       *auth.Authenticator
	statusTmpl *template.Template
}

// NewGatewayRouter creates a new GatewayRouter instance.
func NewGatewayRouter(cm *config.ConfigManager, a *auth.Authenticator) (*GatewayRouter, error) {
	tmplContent, err := web.ReadStaticFile("status.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read status.html: %w", err)
	}

	tmpl, err := template.New("status").Parse(string(tmplContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse status template: %w", err)
	}

	return &GatewayRouter{
		cfgManager: cm,
		auth:       a,
		statusTmpl: tmpl,
	}, nil
}

// BuildCodeServerProxy creates a reverse proxy for code-server.
func (gr *GatewayRouter) BuildCodeServerProxy() http.Handler {
	sysCfg := gr.cfgManager.GetSystem()
	targetURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", sysCfg.CodeServerPort))

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		originalHost := req.Host
		if req.Header.Get("X-Forwarded-Host") == "" {
			req.Header.Set("X-Forwarded-Host", originalHost)
		}
		if req.Header.Get("X-Forwarded-Proto") == "" {
			if req.TLS != nil {
				req.Header.Set("X-Forwarded-Proto", "https")
			} else {
				req.Header.Set("X-Forwarded-Proto", "http")
			}
		}

		// Strip /editor prefix so code-server receives requests at root /
		if strings.HasPrefix(req.URL.Path, "/editor") {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/editor")
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
			req.URL.RawPath = ""
		}
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[CodeServerProxy] Error proxying request to %s: %v", targetURL, err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<meta http-equiv="refresh" content="3">
	<title>Code-Server 準備中</title>
</head>
<body style="background:#131316;color:#ecebe6;font-family:sans-serif;padding:3rem;text-align:center;">
	<h2>Code-Server が準備中または起動処理中です</h2>
	<p style="color:#a9a8a2;">起動中の場合は数秒後に自動で再接続されます。(Port: %d)</p>
	<p style="color:#f28b82;font-family:monospace;font-size:0.85rem;">%v</p>
	<p><a href="/logs" style="color:#6faa8e;">ログダッシュボードで確認</a></p>
</body>
</html>`, sysCfg.CodeServerPort, err)
	}

	return gr.auth.RequireAdmin(proxy)
}

// BuildUserAppProxy creates a reverse proxy for the user dev application.
func (gr *GatewayRouter) BuildUserAppProxy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gr.auth.CheckAppAccess(r) {
			redir := "/login?redirect=" + url.QueryEscape(r.RequestURI)
			http.Redirect(w, r, redir, http.StatusFound)
			return
		}

		wsCfg := gr.cfgManager.GetWorkspace()
		targetURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", wsCfg.App.Port))

		proxy := httputil.NewSingleHostReverseProxy(targetURL)

		origDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			origDirector(req)
			req.Host = targetURL.Host
			if req.Header.Get("X-Forwarded-Host") == "" {
				req.Header.Set("X-Forwarded-Host", req.Host)
			}
			if req.Header.Get("X-Forwarded-Proto") == "" {
				if req.TLS != nil {
					req.Header.Set("X-Forwarded-Proto", "https")
				} else {
					req.Header.Set("X-Forwarded-Proto", "http")
				}
			}
		}

		proxy.ModifyResponse = func(resp *http.Response) error {
			if !wsCfg.App.AllowCrawling {
				resp.Header.Set("X-Robots-Tag", "noindex, nofollow")
			}
			return nil
		}

		proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = gr.statusTmpl.Execute(w, map[string]interface{}{
				"Port":    wsCfg.App.Port,
				"Command": wsCfg.Commands.Dev,
				"Error":   err.Error(),
			})
		}

		proxy.ServeHTTP(w, r)
	})
}

// RobotsHandler serves virtual /robots.txt dynamically according to workspace.yml.
func (gr *GatewayRouter) RobotsHandler(w http.ResponseWriter, r *http.Request) {
	wsCfg := gr.cfgManager.GetWorkspace()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if wsCfg.App.AllowCrawling {
		_, _ = fmt.Fprintln(w, "User-agent: *\nAllow: /")
	} else {
		_, _ = fmt.Fprintln(w, "User-agent: *\nDisallow: /")
	}
}

// CheckPortOpen checks if a TCP port is currently listening.
func CheckPortOpen(port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// RouteTraffic orchestrates all HTTP and WebSocket traffic.
func (gr *GatewayRouter) RouteTraffic(userAppHandler, codeServerHandler http.Handler, logsHandler, logsStreamHandler http.HandlerFunc) http.Handler {
	staticFileServer := http.StripPrefix("/static/", http.FileServer(web.GetStaticFileSystem()))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if strings.HasPrefix(path, "/static/") {
			staticFileServer.ServeHTTP(w, r)
			return
		}

		if path == "/login" {
			gr.auth.LoginHandler(w, r)
			return
		}
		if path == "/logout" {
			gr.auth.LogoutHandler(w, r)
			return
		}

		if path == "/robots.txt" {
			gr.RobotsHandler(w, r)
			return
		}

		if path == "/logs" {
			gr.auth.RequireAdmin(http.HandlerFunc(logsHandler)).ServeHTTP(w, r)
			return
		}
		if path == "/logs/stream" {
			gr.auth.RequireAdmin(http.HandlerFunc(logsStreamHandler)).ServeHTTP(w, r)
			return
		}

		if path == "/editor" {
			http.Redirect(w, r, "/editor/", http.StatusFound)
			return
		}

		if strings.HasPrefix(path, "/editor/") ||
			strings.HasPrefix(path, "/_static/") ||
			strings.HasPrefix(path, "/stable-") ||
			strings.HasPrefix(path, "/vscode/") {
			codeServerHandler.ServeHTTP(w, r)
			return
		}

		userAppHandler.ServeHTTP(w, r)
	})
}
