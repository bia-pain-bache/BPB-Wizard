package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	cf "github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/kv"
	"github.com/cloudflare/cloudflare-go/v4/option"
	"github.com/cloudflare/cloudflare-go/v4/workers"
	"github.com/cloudflare/cloudflare-go/v4/zones"
	"github.com/joeguo/tldextract"
)

type ScriptUpdateParams struct {
	AccountID string                         `form:"account_id,required"`
	Metadata  ScriptUpdateParamsMetadataForm `form:"metadata,required"`
}

type ScriptUpdateParamsMetadataForm struct {
	MainModule         string              `form:"main_module"`
	Bindings           []map[string]string `form:"bindings"`
	CompatibilityDate  string              `form:"compatibility_date"`
	CompatibilityFlags []string            `form:"compatibility_flags"`
	Observability      map[string]bool     `form:"observability"`
	Placement          map[string]string   `form:"placement"`
	UsageModel         string              `form:"usage_model"`
	Tags               []string            `form:"tags"`
	TailConsumers      []string            `form:"tail_consumers"`
	Logpush            bool                `form:"logpush"`
	jsContent          []byte
}

func (sp ScriptUpdateParams) MarshalMultipart() ([]byte, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	metadata := map[string]interface{}{
		"main_module":         sp.Metadata.MainModule,
		"bindings":            sp.Metadata.Bindings,
		"compatibility_date":  sp.Metadata.CompatibilityDate,
		"compatibility_flags": sp.Metadata.CompatibilityFlags,
		"observability":       sp.Metadata.Observability,
		"placement":           sp.Metadata.Placement,
		"usage_model":         sp.Metadata.UsageModel,
		"tags":                sp.Metadata.Tags,
		"tail_consumers":      sp.Metadata.TailConsumers,
		"logpush":             sp.Metadata.Logpush,
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, "", fmt.Errorf("error marshalling metadata: %w", err)
	}

	metadataHeaders := textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="metadata"`},
	}

	metadataPart, err := writer.CreatePart(metadataHeaders)
	if err != nil {
		return nil, "", fmt.Errorf("error creating metadata part: %w", err)
	}

	_, err = metadataPart.Write(metadataJSON)
	if err != nil {
		return nil, "", fmt.Errorf("error writing metadata content: %w", err)
	}

	packageLockHeaders := textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="package-lock.json"; filename="package-lock.json"`},
		"Content-Type":        []string{"text/plain"},
	}

	packageLockPart, err := writer.CreatePart(packageLockHeaders)
	if err != nil {
		return nil, "", fmt.Errorf("error creating package-lock.json part: %w", err)
	}

	_, err = packageLockPart.Write([]byte("{}"))
	if err != nil {
		return nil, "", fmt.Errorf("error writing package-lock.json content: %w", err)
	}

	packageJSONHeaders := textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="package.json"; filename="package.json"`},
		"Content-Type":        []string{"text/plain"},
	}

	packageJSONPart, err := writer.CreatePart(packageJSONHeaders)
	if err != nil {
		return nil, "", fmt.Errorf("error creating package.json part: %w", err)
	}

	_, err = packageJSONPart.Write([]byte("{}"))
	if err != nil {
		return nil, "", fmt.Errorf("error writing package.json content: %w", err)
	}

	fileHeaders := textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="worker.js"; filename="worker.js"`},
		"Content-Type":        []string{"application/javascript+module"},
	}

	filePart, err := writer.CreatePart(fileHeaders)
	if err != nil {
		return nil, "", fmt.Errorf("error creating file part: %w", err)
	}

	if len(sp.Metadata.jsContent) == 0 {
		return nil, "", fmt.Errorf("worker.js content is empty")
	}

	_, err = filePart.Write(sp.Metadata.jsContent)
	if err != nil {
		return nil, "", fmt.Errorf("error copying file content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("error closing multipart writer: %w", err)
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

func createWorker(ctx context.Context, name string, uid string, pass string, proxy string, nat64Prefix string, fallback string, sub string, kv *kv.Namespace) (*workers.ScriptUpdateResponse, error) {

	envVars := []map[string]string{
		{
			"name":         "kv",
			"namespace_id": kv.ID,
			"type":         "kv_namespace",
		},
		{
			"name": "UUID",
			"text": uid,
			"type": "plain_text",
		},
		{
			"name": "TR_PASS",
			"text": pass,
			"type": "plain_text",
		},
		{
			"name": "SUB_PATH",
			"text": sub,
			"type": "plain_text",
		},
	}

	if proxy != "" {
		envVars = append(envVars, map[string]string{
			"name": "PROXY_IP",
			"text": proxy,
			"type": "plain_text",
		})
	}

	if nat64Prefix != "" {
		envVars = append(envVars, map[string]string{
			"name": "PREFIX",
			"text": nat64Prefix,
			"type": "plain_text",
		})
	}

	if fallback != "" {
		envVars = append(envVars, map[string]string{
			"name": "FALLBACK",
			"text": fallback,
			"type": "plain_text",
		})
	}

	param := ScriptUpdateParams{
		AccountID: cfAccount.ID,
		Metadata: ScriptUpdateParamsMetadataForm{
			Bindings:          envVars,
			MainModule:        "worker.js",
			jsContent:         workerJS,
			CompatibilityDate: time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
			CompatibilityFlags: []string{
				"nodejs_compat",
			},
			Observability: map[string]bool{"enabled": false},
			Placement:     map[string]string{},
			Tags:          []string{},
			TailConsumers: []string{},
			Logpush:       false,
			UsageModel:    "standard",
		},
	}

	data, ct, err := param.MarshalMultipart()
	r := bytes.NewBuffer(data)
	if err != nil {
		return nil, fmt.Errorf("error marshalling multipart data: %w", err)
	}

	result, err := cfClient.Workers.Scripts.Update(
		ctx,
		name,
		workers.ScriptUpdateParams{AccountID: cf.F(cfAccount.ID)},
		option.WithRequestBody(ct, r),
		option.WithHeader("Content-Type", ct),
	)
	if err != nil {
		return nil, fmt.Errorf("error updating worker script: %w", err)
	}

	return result, nil
}

func createKVNamespace(ctx context.Context, ns string) (*kv.Namespace, error) {
	res, err := cfClient.KV.Namespaces.New(ctx, kv.NamespaceNewParams{AccountID: cf.F(cfAccount.ID), Title: cf.F(ns)})
	if err != nil {
		return nil, fmt.Errorf("error creating KV namespace: %w", err)
	}

	return res, nil
}

func enableWorkerSubdomain(ctx context.Context, name string) (*workers.ScriptSubdomainNewResponse, error) {
	return cfClient.Workers.Scripts.Subdomain.New(
		ctx,
		name,
		workers.ScriptSubdomainNewParams{
			AccountID:       cf.F(cfAccount.ID),
			Enabled:         cf.F(true),
			PreviewsEnabled: cf.F(false),
		})
}

func addWorkerCustomDomain(ctx context.Context, script string, customDomain string) (string, error) {
	extractor, err := tldextract.New(cachePath, false)
	if err != nil {
		return "", fmt.Errorf("error extracting TLD: %w", err)
	}

	result := extractor.Extract(customDomain)
	domain := fmt.Sprintf("%s.%s", result.Root, result.Tld)

	zones, err := cfClient.Zones.List(ctx, zones.ZoneListParams{
		Account: cf.F(zones.ZoneListParamsAccount{
			ID: cf.F(cfAccount.ID),
		}),
		Match: cf.F(zones.ZoneListParamsMatch("contains")),
		Name:  cf.F(domain),
	})

	if err != nil {
		return "", fmt.Errorf("error listing zones: %w", err)
	}

	zone := zones.Result[0]
	res, err := cfClient.Workers.Domains.Update(ctx, workers.DomainUpdateParams{
		AccountID:   cf.F(cfAccount.ID),
		Environment: cf.F("production"),
		Hostname:    cf.F(customDomain),
		Service:     cf.F(script),
		ZoneID:      cf.F(zone.ID),
	})

	if err != nil {
		return "", fmt.Errorf("error updating worker domain: %w", err)
	}

	return res.Hostname, nil
}

func isWorkerAvailable(ctx context.Context, name string) bool {
	_, err := cfClient.Workers.Scripts.Get(ctx, name, workers.ScriptGetParams{AccountID: cf.F(cfAccount.ID)})
	return err != nil
}

func listWorkers(ctx context.Context) ([]string, error) {
	workersList, err := cfClient.Workers.Scripts.List(ctx, workers.ScriptListParams{AccountID: cf.F(cfAccount.ID)})
	if err != nil {
		return nil, fmt.Errorf("error listing workers: %w", err)
	}

	if len(workersList.Result) == 0 {
		return nil, nil
	}

	var workerNames []string
	for _, worker := range workersList.Result {
		workerNames = append(workerNames, worker.ID)
	}

	return workerNames, nil
}

func deleteWorker(ctx context.Context, name string) error {
	_, err := cfClient.Workers.Scripts.Delete(ctx, name, workers.ScriptDeleteParams{
		AccountID: cf.F(cfAccount.ID),
		Force:     cf.F(true),
	})
	if err != nil {
		return fmt.Errorf("error deleting worker: %w", err)
	}

	return nil
}

func updateWorker(ctx context.Context, name string) error {
	param := ScriptUpdateParams{
		AccountID: cfAccount.ID,
		Metadata: ScriptUpdateParamsMetadataForm{
			MainModule: "worker.js",
			jsContent:  workerJS,
		},
	}

	data, ct, err := param.MarshalMultipart()
	r := bytes.NewBuffer(data)
	if err != nil {
		return fmt.Errorf("error marshalling multipart data: %w", err)
	}

	_, er := cfClient.Workers.Scripts.Content.Update(
		ctx,
		name,
		workers.ScriptContentUpdateParams{
			AccountID: cf.F(cfAccount.ID),
			Metadata: cf.F(workers.WorkerMetadataParam{
				MainModule: cf.F("worker.js"),
				BodyPart:   cf.F(ct),
			}),
		},
		option.WithRequestBody(ct, r),
		option.WithHeader("Content-Type", ct),
	)
	if er != nil {
		return fmt.Errorf("error updating worker script: %w", er)
	}

	return nil
}

func deployWorker(
	ctx context.Context,
	name string,
	uid string,
	pass string,
	proxy string,
	nat64Prefix string,
	fallback string,
	sub string,
	kvNamespace *kv.Namespace,
	customDomain string,
) (
	panelURL string,
	err error,
) {
	for {
		fmt.Printf("\n%s Creating Worker...\n", title)

		_, err := createWorker(ctx, name, uid, pass, proxy, nat64Prefix, fallback, sub, kvNamespace)
		if err != nil {
			failMessage("Failed to deploy worker.")
			log.Printf("%v\n\n", err)
			if response := promptUser("- Would you like to try again? (y/n): ", []string{"y", "n"}); strings.ToLower(response) == "n" {
				return "", nil
			}
			continue
		}

		successMessage("Worker created successfully!")
		break
	}

	for {
		_, err := enableWorkerSubdomain(ctx, name)
		if err != nil {
			failMessage("Failed to enable worker subdomain.")
			log.Printf("%v\n\n", err)
			if response := promptUser("- Would you like to try again? (y/n): ", []string{"y", "n"}); strings.ToLower(response) == "n" {
				return "", nil
			}
			continue
		}

		successMessage("Worker subdomain enabled successfully!")
		break
	}

	if customDomain != "" {
		for {
			_, err := addWorkerCustomDomain(ctx, name, customDomain)
			if err != nil {
				failMessage("Failed to add custom domain.")
				log.Printf("%v\n\n", err)
				if response := promptUser("- Would you like to try again? (y/n): ", []string{"y", "n"}); strings.ToLower(response) == "n" {
					return "", nil
				}
				continue
			}

			successMessage("Custom domain added to worker successfully!")
			return "https://" + customDomain + "/panel", nil
		}
	}

	resp, err := cfClient.Workers.Subdomains.Get(ctx, workers.SubdomainGetParams{AccountID: cf.F(cfAccount.ID)})
	if err != nil {
		return "", fmt.Errorf("error getting worker subdomain - %w", err)
	}

	return "https://" + name + "." + resp.Subdomain + ".workers.dev/panel", nil
}
