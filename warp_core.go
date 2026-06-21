package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Dns struct {
	Servers       []string `json:"servers"`
	Tag           string   `json:"tag"`
	QueryStrategy string   `json:"queryStrategy"`
}

type Log struct {
	Access   string `json:"access"`
	Error    string `json:"error"`
	Loglevel string `json:"warning"`
	DnsLog   bool   `json:"dnsLog,omitempty"`
}

type httpInbound struct {
	Protocol string `json:"protocol"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	Tag      string `json:"tag"`
}

type Peers struct {
	Endpoint  string `json:"endpoint"`
	KeepAlive int    `json:"keepAlive"`
	PublicKey string `json:"publicKey"`
}

type Settings struct {
	Address     []string `json:"address"`
	Mtu         int      `json:"mtu"`
	NoKernelTun bool     `json:"noKernelTun"`
	Reserved    []int    `json:"reserved"`
	SecretKey   string   `json:"secretKey"`
	Peers       []Peers  `json:"peers"`
}

type Sockopt struct {
	DialerProxy string `json:"dialerProxy"`
}

type StreamSettings struct {
	Sockopt struct {
		DialerProxy string `json:"dialerProxy"`
	} `json:"sockopt"`
}

type FreedomOutbound struct {
	Protocol string `json:"protocol"`
	Settings any    `json:"settings"`
	Tag      string `json:"tag"`
}


type FreedomSettings struct {
	Noises *[]Noise `json:"noises,omitempty"`
}

type WgOutbound struct {
	Protocol       string          `json:"protocol"`
	Settings       Settings        `json:"settings"`
	StreamSettings *StreamSettings `json:"streamSettings,omitempty"`
	Tag            string          `json:"tag"`
}

type RoutingRule struct {
	InboundTag  []string `json:"inboundTag"`
	OutboundTag string   `json:"outboundTag"`
	Type        string   `json:"type"`
}

type Routing struct {
	DomainStrategy string        `json:"domainStrategy"`
	Rules          []RoutingRule `json:"rules"`
}

type XrayConfig struct {
	Remarks   string        `json:"remarks"`
	Log       Log           `json:"log"`
	Dns       Dns           `json:"dns"`
	Inbounds  []httpInbound `json:"inbounds"`
	Outbounds []any         `json:"outbounds"`
	Routing   Routing       `json:"routing"`
}

var xrayConfig = filepath.Join(CORE_DIR, "config.json")

func buildHttpInbound(index int) httpInbound {
	inbound := httpInbound{
		Listen:   "127.0.0.1",
		Port:     1080 + index,
		Protocol: "http",
		Tag:      fmt.Sprintf("http-in-%d", index+1),
	}

	return inbound
}

func buildWgOutbound(index int, endpoint string, warpConfig WarpParams) WgOutbound {
	outbound := WgOutbound{
		Protocol: "wireguard",
		Settings: Settings{
			Address: []string{
				"172.16.0.2/32",
				warpConfig.IPv6,
			},
			Mtu:         1280,
			NoKernelTun: true,
			Peers: []Peers{
				{
					Endpoint:  endpoint,
					KeepAlive: 5,
					PublicKey: warpConfig.PublicKey,
				},
			},
			Reserved:  warpConfig.Reserved,
			SecretKey: warpConfig.PrivateKey,
		},
		Tag: fmt.Sprintf("proxy-%d", index+1),
	}

	if scanConfig.UseNoise {
		outbound.StreamSettings = &StreamSettings{
			Sockopt: Sockopt{
				DialerProxy: "udp-noise",
			},
		}
	}

	return outbound
}

func buildRoutingRule(index int) RoutingRule {
	return RoutingRule{
		InboundTag: []string{
			fmt.Sprintf("http-in-%d", index+1),
		},
		OutboundTag: fmt.Sprintf("proxy-%d", index+1),
		Type:        "field",
	}
}

func buildConfig() (XrayConfig, error) {
	queryStrategy := "UseIP"
	if scanConfig.Ipv4Mode && !scanConfig.Ipv6Mode {
		queryStrategy = "UseIPv4"
	}
	if scanConfig.Ipv6Mode && !scanConfig.Ipv4Mode {
		queryStrategy = "UseIPv6"
	}

	config := XrayConfig{
		Remarks: "test",
		Log: Log{
			Access:   "core/log/access.log",
			Error:    "core/log/error.log",
			Loglevel: "warning",
			// DnsLog:   true,
		},
		Dns: Dns{
			Servers: []string{
				"8.8.8.8",
			},
			Tag:           "dns",
			QueryStrategy: queryStrategy,
		},
		Inbounds: []httpInbound{},
		Outbounds: []any{
			FreedomOutbound{
				Protocol: "freedom",
				Settings: FreedomSettings{},
				Tag:      "direct",
			},
		},
		Routing: Routing{
			DomainStrategy: "AsIs",
			Rules: []RoutingRule{
				{
					InboundTag:  []string{"dns"},
					OutboundTag: "direct",
					Type:        "field",
				},
			},
		},
	}

	if scanConfig.UseNoise {
		var noises []Noise
		for range scanConfig.UdpNoise.Count {
			noises = append(noises, scanConfig.UdpNoise)
		}
		udpNoiseOutbound := FreedomOutbound{
			Protocol: "freedom",
			Settings: FreedomSettings{
				Noises: &noises,
			},
			Tag: "udp-noise",
		}

		config.Outbounds = append(config.Outbounds, udpNoiseOutbound)
	}

	warpConfig, err := getWarpParams()
	if err != nil {
		return XrayConfig{}, err
	}

	for index, endpoint := range scanConfig.Endpoints {
		inbound := buildHttpInbound(index)
		config.Inbounds = append(config.Inbounds, inbound)

		outbound := buildWgOutbound(index, endpoint, warpConfig)
		config.Outbounds = append(config.Outbounds, outbound)

		routingRule := buildRoutingRule(index)
		config.Routing.Rules = append(config.Routing.Rules, routingRule)
	}

	return config, nil
}

func createXrayConfig() error {
	config, err := buildConfig()
	if err != nil {
		return fmt.Errorf("error registering Warp account: %w", err)
	}

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("json marshal error: %w", err)
	}

	file, err := os.Create(xrayConfig)
	if err != nil {
		return fmt.Errorf("error creating config.json: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(jsonBytes); err != nil {
		return fmt.Errorf("error writing config.json: %w", err)
	}

	return nil
}

func runXrayCore() (*exec.Cmd, error) {
	cmd := exec.Command(xrayPath, "-c", xrayConfig)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("error starting XRay core: %v", err)
	}

	fmt.Printf("%s Waiting for XRay core to initialize...\n\n", prompt)
	time.Sleep(1000 * time.Millisecond)
	return cmd, nil
}
