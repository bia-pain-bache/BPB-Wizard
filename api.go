package main

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
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
	client *cloudflare.Client
}

func NewCfAccount(token string) *cfAccount {
	client := cloudflare.NewClient(option.WithAPIToken(token))
	return &cfAccount{
		Token:  token,
		client: client,
	}
}

func (acc *cfAccount) VerifyToken(ctx context.Context) error {
	_, err := acc.client.User.Tokens.Verify(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (acc *cfAccount) GetAccountID(ctx context.Context) error {
	res, err := acc.client.Accounts.List(ctx, accounts.AccountListParams{})
	if err != nil {
		return err
	}

	accounts := res.Result
	if len(accounts) == 0 {
		return fmt.Errorf("no Cloudflare accounts found for this token")
	}

	acc.ID = accounts[0].ID
	return nil
}

func (acc *cfAccount) GetUserEmail(ctx context.Context) error {
	user, err := acc.client.User.Get(ctx)
	if err != nil {
		return err
	}

	acc.Email = user.Email
	return nil
}

func (acc *cfAccount) NameTaken(ctx context.Context, deployType, name string) bool {
	var err error
	if deployType == "pages" {
		_, err = acc.client.Pages.Projects.Get(
			ctx,
			name,
			pages.ProjectGetParams{
				AccountID: cloudflare.F(acc.ID),
			},
		)
		return err == nil
	}

	_, err = acc.client.Workers.Scripts.Get(
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
	namespace, err := acc.client.KV.Namespaces.New(ctx, kv.NamespaceNewParams{
		AccountID: cloudflare.F(acc.ID),
		Title:     cloudflare.F(title),
	})
	if err != nil {
		return "", err
	}

	return namespace.ID, nil
}

func (acc *cfAccount) GetWorkersSubdomain(ctx context.Context) (string, error) {
	subdomain, err := acc.client.Workers.Subdomains.Get(ctx, workers.SubdomainGetParams{
		AccountID: cloudflare.F(acc.ID),
	})
	if err != nil {
		return "", err
	}

	return subdomain.Subdomain + ".workers.dev", nil
}

func addPart(w *multipart.Writer, fieldName, filename, contentType string, content []byte) error {
	var disposition string
	if filename != "" {
		disposition = fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename)
	} else {
		disposition = fmt.Sprintf(`form-data; name="%s"`, fieldName)
	}

	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", disposition)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}

	part, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = part.Write(content)

	return err
}

func (acc *cfAccount) DeployWorker(ctx context.Context, name string, script io.Reader, namespaceID string) error {
	_, err := acc.client.Workers.Scripts.Update(
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
	_, err := acc.client.Workers.Scripts.Subdomain.New(
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
	project, err := acc.client.Pages.Projects.New(context.TODO(), pages.ProjectNewParams{
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
	_, er := acc.client.Pages.Projects.Deployments.New(
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
