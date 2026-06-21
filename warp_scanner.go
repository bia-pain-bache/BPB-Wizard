package main

import (
	"fmt"
	"log"
	"sort"
)

func runWarpScanner() {
	scanConfig.EndpointCount = 100 // Default quick scan

	fmt.Printf("\n%s Quick scan - 100 endpoints", fmtStr("1.", BLUE, true))
	fmt.Printf("\n%s Normal scan - 1000 endpoints", fmtStr("2.", BLUE, true))
	fmt.Printf("\n%s Deep scan - 10000 endpoints", fmtStr("3.", BLUE, true))
	fmt.Printf("\n%s Custom scan - you choose how many endpoints", fmtStr("4.", BLUE, true))

	for {
		mode := promptUser("- Please select scan mode (1-4): ", []string{"1", "2", "3", "4"})
		switch mode {
		case "1":
			scanConfig.EndpointCount = 100
		case "2":
			scanConfig.EndpointCount = 1000
		case "3":
			scanConfig.EndpointCount = 10000
		case "4":
			for {
				howMany := promptUser("- Please enter your desired endpoints count: ", nil)
				isValid, c := checkNum(howMany, 1, 10000)
				if !isValid {
					failMessage("Invalid input. Please enter a numeric value between 1-10000.")
				} else {
					scanConfig.EndpointCount = c
					break
				}
			}
		}
		break
	}

	for {
		protocol := promptUser("- Select the protocol (1- IPv4 / 2- IPv6 / 3- Both): ", []string{"1", "2", "3"})
		switch protocol {
		case "1":
			scanConfig.Ipv6Mode = false
			scanConfig.Ipv4Mode = true
		case "2":
			scanConfig.Ipv6Mode = true
			scanConfig.Ipv4Mode = false
		case "3":
			scanConfig.Ipv6Mode = true
			scanConfig.Ipv4Mode = true
		}
		break
	}

	for {
		platform := promptUser("- Select the proxy format (1- Wireguard / 2- V2ray / 3- Sing-Box / 4- Clash): ", []string{"1", "2", "3", "4"})
		switch platform {
		case "1":
			scanConfig.Format = "wg"
		case "2":
			scanConfig.Format = "v2"
		case "3":
			scanConfig.Format = "sing"
		case "4":
			scanConfig.Format = "clash"
		}
		break
	}

	initXray()
	fmt.Printf("\n%s Scanning...", info)

	scanConfig.Endpoints = generateEndpoints()

	if err := createConfig(&scanConfig); err != nil {
		failMessage("Failed to generate scan config.")
		log.Fatal(err)
	}

	results, err := scanEndpoints()
	if err != nil {
		failMessage("Scan failed.")
		log.Fatal(err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Latency < results[j].Latency
	})

	if len(results) == 0 {
		failMessage("Could not find any working endpoint.")
		return
	}

	if len(results) > scanConfig.OutputCount {
		results = results[:scanConfig.OutputCount]
	}

	renderEndpoints(results)
}
