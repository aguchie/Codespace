// Package main provides the entry point for the Codespace workspace-manager service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"workspace-manager/internal/auth"
	"workspace-manager/internal/config"
	"workspace-manager/internal/process"
	"workspace-manager/internal/proxy"
)

func main() {
	sysCfg := config.LoadSystemConfig()

	portFlag := flag.Int("port", sysCfg.Port, "Gateway HTTP port")
	adminUserFlag := flag.String("admin-user", sysCfg.AdminUser, "Admin username")
	adminPassFlag := flag.String("admin-pass", sysCfg.AdminPass, "Admin password")
	workspaceFlag := flag.String("workspace", sysCfg.WorkspaceDir, "Workspace root directory")
	disableCodeServerFlag := flag.Bool("disable-code-server", sysCfg.DisableCodeSrv, "Disable code-server background execution")
	flag.Parse()

	sysCfg.Port = *portFlag
	sysCfg.AdminUser = *adminUserFlag
	sysCfg.AdminPass = *adminPassFlag
	sysCfg.WorkspaceDir = *workspaceFlag
	sysCfg.DisableCodeSrv = *disableCodeServerFlag

	log.Println("==================================================")
	log.Println("  Codespace Workspace Manager & Gateway Starting  ")
	log.Println("==================================================")
	log.Printf("[Main] Workspace Dir: %s", sysCfg.WorkspaceDir)
	log.Printf("[Main] Gateway Listen Port: %d", sysCfg.Port)
	log.Printf("[Main] Code-Server Port: %d", sysCfg.CodeServerPort)
	log.Printf("[Main] Admin User: %s", sysCfg.AdminUser)
	if sysCfg.DisableCodeSrv {
		log.Println("[Main] Code-Server background process: DISABLED")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pm *process.ProcessManager

	cm, err := config.NewConfigManager(sysCfg, func(wCfg config.WorkspaceConfig) {
		log.Printf("[Main] Workspace config updated. Target port: %d, Dev command: '%s'", wCfg.App.Port, wCfg.Commands.Dev)
		if pm != nil {
			pm.SyncUserApp(ctx)
		}
	})
	if err != nil {
		log.Printf("[Main] Warning: Could not read workspace.yml immediately (%v). Using defaults.", err)
	}

	pm = process.NewProcessManager(cm)

	authenticator, err := auth.NewAuthenticator(cm)
	if err != nil {
		log.Fatalf("[Main] Failed to initialize Authenticator: %v", err)
	}

	router, err := proxy.NewGatewayRouter(cm, authenticator)
	if err != nil {
		log.Fatalf("[Main] Failed to initialize GatewayRouter: %v", err)
	}

	codeServerProxy := router.BuildCodeServerProxy()
	userAppProxy := router.BuildUserAppProxy()

	handler := router.RouteTraffic(
		userAppProxy,
		codeServerProxy,
		pm.LogsDashboardHandler,
		pm.LogsStreamHandler,
	)

	pm.StartCodeServer(ctx)
	pm.SyncUserApp(ctx)

	if err := cm.StartWatcher(ctx); err != nil {
		log.Printf("[Main] Warning: Failed to start workspace.yml watcher: %v", err)
	}

	serverAddr := fmt.Sprintf(":%d", sysCfg.Port)
	server := &http.Server{
		Addr:    serverAddr,
		Handler: handler,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stopChan
		log.Println("[Main] Shutting down gracefully...")
		cancel()
		pm.StopAll()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
		log.Println("[Main] Server stopped.")
	}()

	pm.BroadcastLog("gateway", fmt.Sprintf("Codespace Gateway listening on port %d", sysCfg.Port), false)
	log.Printf("[Main] Server started on http://localhost:%d", sysCfg.Port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[Main] HTTP server error: %v", err)
	}
}
