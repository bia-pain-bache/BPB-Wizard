package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/crypto/curve25519"
)

type KeyPair struct {
	PrivateKey string
	PublicKey  string
}

type WarpConfig struct {
	Config struct {
		Interface struct {
			Addresses struct {
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
		ClientID string `json:"client_id"`
		Peers    []struct {
			PublicKey string `json:"public_key"`
		} `json:"peers"`
	} `json:"config"`
}

type WarpParams struct {
	IPv6       string
	Reserved   []int
	PublicKey  string
	PrivateKey string
}

func GenerateWireGuardKeyPair() (publicKey, privateKey string, err error) {
	privateKeyBytes := make([]byte, curve25519.ScalarSize)
	if _, err = rand.Read(privateKeyBytes); err != nil {
		return "", "", fmt.Errorf("error generating wireguard private key: %w", err)
	}

	privateKeyBytes[0] &= 248
	privateKeyBytes[31] &= 127
	privateKeyBytes[31] |= 64

	var publicKeyBytes [32]byte
	curve25519.ScalarBaseMult(&publicKeyBytes, (*[32]byte)(privateKeyBytes))

	publicKey = base64.StdEncoding.EncodeToString(publicKeyBytes[:])
	privateKey = base64.StdEncoding.EncodeToString(privateKeyBytes)

	return publicKey, privateKey, nil
}

func fetchWarpConfig(privateKey string) (WarpConfig, error) {
	payload := map[string]interface{}{
		"install_id":   "",
		"fcm_token":    "",
		"tos":          time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"type":         "Android",
		"model":        "PC",
		"locale":       "en_US",
		"warp_enabled": true,
		"key":          privateKey,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return WarpConfig{}, fmt.Errorf("error marshaling warp reg payload: %w", err)
	}

	initHttpClient(false)
	apiBaseUrl := "https://api.cloudflareclient.com/v0a4005/reg"
	req, err := http.NewRequest("POST", apiBaseUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return WarpConfig{}, fmt.Errorf("error registering warp: %w", err)
	}
	req.Header.Set("User-Agent", "insomnia/8.6.1")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return WarpConfig{}, fmt.Errorf("error registering warp: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return WarpConfig{}, fmt.Errorf("HTTP error: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return WarpConfig{}, fmt.Errorf("error reading warp config: %w", err)
	}

	var result WarpConfig
	if err := json.Unmarshal(body, &result); err != nil {
		return WarpConfig{}, fmt.Errorf("failed to parse API response: %w", err)
	}

	return result, nil
}

func base64ToDecimal(base64Str string) ([]int, error) {
	decoded, err := base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		return nil, fmt.Errorf("error decoding reserved: %w", err)
	}

	decimalArray := make([]int, len(decoded))
	for i, b := range decoded {
		decimalArray[i] = int(b)
	}
	return decimalArray, nil
}

func extractWarpParams(config WarpConfig, privateKey string) (WarpParams, error) {
	reserved, err := base64ToDecimal(config.Config.ClientID)
	if err != nil {
		return WarpParams{}, fmt.Errorf("error extracting warp account: %w", err)
	}

	return WarpParams{
		IPv6:       config.Config.Interface.Addresses.V6 + "/128",
		Reserved:   reserved,
		PublicKey:  config.Config.Peers[0].PublicKey,
		PrivateKey: privateKey,
	}, nil
}

func getWarpParams() (WarpParams, error) {
	PublicKey, PrivateKey, err := GenerateWireGuardKeyPair()
	if err != nil {
		return WarpParams{}, err
	}

	config, err := fetchWarpConfig(PublicKey)
	if err != nil {
		return WarpParams{}, err
	}
	successMessage("Registered a new warp account.\n")

	warpConfig, err := extractWarpParams(config, PrivateKey)
	if err != nil {
		return WarpParams{}, err
	}

	return warpConfig, nil
}
