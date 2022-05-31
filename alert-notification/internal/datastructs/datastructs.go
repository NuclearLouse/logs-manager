package datastructs

import (
	"time"
)

type ServiceControl struct {
	Stop   bool
	Reload bool
}

//TODO: потом сделать поле Сс и Bcc
type AlertSettings struct {
	ServiceID         int
	ServerName        string
	ServiceName       string
	IfMessageContains []string
	EmailTo           []string
	BitrixTo          []string
	SendUrgent        bool
}

func (as AlertSettings) GetUserAddresses(notificator string) []string {
	switch notificator {
	case "email":
		return as.EmailTo
	case "bitrix":
		return as.BitrixTo
	}

	return []string{}
}

type DataJSON struct {
	Message string  `json:"log"`
	Date    float64 `json:"date"`
}

type AlertLog struct {
	RecordID int64
	Time     time.Time
	Log      DataJSON
	Tag      string
}

type ClearOldLogsStatistic struct {
	ServerName  string
	ServiceName string
	AllLogs     int64
	ErrLogs     int64
}

