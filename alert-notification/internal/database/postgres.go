package database

import (
	"context"

	"redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/datastructs"

	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4/pgxpool"
)

type Postgres struct {
	*pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool}
}

func (db *Postgres) GetSignalDB(ctx context.Context) (*datastructs.ServiceControl, error) {
	var signal datastructs.ServiceControl
	if err := db.QueryRow(ctx,
		"SELECT stop_service, reload_config FROM logs_manager.alert_email_control;",
	).Scan(
		&signal.Stop,
		&signal.Reload,
	); err != nil {
		return nil, err
	}
	return &signal, nil
}

func (db *Postgres) ResetFlags(ctx context.Context) error {
	if _, err := db.Exec(ctx,
		"UPDATE logs_manager.alert_email_control SET stop_service=false, reload_config=false"); err != nil {
		return err
	}
	return nil
}

func (db *Postgres) DeleteOldLogs(ctx context.Context) ([]datastructs.ClearOldLogsStatistic, error) {
	var clearData []datastructs.ClearOldLogsStatistic
	rows, err := db.Query(ctx,
		"SELECT * FROM logs_manager.delete_old_logs()")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var col datastructs.ClearOldLogsStatistic
		if err := rows.Scan(
			&col.ServerName,
			&col.ServiceName,
			&col.AllLogs,
			&col.ErrLogs,
		); err != nil {
			return nil, err
		}
		clearData = append(clearData, col)
	}

	return clearData, nil
}

func (db *Postgres) AllServedAppIDs(ctx context.Context) ([]int, error) {
	var appIDs []int
	rows, err := db.Query(ctx,
		"SELECT id_served_app FROM logs_manager.served_application")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		appIDs = append(appIDs, id)
	}
	return appIDs, nil
}

func (db *Postgres) ServedAppAlertSettings(ctx context.Context, appID int) (*datastructs.AlertSettings, error) {
	als := datastructs.AlertSettings{
		ServiceID: appID,
	}
	var messcontains, bitrixto pgtype.TextArray
	if err := db.QueryRow(ctx,
		// "SELECT * FROM logs_manager.served_app_alert_settings($1)", appID,
		`SELECT sa.server_name,sa.service_name,als.send_urgent,als.urgent_if_message_contains, als.email_to, als.bitrix_to
        FROM logs_manager.served_application sa 
        JOIN logs_manager.alert_settings als ON als.id_served_app = sa.id_served_app 
        WHERE sa.id_served_app = $1;`, appID,
	).Scan(
		&als.ServerName,
		&als.ServiceName,
		&als.SendUrgent,
		&messcontains,
		&als.EmailTo,
		&bitrixto,
	); err != nil {
		return nil, err
	}
	if messcontains.Status != pgtype.Null {
		if err := messcontains.AssignTo(&als.IfMessageContains); err != nil {
			return nil, err
		}
	}
	if bitrixto.Status != pgtype.Null {
		if err := bitrixto.AssignTo(&als.BitrixTo); err != nil {
			return nil, err
		}
	}
	return &als, nil
}

func (db *Postgres) AlertLogs(ctx context.Context, appID int) ([]datastructs.AlertLog, error) {
	rows, err := db.Query(ctx,
		"SELECT * FROM logs_manager.alert_logs_from_served_app($1)", appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var (
		logs  []datastructs.AlertLog
		jdata pgtype.JSONB
	)

	for rows.Next() {
		var l datastructs.AlertLog
		if err := rows.Scan(
			&l.Tag,
			&l.Time,
			&jdata,
			&l.RecordID,
		); err != nil {
			return nil, err
		}
		if jdata.Status != pgtype.Null {
			if err := jdata.AssignTo(&l.Log); err != nil {
				return nil, err
			}
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func (db *Postgres) SetSendFlag(ctx context.Context, recIDs []int64) error {
	if _, err := db.Exec(ctx,
		"UPDATE logs_manager.alert_logs SET send_email = true WHERE record_id = ANY($1)", recIDs); err != nil {
		return err
	}
	return nil
}

func (db *Postgres) CheckPgAgent(ctx context.Context) error {
	if _, err := db.Exec(ctx,
		"SELECT FROM public.pgagent_jobs_check()"); err != nil {
		return err
	}
	return nil
}
