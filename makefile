VERSION_AGENT=3.1.0
VERSION_ALERT=4.1.2
LDFLAGS_AGENT=-ldflags "-X redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/service.version=${VERSION_AGENT} -X redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/service.cfgFile=.yaml"
LDFLAGS_ALERT=-ldflags "-X redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/service.version=${VERSION_ALERT} -X redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/service.cfgFile=.yaml"
LDFLAGS_AGENT_DEV=-ldflags "-X redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/service.version=${VERSION_AGENT} -X redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/service.cfgFile=dev.yaml"
LDFLAGS_ALERT_DEV=-ldflags "-X redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/service.version=${VERSION_ALERT} -X redits.oculeus.com/asorokin/logs-manager-src/alert-notification/internal/service.cfgFile=dev.yaml"

.PHONY: build
build:
	go build ${LDFLAGS_AGENT_DEV} -o agent-bit-control ./agent-bit-control/cmd/agent-bit-control 
	go build ${LDFLAGS_ALERT_DEV} -o alert-notification ./alert-notification/cmd/alert-notification

.PHONY: deploy
deploy:
	go build ${LDFLAGS_AGENT} -o ../logs-manager/agent-bit-control ./agent-bit-control/cmd/agent-bit-control 
	go build ${LDFLAGS_ALERT} -o ../logs-manager/alert-notification ./alert-notification/cmd/alert-notification

.PHONY: test-agent
test-agent:
	go test -v ./agent-bit-control/internal/service

DATE = $(shell date /t)

.PHONY: git
git:
	git a 
	git co "${DATE}"
	git pusm

.DEFAULT_GOAL := build
