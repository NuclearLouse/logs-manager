package service

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"path/filepath"
	"strings"
	"time"

	"redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/datastructs"
	"redits.oculeus.com/asorokin/notification"
)

type dataToSend struct {
	Error string
	//field need only for sending alert
	Alert        alertType
	ServerName   string
	ServiceName  string
	NumAlertLogs int
	AlertLogs    []datastructs.AlertLog
}

type alertType struct {
	name       string
	sendPeriod time.Duration
}

type notificate int

const (
	TEST_CONNECT notificate = iota
	INTERNAL_ERROR
	ALERT
	ALERT_WITH_ATTACH
)

func (n notificate) String() string {
	switch n {
	case TEST_CONNECT:
		return "test_connect"
	case INTERNAL_ERROR:
		return "internal_error"
	case ALERT:
		return "alert_logs"
	case ALERT_WITH_ATTACH:
		return "alert_logs_with_attach"
	}
	return ""
}

func (n notificate) makeSubject(serverName, serviceName string) string {
	return fmt.Sprintf("%s: [%s] from %s Server",
		[4]string{"TEST CONNECT", "INTERNAL ERROR", "ALERT MESSAGE", "ALERT MESSAGE"}[n],
		serviceName,
		serverName)
}

type dataTemplate struct {
	Header    string
	Server    string
	Service   string
	Error     string
	AlertLogs []datastructs.AlertLog
}

func (s *Service) sendAdminNotificate(typeNotificate notificate, internalError ...error) {

	if internalError != nil {
		timeNow := time.Now().Unix()
		lastSend := s.checkSendLastInternalError(internalError[0], timeNow)
		switch {
		case lastSend != 0:
			if (timeNow-lastSend)/60 <= s.cfg.SendErrorPeriod {
				return
			}
		}
	}
	header := typeNotificate.makeSubject(s.cfg.ServerName, serviceName)
	var interr string
	if internalError != nil {
		interr = internalError[0].Error()
	}
	data := dataTemplate{
		Header:  header,
		Error:   interr,
		Service: serviceName,
		Server:  s.cfg.ServerName,
	}
	for senderName, sender := range s.notificator {
		body := new(bytes.Buffer)
		err := template.Must(template.ParseFiles(filepath.Join("templates", senderName, typeNotificate.String()+".tmpl"))).Execute(body, data)
		if err != nil {
			s.log.Errorln("parse template:", err)
			return
		}

		if err := sender.SendMessage(notification.Message{
			Addresses: s.getAdminAddresses(senderName),
			Content:   body,
			Subject:   header,
		}); err == nil {
			s.log.Infof("successful sended [%s] message", senderName)
		} else {
			s.log.Errorf("sending [%s] message:%s", senderName, err)
		}
	}
}

func (s *Service) getAdminAddresses(notificator string) []string {
	switch notificator {
	case "email":
		return s.cfg.AdminEmails
	case "bitrix":
		return []string{s.cfg.Notification.Bitrix.AdminID}
	}
	return []string{}
}

/*
Отправку сообщений с вложением пока реализовал только для емейла
Для отправки в Битрикс - просто в шаблоне будет указываться, чтоб смотрели емейл
*/

func (s *Service) sendUserNotificate(alertset *datastructs.AlertSettings, pool []datastructs.AlertLog) error {

	header := ALERT.makeSubject(alertset.ServerName, alertset.ServiceName)
	data := dataTemplate{
		Header:    header,
		Server:    alertset.ServerName,
		Service:   alertset.ServiceName,
		AlertLogs: pool,
	}

	tmplName := ALERT.String()
	var attach notification.Attachment
	if len(pool) >= s.cfg.NumLogsAttach {
		tmplName = ALERT_WITH_ATTACH.String()
		attach.Filename = strings.ReplaceAll(
			fmt.Sprintf("%s_%s_%d_errors.log",
				strings.ToLower(alertset.ServerName),
				strings.ToLower(alertset.ServiceName),
				time.Now().Unix()),
			" ", "-")
		buf := new(bytes.Buffer)
		for _, l := range pool {
			if _, err := io.WriteString(buf, l.Log.Message+"\n"); err != nil {
				return err
			}
		}
		attach.Content = buf
		attach.ContentType = "text/plain"

	}
	for senderName, sender := range s.notificator {
		body := new(bytes.Buffer)
		err := template.Must(template.ParseFiles(filepath.Join("templates", senderName, tmplName+".tmpl"))).Execute(body, data)
		if err != nil {
			return err
		}
		if err := sender.SendMessage(
			notification.Message{
				Addresses: alertset.GetUserAddresses(senderName),
				Content:   body,
				Subject:   header,
			},
			attach); err != nil {
			return err
		}
	}
	return nil
}
