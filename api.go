package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/accounts"
	"github.com/cloudflare/cloudflare-go/v7/kv"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/cloudflare/cloudflare-go/v7/pages"
	"github.com/cloudflare/cloudflare-go/v7/workers"
)

type cfAccount struct {
	Token  string
	ID     string
	Email  string
	Client *cloudflare.Client
}

func NewCfAccount(token string) *cfAccount {
	client := cloudflare.NewClient(option.WithAPIToken(token))
	return &cfAccount{
		Token: token,
		Client: client,
	}
}

func CreateAccount(ctx context.Context, token string) (*cfAccount, error) {
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

func (acc *cfAccount) NameTaken(ctx context.Context, deployType, name string) bool {
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

func (acc *cfAccount) CreateKVNamespace(ctx context.Context, workerName string, deployType string) (string, error) {
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

func (acc *cfAccount) GetWorkersSubdomain(ctx context.Context) (string, error) {
	subdomain, err := acc.Client.Workers.Subdomains.Get(ctx, workers.SubdomainGetParams{
		AccountID: cloudflare.F(acc.ID),
	})
	if err != nil {
		return "", err
	}

	return subdomain.Subdomain + ".workers.dev", nil
}

func (acc *cfAccount) DeployWorker(ctx context.Context, name string, script io.Reader, namespaceID string) error {
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

func (acc *cfAccount) EnableSubdomain(ctx context.Context, subdomain string) error {
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

func (acc *cfAccount) CreatePagesProject(ctx context.Context, name, namespaceID string) (string, error) {
	project, err := acc.Client.Pages.Projects.New(context.TODO(), pages.ProjectNewParams{
		AccountID: cloudflare.F(acc.ID),
		Name:      cloudflare.F(name),
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

func (acc *cfAccount) DeployPagesScript(ctx context.Context, name string, script io.Reader) error {
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
