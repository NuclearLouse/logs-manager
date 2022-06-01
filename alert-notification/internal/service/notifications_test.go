package service

import (
	"testing"

	"redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/datastructs"
)

func TestService_sendAdminNotificate(t *testing.T) {
	type args struct {
		typeNotificate notificate
		internalError  []error
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
			tt.s.sendAdminNotificate(tt.args.typeNotificate, tt.args.internalError...)
		})
	}
}

func TestService_sendUserNotificate(t *testing.T) {
	type args struct {
		alertset *datastructs.AlertSettings
		pool     []datastructs.AlertLog
	}
	tests := []struct {
		name    string
		s       *Service
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.sendUserNotificate(tt.args.alertset, tt.args.pool); (err != nil) != tt.wantErr {
				t.Errorf("Service.sendUserNotificate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
