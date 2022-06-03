package service

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/datastructs"
	"redits.oculeus.com/asorokin/notification"
	mock "redits.oculeus.com/asorokin/notification/mock"
)

func TestService_sendAdminNotificate(t *testing.T) {
	type args struct {
		typeNotificate notificate
		internalError  []error
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s := &Service{
		cfg: &config{
			ServerName: "Work Notebook",
		},
		notificator: map[string]notification.Notificator{
			"email": mock.NewMockNotificator(ctrl),
		},
	}

	tests := []struct {
		name string
		s    *Service
		args args
	}{
		// TODO: Add test cases.
		{
			name: "Send Test Connect",
			s:    s,
			args: args{
				typeNotificate: TEST_CONNECT,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.sendAdminNotification(tt.args.typeNotificate, tt.args.internalError...)
		})
	}
}

func TestService_sendAlertNotification(t *testing.T) {
	type args struct {
		ctx       context.Context
		typeAlert string
		alertset  *datastructs.AlertSettings
		pool      []datastructs.AlertLog
	}
	tests := []struct {
		name string
		s    *Service
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.sendAlertNotification(tt.args.ctx, tt.args.typeAlert, tt.args.alertset, tt.args.pool)
		})
	}
}
