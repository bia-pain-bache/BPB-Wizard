package main

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"fmt"
	"math/rand"
	"time"
	"encoding/base64"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	CORE_DIR = "core"
)

var (
	xrayPath string
	scanConfig = ScanConfig{
		EndpointCount: 100,
		Ipv4Mode:      true,
		Ipv6Mode:      false,
		UseNoise:      true,
		UdpNoise: Noise{
			Type:   "quic",
			Packet: "quic",
			Delay:  "1",
		},
		OutputCount:          5,
		IPv4Retries:          2,
		IPv6Retries:          10,
		RetryStaggeringMs:    500,
		EndpointStaggeringMs: 0,
	}
	prompt   = fmtStr("●", GREEN, true)
)

type ScanConfig struct {
	Format string
	EndpointCount        int
	Ipv4Mode             bool
	Ipv6Mode             bool
	IPv4Retries          int
	IPv6Retries          int
	RetryStaggeringMs    int
	EndpointStaggeringMs int
	UseNoise             bool
	UdpNoise             Noise
	Endpoints            []string
	OutputCount          int
}

type ScanResult struct {
	Endpoint string
	Loss     float64
	Latency  int64
}

type Noise struct {
	Type   string `json:"type"`
	Packet string `json:"packet"`
	Delay  string `json:"delay"`
	Count  int    `json:"-"`
}

func must[T any](v T, _ error) T { return v }

func writeLines(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func checkNum(num string, min int, max int) (bool, int) {
	n, err := strconv.Atoi(num)
	if err != nil {
		return false, 0
	} else if n < min || n > max {
		return false, 0
	} else {
		return true, n
	}
}

func isValidHex(value string) bool {
	matched, err := regexp.MatchString(`^[0-9a-fA-F]+$`, value)
	if err != nil {
		return false
	}
	return len(value) > 0 && matched
}

func isValidBase64(value string) bool {
	_, err := base64.StdEncoding.DecodeString(value)
	return err == nil
}

func isValidRange(value string) bool {
	matched, err := regexp.MatchString(`^\d+-\d+$`, value)
	if err != nil {
		return false
	}
	if !matched {
		return false
	}
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return false
	}
	min, _ := strconv.Atoi(parts[0])
	max, _ := strconv.Atoi(parts[1])
	return min <= max
}
func generateEndpoints() []string {
	ports := []int{
		500, 854, 859, 864, 878, 880, 890, 891, 894, 903,
		908, 928, 934, 939, 942, 943, 945, 946, 955, 968,
		987, 988, 1002, 1010, 1014, 1018, 1070, 1074, 1180, 1387,
		1701, 1843, 2371, 2408, 2506, 3138, 3476, 3581, 3854, 4177,
		4198, 4233, 4500, 5279, 5956, 7103, 7152, 7156, 7281, 7559, 8319, 8742, 8854, 8886,
	}

	ipv4Prefixes := []string{
		"188.114.96.", "188.114.97.", "188.114.98.", "188.114.99.",
		"162.159.192.", "162.159.193.", "162.159.195.", "8.34.146.",
		"8.39.214.", "8.39.204.", "8.6.112.", "8.35.211.", "8.39.125.",
		"8.47.69.",
	}
	ipv6Prefixes := []string{
		"2606:4700:d0::", "2606:4700:d1::",
	}

	rand.New(rand.NewSource(time.Now().UnixNano()))
	endpoints := make([]string, 0, scanConfig.EndpointCount)
	seen := make(map[string]bool)

	ipv4Count, ipv6Count := 0, 0
	if scanConfig.Ipv4Mode && scanConfig.Ipv6Mode {
		ipv4Count = scanConfig.EndpointCount / 2
		ipv6Count = scanConfig.EndpointCount - ipv4Count
	} else if scanConfig.Ipv4Mode {
		ipv4Count = scanConfig.EndpointCount
	} else if scanConfig.Ipv6Mode {
		ipv6Count = scanConfig.EndpointCount
	}

	for len(endpoints) < ipv4Count {
		prefix := ipv4Prefixes[rand.Intn(len(ipv4Prefixes))]
		ip := fmt.Sprintf("%s%d", prefix, rand.Intn(256))
		endpoint := fmt.Sprintf("%s:%d", ip, ports[rand.Intn(len(ports))])
		if !seen[endpoint] {
			seen[endpoint] = true
			endpoints = append(endpoints, endpoint)
		}
	}

	for len(endpoints) < ipv4Count+ipv6Count {
		prefix := ipv6Prefixes[rand.Intn(len(ipv6Prefixes))]
		ip := fmt.Sprintf("[%s%x:%x:%x:%x]", prefix,
			rand.Intn(65536), rand.Intn(65536),
			rand.Intn(65536), rand.Intn(65536))
		endpoint := fmt.Sprintf("%s:%d", ip, ports[rand.Intn(len(ports))])
		if !seen[endpoint] {
			seen[endpoint] = true
			endpoints = append(endpoints, endpoint)
		}
	}

	message := fmt.Sprintf("Generated %d endpoints to test", len(endpoints))
	successMessage(message)
	return endpoints
}

func renderEndpoints(results []ScanResult) {
	message := fmt.Sprintf("Top %d Endpoints:\n", len(results))
	successMessage(message)

	var tableRows [][]string
	for _, r := range results {
		tableRows = append(tableRows, []string{
			r.Endpoint,
			fmt.Sprintf("%.1f %%", r.Loss),
			fmt.Sprintf("%d ms", r.Latency),
		})
	}

	table := table.New().
		Border(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderBottom(true).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(GREEN))).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 2).Align(lipgloss.Center)
			if row == table.HeaderRow {
				style = style.Bold(true)
				if col == 0 {
					style = style.Foreground(lipgloss.Color(GREEN))
				} else {
					style = style.Foreground(lipgloss.Color(ORANGE))
				}
			}
			return style
		}).
		Headers("Endpoint", "Loss rate", "Latency").
		Rows(tableRows...)
	fmt.Println(table.Render())
}


type Endpoint struct {
	IP   string
	Ping int
}

func createConfig(scanConfig *ScanConfig) error {
	return createXrayConfig()
}
