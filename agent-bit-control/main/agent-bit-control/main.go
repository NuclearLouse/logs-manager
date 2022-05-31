package main

import (
	"flag"
	"log"

	"redits.oculeus.com/asorokin/logs-manager-src/agent-bit-control/internal/service"
)

func main() {
	info := flag.Bool("v", false, "will display the version of the program")
	flag.Parse()
	if *info {
		service.Version()
		return
	}
	service, err := service.New()
	if err != nil {
		log.Fatal("init new fluent-bit-control service:", err)
	}

	service.Start()
}
