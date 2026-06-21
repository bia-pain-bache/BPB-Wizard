package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
)

func initXray() {
	if err := os.MkdirAll(CORE_DIR, 0755); err != nil {
		failMessage("Failed to create core directory")
		log.Fatal(err)
	}

	logDir := filepath.Join(CORE_DIR, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		failMessage("Failed to create logs directory")
		log.Fatal(err)
	}

	accessLog := filepath.Join(logDir, "access.log")
	errorLog := filepath.Join(logDir, "error.log")
	for _, file := range []string{accessLog, errorLog} {
		f, err := os.Create(file)
		if err != nil {
			failMessage("Failed to create Xray log file")
			log.Fatal(err)
		}
		f.Close()
	}

	var binary string
	if runtime.GOOS == "windows" {
		binary = "xray.exe"
	} else {
		binary = "xray"
	}
	xrayPath = filepath.Join(CORE_DIR, binary)

	if _, err := os.Stat(xrayPath); err != nil {
		failMessage("Xray core not found! Please place the xray binary inside the 'core' folder.")
		log.Fatal(err)
	}

	err := os.Chmod(xrayPath, 0755)
	if err != nil {
		failMessage("Failed to set Xray core permissions.")
		log.Fatal(err)
	}
}
