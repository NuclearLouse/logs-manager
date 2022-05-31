package service

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/database"
	"redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/datastructs"

	"redits.oculeus.com/asorokin/connect/postgres"
	"redits.oculeus.com/asorokin/logging"

	conf "github.com/tinrab/kit/cfg"
)

var (
	version, cfgFile string
)

type Service struct {
	cfg        *config
	store      storer
	log        *logging.Logger
	stop       chan struct{}
	servedApps []*datastructs.ServedApplication
}

type config struct {
	ServerName        string           `cfg:"server_name"`
	UserName          string           `cfg:"user_name"`
	Password          string           `cfg:"user_pass"`
	TableAllLogs      string           `cfg:"table_all_logs"`
	TableAlertLogs    string           `cfg:"table_alerts_logs"`
	FluentServiceFile string           `cfg:"fluent_service_file"`
	PathFluentConfig  string           `cfg:"path_fluent_config"`
	Logger            *logging.Config  `cfg:"logger"`
	Postgres          *postgres.Config `cfg:"postgres"`
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
	mainCtx, globCancel := context.WithCancel(context.Background())
	defer globCancel()
	pool, err := postgres.Connect(mainCtx, s.cfg.Postgres)
	if err != nil {
		s.log.Fatalln("Service: database connect:", err)
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
			s.log.Info("Monitor signal OS: stop monitor OS.Sygnal")
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
	// serverName := s.ini.Section("server").Key("name").String()
	for {
		select {
		case <-ctx.Done():
			s.log.Info("Monitor signal DB: stop monitor user-interface")
			return
		default:
			sig, err := s.store.GetSignalDB(ctx, s.cfg.ServerName)
			if err != nil {
				s.log.Errorln("Monitor signal DB: read control signals:", err)
			}
			switch {
			case sig.Stop:
				s.log.Info("Monitor signal DB: stop signal received")
				if err := s.store.ResetFlags(ctx, s.cfg.ServerName); err != nil {
					s.log.Errorln("Monitor signal DB: reset control flags:", err)
				}
				s.stop <- struct{}{}
				return
			case sig.Reload:
				s.log.Info("Monitor signal DB: reload config signal received")
				if err := func() error {
					//TODO: Надо сохранить в переменную весь файл и при любой ошибке откатываться на старый конфиг
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
						return fmt.Errorf("restart td-agent-bit.service: %w", err)
					}
					return nil
				}(); err != nil {
					s.log.Errorln("Monitor signal DB: reload td-agent-bit:", err)
				}
				if err := s.store.ResetFlags(ctx,
					s.cfg.ServerName); err != nil {
					s.log.Errorln("Monitor signal DB: reset control flags:", err)
				}
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (s *Service) restartAgent() error {
	// _, err := exec.Command("sh", "-c", "echo '"+ sudopassword +"' | sudo -S pkill -SIGINT my_app_name").Output()
	//TODO: Еще можно добавить проверку на whoami и использовать переменную окружения USER_NAME
	out, err := exec.Command("id", "-u").Output()
	if err != nil {
		return fmt.Errorf("check root permisions: %w", err)
	}
	// 0 = root, 501 = non-root user
	i, err := strconv.Atoi(string(out[:len(out)-1]))
	if err != nil {
		return fmt.Errorf("convert answer check root: %w", err)
	}
	type coms struct {
		command string
		args    []string
	}
	var commands []coms
	switch i {
	case 0:
		commands = []coms{
			{
				command: "systemctl",
				args:    []string{"stop", s.cfg.FluentServiceFile},
			},
			{
				command: "systemctl",
				args:    []string{"start", s.cfg.FluentServiceFile},
			},
		}
	default:
		// echo 'PASSWORD' | sudo -S systemctl stop td-agent-bit.service
		commands = []coms{
			{
				command: "echo",
				args: []string{
					fmt.Sprintf("'%s'", s.cfg.Password),
					"|",
					"sudo",
					"-S",
					"systemctl",
					"stop",
					s.cfg.FluentServiceFile,
				},
			},
			{
				command: "echo",
				args: []string{
					fmt.Sprintf("'%s'", s.cfg.Password),
					"|",
					"sudo",
					"-S",
					"systemctl",
					"start",
					s.cfg.FluentServiceFile,
				},
			},
		}
	}
	for _, c := range commands {
		if err := func() error {
			if err := exec.Command(c.command, c.args...).Run(); err != nil {
				return err
			}
			return nil
		}(); err != nil {
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
		PluginsConfFile: "/etc/td-agent-bit/plugins.conf",
		HTTPServer:      "off",
		StorageMetrics:  "on",
	}
	buf := new(bytes.Buffer)
	if err := template.Must(template.ParseFiles(fileTemplate("service"))).Execute(buf, tmplData); err != nil {
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

	separator := template.Must(template.ParseFiles(fileTemplate("separator")))
	input := template.Must(template.ParseFiles(fileTemplate("input")))
	output := template.Must(template.ParseFiles(fileTemplate("output")))
	filter := template.Must(template.ParseFiles(fileTemplate("filter")))

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
	return filepath.Join("./templates", name+".tmpl")
}
