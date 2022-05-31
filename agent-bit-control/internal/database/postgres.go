package database

import (
	"redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/datastructs"

	"context"

	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"
)

type Postgres struct {
	*pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool}
}

func (db *Postgres) GetSignalDB(ctx context.Context, serverName string) (*datastructs.AgentControl, error) {
	var signal datastructs.AgentControl
	if err := db.QueryRow(ctx,
		"SELECT stop_agent, reload_config FROM logs_manager.agent_bit_control WHERE server_name=$1;",
		serverName,
	).Scan(
		&signal.Stop,
		&signal.Reload,
	); err != nil {
		return nil, err
	}
	return &signal, nil
}

func (db *Postgres) ResetFlags(ctx context.Context, serverName string) error {
	if _, err := db.Exec(ctx,
		"UPDATE logs_manager.agent_bit_control SET stop_agent=false, reload_config=false WHERE server_name=$1;", serverName,
	); err != nil {
		return err
	}

	return nil
}

func (db *Postgres) GetServedApplications(ctx context.Context, serverName string) ([]*datastructs.ServedApplication, error) {
	rows, err := db.Query(ctx,
		`SELECT service_name, service_tag_general, service_tags_alert, path_to_logfile, path_to_errlogfile, keywords_alert
		FROM logs_manager.served_application WHERE server_name=$1;`,
		serverName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []*datastructs.ServedApplication
	for rows.Next() {
		var (
			srv           datastructs.ServedApplication
			errLogFile    pgtype.Text
			alertKeywords pgtype.TextArray
		)
		if err := rows.Scan(
			&srv.AppName,
			&srv.GeneralTag,
			&srv.AlertTags,
			&srv.LogFile,
			&errLogFile,
			&alertKeywords,
		); err != nil {
			return nil, err
		}

		if errLogFile.Status != pgtype.Null {
			srv.ErrLogFile = errLogFile.String
		}
		if alertKeywords.Status != pgtype.Null {
			if err := alertKeywords.AssignTo(&srv.AlertKeywords); err != nil {
				return nil, err
			}
			if len(srv.AlertTags) != len(srv.AlertKeywords) {
				return nil, errors.New("the number of alert tags does not match the number of key alert words")
			}
		}

		services = append(services, &srv)
	}

	return services, nil
}
