package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"slices"

	"github.com/schollz/progressbar/v3"
)

var httpClient *http.Client

func initHttpClient(preferIPv6 bool) {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: 3 * time.Second,
			}
			return d.DialContext(ctx, "udp", "8.8.8.8:53")
		},
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := resolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}

			for _, ip := range ips {
				if preferIPv6 {
					if ip.To4() == nil {
						return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
					}
				} else {
					if ip.To4() != nil {
						return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
					}
				}
			}

			return nil, fmt.Errorf("no suitable IP found for %s", host)
		},
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	httpClient = &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
}

func checkNetworkStats(preferIPv6 bool) {
	fmt.Printf("\n%s Determining network quality to adjust scan options...\n\n", prompt)
	const (
		testTargetURL     = "http://www.google.com/generate_204"
		initialTestCount  = 100
		goodLatencyMs     = 50
		moderateLatencyMs = 100
		poorLatencyMs     = 200
		acceptableLoss    = 5.0
		highLoss          = 10.0
		moderateJitterMs  = 5.0
		highJitterMs      = 10.0
		maxConcurrency    = 5
	)

	var (
		totalLatency       int64
		successCount       int
		wg                 sync.WaitGroup
		latencyResults     = make(chan int64, initialTestCount)
		concurrencyLimiter = make(chan struct{}, maxConcurrency)
	)

	networkMode := "IPv4"
	if preferIPv6 {
		networkMode = "IPv6"
	}
	desc := fmt.Sprintf("Testing %s network...", networkMode)
	bar := progressbar.NewOptions(initialTestCount,
		progressbar.OptionShowBytes(false),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetDescription(desc),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]#[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	initHttpClient(preferIPv6)
	for range initialTestCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			concurrencyLimiter <- struct{}{}
			defer func() { <-concurrencyLimiter }()
			start := time.Now()
			resp, err := httpClient.Head(testTargetURL)
			bar.Add(1)
			latency := time.Since(start).Milliseconds()
			if err == nil && resp.StatusCode == http.StatusNoContent {
				if resp.Body != nil {
					resp.Body.Close()
				}
				latencyResults <- latency
			} else {
				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				latencyResults <- -1
			}
		}()
	}

	wg.Wait()
	close(latencyResults)
	fmt.Println()

	successfulLatencies := make([]int64, 0, successCount)
	for res := range latencyResults {
		if res >= 0 {
			successCount++
			totalLatency += res
			successfulLatencies = append(successfulLatencies, res)
		}
	}

	if successCount == 0 {
		failMessage("Initial network quality test failed. Could not reach test server.")
		fmt.Printf("\n%s Fallback to default scan settings.\n", prompt)
		return
	}

	slices.Sort(successfulLatencies)
	medianLatency := successfulLatencies[len(successfulLatencies)/2]
	lossRate := float64(initialTestCount-successCount) / float64(initialTestCount) * 100
	var avgJitter float64

	if successCount > 1 {
		var totalJitter int64
		for i := 0; i < len(successfulLatencies)-1; i++ {
			diff := successfulLatencies[i+1] - successfulLatencies[i]
			if diff < 0 {
				diff = -diff
			}
			totalJitter += diff
		}
		avgJitter = float64(totalJitter) / float64(successCount-1)
		fmt.Printf("\n%s Avg Latency: %dms | Jitter: %.1fms | Loss: %.1f%%\n", prompt, medianLatency, avgJitter, lossRate)
	} else {
		fmt.Printf("\n%s Avg Latency: %dms | Jitter not calculated (unsuccessful tests) | Loss: %.1f%%\n", prompt, medianLatency, lossRate)
	}

	if medianLatency >= int64(poorLatencyMs) || lossRate >= highLoss || (successCount > 1 && avgJitter >= highJitterMs) {
		if preferIPv6 {
			scanConfig.IPv6Retries = 7
		} else {
			scanConfig.IPv4Retries = 7
		}
		successMessage("Network appears slow/unstable.")
	} else if medianLatency >= int64(moderateLatencyMs) || lossRate >= acceptableLoss || (successCount > 1 && avgJitter >= moderateJitterMs) {
		if preferIPv6 {
			scanConfig.IPv6Retries = 5
		} else {
			scanConfig.IPv4Retries = 5
		}
		successMessage("Network is moderate or some packet loss detected.")
	} else {
		successMessage("Network quality seems good. Using default scan settings.")
	}
}

func scanEndpoints() ([]ScanResult, error) {
	err := createXrayConfig()
	if err != nil {
		return nil, err
	}

	cmd, err := runXrayCore()
	if err != nil {
		log.Print(err)
		return nil, err
	}

	var wg sync.WaitGroup
	results := make(chan ScanResult, len(scanConfig.Endpoints))
	transports := make([]*http.Transport, len(scanConfig.Endpoints))

	for i, endpoint := range scanConfig.Endpoints {
		wg.Add(1)
		go func(endpoint string, portIdx int) {
			defer wg.Done()
			time.Sleep(time.Duration(portIdx*scanConfig.EndpointStaggeringMs) * time.Millisecond)
			proxyURL := must(url.Parse(fmt.Sprintf("http://127.0.0.1:%d", 1080+portIdx)))
			transport := &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
			transports[portIdx] = transport

			currentRetries := scanConfig.IPv4Retries
			if scanConfig.Ipv6Mode && !scanConfig.Ipv4Mode ||
				scanConfig.Ipv4Mode && scanConfig.Ipv6Mode && i >= len(scanConfig.Endpoints)/2 {
				currentRetries = scanConfig.IPv6Retries
			}

			var successCount int
			var totalLatency int64

			var innerWg sync.WaitGroup
			latencies := make(chan int64, currentRetries)

			for t := range currentRetries {
				innerWg.Add(1)
				go func(delay int) {
					defer innerWg.Done()
					time.Sleep(time.Duration(delay) * time.Millisecond)
					client := &http.Client{
						Timeout:   2 * time.Second,
						Transport: transport,
					}

					start := time.Now()
					resp, err := client.Head("http://www.gstatic.com/generate_204")
					latency := time.Since(start).Milliseconds()
					if err == nil && resp.StatusCode == 204 {
						if resp.Body != nil {
							resp.Body.Close()
						}
						latencies <- latency
					} else {
						latencies <- -1
					}
				}(t * scanConfig.RetryStaggeringMs)
			}
			innerWg.Wait()
			close(latencies)

			for l := range latencies {
				if l >= 0 {
					successCount++
					totalLatency += l
				}
			}

			if successCount == 0 {
				log.Printf("[%d] %s -> %s\n", i+1, fmtStr(endpoint, ORANGE, false), fmtStr("Failed", RED, true))
			} else {
				avgLatency := totalLatency / int64(successCount)
				lossRate := float64(currentRetries-successCount) / float64(currentRetries) * 100
				results <- ScanResult{Endpoint: endpoint, Loss: lossRate, Latency: avgLatency}
				log.Printf("[%d] %s -> %s - %s %.1f %% - %s %d ms\n",
					i+1,
					fmtStr(endpoint, ORANGE, false),
					fmtStr("Success", GREEN, true),
					fmtStr("Loss rate:", "", true),
					lossRate,
					fmtStr("Avg. Latency:", "", true),
					avgLatency,
				)
			}
		}(endpoint, i)
	}
	wg.Wait()
	close(results)

	var allResults []ScanResult
	for r := range results {
		allResults = append(allResults, r)
	}

	if err := cmd.Process.Kill(); err != nil {
		return nil, fmt.Errorf("error killing Xray core: %w", err)
	}

	cmd.Wait()

	return allResults, nil
}
