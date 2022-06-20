package service

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/datastructs"
	"redits.oculeus.com/asorokin/notification"

	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
)

//go:embed templates/*
var tmpl embed.FS

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
	TEST notificate = iota
	ERROR
	ALERT
	ATTACH
)

func (n notificate) String() string {
	switch n {
	case TEST:
		return "test_connect"
	case ERROR:
		return "internal_error"
	case ALERT:
		return "alert_logs"
	case ATTACH:
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
	flog := s.log.WithField("Root", "Admin Notification")
	flog.Debugln("try send admin notification:", typeNotificate.String())
	if internalError != nil {
		flog.Debug("check time last send notification")
		if !s.expirePeriodSendLastInternalError(internalError[0].Error(), time.Now()) {
			flog.Debug("the last notice period has not expired")
			return
		}
	}
	header := typeNotificate.makeSubject(s.cfg.ServerName, serviceName)
	flog.Debugln("make message header/subject:", header)
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
	for _, sender := range s.senders {
		body := new(bytes.Buffer)
		flog.Debug("trying to fill the template with data")
		err := template.Must(template.ParseFS(tmpl, fileTemplate(sender.String(), typeNotificate.String()))).Execute(body, data)
		if err != nil {
			flog.Errorln("parse template:", err)
			return
		}
		addresses := s.getAdminAddresses(sender)
		flog.Debugf("trying send message via [%s] to: %s", sender, addresses)
		if err := sender.SendMessage(notification.Message{
			Addresses: addresses,
			Content:   body,
			Subject:   header,
		}); err == nil {
			flog.Infof("successful sended [%s] message", sender)
		} else {
			flog.Errorf("sending [%s] message:%s", sender, err)
		}
	}
}

func (s *Service) getAdminAddresses(sender notification.Notificator) []string {
	switch sender.String() {
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

func (s *Service) sendAlertNotification(ctx context.Context, typeAlert string, alertset *datastructs.AlertSettings, pool []datastructs.AlertLog) error {

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
		tmplName = ATTACH.String()
		attach.Filename = strings.ReplaceAll(
			fmt.Sprintf("%s_%s_%d_errors.log",
				strings.ToLower(alertset.ServerName),
				strings.ToLower(alertset.ServiceName),
				time.Now().Unix()),
			" ", "-")
		buf := new(bytes.Buffer)
		for _, l := range pool {
			if _, err := io.WriteString(buf, l.Log.Message+"\n"); err != nil {
				return fmt.Errorf("writing pool logs to string: %w", err)
			}
		}
		attach.Content = buf
		attach.ContentType = "text/plain"

	}

	errorsChan := make(chan error, len(s.senders))
	//если хоть один нотификатор сработал, то надо выставлять флаг успешной отправки
	for _, sender := range s.senders {
		notificator := sender
		body := new(bytes.Buffer)
		err := template.Must(template.ParseFS(tmpl, fileTemplate(notificator.String(), tmplName))).Execute(body, data)
		if err != nil {
			return fmt.Errorf("must template: %w", err)
		}
		message := notification.Message{
			Addresses: alertset.GetUserAddresses(notificator.String()),
			Content:   body,
			Subject:   header,
		}
		go s.trySendMessage(ctx, notificator, message, attach, errorsChan)
	}

	var (
		flag bool
		serr error
		i    int
	)
	flog := s.log.WithFields(logrus.Fields{
		"Root":    "Alert Notification",
		"Type":    typeAlert,
		"Server":  alertset.ServerName,
		"Service": alertset.ServiceName,
	})
TRY:
	for {
		select {
		case err := <-errorsChan:
			i++
			if err == nil && !flag {
				serr = multierror.Append(serr, fmt.Errorf("send %s message [%s] from %s :%w", typeAlert, alertset.ServiceName, alertset.ServerName, err))

				var ids []int64
				for _, data := range pool {
					ids = append(ids, data.RecordID)
				}
				flog.Tracef("set send TRUE flags for: %d log records", len(ids))
				if err := s.store.SetSendFlag(ctx, ids); err != nil {
					go s.sendAdminNotification(ERROR, fmt.Errorf("set send TRUE flags: %w", err))
				} else {
					flag = true
				}
			}

			if i == len(s.senders) {
				break TRY
			}

		default:
			time.Sleep(50 * time.Millisecond)
		}

	}
	return serr

}

func (s *Service) trySendMessage(
	ctx context.Context,
	sender notification.Notificator,
	message notification.Message,
	attach notification.Attachment,
	errChan chan error) {
	flog := s.log.WithFields(logrus.Fields{
		"Root":   "Sender",
		"Sender": strings.ToUpper(sender.String()),
	})
	var muerr error
	for tries := 0; tries < s.cfg.NumAttemptsFail; tries++ {
		select {
		case <-ctx.Done():
			return
		default:
			err := sender.SendMessage(message, attach)
			if err == nil {
				errChan <- nil
				return
			}
			muerr = multierror.Append(muerr, fmt.Errorf("%s notification: %w", sender, err))
			flog.Errorf("tries: %d | could not send message alerts: %s", tries+1, err)
			if tries+1 < s.cfg.NumAttemptsFail {
				time.Sleep(time.Second << uint(tries))
			}

		}
	}

	errChan <- fmt.Errorf("could not send %s mesage alerts - all attempts have been exhausted: %w", sender, muerr)
}

func fileTemplate(sender, name string) string {
	return fmt.Sprintf("templates/%s/%s.*", sender, name)
}
