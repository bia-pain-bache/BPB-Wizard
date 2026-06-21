package main

import (
	"fmt"
	"Che-Wizard/task"
	"Che-Wizard/utils"
	_ "embed"
)

//go:embed ip.txt
var ipTxt []byte

//go:embed ipv6.txt
var ipv6Txt []byte

func runIPScanner() {
	task.InitRandSeed()

	fmt.Printf("\n%s Starting Clean IP Scan...\n\n", info)

	task.IPFileContent = ipTxt
	task.IPv6FileContent = ipv6Txt

	pingData := task.NewPing().Run().FilterDelay().FilterLossRate()
	speedData := task.TestDownloadSpeed(pingData)
	utils.ExportCsv(speedData)
	speedData.Print()
}
