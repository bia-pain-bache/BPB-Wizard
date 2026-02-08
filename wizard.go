package main

import (
	"bufio"
	"context"
	"errors"
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
	CharsetSpecialCharacters = "!@#$%^&*()_+[]{}|;:',.<>?"
	CharsetTrojanPassword    = CharsetAlphaNumeric + CharsetSpecialCharacters
	CharsetSubDomain         = "abcdefghijklmnopqrstuvwxyz0123456789-"
	CharsetURIPath           = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@$&*_-+;:,."
	DomainRegex              = `^(?i)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`
)

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error downloading worker.js: %s", resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	finalContent := append(content, []byte(generateJunkCode())...)
	if err := os.WriteFile(dest, finalContent, 0644); err != nil {
		return err
	}

	return nil
}

func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, input, 0644)
}

func downloadWorker() error {
	fmt.Printf("\n%s Getting %s...\n", title, fmtStr("worker.js", GREEN, true))

	if _, err := os.Stat("worker.js"); err == nil {
		copyFile("worker.js", workerPath)
	} else if errors.Is(err, os.ErrNotExist) {
		fmt.Printf("\nThe file worker.js was %s alongside the BPB-Wizard file\n", fmtStr("not found", RED, true))
	}

	for {
		if _, err := os.Stat(workerPath); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to check worker.js: %w", err)
			}
		} else {
			successMessage("worker.js already exists, skipping download.")
			return nil
		}

		err := downloadFile(workerURL, workerPath)
		if err == nil {
			successMessage("worker.js downloaded successfully!")
			return nil
		}

		failMessage("Failed to download worker.js\n")
		log.Printf("%v\n", err)

		fmt.Printf("\nChoose an option:")
		fmt.Printf("\n1) Retry download")
		fmt.Printf("\n2) Load worker.js manually %s\n", fmtStr("BPB-Wizard folder", RED, true))
		fmt.Printf("\n3) Exit")

		choice := promptUser(
			"- Select (1/2/3): ",
			[]string{"1", "2", "3"},
		)

		switch choice {

		// Retry
		case "1":
			continue

		// Manual load
		case "2":
			localPath := promptUser(
				"- Enter path to worker.js file: ",
				nil,
			)

			err := copyFile(localPath, workerPath)
			if err != nil {
				failMessage("Failed to load local worker.js file.")
				log.Printf("%v\n", err)
				continue
			}

			successMessage("worker.js loaded successfully from local file!")
			return nil

		// Exit
		case "3":
			os.Exit(0)
		}
		return nil
	}
}

func generateJunkCode() string {
	var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	minVars, maxVars := 50, 500
	minFuncs, maxFuncs := 50, 500

	varCount := rng.Intn(maxVars-minVars+1) + minVars
	funcCount := rng.Intn(maxFuncs-minFuncs+1) + minFuncs

	var sb strings.Builder

	for i := range varCount {
		varName := fmt.Sprintf("__var_%s_%d", generateRandomString(CharsetAlphaNumeric, 8, false), i)
		value := rng.Intn(100000)
		sb.WriteString(fmt.Sprintf("let %s = %d; ", varName, value))
	}

	for i := range funcCount {
		funcName := fmt.Sprintf("__Func_%s_%d", generateRandomString(CharsetAlphaNumeric, 8, false), i)
		ret := rng.Intn(1000)
		sb.WriteString(fmt.Sprintf("function %s() { return %d; } ", funcName, ret))
	}

	return sb.String()
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
	if strings.Contains(subDomain, "bpb") {
		message := fmt.Sprintf("Name cannot contain %s. Please try again.\n", fmtStr("bpb", RED, true))
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
	return generateRandomString(CharsetTrojanPassword, passwordLength, false)
}

func isValidTrPassword(trojanPassword string) bool {
	for _, c := range trojanPassword {
		if !strings.ContainsRune(CharsetTrojanPassword, c) {
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

func checkBPBPanel(url string) error {
	// ticker := time.NewTicker(5 * time.Second)
	// defer ticker.Stop()

	// dialer := &net.Dialer{
	// 	Resolver: &net.Resolver{
	// 		PreferGo: true,
	// 		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
	// 			d := net.Dialer{
	// 				Timeout: time.Duration(5000) * time.Millisecond,
	// 			}

	// 			return d.DialContext(ctx, "udp", "8.8.8.8:53")
	// 		},
	// 	},
	// }

	// dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
	// 	conn, err := dialer.DialContext(ctx, network, addr)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	return conn, nil
	// }

	// transport := &http.Transport{
	// 	DisableKeepAlives: true,
	// 	DialContext:       dialContext,
	// }

	// client := &http.Client{
	// 	Transport: transport,
	// 	Timeout:   15 * time.Second,
	// }

	// for range ticker.C {
	// 	resp, err := client.Get(url)
	// 	if err != nil {
	// 		fmt.Printf(".")
	// 		continue
	// 	}

	// 	if resp.StatusCode != http.StatusOK {
	// 		fmt.Printf(".")
	// 		resp.Body.Close()
	// 		continue
	// 	}

	// 	resp.Body.Close()
	message := fmt.Sprintf("BPB panel is ready -> %s", fmtStr(url, BLUE, true))
	successMessage(message)
	prompt := fmt.Sprintf("- Would you like to open %s in browser? (y/n): ", fmtStr("BPB panel", BLUE, true))

	if response := promptUser(prompt, []string{"y", "n"}); strings.ToLower(response) == "n" {
		return nil
	}

	if err := openURL(url); err != nil {
		return err
	}

	return nil
	// }

	// return nil
}

func runWizard() {
	renderHeader()
	fmt.Printf("\n%s Welcome to %s!\n", title, fmtStr("BPB Wizard", GREEN, true))
	fmt.Printf("%s This wizard will help you to deploy or modify %s on Cloudflare.\n", info, fmtStr("BPB Panel", BLUE, true))
	fmt.Printf("%s Please make sure you have a verified %s account.\n", info, fmtStr("Cloudflare", ORANGE, true))

	for {
		message := fmt.Sprintf("1- %s a new panel.\n2- %s an existing panel.\n\n- Select: ", fmtStr("CREATE", GREEN, true), fmtStr("MODIFY", RED, true))
		response := promptUser(message, []string{"1", "2"})
		switch response {
		case "1":
			createPanel()
		case "2":
			modifyPanel()
		}

		res := promptUser("- Would you like to run the wizard again? (y/n): ", []string{"y", "n"})
		if strings.ToLower(res) == "n" {
			fmt.Printf("\n%s Exiting...\n", title)
			return
		}
	}
}

func createPanel() {
	ctx := context.Background()
	var err error
	if cfClient == nil || cfAccount == nil {
		go login()
		token := <-obtainedToken
		cfClient = NewClient(token)

		cfAccount, err = getAccount(ctx)
		if err != nil {
			failMessage("Failed to get Cloudflare account.")
			log.Fatalln(err)
		}
	}

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

	var projectName string
	for {
		projectName = generateRandomSubDomain(32)
		fmt.Printf("\n%s The random generated name (%s) is: %s", info, fmtStr("Subdomain", GREEN, true), fmtStr(projectName, ORANGE, true))
		if response := promptUser("- Please enter a custom name or press ENTER to use generated one: ", nil); response != "" {
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
			prompt := fmt.Sprintf("- This already exists! This will %s all panel settings, would you like to override it? (y/n): ", fmtStr("RESET", RED, true))
			if response := promptUser(prompt, []string{"y", "n"}); strings.ToLower(response) == "n" {
				continue
			}
		}

		successMessage("Available!")
		break
	}

	uid := uuid.NewString()
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

	trPass := generateTrPassword(12)
	fmt.Printf("\n%s The random generated %s is: %s", info, fmtStr("Trojan password", GREEN, true), fmtStr(trPass, ORANGE, true))
	for {
		if response := promptUser("- Please enter a custom Trojan password or press ENTER to use generated one: ", nil); response != "" {
			if !isValidTrPassword(response) {
				failMessage("Trojan password cannot contain none standard character! Please try again.")
				continue
			}

			trPass = response
		}

		break
	}

	proxyIP := ""
	fmt.Printf("\n%s The default %s is: %s", info, fmtStr("Proxy IP", GREEN, true), fmtStr("bpb.yousef.isegaro.com", ORANGE, true))
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

	nat64Prefix := ""
	fmt.Printf("\n%s The default %s are listed here: %s", info, fmtStr("Nat64 Prefixes", GREEN, true), fmtStr("https://github.com/bia-pain-bache/BPB-Worker-Panel/blob/main/NAT64Prefixes.md", ORANGE, true))
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

	fallback := ""
	fmt.Printf("\n%s The default %s is: %s", info, fmtStr("Fallback domain", GREEN, true), fmtStr("speed.cloudflare.com", ORANGE, true))
	if response := promptUser("- Please enter a custom Fallback domain or press ENTER to use default: ", nil); response != "" {
		fallback = response
	}

	subPath := generateSubURIPath(16)
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

	var customDomain string
	fmt.Printf("\n%s You can set %s ONLY if you registered domain on this cloudflare account.", info, fmtStr("Custom domain", GREEN, true))
	if response := promptUser("- Please enter a custom domain (if you have any) or press ENTER to ignore: ", nil); response != "" {
		customDomain = response
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

	if err := checkBPBPanel(panel); err != nil {
		failMessage("Failed to checkout BPB panel.")
		log.Fatalln(err)
	}
}

func modifyPanel() {
	ctx := context.Background()
	var err error
	if cfClient == nil || cfAccount == nil {
		go login()
		token := <-obtainedToken
		cfClient = NewClient(token)

		cfAccount, err = getAccount(ctx)
		if err != nil {
			failMessage("Failed to get Cloudflare account.")
			log.Fatalln(err)
		}
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
