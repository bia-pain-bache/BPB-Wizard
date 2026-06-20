package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/accounts"
	"github.com/cloudflare/cloudflare-go/v4/option"
	"golang.org/x/oauth2"
)

//go:embed static/index.html
var indexHTML []byte

var (
	cfClient      *cf.Client
	cfAccount     *accounts.Account
	state         string
	codeVerifier  string
	obtainedToken = make(chan *oauth2.Token)
	config        = &oauth2.Config{
		ClientID:     "54d11594-84e4-41aa-b438-e81b8fa78ee7",
		ClientSecret: "",
		RedirectURL:  "http://localhost:8976/oauth/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://dash.cloudflare.com/oauth2/auth",
			TokenURL: "https://dash.cloudflare.com/oauth2/token",
		},
		Scopes: []string{
			"account:read", "user:read", "workers:write", "workers_kv:write",
			"workers_routes:write", "workers_scripts:write", "workers_tail:read",
			"d1:write", "pages:write", "pages:read", "zone:read", "ssl_certs:write",
			"ai:write", "queues:write", "pipelines:write", "secrets_store:write",
			"offline_access",
		},
	}
)

func NewClient(ctx context.Context, token *oauth2.Token) (*cf.Client, *oauth2.Token, error) {
	tokenSource := config.TokenSource(ctx, token)
	refreshedToken, err := tokenSource.Token()
	if err != nil {
		return nil, nil, err
	}

	return cf.NewClient(option.WithAPIToken(refreshedToken.AccessToken)), refreshedToken, nil
}

func getAccounts(ctx context.Context) ([]accounts.Account, error) {
	res, err := cfClient.Accounts.List(ctx, accounts.AccountListParams{})
	if err != nil {
		return nil, fmt.Errorf("error listing accounts - %v", err)
	}

	return res.Result, nil
}

func generateAuthURL() string {
	state = generateState()
	codeVerifier = generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)

	return config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func generateState() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	buff := make([]byte, 16)

	for i := range buff {
		buff[i] = charset[rand.Int64()%int64(len(charset))]
	}

	return base64.URLEncoding.EncodeToString(buff)
}

func generateCodeVerifier() string {
	b := make([]byte, 32)

	for i := range b {
		b[i] = byte(rand.IntN(256))
	}

	return base64.RawURLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func login() {
	url := generateAuthURL()
	fmt.Printf("\n%s Login %s...\n", title, fmtStr("Cloudflare", ORANGE, true))

	if err := openURL(url); err != nil {
		failMessage("Failed to login.")
		log.Fatalln(err)
	}
}

func ensureCloudflareAuth(ctx context.Context) error {
	if cfClient != nil && cfAccount != nil {
		return nil
	}

	store := newTokenStore()
	loginStore, err := store.LoadLogins()
	if err == nil && len(loginStore.Logins) > 0 {
		login, shouldLogin := selectStoredCloudflareLogin(loginStore)
		if !shouldLogin {
			if err := useCloudflareLogin(ctx, store, login); err == nil {
				return nil
			}

			failMessage("Saved Cloudflare login is expired or invalid.")
			_ = store.DeleteLogin(login.Email)
		}
	}

	return loginCloudflare(ctx, store)
}

func useCloudflareLogin(ctx context.Context, store tokenStore, login cloudflareLogin) error {
	if login.Token == nil {
		return fmt.Errorf("saved Cloudflare login has no token")
	}
	originalEmail := login.Email

	client, refreshedToken, err := NewClient(ctx, login.Token)
	if err != nil {
		return err
	}

	cfClient = client

	accountsList, err := getAccounts(ctx)
	if err != nil {
		return err
	}

	account, err := selectCloudflareAccount(accountsList)
	if err != nil {
		return err
	}

	cfAccount = account
	login.Token = refreshedToken

	user, err := cfClient.User.Get(ctx)
	if err == nil {
		login.Email = cloudflareUserEmail(user.JSON.RawJSON())
	}

	if login.Email == "" {
		login.Email = originalEmail
	}

	if err := store.SaveLogin(login); err != nil {
		return err
	}

	if originalEmail != "" && originalEmail != login.Email {
		_ = store.DeleteLogin(originalEmail)
	}

	return nil
}

func loginCloudflare(ctx context.Context, store tokenStore) error {
	go login()
	token := <-obtainedToken

	client, refreshedToken, err := NewClient(ctx, token)
	if err != nil {
		return err
	}

	cfClient = client
	accountsList, err := getAccounts(ctx)
	if err != nil {
		return err
	}

	account, err := selectCloudflareAccount(accountsList)
	if err != nil {
		return err
	}

	cfAccount = account

	login := cloudflareLogin{
		Email: "Cloudflare user",
		Token: refreshedToken,
	}

	user, err := cfClient.User.Get(ctx)
	if err == nil {
		login.Email = cloudflareUserEmail(user.JSON.RawJSON())
	}

	return store.SaveLogin(login)
}

func selectStoredCloudflareLogin(store cloudflareLoginStore) (cloudflareLogin, bool) {
	var message strings.Builder
	var answers []string

	for i, login := range store.Logins {
		active := ""
		if login.Email == store.ActiveEmail {
			active = fmtStr(" [active]", GREEN, true)
		}
		message.WriteString(fmt.Sprintf("%d- %s%s\n", i+1, login.Email, active))
		answers = append(answers, strconv.Itoa(i+1))
	}

	loginOption := len(store.Logins) + 1
	message.WriteString(fmt.Sprintf("%d- %s\n\n- Select: ", loginOption, fmtStr("Login with a new Cloudflare account.", ORANGE, true)))
	answers = append(answers, strconv.Itoa(loginOption))

	response := promptUser(message.String(), answers)
	selected, _ := strconv.Atoi(response)
	if selected == loginOption {
		return cloudflareLogin{}, true
	}

	return store.Logins[selected-1], false
}

func selectCloudflareAccount(accountsList []accounts.Account) (*accounts.Account, error) {
	if len(accountsList) == 0 {
		return nil, fmt.Errorf("no Cloudflare accounts found")
	}

	if len(accountsList) == 1 {
		return &accountsList[0], nil
	}

	var message strings.Builder
	var answers []string
	message.WriteString("Cloudflare accounts:\n")
	for i, account := range accountsList {
		message.WriteString(fmt.Sprintf("%d- %s\n", i+1, account.Name))
		answers = append(answers, strconv.Itoa(i+1))
	}
	message.WriteString("\n- Select: ")

	response := promptUser(message.String(), answers)
	selected, _ := strconv.Atoi(response)
	return &accountsList[selected-1], nil
}

func cloudflareUserEmail(rawJSON string) string {
	var user struct {
		Email string `json:"email"`
		ID    string `json:"id"`
	}

	if err := json.Unmarshal([]byte(rawJSON), &user); err != nil {
		return ""
	}

	if user.Email != "" {
		return user.Email
	}

	return user.ID
}

func callback(w http.ResponseWriter, r *http.Request) {
	param := r.URL.Query().Get("state")
	if param != state {
		failMessage("Invalid OAuth state.")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		failMessage("No code returned.")
		return
	}

	token, err := config.Exchange(
		context.Background(),
		code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)

	if err != nil {
		failMessage("Failed to exchange oauthToken.")
		log.Fatalln(err)
	}

	obtainedToken <- token
	successMessage("Cloudflare logged in successfully!")

	w.Header().Set("Content-Type", "text/html")
	w.Write(indexHTML)
}
