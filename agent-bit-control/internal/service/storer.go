package service

import (
	"context"

	"redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/datastructs"
)

type storer interface {
	GetSignalDB(ctx context.Context, serverName string) (*datastructs.AgentControl, error)
	ResetFlags(ctx context.Context, serverName string) error
	GetServedApplications(ctx context.Context, serverName string) ([]*datastructs.ServedApplication, error)
}
