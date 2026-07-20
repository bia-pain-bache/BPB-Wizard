package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type Color struct {
	R, G, B uint8
}

var (
	ColorRed   = Color{224, 40, 40}
	ColorGreen = Color{40, 180, 70}
	ColorBlue  = Color{96, 165, 250}
)

var VERSION string
var isTermux = false

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

	logger := NewLogger()
	configTermux(logger)

	header := fmt.Sprintf(`
 _____ _____ _____ 
| __  |  _  | __  |
| __ -|   __| __ -|
|_____|__|  |_____| Wizard CLI %s
`, VERSION)
	fmt.Print(fmtStr(header, ColorBlue, true))

	for {
		tokenStore := newTokenStore()
		store, err := tokenStore.LoadLogins()
		if err != nil {
			logger.Fatal(err)
		}
		noStore := store.ActiveEmail == ""

		var acc *cfAccount
		if noStore {
			acc = createAccount(ctx, logger)
			tokenStore.SaveLogin(cfLogin{
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
					active = fmtStr("[active]", ColorGreen, true)
				}

				fmt.Printf("%s %s %s\n", fmtStr(counter, ColorBlue, true), account.Email, active)
			}
			fmt.Printf("  %s Add a new token\n", fmtStr("0.", ColorBlue, true))

			index := promptLogin(logger, len(store.Logins))
			if index == 0 {
				acc = createAccount(ctx, logger)
				tokenStore.SaveLogin(cfLogin{
					Email: acc.Email,
					ID:    acc.ID,
					Token: acc.Token,
				})
			} else {
				acc = NewCfAccount(store.Logins[index-1].Token)
				acc.ID = store.Logins[index-1].ID
				acc.Email = store.Logins[index-1].Email
			}
		}

		deployType := promptDeployType(logger)
		var workerName string
		for {
			subdomain := promptSubdomain(logger)
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
			panelURL = deployToPages(ctx, acc, logger, workerName, namespaceID)
		} else {
			panelURL = deployToWorkers(ctx, acc, logger, workerName, namespaceID)
		}

		logger.Success("BPB Panel successfully installed!")
		fmt.Println()
		logger.Info("Panel URL: " + fmtStr(panelURL, ColorBlue, true))

		tryAgain := promptWizard(logger)
		if tryAgain {
			continue
		}
		break
	}
}

func createAccount(ctx context.Context, logger *Logger) *cfAccount {
	fmt.Println()
	tokenUrl, err := buildTokenURL()
	if err != nil {
		logger.Fatal(err)
	}
	msg := fmt.Sprintf(
		"Please visit link below to create an API token. You should %s, %s, copy it and come back here.\n\n%s",
		fmtStr("Continue to summary", ColorBlue, true),
		fmtStr("Create Token", ColorBlue, true),
		tokenUrl,
	)
	logger.Info(msg)

	token := promptToken(logger)
	acc, err := CreateAccount(ctx, token)
	if err != nil {
		logger.Fatal(err)
	}

	return acc
}

func deployToWorkers(ctx context.Context, acc *cfAccount, logger *Logger, workerName, namespaceID string) string {
	subdomain, err := acc.GetWorkersSubdomain(ctx)
	if err != nil {
		logger.Fatal(err)
	}

	script, settings, err := buildScript(acc, workerName, subdomain)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Success("Script built successfully!")

	if err := acc.DeployWorker(ctx, workerName, script, namespaceID); err != nil {
		logger.Fatal(err)
	}
	logger.Success("Worker deployed successfully!")

	if err := acc.EnableSubdomain(ctx, workerName); err != nil {
		logger.Fatal(err)
	}
	logger.Success("Worker subdomain enabled successfully!")

	path := url.QueryEscape(settings.SecurePath)
	return fmt.Sprintf("https://%s.%s/%s/panel", workerName, subdomain, path)
}

func deployToPages(ctx context.Context, acc *cfAccount, logger *Logger, workerName, namespaceID string) string {
	script, settings, err := buildScript(acc, workerName, "pages.dev")
	if err != nil {
		logger.Fatal(err)
	}
	logger.Success("Script built successfully!")

	subdomain, err := acc.CreatePagesProject(ctx, workerName, namespaceID)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Success("Pages project created successfully!")

	if err := acc.DeployPagesScript(ctx, workerName, script); err != nil {
		logger.Fatal(err)
	}
	logger.Success("Pages deployed successfully!")

	path := url.QueryEscape(settings.SecurePath)
	return fmt.Sprintf("https://%s/%s/panel", subdomain, path)
}

func promptWizard(logger *Logger) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("%s Run wizard again [y/n]: ", fmtStr(">", ColorBlue, true))
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Fatal(err)
		}
		resp := strings.TrimSpace(line)
		switch resp {
		case "y":
			return true
		case "n":
			return false
		default:
			logger.Error("Only 'y' or 'n', try again...")
			continue
		}
	}
}

func promptLogin(logger *Logger, total int) int {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("%s Choose a Cloudflare account [Default: active]: ", fmtStr(">", ColorBlue, true))
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Fatal(err)
		}
		resp := strings.TrimSpace(line)
		if resp == "" {
			return 1
		}

		number, err := strconv.Atoi(resp)
		if err != nil {
			logger.Fatal(err)
		}

		if number > total {
			logger.Error("Out of range, try again...")
			continue
		}

		return number
	}
}

func promptToken(logger *Logger) string {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("%s Cloudflare API Token: ", fmtStr(">", ColorBlue, true))
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Fatal(err)
		}

		resp := strings.TrimSpace(line)
		if resp == "" {
			logger.Error("Cloudflare API token is required, try again...")
			continue
		}

		return resp
	}
}

func promptSubdomain(logger *Logger) string {
	reader := bufio.NewReader(os.Stdin)
	for {
		subdomain, err := randSubdomain()
		if err != nil {
			logger.Fatal(err)
			continue
		}
		fmt.Println()
		msg := fmt.Sprintf("Random subdomain: %s", fmtStr(subdomain, ColorBlue, false))
		logger.Info(msg)

		fmt.Printf("%s Enter a subdomain or use the random [Default: random]: ", fmtStr(">", ColorBlue, true))
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Fatal(err)
		}
		resp := strings.TrimSpace(line)

		if resp != "" {
			regex := regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
			if !regex.MatchString(resp) {
				logger.Error("Subdomain consists of [a-z], [0-9] and '-', It can not start or end with '-'.")
				continue
			}
			return resp
		}

		return subdomain
	}
}

func promptDeployType(logger *Logger) string {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("  %s Workers\n", fmtStr("1.", ColorBlue, true))
		fmt.Printf("  %s Pages\n\n", fmtStr("2.", ColorBlue, true))
		fmt.Printf("%s Choose a deployment method [Default: Workers]: ", fmtStr(">", ColorBlue, true))
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Fatal(err)
		}
		choice := strings.ToLower(strings.TrimSpace(line))

		switch choice {
		case "1", "":
			return "workers"
		case "2":
			return "pages"
		default:
			logger.Error("Out of range, try again...")
		}
	}
}

func fmtStr(s string, color Color, bold bool) string {
	boldCode := ""
	if bold {
		boldCode = "1;"
	}
	return fmt.Sprintf("\033[%s38;2;%d;%d;%dm%s\033[0m", boldCode, color.R, color.G, color.B, s)
}

type Permission struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

func buildTokenURL() (string, error) {
	permissions := []Permission{
		{Key: "workers_scripts", Type: "edit"},
		{Key: "workers_kv_storage", Type: "edit"},
		{Key: "page", Type: "edit"},
		{Key: "dns", Type: "edit"},
		{Key: "user_details", Type: "read"},
	}

	permissionJSON, err := json.Marshal(permissions)
	if err != nil {
		return "", err
	}

	u, err := url.Parse("https://dash.cloudflare.com/profile/api-tokens")
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("permissionGroupKeys", string(permissionJSON))
	q.Set("accountId", "*")
	q.Set("zoneId", "all")
	q.Set("name", "BPB-Wizard")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func configTermux(logger *Logger) {
	path := os.Getenv("PATH")
	if runtime.GOOS != "android" && !strings.Contains(path, "com.termux") {
		return
	}

	isTermux = true
	if os.Getenv("SSL_CERT_FILE") != "" {
		return
	}
	candidates := []string{
		filepath.Join(os.Getenv("PREFIX"), "etc/tls/cert.pem"),
		"/data/data/com.termux/files/usr/etc/tls/cert.pem",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			os.Setenv("SSL_CERT_FILE", p)
			return
		}
	}

	logger.Fatal(fmt.Errorf("No CA cert bundle found. Cloudflare API calls will likely fail."))
}
