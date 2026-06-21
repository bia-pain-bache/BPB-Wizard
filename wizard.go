package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go/v4/kv"
	"github.com/google/uuid"
)

type DeployType int

const (
	DTWorker DeployType = iota
	DTPage
)

var DeployTypeNames = map[DeployType]string{
	DTWorker: "worker",
	DTPage:   "page",
}

func (dt DeployType) String() string {
	return DeployTypeNames[dt]
}

type Panel struct {
	Name string
	Type string
}

const (
	CharsetAlphaNumeric      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	CharsetSpecialCharacters = "!@$*-_."
	CharsetTrPassword        = CharsetAlphaNumeric + CharsetSpecialCharacters
	CharsetURIPath           = CharsetAlphaNumeric + CharsetSpecialCharacters
	CharsetSubDomain         = "abcdefghijklmnopqrstuvwxyz0123456789-"
	DomainRegex              = `^(?i)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`
)

func downloadFile(url string) ([]byte, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}

			if req.URL.Scheme != "https" || !isTrustedWorkerDownloadHost(req.URL.Hostname()) {
				return fmt.Errorf("untrusted worker.js redirect target: %s", req.URL.String())
			}

			return nil
		},
		Timeout: 60 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.Request.URL.Scheme != "https" || !isTrustedWorkerDownloadHost(resp.Request.URL.Hostname()) {
		return nil, fmt.Errorf("untrusted worker.js download source: %s", resp.Request.URL.String())
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error downloading worker.js: %s", resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(content)
	fmt.Printf("%s worker.js SHA-256: %x\n", info, sum)
	return content, nil
}

func isTrustedWorkerDownloadHost(host string) bool {
	return host == "github.com" ||
		host == "githubusercontent.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
}

func downloadWorker() error {
	if workerJS != nil {
		successMessage("worker.js loaded successfully!")
		return nil
	}
	return fmt.Errorf("worker.js is nil")
}

func generateRandomString(charSet string, length int, isDomain bool) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	randomBytes := make([]byte, length)

	for i := range randomBytes {
		for {
			char := charSet[r.Intn(len(charSet))]
			if isDomain && (i == 0 || i == length-1) && char == byte('-') {
				continue
			}
			randomBytes[i] = char
			break
		}
	}

	return string(randomBytes)
}

func generateRandomSubDomain(subDomainLength int) string {
	return generateRandomString(CharsetSubDomain, subDomainLength, true)
}

func isValidSubDomain(subDomain string) error {
	if strings.Contains(subDomain, "\u0062\u0070\u0062") {
		message := fmt.Sprintf("Name cannot contain %s. Please try again.\n", fmtStr("\u0062\u0070\u0062", RED, true))
		return fmt.Errorf("%s", message)
	}

	subdomainRegex := regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	isValid := subdomainRegex.MatchString(subDomain)
	if !isValid {
		message := fmt.Sprintf("Subdomain cannot start with %s and should only contain %s and %s. Please try again.\n", fmtStr("-", RED, true), fmtStr("A-Z", GREEN, true), fmtStr("0-9", GREEN, true))
		return fmt.Errorf("%s", message)
	}
	return nil
}

func isValidIpDomain(value string) bool {
	if net.ParseIP(value) != nil && !strings.Contains(value, ":") {
		return true
	}

	if isValidIPv6(value) {
		return true
	}

	domainRegex := regexp.MustCompile(DomainRegex)
	return domainRegex.MatchString(value)
}

func isValidIPv6(value string) bool {
	regex := regexp.MustCompile(`^\[(.+)\]$`)
	matches := regex.FindStringSubmatch(value)
	return matches != nil && net.ParseIP(matches[1]) != nil
}

func isValidHost(value string) bool {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return false
	}

	if !isValidIpDomain(host) {
		return false
	}

	intPort, err := strconv.Atoi(port)
	if err != nil || intPort < 1 || intPort > 65535 {
		return false
	}

	return true
}

func generateTrPassword(passwordLength int) string {
	return generateRandomString(CharsetTrPassword, passwordLength, false)
}

func isValidTrPassword(trPassword string) bool {
	for _, c := range trPassword {
		if !strings.ContainsRune(CharsetTrPassword, c) {
			return false
		}
	}

	return true
}

func generateSubURIPath(uriLength int) string {
	return generateRandomString(CharsetURIPath, uriLength, false)
}

func isValidSubURIPath(uri string) bool {
	for _, c := range uri {
		if !strings.ContainsRune(CharsetURIPath, c) {
			return false
		}
	}

	return true
}

func promptUser(prompt string, answers []string) string {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("\n%s", prompt)
		input, err := reader.ReadString('\n')

		if err != nil {
			fmt.Printf("\n%s Exiting...\n", title)
			if err == io.EOF {
				os.Exit(0)
			}
			os.Exit(1)
		}

		input = strings.TrimSpace(input)

		if answers == nil {
			return input
		} else {
			for _, ans := range answers {
				if strings.EqualFold(input, ans) {
					return input
				}
			}

			failMessage("Invalid answer. Try again...")
		}
	}
}

func failMessage(message string) {
	errMark := fmtStr("✗", RED, true)
	fmt.Printf("%s %s\n", errMark, message)
}

func successMessage(message string) {
	succMark := fmtStr("✓", GREEN, true)
	fmt.Printf("%s %s\n", succMark, message)
}

func openURL(url string) error {
	var cmd string
	var args = []string{url}

	switch runtime.GOOS {
	case "darwin": // MacOS
		cmd = "open"
	case "windows": // Windows
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default: // Linux, BSD, Android, etc.
		if isAndroid {
			termuxBin := os.Getenv("PATH")
			cmd = filepath.Join(termuxBin, "termux-open-url")
		} else {
			cmd = "xdg-open"
		}
	}

	err := exec.Command(cmd, args...).Start()
	if err != nil {
		return err
	}

	return nil
}

func checkPanel(url string) error {
	message := fmt.Sprintf("Che panel is ready -> %s", fmtStr(url, BLUE, true))
	successMessage(message)
	prompt := fmt.Sprintf("- Would you like to open %s in browser? (y/n): ", fmtStr("Che panel", BLUE, true))

	if response := promptUser(prompt, []string{"y", "n"}); strings.ToLower(response) == "n" {
		return nil
	}

	if err := openURL(url); err != nil {
		return err
	}

	return nil
}

func runWizard() {
	renderHeader()
	fmt.Printf("\n%s Welcome to %s!\n", title, fmtStr("Che Wizard", GREEN, true))
	fmt.Printf("%s This wizard will help you to deploy or modify %s on Cloudflare, and scan for IPs/Warp.\n", info, fmtStr("Che Panel", BLUE, true))

	for {
		message := fmt.Sprintf("1- %s or %s Panel.\n2- %s Cloudflare Clean IP.\n3- %s Warp Endpoints.\n\n- Select: ",
			fmtStr("DEPLOY", GREEN, true), fmtStr("MODIFY", RED, true),
			fmtStr("SCAN", BLUE, true), fmtStr("SCAN", ORANGE, true))
		response := promptUser(message, []string{"1", "2", "3"})
		switch response {
		case "1":
			fmt.Printf("%s Please make sure you have a verified %s account.\n", info, fmtStr("Cloudflare", ORANGE, true))
			ctx := context.Background()
			if err := ensureCloudflareAuth(ctx); err != nil {
				failMessage("Failed to login Cloudflare.")
				log.Fatalln(err)
			}
			panelMenu()
		case "2":
			runIPScanner()
		case "3":
			runWarpScanner()
		}

		res := promptUser("- Would you like to run the wizard again? (y/n): ", []string{"y", "n"})
		if strings.ToLower(res) == "n" {
			fmt.Printf("\n%s Exiting...\n", title)
			return
		}
	}
}

func panelMenu() {
	message := fmt.Sprintf("1- %s a new panel.\n2- %s an existing panel.\n\n- Select: ", fmtStr("CREATE", GREEN, true), fmtStr("MODIFY", RED, true))
	response := promptUser(message, []string{"1", "2"})
	switch response {
	case "1":
		createPanel()
	case "2":
		modifyPanel()
	}
}

func createPanel() {
	ctx := context.Background()
	var err error
	
	fmt.Printf("\n%s Get settings...\n", title)
	fmt.Printf("\n%s You can use %s or %s method to deploy.\n", info, fmtStr("Workers", ORANGE, true), fmtStr("Pages", ORANGE, true))
	fmt.Printf("%s %s: If you choose %s, sometimes it takes up to 5 minutes until you can access panel, so please keep calm!\n", info, warning, fmtStr("Pages", ORANGE, true))
	var deployType DeployType

	response := promptUser("1- Workers method.\n2- Pages method.\n\n- Select: ", []string{"1", "2"})
	switch response {
	case "1":
		deployType = DTWorker
	case "2":
		deployType = DTPage
	}

	deployMode := promptUser("1- Easy mode.\n2- Custom mode.\n\n- Select: ", []string{"1", "2"})
	projectName := generateRandomSubDomain(32)
	uid := uuid.NewString()
	trPass := generateTrPassword(12)
	subPath := generateSubURIPath(16)
	proxyIP := ""
	nat64Prefix := ""
	fallback := ""
	var customDomain string

	if deployMode == "2" {
		for {
			fmt.Printf("\n%s The random generated subdomain (%s) is: %s", info, fmtStr("Subdomain", GREEN, true), fmtStr(projectName, ORANGE, true))
			if response := promptUser("- Please enter a custom subdomain or press ENTER to use generated one: ", nil); response != "" {
				if err := isValidSubDomain(response); err != nil {
					failMessage(err.Error())
					continue
				}

				projectName = response
			}

			var isAvailable bool
			fmt.Printf("\n%s Checking domain availablity...\n", title)

			if deployType == DTWorker {
				isAvailable = isWorkerAvailable(ctx, projectName)
			} else {
				isAvailable = isPagesProjectAvailable(ctx, projectName)
			}

			if !isAvailable {
				prompt := fmt.Sprintf("- Subdomain already exists! This will %s all panel settings, would you like to override it? (y/n): ", fmtStr("RESET", RED, true))
				if response := promptUser(prompt, []string{"y", "n"}); strings.ToLower(response) == "n" {
					continue
				}
			}

			successMessage("Available!")
			break
		}

		fmt.Printf("\n%s The random generated %s is: %s", info, fmtStr("UUID", GREEN, true), fmtStr(uid, ORANGE, true))
		for {
			if response := promptUser("- Please enter a custom uid or press ENTER to use generated one: ", nil); response != "" {
				if _, err := uuid.Parse(response); err != nil {
					failMessage("UUID is not standard, please try again.")
					continue
				}

				uid = response
			}

			break
		}

		fmt.Printf("\n%s The random generated %s is: %s", info, fmtStr("\u0054\u0072\u006f\u006a\u0061\u006e password", GREEN, true), fmtStr(trPass, ORANGE, true))
		for {
			if response := promptUser("- Please enter a custom panel password or press ENTER to use generated one: ", nil); response != "" {
				if !isValidTrPassword(response) {
					failMessage("\u0054\u0072\u006f\u006a\u0061\u006e password cannot contain none standard character! Please try again.")
					continue
				}

				trPass = response
			}

			break
		}

		fmt.Printf("\n%s The default %s is: %s", info, fmtStr("Proxy IP", GREEN, true), fmtStr("\u0062\u0070\u0062.yousef.isegaro.com", ORANGE, true))
		for {
			if response := promptUser("- Please enter custom Proxy IP/Domains or press ENTER to use default: ", nil); response != "" {
				areValid := true
				values := strings.SplitSeq(response, ",")
				for v := range values {
					trimmedValue := strings.TrimSpace(v)
					if !isValidIpDomain(trimmedValue) && !isValidHost(trimmedValue) {
						areValid = false
						message := fmt.Sprintf("%s is not a valid IP or Domain. Please try again.", trimmedValue)
						failMessage(message)
					}
				}

				if !areValid {
					continue
				}

				proxyIP = response
			}

			break
		}

		fmt.Printf("\n%s The default %s are listed here: %s", info, fmtStr("Nat64 Prefixes", GREEN, true), fmtStr("https://github.com/bia-pain-bache/Che-Worker-Panel/blob/main/docs/NAT64Prefixes.md", ORANGE, true))
		for {
			if response := promptUser("- Please enter custom NAT64 Prefixes or press ENTER to use default: ", nil); response != "" {
				areValid := true
				values := strings.SplitSeq(response, ",")
				for v := range values {
					trimmedValue := strings.TrimSpace(v)
					if !isValidIPv6(trimmedValue) {
						areValid = false
						message := fmt.Sprintf("%s is not a valid IPv6 address. Please try again.", trimmedValue)
						failMessage(message)
					}
				}

				if !areValid {
					continue
				}

				nat64Prefix = response
			}

			break
		}

		fmt.Printf("\n%s The default %s is: %s", info, fmtStr("Fallback domain", GREEN, true), fmtStr("www.hcaptcha.com", ORANGE, true))
		if response := promptUser("- Please enter a custom Fallback domain or press ENTER to use default: ", nil); response != "" {
			fallback = response
		}

		fmt.Printf("\n%s The random generated %s is: %s", info, fmtStr("Subscription path", GREEN, true), fmtStr(subPath, ORANGE, true))
		for {
			if response := promptUser("- Please enter a custom Subscription path or press ENTER to use generated one: ", nil); response != "" {
				if !isValidSubURIPath(response) {
					failMessage("URI cannot contain none standard character! Please try again.")
					continue
				}

				subPath = response
			}

			break
		}

		fmt.Printf("\n%s You can set %s ONLY if you registered domain on this cloudflare account.", info, fmtStr("Custom domain", GREEN, true))
		if response := promptUser("- Please enter a custom domain (if you have any) or press ENTER to ignore: ", nil); response != "" {
			customDomain = response
		}
	}

	fmt.Printf("\n%s Creating KV namespace...\n", title)
	var kvNamespace *kv.Namespace

	for {
		now := time.Now().Format("2006-01-02_15-04-05")
		kvName := fmt.Sprintf("kv-%s", now)
		kvNamespace, err = createKVNamespace(ctx, kvName)
		if err != nil {
			failMessage("Failed to create KV.")
			log.Printf("%v\n\n", err)
			if response := promptUser("- Would you like to try again? (y/n): ", []string{"y", "n"}); strings.ToLower(response) == "n" {
				return
			}
			continue
		}

		successMessage("KV created successfully!")
		break
	}

	var panel string
	if err := downloadWorker(); err != nil {
		failMessage("Failed to download worker.js")
		log.Fatalln(err)
	}

	switch deployType {
	case DTWorker:
		panel, err = deployWorker(ctx, projectName, uid, trPass, proxyIP, nat64Prefix, fallback, subPath, kvNamespace, customDomain)
	case DTPage:
		panel, err = deployPagesProject(ctx, projectName, uid, trPass, proxyIP, nat64Prefix, fallback, subPath, kvNamespace, customDomain)
	}

	if err != nil {
		failMessage("Failed to get panel URL.")
		log.Fatalln(err)
	}

	if err := checkPanel(panel); err != nil {
		failMessage("Failed to checkout Che panel.")
		log.Fatalln(err)
	}
}

func modifyPanel() {
	ctx := context.Background()
	if err := ensureCloudflareAuth(ctx); err != nil {
		failMessage("Failed to login Cloudflare.")
		log.Fatalln(err)
	}

	for {
		var panels []Panel
		var message string

		fmt.Printf("\n%s Getting panels list...\n", title)
		workersList, err := listWorkers(ctx)
		if err != nil {
			failMessage("Failed to get workers list.")
			log.Println(err)
		} else {
			for _, worker := range workersList {
				panels = append(panels, Panel{
					Name: worker,
					Type: "workers",
				})
			}
		}

		pagesList, err := listPages(ctx)
		if err != nil {
			failMessage("Failed to get pages list.")
			log.Println(err)
		} else {
			for _, pages := range pagesList {
				panels = append(panels, Panel{
					Name: pages,
					Type: "pages",
				})
			}
		}

		if len(panels) == 0 {
			failMessage("No Workers or Pages found, Exiting...")
			return
		}

		message = fmt.Sprintf("Found %d workers and pages projects:\n", len(panels))
		successMessage(message)
		for i, panel := range panels {
			fmt.Printf(" %s %s - %s\n", fmtStr(strconv.Itoa(i+1)+".", BLUE, true), panel.Name, fmtStr(panel.Type, ORANGE, true))
		}

		var index int
		for {
			response := promptUser("- Please select the number you want to modify: ", nil)
			index, err = strconv.Atoi(response)
			if err != nil || index < 1 || index > len(panels) {
				failMessage("Invalid selection, please try again.")
				continue
			}

			break
		}

		panelName := panels[index-1].Name
		panelType := panels[index-1].Type

		message = fmt.Sprintf("1- %s panel.\n2- %s panel.\n\n- Select: ", fmtStr("UPDATE", GREEN, true), fmtStr("DELETE", RED, true))
		response := promptUser(message, []string{"1", "2"})
		for {
			switch response {
			case "1":

				if err := downloadWorker(); err != nil {
					failMessage("Failed to download worker.js")
					log.Fatalln(err)
				}

				if panelType == "workers" {
					if err := updateWorker(ctx, panelName); err != nil {
						failMessage("Failed to update panel.")
						log.Fatalln(err)
					}

					successMessage("Panel updated successfully!")
					break
				}

				if err := updatePagesProject(ctx, panelName); err != nil {
					failMessage("Failed to update panel.")
					log.Fatalln(err)
				}

				successMessage("Panel updated successfully!")

			case "2":

				if panelType == "workers" {
					if err := deleteWorker(ctx, panelName); err != nil {
						failMessage("Failed to delete panel.")
						log.Fatalln(err)
					}

					successMessage("Panel deleted successfully!")
					break
				}

				if err := deletePagesProject(ctx, panelName); err != nil {
					failMessage("Failed to delete panel.")
					log.Fatalln(err)
				}

				successMessage("Panel deleted successfully!")

			default:
				failMessage("Wrong selection, Please choose 1 or 2 only!")
				continue
			}

			break
		}

		if response := promptUser("- Would you like to modify another panel? (y/n): ", []string{"y", "n"}); strings.ToLower(response) == "n" {
			break
		}
	}
}
