// init registers the usage statistics lifecycle hooks with the SDK usage manager.
// This is the single integration point — no other upstream files need to be
// modified for statistics lifecycle management.
package usage

import (
	"context"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

type lifecycle struct {
	savePath string
}

func init() {
	coreusage.RegisterLifecycle(&lifecycle{})
}

func (l *lifecycle) OnStart(ctx context.Context, cfg coreusage.LifecycleConfig) error {
	l.savePath = DefaultStatsSavePath(cfg.AuthDir)
	stats := GetRequestStatistics()

	SetStatisticsEnabled(cfg.UsageStatisticsEnabled)

	if cfg.AutoSaveIntervalSec > 0 {
		SetAutoSaveInterval(time.Duration(cfg.AutoSaveIntervalSec) * time.Second)
	}
	if cfg.MaxDetailsPerModel > 0 {
		SetMaxDetailsPerModel(cfg.MaxDetailsPerModel)
	}

	if err := stats.LoadFromFile(l.savePath); err != nil {
		log.WithError(err).Warn("usage: failed to load persisted statistics")
	}
	if cfg.AutoSaveIntervalSec > 0 {
		StartAutoSave(ctx, l.savePath)
	}
	return nil
}

func (l *lifecycle) OnShutdown() error {
	if l.savePath == "" {
		return nil
	}
	stats := GetRequestStatistics()
	if err := stats.SaveToFile(l.savePath); err != nil {
		log.WithError(err).Warn("usage: failed to persist statistics on shutdown")
	}
	return nil
}
