package utils

import (
	"encoding/csv"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

const (
	defaultOutput         = "result.csv"
	maxDelay              = 9999 * time.Millisecond
	minDelay              = 0 * time.Millisecond
	maxLossRate   float32 = 1.0
)

var (
	InputMaxDelay    = maxDelay
	InputMinDelay    = minDelay
	InputMaxLossRate = maxLossRate
	Output           = defaultOutput
	PrintNum         = 10
)

// Check if to print test results
func NoPrintResult() bool {
	return PrintNum == 0
}

// Check if to output to file
func noOutput() bool {
	return Output == "" || Output == " "
}

type PingData struct {
	IP       *net.IPAddr
	Sended   int
	Received int
	Delay    time.Duration
}

type CloudflareIPData struct {
	*PingData
	lossRate      float32
	DownloadSpeed float64
}

// Calculate packet loss rate
func (cf *CloudflareIPData) getLossRate() float32 {
	if cf.lossRate == 0 {
		pingLost := cf.Sended - cf.Received
		cf.lossRate = float32(pingLost) / float32(cf.Sended)
	}
	return cf.lossRate
}

func (cf *CloudflareIPData) toString() []string {
	result := make([]string, 6)
	result[0] = cf.IP.String()
	result[1] = strconv.Itoa(cf.Sended)
	result[2] = strconv.Itoa(cf.Received)
	result[3] = strconv.FormatFloat(float64(cf.getLossRate()), 'f', 2, 32)
	result[4] = strconv.FormatFloat(cf.Delay.Seconds()*1000, 'f', 2, 32)
	result[5] = strconv.FormatFloat(cf.DownloadSpeed/1024/1024, 'f', 2, 32)
	return result
}

func ExportCsv(data []CloudflareIPData) {
	if noOutput() || len(data) == 0 {
		return
	}
	fp, err := os.Create(Output)
	if err != nil {
		log.Fatalf("Failed to create file [%s]: %v", Output, err)
		return
	}
	defer fp.Close()
	w := csv.NewWriter(fp) // Create a new file writing stream
	_ = w.Write([]string{"IP Address", "Sent", "Received", "Loss Rate", "Average Delay", "Download Speed (MB/s)"})
	_ = w.WriteAll(convertToString(data))
	w.Flush()
}

func convertToString(data []CloudflareIPData) [][]string {
	result := make([][]string, 0)
	for _, v := range data {
		result = append(result, v.toString())
	}
	return result
}

// Delay and packet loss sorting
type PingDelaySet []CloudflareIPData

// Delay condition filtering
func (s PingDelaySet) FilterDelay() (data PingDelaySet) {
	if InputMaxDelay > maxDelay || InputMinDelay < minDelay { // When the input delay condition is not within the default range, no filtering is performed
		return s
	}
	if InputMaxDelay == maxDelay && InputMinDelay == minDelay { // When the input delay condition is the default value, no filtering is performed
		return s
	}
	for _, v := range s {
		if v.Delay > InputMaxDelay { // Upper limit of average delay, when the delay is greater than the maximum value of the condition, no subsequent data meets the condition, directly exit the loop
			break
		}
		if v.Delay < InputMinDelay { // Lower limit of average delay, when the delay is less than the minimum value of the condition, it does not meet the condition, skip
			continue
		}
		data = append(data, v) // When the delay meets the condition, add it to the new array
	}
	return
}

// Packet loss condition filtering
func (s PingDelaySet) FilterLossRate() (data PingDelaySet) {
	if InputMaxLossRate >= maxLossRate { // When the input packet loss condition is the default value, no filtering is performed
		return s
	}
	for _, v := range s {
		if v.getLossRate() > InputMaxLossRate { // Upper limit of packet loss rate
			break
		}
		data = append(data, v) // When the packet loss rate meets the condition, add it to the new array
	}
	return
}

func (s PingDelaySet) Len() int {
	return len(s)
}
func (s PingDelaySet) Less(i, j int) bool {
	iRate, jRate := s[i].getLossRate(), s[j].getLossRate()
	if iRate != jRate {
		return iRate < jRate
	}
	return s[i].Delay < s[j].Delay
}
func (s PingDelaySet) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Download speed sorting
type DownloadSpeedSet []CloudflareIPData

func (s DownloadSpeedSet) Len() int {
	return len(s)
}
func (s DownloadSpeedSet) Less(i, j int) bool {
	return s[i].DownloadSpeed > s[j].DownloadSpeed
}
func (s DownloadSpeedSet) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s DownloadSpeedSet) Print() {
	if NoPrintResult() {
		return
	}
	if len(s) <= 0 { // When the length of the IP array (number of IPs) is 0, skip outputting results
		fmt.Println("\n[Info] The number of complete test results IP is 0, skipping output results.")
		return
	}
	dateString := convertToString(s) // Convert to multi-dimensional array [][]string
	if len(dateString) < PrintNum {  // If the length of the IP array (number of IPs) is less than the printing times, change the times to the number of IPs
		PrintNum = len(dateString)
	}
	headFormat := "%-15s%-5s%-9s%-10s%-14s%-21s\n"
	dataFormat := "%-17s%-7s%-7s%-13s%-15s%-15s\n"
	for i := 0; i < PrintNum; i++ { // If the IPs to be output contain IPv6, adjust the spacing
		if len(dateString[i][0]) > 15 {
			headFormat = "%-40s%-5s%-9s%-10s%-14s%-21s\n"
			dataFormat = "%-42s%-7s%-7s%-13s%-15s%-15s\n"
			break
		}
	}
	fmt.Printf(headFormat, "IP Address", "Sent", "Received", "Loss-Rate", "Average-Delay", "Download-Speed (MB/s)")
	for i := 0; i < PrintNum; i++ {
		fmt.Printf(dataFormat, dateString[i][0], dateString[i][1], dateString[i][2], dateString[i][3], dateString[i][4], dateString[i][5])
	}
	if !noOutput() {
		fmt.Printf("\nComplete test results have been written to %v file, which can be viewed using Notepad/Spreadsheet software.\n", Output)
	}
}
