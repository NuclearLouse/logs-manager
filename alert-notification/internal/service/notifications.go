package service

import (
	"bytes"
	"context"
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

func (s *Service) sendAdminNotification(typeNotificate notificate, internalError ...error) {
	s.log.Debugln("try send admin notification:", typeNotificate.String())
	if internalError != nil {
		s.log.Debug("check time last send notification")
		timeNow := time.Now().Unix()
		lastSend := s.checkSendLastInternalError(internalError[0], timeNow)
		switch {
		case lastSend != 0:
			if (timeNow-lastSend)/60 <= s.cfg.SendErrorPeriod {
				s.log.Debug("the last notice period has not expired")
				return
			}
		}
	}
	header := typeNotificate.makeSubject(s.cfg.ServerName, serviceName)
	s.log.Debugln("make message header/subject:", header)
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
		s.log.Debug("trying to fill the template with data")
		err := template.Must(template.ParseFiles(filepath.Join("templates", senderName, typeNotificate.String()+".tmpl"))).Execute(body, data)
		if err != nil {
			s.log.Errorln("parse template:", err)
			return
		}
		addresses := s.getAdminAddresses(senderName)
		s.log.Debugf("trying send message via [%s] to: %s", senderName, addresses)
		if err := sender.SendMessage(notification.Message{
			Addresses: addresses,
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

func (s *Service) sendAlertNotification(ctx context.Context, typeAlert string, alertset *datastructs.AlertSettings, pool []datastructs.AlertLog) {
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
				s.log.Errorf("Alert Worker: [%s] alert [%s] from %s: %s", typeAlert, alertset.ServiceName, alertset.ServerName, err)
				return
			}
		}
		attach.Content = buf
		attach.ContentType = "text/plain"

	}

	successChan := make(chan error, len(s.notificator))
	//если хоть один нотификатор сработал, то надо выставлять флаг успешной отправки
	for key, value := range s.notificator {
		senderName := key
		sender := value
		body := new(bytes.Buffer)
		err := template.Must(template.ParseFiles(filepath.Join("templates", senderName, tmplName+".tmpl"))).Execute(body, data)
		if err != nil {
			s.log.Errorf("Alert Worker: [%s] alert [%s] from %s: %s", typeAlert, alertset.ServiceName, alertset.ServerName, err)
			return
		}
		message := notification.Message{
			Addresses: alertset.GetUserAddresses(senderName),
			Content:   body,
			Subject:   header,
		}
		go s.trySendMessage(ctx, typeAlert, alertset, senderName, sender, message, attach, successChan)
	}

	var i int
	for {
		if i == len(s.notificator) {
			close(successChan)
			break
		}
		select {
		case <-ctx.Done():
			s.log.Error("Alert Worker: termination command received")
			return
		case err := <-successChan:
			i++
			if err == nil {
				//выставить флаги удачной рассылки для ids если хоть один из нотификаторов вернулся без ошибки
				var ids []int64
				for _, data := range pool {
					ids = append(ids, data.RecordID)
				}
				s.log.Tracef("Alert Worker:for application:%s %s set send TRUE flags: %d %s ALERTS", alertset.ServerName, alertset.ServiceName, len(ids), typeAlert)
				if err := s.store.SetSendFlag(ctx, ids); err != nil {
					s.log.Errorf("Alert Worker: %s for application:%s %s set send TRUE flags: %s", typeAlert, alertset.ServerName, alertset.ServiceName, err)
					go s.sendAdminNotification(INTERNAL_ERROR, fmt.Errorf("set send TRUE flags: %w", err))
				}
				return
			}
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

}

func (s *Service) trySendMessage(
	ctx context.Context,
	typeAlert string,
	alertset *datastructs.AlertSettings,
	senderName string,
	sender notification.Notificator,
	message notification.Message,
	attach notification.Attachment,
	success chan error) {

	for tries := 0; tries < s.cfg.NumAttemptsFail; tries++ {
		select {
		case <-ctx.Done():
			return
		default:
			err := sender.SendMessage(message, attach)
			if err == nil {
				success <- nil
				return
			}
			s.log.Errorf("Tries: %d | Could not send emails with [%s] alerts for [%s] from %s: %s", tries+1, typeAlert, alertset.ServiceName, alertset.ServerName, err)
			if tries+1 < s.cfg.NumAttemptsFail {
				time.Sleep(time.Second << uint(tries))
			}
		}
	}
	success <- fmt.Errorf("Could not send emails with [%s] alerts for [%s] from %s: all attempts have been exhausted", typeAlert, alertset.ServiceName, alertset.ServerName)
}
