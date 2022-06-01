package service

import (
	"fmt"
	"os"
	"testing"

	"redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/datastructs"

	"github.com/stretchr/testify/assert"
	conf "github.com/tinrab/kit/cfg"
	"redits.oculeus.com/asorokin/logging"
)

var testConfig = `c:\Users\android\go\src\redits.oculeus.com\asorokin\logs-manager-src\test-data\agent-bit-control-test.yaml`

//1. OK. 1 файл есть алерт фразы = ок, количество тегов == алерт-фразам, в обоих блоках общий путь к логам
//2. OK. 1 файл нет алерт фраз = !ок, что отсеивать? - это ошибка, проверять на уровне сканирования таблицы?
//3. OK. 2 файла есть алерт фразы = ок, количество тегов == алерт-фразам, в алерт блоке изменить путь к логам
//4. OK. 2 файла нет алерт фраз = ок, нет фильтр блока, только инпут-оутпуты с путями к логам

func TestWriteNewConfig(t *testing.T) {
	cfg, err := testingConfig()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := testingService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv.servedApps = testingCasesServedApps(t)
	file := srv.cfg.PathFluentConfig
	f, err := os.Create(file)
	if err != nil {
		t.Error(err)
	}
	defer f.Close()
	defer os.Remove(file)
	f.Close()
	if err := srv.writeNewConfig(); err != nil {
		t.Error(err)
	}
	actual, err := os.ReadFile(file)
	if err != nil {
		t.Error(err)
	}
	tests, ok := cfg.Data["tests"]
	if !ok {
		t.Fatal("нет тест кейса")
	}
	cases, ok := tests.(map[string]string)
	if !ok {
		t.Fatal("не смог получить карту тестов")
	}
	expected, err := os.ReadFile(cases["case_1"])
	if err != nil {
		t.Error(err)
	}
	t.Run("Общий лог файл", func(t *testing.T) {
		assert.Equal(t, expected, actual)
	})

}

func testingCasesServedApps(t *testing.T) []*datastructs.ServedApplication {
	t.Helper()
	var testingCases []*datastructs.ServedApplication

	//1. Один общий лог файл и есть алерт-фразы, количество лог-тегов равно количеству алерт-фраз
	// Должно записаться количество блоков равных тегам. В блоках с алерт фразами нужны фильтры
	tc := &datastructs.ServedApplication{
		AppName:       "Общий лог файл",
		LogFile:       "common.log",
		GeneralTag:    "all-log",
		AlertTags:     []string{"warn-log", "error-log"},
		AlertKeywords: []string{"WAR", "ERR"},
	}
	testingCases = append(testingCases, tc)
	return testingCases
}

func testingConfig() (*conf.Config, error) {
	cfg := conf.New()
	if err := cfg.LoadFile(testConfig); err != nil {
		return nil, fmt.Errorf("load config files: %w", err)
	}
	return cfg, nil
}

func testingService(c *conf.Config) (*Service, error) {
	cfg := &config{
		Logger: logging.DefaultConfig(),
		// Postgres: postgres.DefaultConfig(),
	}
	if err := c.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("mapping config files: %w", err)
	}

	return &Service{
		log: logging.New(cfg.Logger),
		cfg: cfg,
	}, nil
}
