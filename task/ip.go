package task

import (
	"bytes"
	"bufio"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"
)

const defaultInputFile = "ip.txt"
var IPFileContent []byte
var IPv6FileContent []byte

var (
	// TestAll tests all IPs
	TestAll = false
	// IPFile is the filename of IP ranges
	IPFile = defaultInputFile
	IPText string
)

func InitRandSeed() {
	rand.Seed(time.Now().UnixNano())
}

func isIPv4(ip string) bool {
	return strings.Contains(ip, ".")
}

func randIPEndWith(num byte) byte {
	if num == 0 { // For single IP like /32
		return byte(0)
	}
	return byte(rand.Intn(int(num)))
}

type IPRanges struct {
	ips     []*net.IPAddr
	mask    string
	firstIP net.IP
	ipNet   *net.IPNet
}

func newIPRanges() *IPRanges {
	return &IPRanges{
		ips: make([]*net.IPAddr, 0),
	}
}

// If it's a single IP, add a subnet mask, otherwise get the subnet mask (r.mask)
func (r *IPRanges) fixIP(ip string) string {
	// If it doesn't contain '/', it's not an IP range but a single IP, so add /32 or /128 subnet mask
	if i := strings.IndexByte(ip, '/'); i < 0 {
		if isIPv4(ip) {
			r.mask = "/32"
		} else {
			r.mask = "/128"
		}
		ip += r.mask
	} else {
		r.mask = ip[i:]
	}
	return ip
}

// Parse IP range to get IP, IP range, and subnet mask
func (r *IPRanges) parseCIDR(ip string) {
	var err error
	if r.firstIP, r.ipNet, err = net.ParseCIDR(r.fixIP(ip)); err != nil {
		log.Fatalln("ParseCIDR error", err)
	}
}

func (r *IPRanges) appendIPv4(d byte) {
	r.appendIP(net.IPv4(r.firstIP[12], r.firstIP[13], r.firstIP[14], d))
}

func (r *IPRanges) appendIP(ip net.IP) {
	r.ips = append(r.ips, &net.IPAddr{IP: ip})
}

// Get the minimum value and available number of the fourth segment of the IP
func (r *IPRanges) getIPRange() (minIP, hosts byte) {
	minIP = r.firstIP[15] & r.ipNet.Mask[3] // Minimum value of the fourth segment of the IP

	// Get the number of hosts based on the subnet mask
	m := net.IPv4Mask(255, 255, 255, 255)
	for i, v := range r.ipNet.Mask {
		m[i] ^= v
	}
	total, _ := strconv.ParseInt(m.String(), 16, 32) // Total available IPs
	if total > 255 {                                 // Correct the available IP count of the fourth segment
		hosts = 255
		return
	}
	hosts = byte(total)
	return
}

func (r *IPRanges) chooseIPv4() {
	if r.mask == "/32" { // Single IP, no need to randomize, just add itself
		r.appendIP(r.firstIP)
	} else {
		minIP, hosts := r.getIPRange()    // Get the minimum value and available number of the fourth segment of the IP
		for r.ipNet.Contains(r.firstIP) { // Continue looping as long as the IP does not exceed the IP range
			if TestAll { // If testing all IPs
				for i := 0; i <= int(hosts); i++ { // Iterate through the last segment of the IP from the minimum value to the maximum value
					r.appendIPv4(byte(i) + minIP)
				}
			} else { // Randomize the last segment of the IP 0.0.0.X
				r.appendIPv4(minIP + randIPEndWith(hosts))
			}
			r.firstIP[14]++ // 0.0.(X+1).X
			if r.firstIP[14] == 0 {
				r.firstIP[13]++ // 0.(X+1).X.X
				if r.firstIP[13] == 0 {
					r.firstIP[12]++ // (X+1).X.X.X
				}
			}
		}
	}
}

func (r *IPRanges) chooseIPv6() {
	if r.mask == "/128" { // Single IP, no need to randomize, just add itself
		r.appendIP(r.firstIP)
	} else {
		var tempIP uint8                  // Temporary variable to record the value of the previous bit
		for r.ipNet.Contains(r.firstIP) { // Continue looping as long as the IP does not exceed the IP range
			r.firstIP[15] = randIPEndWith(255) // Randomize the last segment of the IP
			r.firstIP[14] = randIPEndWith(255) // Randomize the last segment of the IP

			targetIP := make([]byte, len(r.firstIP))
			copy(targetIP, r.firstIP)
			r.appendIP(targetIP) // Add to the IP address pool

			for i := 13; i >= 0; i-- { // Randomize from the third to the first bit
				tempIP = r.firstIP[i]              // Save the value of the previous bit
				r.firstIP[i] += randIPEndWith(255) // Randomize 0~255 and add it to the current bit
				if r.firstIP[i] >= tempIP {        // If the value of the current bit is greater than or equal to the value of the previous bit, the randomization is successful and the loop can be exited
					break
				}
			}
		}
	}
}

func loadIPRanges() []*net.IPAddr {
	ranges := newIPRanges()
	if IPText != "" { // Get IP range data from the parameter
		IPs := strings.Split(IPText, ",") // Split by comma and iterate over the array
		for _, IP := range IPs {
			IP = strings.TrimSpace(IP) // Trim leading and trailing whitespace (spaces, tabs, newline characters, etc.)
			if IP == "" {              // Skip empty lines (e.g., consecutive ,, at the beginning, end, or in between)
				continue
			}
			ranges.parseCIDR(IP) // Parse IP range to get IP, IP range, and subnet mask
			if isIPv4(IP) {      // Generate all IPv4 / IPv6 addresses to be tested (single / random / all)
				ranges.chooseIPv4()
			} else {
				ranges.chooseIPv6()
			}
		}
	} else { // Get IP range data from the file
		if IPFile == "" {
			IPFile = defaultInputFile
		}
		if len(IPFileContent) == 0 {
			log.Fatal("IP content is empty")
		}
		scanner := bufio.NewScanner(bytes.NewReader(IPFileContent))
		for scanner.Scan() { // Iterate over each line in the file
			line := strings.TrimSpace(scanner.Text()) // Trim leading and trailing whitespace (spaces, tabs, newline characters, etc.)
			if line == "" {                           // Skip empty lines
				continue
			}
			ranges.parseCIDR(line) // Parse IP range to get IP, IP range, and subnet mask
			if isIPv4(line) {      // Generate all IPv4 / IPv6 addresses to be tested (single / random / all)
				ranges.chooseIPv4()
			} else {
				ranges.chooseIPv6()
			}
		}
	}
	return ranges.ips
}
