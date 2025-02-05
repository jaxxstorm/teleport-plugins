package msapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gravitational/teleport-plugins/lib/backoff"
	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
)

const (
	getTokenBaseURL     = "https://login.microsoftonline.com"
	getTokenContentType = "application/x-www-form-urlencoded"
)

// Token represents utility struct used for parsing GetToken resposne
type Token struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// tokenWithTTL represents struct which handles token refresh on expiration
type tokenWithTTL struct {
	mu        sync.RWMutex
	token     Token
	scope     string
	expiresAt int64
	baseURL   string
}

// Bearer returns current token value and refreshes it if token is expired.
//
// MS Graph API issues no refresh_token for client_credentials grant type. There also is no
// extended validity window for this grant type.
func (c *tokenWithTTL) Bearer(ctx context.Context, config Config) (string, error) {
	c.mu.RLock()
	expiresAt := c.expiresAt
	c.mu.RUnlock()

	if expiresAt == 0 || expiresAt > time.Now().UnixNano() {
		token, err := c.getToken(ctx, c.scope, config)
		if err != nil {
			return "", trace.Wrap(err)
		}

		c.mu.Lock()
		defer c.mu.Unlock()

		c.token = token
		c.expiresAt = time.Now().UnixNano() + (token.ExpiresIn * int64(time.Second))
	}

	return "Bearer " + c.token.AccessToken, nil
}

// getToken calls /token endpoint and returns Bearer string
func (c *tokenWithTTL) getToken(ctx context.Context, scope string, config Config) (Token, error) {
	client := http.Client{Timeout: httpTimeout}
	t := Token{}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", config.AppID)
	data.Set("client_secret", config.AppSecret)
	data.Set("scope", scope)

	baseURL := c.baseURL
	if baseURL == "" {
		baseURL = getTokenBaseURL
	}

	getTokenURL := baseURL + "/" + config.TenantID + "/oauth2/v2.0/token"

	r, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		getTokenURL,
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return t, trace.Wrap(err)
	}

	u, err := url.Parse(getTokenBaseURL)
	if err != nil {
		return t, trace.Wrap(err)
	}

	r.Header.Add("Host", u.Host)
	r.Header.Add("Content-Type", getTokenContentType)

	backoff := backoff.NewDecorr(backoffBase, backoffMax, clockwork.NewRealClock())
	for {
		resp, err := client.Do(r)
		if err != nil {
			return t, trace.Wrap(err)
		}

		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return t, trace.Wrap(err)
		}

		if resp.StatusCode != http.StatusOK {
			err = backoff.Do(ctx)
			if err != nil {
				return t, trace.Errorf("Failed to get auth token %v %v %v", resp.StatusCode, scope, string(b))
			}
			continue
		}

		err = json.NewDecoder(bytes.NewReader(b)).Decode(&t)
		if err != nil {
			return t, trace.Wrap(err)
		}

		return t, nil
	}
}
