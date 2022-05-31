package service

import (
	"context"

	"redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/datastructs"
)

type storer interface {
	GetSignalDB(ctx context.Context) (*datastructs.ServiceControl, error)
	ResetFlags(ctx context.Context) error
	DeleteOldLogs(ctx context.Context) ([]datastructs.ClearOldLogsStatistic, error)
	AllServedAppIDs(ctx context.Context) ([]int, error)
	ServedAppAlertSettings(ctx context.Context, appID int) (*datastructs.AlertSettings, error)
	AlertLogs(ctx context.Context, appID int) ([]datastructs.AlertLog, error)
	SetSendFlag(ctx context.Context, recIDs []int64) error
	CheckPgAgent(ctx context.Context) error
}
