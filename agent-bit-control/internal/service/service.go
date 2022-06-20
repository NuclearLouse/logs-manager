package service

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/template"
	"time"

	"redits.oculeus.com/asorokin/connect/postgres"
	"redits.oculeus.com/asorokin/logging"
	"redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/database"
	"redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/datastructs"
	"redits.oculeus.com/asorokin/systemctl"

	conf "github.com/tinrab/kit/cfg"
)

var (
	version, cfgFile string
)

//go:embed templates/*.tmpl
var tmpl embed.FS

const sysctlFile = "agent-bit-control.service"

type Service struct {
	cfg        *config
	store      storer
	log        *logging.Logger
	stop       chan struct{}
	servedApps []*datastructs.ServedApplication
}

type config struct {
	ServerName        string `cfg:"server_name"`
	RootPassword      string `cfg:"root_password"`
	TableAllLogs      string `cfg:"table_all_logs"`
	TableAlertLogs    string `cfg:"table_alerts_logs"`
	FluentServiceFile string `cfg:"fluent_service_file"`
	PathFluentConfig  string `cfg:"path_fluent_config"`

	Logger   *logging.Config  `cfg:"logger"`
	Postgres *postgres.Config `cfg:"postgres"`
}

func Version() {
	fmt.Print("Version=", version)
}

func New() (*Service, error) {

	c := conf.New()
	if err := c.LoadFile(cfgFile); err != nil {
		return nil, fmt.Errorf("load config files: %w", err)
	}
	cfg := &config{
		Logger:   logging.DefaultConfig(),
		Postgres: postgres.DefaultConfig(),
	}
	if err := c.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("mapping config files: %w", err)
	}

	return &Service{
		cfg:  cfg,
		log:  logging.New(cfg.Logger),
		stop: make(chan struct{}, 1),
	}, nil
}

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

	go s.monitorSignalOS(mainCtx)
	go s.monitorSignalDB(mainCtx)

CONTROL:
	for {
		select {
		case <-s.stop:
			break CONTROL
		default:
			time.Sleep(1 * time.Second)
		}
	}
	globCancel()
	flog.Info("database connection closed")
	s.log.Info("***********************SERVICE STOP************************")
	if err := s.Stop(); err != nil {
		s.log.Errorln("unexpected error on shutdown: %s", err)
	}
}

func (s *Service) Stop() error {
	root, err := systemctl.IsRootPermissions()
	if err != nil {
		return err
	}

	if root {
		_, err = systemctl.ServiceWithRootPermissions("stop", sysctlFile)
	} else {
		_, err = systemctl.ServiceNoRootPermissions("stop", sysctlFile, s.cfg.RootPassword)
	}
	return err
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
		default:
			time.Sleep(1 * time.Second)
		}
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
			sig, err := s.store.GetSignalDB(ctx, s.cfg.ServerName)
			if err != nil {
				flog.Errorln("read control signals:", err)
			}
			switch {
			case sig.Stop:
				flog.Info("stop signal received")
				if err := s.store.ResetFlags(ctx, s.cfg.ServerName); err != nil {
					flog.Errorln("reset control flags:", err)
				}
				s.stop <- struct{}{}
				return
			case sig.Reload:
				s.log.Info("reload config signal received")
				if err := func() error {
					//TODO: Надо сохранить в переменную весь файл и при любой ошибке откатываться на старый конфиг
					//TODO: не надо выходить из функции с ошибкой!
					data, err := s.readServiceBlock()
					if err != nil {
						return fmt.Errorf("read config file service block: %w", err)
					}

					if len(data) == 0 {
						data, err = s.defaultConfigServiceBlock()
						if err != nil {
							return fmt.Errorf("get default service block: %w", err)
						}
					}

					if err := s.rewriteServiceBlock(data); err != nil {
						return fmt.Errorf("rewrite config file service block: %w", err)
					}

					apps, err := s.store.GetServedApplications(ctx, s.cfg.ServerName)
					if err != nil {
						return fmt.Errorf("get served applications: %w", err)
					}
					s.servedApps = apps

					if err := s.writeNewConfig(); err != nil {
						return fmt.Errorf("write new config file: %w", err)
					}

					if err := s.restartAgent(); err != nil {
						return fmt.Errorf("restart fluent-bit.service: %w", err)
					}
					return nil
				}(); err != nil {
					flog.Errorln("reload fluent-bit:", err)
				}
				if err := s.store.ResetFlags(ctx,
					s.cfg.ServerName); err != nil {
					flog.Errorln("reset control flags:", err)
				}
			}
		}
	}
}

func (s *Service) restartAgent() error {
	root, err := systemctl.IsRootPermissions()
	if err != nil {
		return err
	}
	if root {
		if _, err := systemctl.ServiceWithRootPermissions("stop", s.cfg.FluentServiceFile); err != nil {
			return err
		}
		if _, err := systemctl.ServiceWithRootPermissions("start", s.cfg.FluentServiceFile); err != nil {
			return err
		}
	} else {
		if _, err := systemctl.ServiceNoRootPermissions("stop", s.cfg.FluentServiceFile, s.cfg.RootPassword); err != nil {
			return err
		}
		if _, err := systemctl.ServiceNoRootPermissions("start", s.cfg.FluentServiceFile, s.cfg.RootPassword); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) readServiceBlock() ([]string, error) {
	file, err := os.Open(s.cfg.PathFluentConfig)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	r := bufio.NewReader(file)
	var data []string
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			switch err {
			case io.EOF:
				return data, nil
			default:
				return nil, fmt.Errorf("unable to read config file: %w", err)
			}
		}
		if strings.Contains(string(line), "*Block") || strings.Contains(string(line), "[INPUT]") {
			return data, nil
		}
		data = append(data, string(line))
	}
}

func (s *Service) rewriteServiceBlock(data []string) error {
	file, err := os.Create(s.cfg.PathFluentConfig)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, line := range data {
		if _, err := file.WriteString(fmt.Sprintf("%s\n", line)); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) defaultConfigServiceBlock() ([]string, error) {
	tmplData := datastructs.TemplatesData{
		Flush:           "5",
		Daemon:          "off",
		LogLevel:        "info",
		PluginsConfFile: "/etc/fluent-bit/plugins.conf",
		HTTPServer:      "off",
		StorageMetrics:  "on",
	}
	buf := new(bytes.Buffer)
	if err := template.Must(template.ParseFS(tmpl, fileTemplate("service"))).Execute(buf, tmplData); err != nil {
		return nil, fmt.Errorf("execute data template: %w", err)
	}
	r := bufio.NewReader(buf)
	var data []string
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			switch err {
			case io.EOF:
				return data, nil
			default:
				return nil, fmt.Errorf("unable to read buffer: %w", err)
			}
		}
		data = append(data, string(line))
	}
}

//TODO: при ошибке не перезаписывает весь конфиг, а только блок [SERVICE] !!!
func (s *Service) writeNewConfig() error {

	separator := template.Must(template.ParseFS(tmpl, fileTemplate("separator")))
	input := template.Must(template.ParseFS(tmpl, fileTemplate("input")))
	output := template.Must(template.ParseFS(tmpl, fileTemplate("output")))
	filter := template.Must(template.ParseFS(tmpl, fileTemplate("filter")))

	var templates []struct {
		template *template.Template
		data     interface{}
	}
	for _, srv := range s.servedApps {
		templates = append(templates, []struct {
			template *template.Template
			data     interface{}
		}{
			{
				template: separator,
				data: datastructs.TemplatesData{
					AppName: srv.AppName,
				},
			},
			{
				template: input,
				data: datastructs.TemplatesData{
					Tag:  srv.GeneralTag,
					Path: srv.LogFile,
				},
			},
			{
				template: output,
				data: datastructs.TemplatesData{
					Match:    srv.GeneralTag,
					Host:     s.cfg.Postgres.Host,
					Port:     s.cfg.Postgres.Port,
					User:     s.cfg.Postgres.User,
					Password: s.cfg.Postgres.Pass,
					Database: s.cfg.Postgres.Database,
					Table:    s.cfg.TableAllLogs,
					Schema:   s.cfg.Postgres.Schema,
				},
			}}...)

		var alertLog string
		if srv.ErrLogFile == "" {
			alertLog = srv.LogFile
		} else {
			alertLog = srv.ErrLogFile
		}

		for i := 0; i < len(srv.AlertTags); i++ {
			templates = append(templates, []struct {
				template *template.Template
				data     interface{}
			}{
				{
					template: input,
					data: datastructs.TemplatesData{
						Tag:  srv.AlertTags[i],
						Path: alertLog,
					},
				},
				{
					template: output,
					data: datastructs.TemplatesData{
						Match:    srv.AlertTags[i],
						Host:     s.cfg.Postgres.Host,
						Port:     s.cfg.Postgres.Port,
						User:     s.cfg.Postgres.User,
						Password: s.cfg.Postgres.Pass,
						Database: s.cfg.Postgres.Database,
						Table:    s.cfg.TableAlertLogs,
						Schema:   s.cfg.Postgres.Schema,
					},
				},
			}...)
			if len(srv.AlertKeywords) > 0 && srv.LogFile != srv.ErrLogFile || srv.LogFile == "" {
				templates = append(templates, struct {
					template *template.Template
					data     interface{}
				}{
					template: filter,
					data: datastructs.TemplatesData{
						Match: srv.AlertTags[i],
						Regex: srv.AlertKeywords[i],
					},
				})
			}
		}
	}

	file, err := os.OpenFile(s.cfg.PathFluentConfig, os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	for _, t := range templates {
		if err := t.template.Execute(file, t.data); err != nil {
			if err != nil {
				return fmt.Errorf("execute data template: %w", err)
			}
		}
	}
	return nil
}

func fileTemplate(name string) string {
	return fmt.Sprintf("template/%s.*", name)
}
