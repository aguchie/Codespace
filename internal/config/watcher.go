package config

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// StartWatcher begins watching workspace.yml for file changes and reloads configuration.
func (cm *ConfigManager) StartWatcher(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	watchDir := filepath.Dir(cm.configPath)
	targetFileName := filepath.Base(cm.configPath)

	if _, err := os.Stat(watchDir); err != nil {
		return err
	}

	if err := watcher.Add(watchDir); err != nil {
		_ = watcher.Close()
		return err
	}

	go func() {
		defer watcher.Close()

		var debounceTimer *time.Timer
		var debounceDuration = 250 * time.Millisecond

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				base := filepath.Base(event.Name)
				if strings.EqualFold(base, targetFileName) {
					if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
						if debounceTimer != nil {
							debounceTimer.Stop()
						}
						debounceTimer = time.AfterFunc(debounceDuration, func() {
							log.Printf("[ConfigWatcher] Detected change in %s. Reloading...", base)
							if err := cm.Reload(); err != nil {
								log.Printf("[ConfigWatcher] Error reloading config: %v", err)
							} else {
								log.Printf("[ConfigWatcher] Successfully reloaded workspace.yml")
							}
						})
					}
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[ConfigWatcher] Watcher error: %v", err)
			}
		}
	}()

	log.Printf("[ConfigWatcher] Watching %s for changes", cm.configPath)
	return nil
}
