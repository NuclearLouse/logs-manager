package datastructs

type ServedApplication struct {
	AppName       string
	LogFile       string
	ErrLogFile    string
	GeneralTag    string
	AlertTags     []string
	AlertKeywords []string
}

type AgentControl struct {
	Stop   bool
	Reload bool
}

type TemplatesData struct {
	Flush           string
	Daemon          string
	LogLevel        string
	PluginsConfFile string
	HTTPServer      string
	StorageMetrics  string
	AppName         string
	Tag             string
	Path            string
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	Schema          string
	Table           string
	Match           string
	Regex           string
}
