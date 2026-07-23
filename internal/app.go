package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type Color struct {
	R, G, B uint8
}

var (
	ColorRed   = Color{224, 40, 40}
	ColorGreen = Color{40, 180, 70}
	ColorBlue  = Color{96, 165, 250}
)

var isTermux = false

func CreateAccount(ctx context.Context, logger *Logger) *CfAccount {
	fmt.Println()
	tokenUrl, err := BuildTokenURL()
	if err != nil {
		logger.Fatal(err)
	}
	msg := fmt.Sprintf(
		"Please visit link below to create an API token. You should %s, %s, copy it and come back here.\n\n%s",
		FmtStr("Continue to summary", ColorBlue, true),
		FmtStr("Create Token", ColorBlue, true),
		tokenUrl,
	)
	logger.Info(msg)

	token := PromptToken(logger)
	acc, err := CreateCfAccount(ctx, token)
	if err != nil {
		logger.Fatal(err)
	}

	return acc
}

func DeployToWorkers(ctx context.Context, acc *CfAccount, logger *Logger, workerName, namespaceID string) string {
	subdomain, err := acc.GetWorkersDevSubdomain(ctx)
	if err != nil {
		subdomain, err = acc.CreateWorkersDevSubdomain(ctx)
		if err != nil {
			logger.Fatal(err)
		} else {
			logger.Success("Fresh account, Workers subdomain created successfully!")
		}
	} else {
		logger.Success("Account workers subdomain is available!")
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

func DeployToPages(ctx context.Context, acc *CfAccount, logger *Logger, workerName, namespaceID string) string {
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

func PromptWizard(logger *Logger) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("%s Run wizard again [y/n]: ", FmtStr(">", ColorBlue, true))
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

func PromptLogin(logger *Logger, total int) int {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("%s Choose a Cloudflare account [Default: active]: ", FmtStr(">", ColorBlue, true))
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

func PromptToken(logger *Logger) string {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("%s Cloudflare API Token: ", FmtStr(">", ColorBlue, true))
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

func PromptSubdomain(logger *Logger) string {
	reader := bufio.NewReader(os.Stdin)
	for {
		subdomain, err := randSubdomain()
		if err != nil {
			logger.Fatal(err)
			continue
		}
		fmt.Println()
		msg := fmt.Sprintf("Random subdomain: %s", FmtStr(subdomain, ColorBlue, false))
		logger.Info(msg)

		fmt.Printf("%s Enter a subdomain or use the random [Default: random]: ", FmtStr(">", ColorBlue, true))
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

func PromptDeployType(logger *Logger) string {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println()
		fmt.Printf("  %s Workers\n", FmtStr("1.", ColorBlue, true))
		fmt.Printf("  %s Pages\n\n", FmtStr("2.", ColorBlue, true))
		fmt.Printf("%s Choose a deployment method [Default: Workers]: ", FmtStr(">", ColorBlue, true))
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

func FmtStr(s string, color Color, bold bool) string {
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

func BuildTokenURL() (string, error) {
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

func ConfigTermux(logger *Logger) {
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