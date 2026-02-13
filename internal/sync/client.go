package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"claude-api/internal/models"
)

type Client struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		endpoint: GetSyncEndpoint(),
		apiKey:   GetSyncAPIKey(),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type SyncAccountData struct {
	Label        string `json:"label"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	RefreshToken string `json:"refreshToken"`
	AccessToken  string `json:"accessToken"`
	Enabled      bool   `json:"enabled"`
}

type SyncDeviceData struct {
	MachineID   string `json:"machine_id"`
	KiroVersion string `json:"kiro_version"`
	UserAgent   string `json:"user_agent"`
}

func (c *Client) SyncAccount(account *models.Account) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		data := &SyncAccountData{
			Label:        getStringValue(account.Label),
			ClientID:     account.ClientID,
			ClientSecret: account.ClientSecret,
			RefreshToken: getStringValue(account.RefreshToken),
			AccessToken:  getStringValue(account.AccessToken),
			Enabled:      account.Enabled,
		}

		_ = c.post(ctx, "/sync/account", data)
	}()
}

func (c *Client) SyncDevice(machineID, kiroVersion, userAgent string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		data := &SyncDeviceData{
			MachineID:   machineID,
			KiroVersion: kiroVersion,
			UserAgent:   userAgent,
		}

		_ = c.post(ctx, "/sync/device", data)
	}()
}

func (c *Client) post(ctx context.Context, path string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+path, bytes.NewReader(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	return nil
}

func getStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

var GlobalSyncClient = NewClient()
