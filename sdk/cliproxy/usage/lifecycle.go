// Lifecycle hooks allow optional packages to plug into the service startup/shutdown
// without the service package importing them directly. This keeps the service
// package clean and reduces merge conflicts.
package usage

import "context"

// LifecycleConfig carries the configuration values needed by lifecycle hooks.
type LifecycleConfig struct {
	AuthDir                string
	AutoSaveIntervalSec    int
	MaxDetailsPerModel     int
	UsageStatisticsEnabled bool
}

// LifecycleHooks are optional callbacks invoked at service start and shutdown.
type LifecycleHooks interface {
	OnStart(ctx context.Context, cfg LifecycleConfig) error
	OnShutdown() error
}

var registeredLifecycle LifecycleHooks

// RegisterLifecycle stores lifecycle hooks. Only one registration is permitted;
// subsequent calls are ignored.
func RegisterLifecycle(hooks LifecycleHooks) {
	if registeredLifecycle == nil {
		registeredLifecycle = hooks
	}
}

// GetLifecycle returns the registered lifecycle hooks, or nil.
func GetLifecycle() LifecycleHooks { return registeredLifecycle }
