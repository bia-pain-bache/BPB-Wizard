package internal

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/accounts"
	"github.com/cloudflare/cloudflare-go/v7/kv"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/cloudflare/cloudflare-go/v7/pages"
	"github.com/cloudflare/cloudflare-go/v7/workers"
)

type CfAccount struct {
	Token  string
	ID     string
	Email  string
	Client *cloudflare.Client
}

func newHTTPClient(useCustomDNS bool) *http.Client {
	if !useCustomDNS {
		return &http.Client{Timeout: 30 * time.Second}
	}
	dnsServers := []string{
		"8.8.8.8:53",
		"1.1.1.1:53",
		"9.9.9.9:53",
		"223.5.5.5:53",
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			var lastErr error
			for _, server := range dnsServers {
				conn, err := d.DialContext(ctx, "tcp", server)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, fmt.Errorf("all DNS servers failed: %w", lastErr)
		},
	}

	dialer := &net.Dialer{
		Timeout:  10 * time.Second,
		Resolver: resolver,
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

func NewCfAccount(token string) *CfAccount {
	httpClient := newHTTPClient(isTermux)
	client := cloudflare.NewClient(option.WithAPIToken(token), option.WithHTTPClient(httpClient))
	return &CfAccount{
		Token:  token,
		Client: client,
	}
}

func CreateCfAccount(ctx context.Context, token string) (*CfAccount, error) {
	acc := NewCfAccount(token)

	tokenRes, err := acc.Client.User.Tokens.Verify(ctx)
	if err != nil || tokenRes.Status != "active" {
		return nil, err
	}

	accountsRes, err := acc.Client.Accounts.List(ctx, accounts.AccountListParams{})
	if err != nil {
		return nil, err
	}
	acc.ID = accountsRes.Result[0].ID

	userRes, err := acc.Client.User.Get(ctx)
	if err != nil {
		return nil, err
	}
	acc.Email = userRes.Email

	return acc, nil
}

func (acc *CfAccount) NameTaken(ctx context.Context, deployType, name string) bool {
	var err error
	if deployType == "pages" {
		_, err = acc.Client.Pages.Projects.Get(
			ctx,
			name,
			pages.ProjectGetParams{
				AccountID: cloudflare.F(acc.ID),
			},
		)
		return err == nil
	}

	_, err = acc.Client.Workers.Scripts.Get(
		ctx,
		name,
		workers.ScriptGetParams{
			AccountID: cloudflare.F(acc.ID),
		},
	)

	return err == nil
}

func (acc *CfAccount) CreateKVNamespace(ctx context.Context, workerName string, deployType string) (string, error) {
	title := fmt.Sprintf("%s-%s-%s", workerName, deployType, time.Now().UTC().Format(time.RFC3339))
	namespace, err := acc.Client.KV.Namespaces.New(ctx, kv.NamespaceNewParams{
		AccountID: cloudflare.F(acc.ID),
		Title:     cloudflare.F(title),
	})
	if err != nil {
		return "", err
	}

	return namespace.ID, nil
}

func (acc *CfAccount) GetWorkersDevSubdomain(ctx context.Context) (string, error) {
	subdomain, err := acc.Client.Workers.Subdomains.Get(ctx, workers.SubdomainGetParams{
		AccountID: cloudflare.F(acc.ID),
	})
	if err != nil {
		return "", err
	}

	return subdomain.Subdomain + ".workers.dev", nil
}

func (acc *CfAccount) CreateWorkersDevSubdomain(ctx context.Context) (string, error) {
	maxAttempts := 3
	for range maxAttempts {
		randSub, err := randSubdomain()
		if err != nil {
			return "", err
		}

		if res, err := acc.Client.Workers.Subdomains.Update(ctx, workers.SubdomainUpdateParams{
			AccountID: cloudflare.F(acc.ID),
			Subdomain: cloudflare.F(randSub),
		}); err != nil {
			continue
		} else {
			return res.Subdomain + ".workers.dev", nil
		}
	}
	
	return "", fmt.Errorf("Failed to create a unique workers.dev subdomain after %d attempts.", maxAttempts)
}

func (acc *CfAccount) DeployWorker(ctx context.Context, name string, script io.Reader, namespaceID string) error {
	_, err := acc.Client.Workers.Scripts.Update(
		ctx,
		name,
		workers.ScriptUpdateParams{
			AccountID: cloudflare.F(acc.ID),
			Metadata: cloudflare.F(workers.ScriptUpdateParamsMetadata{
				MainModule:         cloudflare.F("worker.js"),
				CompatibilityDate:  cloudflare.F(time.Now().UTC().Format("2006-01-02")),
				CompatibilityFlags: cloudflare.F([]string{"nodejs_compat"}),
				Bindings: cloudflare.F([]workers.ScriptUpdateParamsMetadataBindingUnion{
					workers.ScriptUpdateParamsMetadataBindingsWorkersBindingKindKVNamespace{
						Name:        cloudflare.F("kv"),
						NamespaceID: cloudflare.F(namespaceID),
						Type:        cloudflare.F(workers.ScriptUpdateParamsMetadataBindingsWorkersBindingKindKVNamespaceTypeKVNamespace),
					},
				}),
			}),
			Files: cloudflare.F([]io.Reader{
				cloudflare.FileParam(
					script,
					"worker.js",
					"application/javascript+module",
				).Value,
			}),
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func (acc *CfAccount) EnableSubdomain(ctx context.Context, subdomain string) error {
	_, err := acc.Client.Workers.Scripts.Subdomain.New(
		ctx,
		subdomain,
		workers.ScriptSubdomainNewParams{
			AccountID: cloudflare.F(acc.ID),
			Enabled:   cloudflare.F(true),
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func (acc *CfAccount) CreatePagesProject(ctx context.Context, name, namespaceID string) (string, error) {
	project, err := acc.Client.Pages.Projects.New(context.TODO(), pages.ProjectNewParams{
		AccountID:        cloudflare.F(acc.ID),
		Name:             cloudflare.F(name),
		ProductionBranch: cloudflare.F("main"),
		DeploymentConfigs: cloudflare.F(pages.ProjectNewParamsDeploymentConfigs{
			Production: cloudflare.F(pages.ProjectNewParamsDeploymentConfigsProduction{
				Browsers:           cloudflare.F(map[string]pages.ProjectNewParamsDeploymentConfigsProductionBrowsers{}),
				CompatibilityDate:  cloudflare.F(time.Now().UTC().Format("2006-01-02")),
				CompatibilityFlags: cloudflare.F([]string{"nodejs_compat"}),
				KVNamespaces: cloudflare.F(map[string]pages.ProjectNewParamsDeploymentConfigsProductionKVNamespaces{
					"kv": {NamespaceID: cloudflare.F(namespaceID)},
				}),
			}),
		}),
	})
	if err != nil {
		return "", err
	}

	return project.Subdomain, nil
}

func (acc *CfAccount) DeployPagesScript(ctx context.Context, name string, script io.Reader) error {
	_, er := acc.Client.Pages.Projects.Deployments.New(
		ctx,
		name,
		pages.ProjectDeploymentNewParams{
			AccountID: cloudflare.F(acc.ID),
			Branch:    cloudflare.F("main"),
			Manifest:  cloudflare.F("{}"),
			WorkerJS:  cloudflare.F(script),
		},
	)
	if er != nil {
		return er
	}

	return nil
}
