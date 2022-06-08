package service

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/database"
	"redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/datastructs"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"redits.oculeus.com/asorokin/connect/postgres"
	"redits.oculeus.com/asorokin/logging"
	"redits.oculeus.com/asorokin/notification"
	"redits.oculeus.com/asorokin/notification/bitrix"
	"redits.oculeus.com/asorokin/notification/email"
	"redits.oculeus.com/asorokin/notification/telegram"

	"github.com/NuclearLouse/scheduler"
	conf "github.com/tinrab/kit/cfg"

	"github.com/hashicorp/go-multierror"
)

var (
	version, cfgFile string
	REGULAR, URGENT  alertType
)

const serviceName = "Alert-Service"

type Service struct {
	cfg        *config
	log        *logging.Logger
	store      storer
	senders    []notification.Notificator
	stop       chan struct{}
	reload     chan struct{}
	servedApps map[int]context.CancelFunc
	// sync.RWMutex
	// journalUrgentAlertLog map[int]time.Time
	// journalInternalErrors map[string]time.Time
	journalUrgentAlertLog sync.Map
	journalInternalErrors sync.Map
}

type config struct {
	ServerName          string           `cfg:"server_name"`
	StartCleanOldLogs   string           `cfg:"start_clean_old_logs"`
	CheckRegularAlerts  time.Duration    `cfg:"check_regular_alerts"`
	CheckUrgentAlerts   time.Duration    `cfg:"check_urgent_alerts"`
	SendUrgentPeriod    time.Duration    `cfg:"send_urgent_period"`
	SendErrorPeriod     time.Duration    `cfg:"send_error_period"`
	NumLogsAttach       int              `cfg:"num_logs_for_attach"`
	NumAttemptsFail     int              `cfg:"num_attempts_fail"`
	AdminEmails         []string         `cfg:"admin_emails"`
	WithoutCheckPgAgent bool             `cfg:"without_check_pgagent"`
	Logger              *logging.Config  `cfg:"logger"`
	Postgres            *postgres.Config `cfg:"postgres"`
	Notification        struct {
		Enabled  []string
		Email    *email.Config    `cfg:"email"`
		Bitrix   *bitrix.Config   `cfg:"bitrix"`
		Telegram *telegram.Config `cfg:"telegram"`
	} `cfg:"notification"`
}

func Version() {
	fmt.Println("Version =", version)
}

func New() (*Service, error) {

	c := conf.New()
	if err := c.LoadFile(cfgFile); err != nil {
		return nil, errors.Wrap(err, "load config files")
	}
	cfg := &config{
		Logger:   logging.DefaultConfig(),
		Postgres: postgres.DefaultConfig(),
	}
	if err := c.Decode(&cfg); err != nil {
		return nil, errors.Wrap(err, "mapping config files")
	}

	REGULAR = alertType{
		name:       "REGULAR",
		sendPeriod: cfg.CheckRegularAlerts,
	}
	URGENT = alertType{
		name:       "URGENT",
		sendPeriod: cfg.CheckUrgentAlerts,
	}

	return &Service{
		cfg:    cfg,
		log:    logging.New(cfg.Logger),
		stop:   make(chan struct{}, 1),
		reload: make(chan struct{}, 1),
		// journalUrgentAlertLog: make(map[int]time.Time),
		// journalInternalErrors: make(map[string]time.Time),
		servedApps: make(map[int]context.CancelFunc),
		senders:    make([]notification.Notificator, len(cfg.Notification.Enabled)),
	}, nil
}

//TODO: Переделать логи!!

func (s *Service) Start() {
	s.log.Infof("***********************SERVICE [%s] START***********************", version)
	flog := s.log.WithField("Root", "Service")
	mainCtx, globCancel := context.WithCancel(context.Background())
	defer globCancel()
	pool, err := postgres.Connect(mainCtx, s.cfg.Postgres)
	if err != nil {
		flog.Fatalln("database connect:", err)
	}

	s.store = database.New(pool)

	for _, messenger := range s.cfg.Notification.Enabled {

		switch messenger {
		case "email":
			s.senders = append(s.senders, email.New(s.cfg.Notification.Email))
		case "telegram":
			s.senders = append(s.senders, telegram.New(s.cfg.Notification.Telegram))
		case "bitrix":
			s.senders = append(s.senders, bitrix.New(s.cfg.Notification.Bitrix))
		}
		s.log.Debugf("Added Notificator: %s : %#v", messenger, s.senders)
	}
	if len(s.senders) == 0 {
		flog.Fatal("no connected notificators")
	}
	s.logconfigInfo()

	go s.sendAdminNotification(TEST)

	go s.monitorSignalOS(mainCtx)
	go s.monitorSignalDB(mainCtx)
	go s.cleanerOldLogs(mainCtx)

	if !s.cfg.WithoutCheckPgAgent {
		go s.checkPgAgent(mainCtx)
	}

	var wgWorkers sync.WaitGroup
	if err := s.startAllAlertWorkers(mainCtx, &wgWorkers); err != nil {
		flog.Fatalln("start alert workers:", err)
	}

CONTROL:
	for {
		select {
		case <-s.stop:
			for _, stop := range s.servedApps {
				stop()
			}
			break CONTROL
		case <-s.reload:
			for _, stop := range s.servedApps {
				stop()
			}
			wgWorkers.Wait()
			if err := s.startAllAlertWorkers(mainCtx, &wgWorkers); err != nil {
				flog.Errorln("start alert workers:", err)
				s.sendAdminNotification(ERROR, err)
				//TODO: пробовать опять спустя время
			}

		default:
			time.Sleep(1 * time.Second)
		}
	}

	wgWorkers.Wait()
	globCancel()
	flog.Info("database connection closed")
	s.log.Info("***********************SERVICE STOP************************")
}

func (s *Service) monitorSignalOS(ctx context.Context) {
	flog := s.log.WithField("Root", "Monitor signal OS")
	flog.Info("start monitor OS.Signal")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig)
	for {
		select {
		case <-ctx.Done():
			flog.Warn("stop monitor OS.Sygnal")
			return
		case c := <-sig:
			switch c {
			case syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL, syscall.SIGABRT:
				flog.Infof("signal recived: %v", c)
				s.stop <- struct{}{}
				return
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (s *Service) monitorSignalDB(ctx context.Context) {
	flog := s.log.WithField("Root", "Monitor signal DB")
	flog.Info("start monitor user-interface")
	for {
		select {
		case <-ctx.Done():
			flog.Warn("stop monitor user-interface")
			return
		default:
			sig, err := s.store.GetSignalDB(ctx)
			if err != nil {
				flog.Errorln("read control signals:", err)
				go s.sendAdminNotification(ERROR, fmt.Errorf("read control signals from DB: %w", err))
			}
			switch {
			case sig.Stop:
				s.log.Info("stop signal received")
				if err := s.store.ResetFlags(ctx); err != nil {
					flog.Errorln("reset control flags:", err)
				}
				s.stop <- struct{}{}
				return
			case sig.Reload:
				s.log.Info("reload config signal received")
				if err := s.store.ResetFlags(ctx); err != nil {
					flog.Errorln("reset control flags:", err)
				}
				s.reload <- struct{}{}
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (s *Service) checkPgAgent(ctx context.Context) {
	flog := s.log.WithField("Root", "Check health Pg-Agent")
	flog.Info("start scheduler")
	for {
		select {
		case <-ctx.Done():
			flog.Warn("stop scheduler")
			return
		default:
			if err := s.store.CheckPgAgent(ctx); err != nil {
				flog.Errorln("invoke function public.pgagent_jobs_check():", err)
				go s.sendAdminNotification(ERROR, err)
			}
		}
		time.Sleep(24 * time.Hour)
	}
}

func (s *Service) cleanerOldLogs(ctx context.Context) {
	flog := s.log.WithField("Root", "Cleaner DB")
	alarm := make(chan time.Time)
	signal := func() {
		alarm <- time.Now()
	}
	sig, err := scheduler.Every().Day().At(s.cfg.StartCleanOldLogs).Run(signal)
	if err != nil {
		flog.Errorln("create scheduler:", err)
		flog.Warn("the cleaning schedule will be set to the current time")
		go func() {
			for {
				alarm <- time.Now()
				time.Sleep(24 * time.Hour)
			}
		}()
	}
CLEANER:
	for {
		select {
		case <-ctx.Done():
			flog.Warn("stop worker and scheduler")
			sig.Quit <- true
			break CLEANER
		case <-alarm:
			clearDataStatistic, err := s.store.DeleteOldLogs(ctx)
			if err != nil {
				flog.Errorln("cleaning the database from old logs:", err)
				go s.sendAdminNotification(ERROR, err)
				continue
			}
			for _, cd := range clearDataStatistic {
				flog.Infof("server:[%s] application:[%s] deleted all_logs:%d | alert_logs:%d",
					cd.ServerName,
					cd.ServiceName,
					cd.AllLogs,
					cd.ErrLogs)
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func (s *Service) startAllAlertWorkers(ctx context.Context, wgWorkers *sync.WaitGroup) error {
	ids, err := s.store.AllServedAppIDs(ctx)
	if err != nil {
		return fmt.Errorf("getting the id of all serviced applications: %w", err)
	}
	if len(ids) == 0 {
		return errors.New("the service has no supported applications")
	}
	if s.servedApps == nil {
		s.servedApps = make(map[int]context.CancelFunc)
	}
	for _, id := range ids {
		workerCtx, stopFunc := context.WithCancel(ctx)
		s.servedApps[id] = stopFunc
		alertset, err := s.store.ServedAppAlertSettings(ctx, id)
		if err != nil {
			return fmt.Errorf("getting the alert settings appID=%d : %w", id, err)
		}
		wgWorkers.Add(1)
		go s.alertWorker(workerCtx, alertset, REGULAR, wgWorkers)
		wgWorkers.Add(1)
		go s.alertWorker(workerCtx, alertset, URGENT, wgWorkers)
	}
	return nil
}

func (s *Service) alertWorker(ctx context.Context, alertset *datastructs.AlertSettings, at alertType, wg *sync.WaitGroup) {
	flog := s.log.WithFields(logrus.Fields{
		"Root":    "Alert Worker",
		"Type":    at.name,
		"Server":  alertset.ServerName,
		"Service": alertset.ServiceName,
	})
	flog.Info("start worker")
	ticker := time.NewTicker(at.sendPeriod)
	flog.Tracef("create new ticker for check alerts with period: %s", at.sendPeriod)
	defer wg.Done()
	for {
		var (
			err    error
			alerts []datastructs.AlertLog
		)
		select {
		case <-ctx.Done():
			flog.Warn("stop worker")
			return
		case <-ticker.C:
			flog.Trace("check alerts")
			alerts, err = s.store.AlertLogs(ctx, alertset.ServiceID)
			if err != nil {
				flog.Errorf("obtain alert logs: %s", err)
				go s.sendAdminNotification(ERROR, fmt.Errorf("obtain %s alert logs: %w", at.name, err))
				continue
			}
			if len(alerts) == 0 {
				continue
			}

			//разделяю полученные логи на срочные и ежечасные в зависимости от воркера
			var sendPool []datastructs.AlertLog
			for _, a := range alerts {
				switch {
				case at.name == "URGENT":
					if !alertset.SendUrgent {
						continue
					}
					if len(alertset.IfMessageContains) != 0 {
						if !contains(a.Log.Message, alertset.IfMessageContains) {
							continue
						}
					}

				case at.name == "REGULAR":
					if alertset.SendUrgent {
						continue
					}
				}
				sendPool = append(sendPool, a)
			}

			if len(sendPool) == 0 {
				continue
			}

			flog.Tracef("obtain:%d all unsend alerts", len(sendPool))

			//Надо проверять, что для данного сервиса уже истек интервал отсылки срочных логов
			if at.name == "URGENT" {
				flog.Trace("check send last urgent")
				if !s.expirePeriodSendLastUrgent(alertset.ServiceID, time.Now()) {
					flog.Trace("last send urgent not expired period")
					continue
				}
			}

			flog.Trace("try send alerts")

			if err := s.sendAlertNotification(ctx, at.name, alertset, sendPool); err != nil {
				if muerr, ok := err.(*multierror.Error); ok {
					if muerr.ErrorOrNil() != nil {
						flog.Errorf("not all messages completed successfully - %s", muerr)
					}
				}
			}

		default:
			time.Sleep(1 * time.Second)
		}

	}
}

// Checking if the text contains any phrase from the list
func contains(text string, phrases []string) bool {
	if phrases[0] != "" {
		for _, p := range phrases {
			if strings.Contains(text, strings.TrimSpace(p)) {
				return true
			}
		}
	}
	return false
}

func (s *Service) expirePeriodSendLastInternalError(err string, tn time.Time) bool {
	ti, ok := s.journalInternalErrors.Load(err)
	if ok {
		if tn.Sub(ti.(time.Time)) <= s.cfg.SendErrorPeriod {
			return false
		}
	}
	s.journalInternalErrors.Store(err, tn)
	return true
}

func (s *Service) expirePeriodSendLastUrgent(appID int, tn time.Time) bool {
	ti, ok := s.journalUrgentAlertLog.Load(appID)
	if ok {
		if tn.Sub(ti.(time.Time)) <= s.cfg.SendUrgentPeriod {
			return false
		}
	}
	s.journalUrgentAlertLog.Store(appID, tn)
	return true
}

func (s *Service) logconfigInfo() {
	s.log.Debugf(`Obtain Service Configuration:

    Server Name         : %s
    Check Urgent Alerts : %s
    Check Regular Alerts: %s
    Send Error Period   : %d
    Send Urgent Period  : %d
    Start Clean Old Logs: %s
    Num Logs Attach     : %d
    Num Attempts Fail   : %d
	Notification Enabled: %s

Postgres:
    User         : %s
    Pass         : %s
    Host         : %s
    Port         : %d
    Database     : %s
    Schema       : %s
    SSLMode      : %s
    PoolMaxConns : %d

Email:
	SmtpUser    : %s
	SmtpPass    : %s
	SmtpHost    : %s
	SmtpPort    : %s
	VisibleName : %s
	Timeout     : %s
	WithoutAuth : %t
	Admin Emails: %s
`,
		s.cfg.ServerName,
		s.cfg.CheckUrgentAlerts,
		s.cfg.CheckRegularAlerts,
		s.cfg.SendErrorPeriod,
		s.cfg.SendUrgentPeriod,
		s.cfg.StartCleanOldLogs,
		s.cfg.NumLogsAttach,
		s.cfg.NumAttemptsFail,
		s.cfg.Notification.Enabled,
		s.cfg.Postgres.User,
		s.cfg.Postgres.Pass,
		s.cfg.Postgres.Host,
		s.cfg.Postgres.Port,
		s.cfg.Postgres.Database,
		s.cfg.Postgres.Schema,
		s.cfg.Postgres.SSLMode,
		s.cfg.Postgres.PoolMaxConns,
		s.cfg.Notification.Email.SmtpUser,
		s.cfg.Notification.Email.SmtpPass,
		s.cfg.Notification.Email.SmtpHost,
		s.cfg.Notification.Email.SmtpPort,
		s.cfg.Notification.Email.VisibleName,
		s.cfg.Notification.Email.Timeout,
		s.cfg.Notification.Email.WithoutAuth,
		s.cfg.AdminEmails,
	)
	s.log.Debug("Check Notificators:")
	for _, name := range s.senders {
		s.log.Debugln(name.String(), " = OK!")
	}
}
