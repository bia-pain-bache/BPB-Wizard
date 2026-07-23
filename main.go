package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"bpb-wizard/internal"
)

var VERSION string

func main() {
	versionFlag := flag.Bool("version", false, "print version info")
	flag.Parse()
	if *versionFlag {
		fmt.Println(VERSION)
		os.Exit(0)
	}

	ctx := context.Background()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		os.Exit(0)
	}()

	if val, ok := os.LookupEnv("LAUNCHED_BY_SCRIPT"); !ok || val != "1" {
		fmt.Println("BPB Wizard can not be executed standalone.")
		os.Exit(1)
	}

	logger := internal.NewLogger()
	internal.ConfigTermux(logger)

	header := fmt.Sprintf(`
 _____ _____ _____ 
| __  |  _  | __  |
| __ -|   __| __ -|
|_____|__|  |_____| Wizard CLI %s
`, VERSION)
	fmt.Print(internal.FmtStr(header, internal.ColorBlue, true))

	for {
		tokenStore := internal.NewTokenStore()
		store, err := tokenStore.LoadLogins()
		if err != nil {
			logger.Fatal(err)
		}
		noStore := store.ActiveEmail == ""

		var acc *internal.CfAccount
		if noStore {
			acc = internal.CreateAccount(ctx, logger)
			tokenStore.SaveLogin(internal.CfLogin{
				Email: acc.Email,
				ID:    acc.ID,
				Token: acc.Token,
			})
		} else {
			fmt.Println()
			for index, account := range store.Logins {
				counter := fmt.Sprintf("  %d.", index+1)
				active := ""
				if account.Email == store.ActiveEmail {
					active = internal.FmtStr("[active]", internal.ColorGreen, true)
				}

				fmt.Printf("%s %s %s\n", internal.FmtStr(counter, internal.ColorBlue, true), account.Email, active)
			}
			fmt.Printf("  %s Add a new token\n", internal.FmtStr("0.", internal.ColorBlue, true))

			index := internal.PromptLogin(logger, len(store.Logins))
			if index == 0 {
				acc = internal.CreateAccount(ctx, logger)
				tokenStore.SaveLogin(internal.CfLogin{
					Email: acc.Email,
					ID:    acc.ID,
					Token: acc.Token,
				})
			} else {
				acc = internal.NewCfAccount(store.Logins[index-1].Token)
				acc.ID = store.Logins[index-1].ID
				acc.Email = store.Logins[index-1].Email
			}
		}

		deployType := internal.PromptDeployType(logger)
		var workerName string
		for {
			subdomain := internal.PromptSubdomain(logger)
			taken := acc.NameTaken(ctx, deployType, subdomain)
			if taken {
				logger.Error("This subdomain is taken, try again...")
				continue
			}

			workerName = subdomain
			break
		}

		fmt.Println()
		logger.Info("Installing BPB Panel...")
		namespaceID, err := acc.CreateKVNamespace(ctx, workerName, deployType)
		if err != nil {
			logger.Fatal(err)
		}
		logger.Success("KV namespace created successfully!")

		var panelURL string
		if deployType == "pages" {
			panelURL = internal.DeployToPages(ctx, acc, logger, workerName, namespaceID)
		} else {
			panelURL = internal.DeployToWorkers(ctx, acc, logger, workerName, namespaceID)
		}

		logger.Success("BPB Panel successfully installed!")
		fmt.Println()
		logger.Info("Panel URL: " + internal.FmtStr(panelURL, internal.ColorBlue, true))

		tryAgain := internal.PromptWizard(logger)
		if tryAgain {
			continue
		}
		break
	}
}
