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
	"redits.oculeus.com/asorokin/connect/postgres"
	"redits.oculeus.com/asorokin/logging"
	"redits.oculeus.com/asorokin/notification"
	"redits.oculeus.com/asorokin/notification/bitrix"
	"redits.oculeus.com/asorokin/notification/email"
	"redits.oculeus.com/asorokin/notification/telegram"

	"github.com/NuclearLouse/scheduler"
	conf "github.com/tinrab/kit/cfg"
)

var (
	version, cfgFile string
	REGULAR, URGENT  alertType
)

const serviceName = "Alert-Service"

type Service struct {
	cfg         *config
	log         *logging.Logger
	store       storer
	notificator map[string]notification.Notificator
	stop        chan struct{}
	reload      chan struct{}
	servedApps  map[int]context.CancelFunc
	sync.RWMutex
	journalUrgentAlertLog map[int]int64
	journalInternalErrors map[string]int64
}

type config struct {
	ServerName         string           `cfg:"server_name"`
	StartCleanOldLogs  string           `cfg:"start_clean_old_logs"`
	CheckRegularAlerts time.Duration    `cfg:"check_regular_alerts"`
	CheckUrgentAlerts  time.Duration    `cfg:"check_urgent_alerts"`
	SendUrgentPeriod   int64            `cfg:"send_urgent_period"`
	SendErrorPeriod    int64            `cfg:"send_error_period"`
	NumLogsAttach      int              `cfg:"num_logs_for_attach"`
	NumAttemptsFail    int              `cfg:"num_attempts_fail"`
	AdminEmails        []string         `cfg:"admin_emails"`
	Logger             *logging.Config  `cfg:"logger"`
	Postgres           *postgres.Config `cfg:"postgres"`
	Notification       struct {
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
		cfg:                   cfg,
		log:                   logging.New(cfg.Logger),
		stop:                  make(chan struct{}, 1),
		reload:                make(chan struct{}, 1),
		journalUrgentAlertLog: make(map[int]int64),
		journalInternalErrors: make(map[string]int64),
		servedApps:            make(map[int]context.CancelFunc),
		notificator:           make(map[string]notification.Notificator),
	}, nil
}

func (s *Service) Start() {
	s.log.Infof("***********************SERVICE [%s] START***********************", version)
	mainCtx, globCancel := context.WithCancel(context.Background())
	defer globCancel()
	pool, err := postgres.Connect(mainCtx, s.cfg.Postgres)
	if err != nil {
		s.log.Fatalln("Service: database connect:", err)
	}

	s.store = database.New(pool)

	for _, messenger := range s.cfg.Notification.Enabled {
		
		switch messenger {
		case "email":
			s.notificator[messenger] = email.New(s.cfg.Notification.Email)
		case "telegram":
			s.notificator[messenger] = telegram.New(s.cfg.Notification.Telegram)
		case "bitrix":
			s.notificator[messenger] = bitrix.New(s.cfg.Notification.Bitrix)
		}
		s.log.Debugf("Added Notificator: %s : %#v", messenger, s.notificator[messenger])
	}
	s.log.Debugf("Notificators: %#v\n", s.notificator)
	s.logconfigInfo()

	go s.sendAdminNotificate(TEST_CONNECT)

	go s.monitorSignalOS(mainCtx)
	go s.monitorSignalDB(mainCtx)
	go s.cleanerOldLogs(mainCtx)
	go s.checkPgAgent(mainCtx)

	var wgWorkers sync.WaitGroup
	if err := s.startAllAlertWorkers(mainCtx, &wgWorkers); err != nil {
		s.log.Fatalln("Service: start alert workers:", err)
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
				s.log.Fatalln("Service: start alert workers:", err)
			}

		default:
			time.Sleep(1 * time.Second)
		}
	}

	wgWorkers.Wait()
	globCancel()
	s.log.Info("Database connection closed")
	s.log.Info("***********************SERVICE STOP************************")
}

func (s *Service) monitorSignalOS(ctx context.Context) {
	s.log.Info("Monitor signal OS: start monitor OS.Signal")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig)
	for {
		select {
		case <-ctx.Done():
			s.log.Warn("Monitor signal OS: stop monitor OS.Sygnal")
			return
		case c := <-sig:
			switch c {
			case syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL, syscall.SIGABRT:
				s.log.Infof("Monitor signal OS: signal recived: %v", c)
				s.stop <- struct{}{}
				return
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (s *Service) monitorSignalDB(ctx context.Context) {
	s.log.Info("Monitor signal DB: start monitor user-interface")
	for {
		select {
		case <-ctx.Done():
			s.log.Warn("Monitor signal DB: stop monitor user-interface")
			return
		default:
			sig, err := s.store.GetSignalDB(ctx)
			if err != nil {
				s.log.Errorln("Monitor signal DB: read control signals:", err)
				go s.sendAdminNotificate(INTERNAL_ERROR, fmt.Errorf("read control signals from DB: %w", err))
			}
			switch {
			case sig.Stop:
				s.log.Info("Monitor signal DB: stop signal received")
				if err := s.store.ResetFlags(ctx); err != nil {
					s.log.Errorln("Monitor signal DB: reset control flags:", err)
				}
				s.stop <- struct{}{}
				return
			case sig.Reload:
				s.log.Info("Monitor signal DB: reload config signal received")
				if err := s.store.ResetFlags(ctx); err != nil {
					s.log.Errorln("Monitor signal DB: reset control flags:", err)
				}
				s.reload <- struct{}{}
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (s *Service) checkPgAgent(ctx context.Context) {
	s.log.Info("Check health Pg-Agent: start scheduler")
	for {
		select {
		case <-ctx.Done():
			s.log.Warn("Check health Pg-Agent: stop scheduler")
			return
		default:
			if err := s.store.CheckPgAgent(ctx); err != nil {
				s.log.Errorln("Check health Pg-Agent: invoke function public.pgagent_jobs_check():", err)
				go s.sendAdminNotificate(INTERNAL_ERROR, err)
			}
		}
		time.Sleep(24 * time.Hour)
	}
}

func (s *Service) cleanerOldLogs(ctx context.Context) {
	alarm := make(chan time.Time)
	signal := func() {
		alarm <- time.Now()
	}
	sig, err := scheduler.Every().Day().At(s.cfg.StartCleanOldLogs).Run(signal)
	if err != nil {
		s.log.Errorln("Cleaner DB: create scheduler:", err)
		s.log.Warn("Cleaner DB: the cleaning schedule will be set to the current time")
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
			s.log.Warn("Cleaner DB: stop worker and scheduler")
			sig.Quit <- true
			break CLEANER
		case <-alarm:
			clearDataStatistic, err := s.store.DeleteOldLogs(ctx)
			if err != nil {
				s.log.Errorln("Cleaner DB: cleaning the database from old logs:", err)
				go s.sendAdminNotificate(INTERNAL_ERROR, err)
				continue
			}
			for _, cd := range clearDataStatistic {
				s.log.Infof("Cleaner DB: Server[%s] Application[%s] delete all_logs:%d | alert_logs:%d",
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

	s.log.Infof("Alert Worker: start %s alert worker for application:%s %s", at.name, alertset.ServerName, alertset.ServiceName)
	ticker := time.NewTicker(at.sendPeriod)
	s.log.Tracef("Alert Worker:for application:%s %s create new ticker for check alerts %s with period: %.f minute", alertset.ServerName, alertset.ServiceName, at.name, at.sendPeriod.Minutes())
	defer wg.Done()
	for {
		var (
			err    error
			alerts []datastructs.AlertLog
		)
		select {
		case <-ctx.Done():
			s.log.Warnf("Alert Worker: stop %s alert worker for application:%s %s", at.name, alertset.ServerName, alertset.ServiceName)
			return
		case <-ticker.C:
			s.log.Tracef("Alert Worker:for application:%s %s check %s ALERTS", alertset.ServerName, alertset.ServiceName, at.name)
			alerts, err = s.store.AlertLogs(ctx, alertset.ServiceID)
			if err != nil {
				s.log.Errorf("Alert Worker: %s for application:%s %s obtain %s alert logs: %s", at.name, alertset.ServerName, alertset.ServiceName, at.name, err)
				go s.sendAdminNotificate(INTERNAL_ERROR, fmt.Errorf("obtain %s alert logs: %w", at.name, err))
				continue
			}
		}
		s.log.Tracef("Alert Worker:for application:%s %s obtain:%d all unsend alerts", alertset.ServerName, alertset.ServiceName, len(alerts))
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

		s.log.Tracef("Alert Worker:for application:%s %s obtain:%d %s ALERTS", alertset.ServerName, alertset.ServiceName, len(sendPool), at.name)
		if len(sendPool) == 0 {
			continue
		}

		//Надо проверять, что для данного сервиса уже истек интервал отсылки срочных логов
		if at.name == "URGENT" {
			s.log.Tracef("Alert Worker:for application:%s %s check send last urgent", alertset.ServerName, alertset.ServiceName)
			timeNow := time.Now().Unix()
			lastSend := s.checkSendLastUrgent(alertset.ServiceID, timeNow)
			s.log.Tracef("Alert Worker:for application:%s %s last send urgent:%s", alertset.ServerName, alertset.ServiceName, time.Unix(lastSend, 0))
			if lastSend != 0 {
				if (timeNow-lastSend)/60 <= s.cfg.SendUrgentPeriod {
					continue
				}
			}
		}
		s.log.Tracef("Alert Worker:for application:%s %s try send %s ALERTS", alertset.ServerName, alertset.ServiceName, at.name)
		if err := s.trySendAlertEmail(ctx, at.name, alertset, sendPool); err != nil {
			s.log.Errorf("Alert Worker: %s for application:%s %s try send alerts email: %s", at.name, alertset.ServerName, alertset.ServiceName, err)
			continue
		}
		//выставить флаги удачной рассылки для ids
		var ids []int64
		for _, data := range sendPool {
			ids = append(ids, data.RecordID)
		}
		s.log.Tracef("Alert Worker:for application:%s %s set send TRUE flags: %d %s ALERTS", alertset.ServerName, alertset.ServiceName, len(ids), at.name)
		if err := s.store.SetSendFlag(ctx, ids); err != nil {
			s.log.Errorf("Alert Worker: %s for application:%s %s set send TRUE flags: %s", at.name, alertset.ServerName, alertset.ServiceName, err)
		}

		time.Sleep(1 * time.Second)
		// time.Sleep(at.sendPeriod)
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

func (s *Service) trySendAlertEmail(ctx context.Context, typeAlert string, alertset *datastructs.AlertSettings, pool []datastructs.AlertLog) error {

	for tries := 0; tries < s.cfg.NumAttemptsFail; tries++ {
		select {
		case <-ctx.Done():
			return errors.New("termination command received")
		default:
			err := s.sendUserNotificate(alertset, pool)
			if err == nil {
				return nil
			}
			s.log.Errorf("Tries: %d | Could not send emails with %s alerts for %s %s: %s", tries+1, typeAlert, alertset.ServerName, alertset.ServiceName, err)
			if tries+1 < s.cfg.NumAttemptsFail {
				time.Sleep(time.Second << uint(tries))
			}
		}

	}
	return errors.New("all attempts have been exhausted")
}

func (s *Service) registrInternalError(err error, tn int64) {
	s.Lock()
	if s.journalInternalErrors == nil {
		s.journalInternalErrors = make(map[string]int64)
	}
	s.journalInternalErrors[err.Error()] = tn
	s.Unlock()
}

func (s *Service) checkSendLastInternalError(err error, tn int64) int64 {
	s.RLock()
	t, ok := s.journalInternalErrors[err.Error()]
	if ok {
		s.RUnlock()
		return t
	}
	s.RUnlock()
	s.registrInternalError(err, tn)
	return 0
}

func (s *Service) registrUrgent(appID int, tn int64) {
	s.Lock()
	if s.journalUrgentAlertLog == nil {
		s.journalUrgentAlertLog = make(map[int]int64)
	}
	s.journalUrgentAlertLog[appID] = tn
	s.Unlock()
}

func (s *Service) checkSendLastUrgent(appID int, tn int64) int64 {
	s.RLock()
	t, ok := s.journalUrgentAlertLog[appID]
	if ok {
		s.RUnlock()
		return t
	}
	s.RUnlock()
	s.registrUrgent(appID, tn)
	return 0
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
	for name, notif := range s.notificator {
		s.log.Debugln(name, " = ", notif != nil)
	}
}
