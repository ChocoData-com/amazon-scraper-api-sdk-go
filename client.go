// Package amazonscraperapi is the official Go client for
// https://amazonscraperapi.com.
//
// Usage:
//
//	client := amazonscraperapi.New("asa_live_...")
//	product, err := client.Product(context.Background(), amazonscraperapi.ProductParams{
//	    Query:  "B09HN3Q81F",
//	    Domain: "com",
//	})
package amazonscraperapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultBaseURL is the production API base URL.
const DefaultBaseURL = "https://api.amazonscraperapi.com"

// Client is a thread-safe client for the Amazon Scraper API.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL (useful for testing).
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithHTTPClient supplies a custom *http.Client for connection pooling / timeouts.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.http = httpClient }
}

// New creates a client. Panics if apiKey is empty.
func New(apiKey string, opts ...Option) *Client {
	if apiKey == "" {
		panic("amazonscraperapi: apiKey is required")
	}
	c := &Client{
		apiKey:  apiKey,
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Error is returned on non-2xx responses.
type Error struct {
	StatusCode int
	Body       map[string]any
}

func (e *Error) Error() string {
	if msg, ok := e.Body["error"].(string); ok {
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, msg)
	}
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}

// ProductParams for the /v1/amazon/product endpoint.
type ProductParams struct {
	Query    string `json:"query"`
	Domain   string `json:"domain,omitempty"`
	Language string `json:"language,omitempty"`
	AddHTML  bool   `json:"add_html,omitempty"`
}

// SearchParams for the /v1/amazon/search endpoint.
type SearchParams struct {
	Query     string `json:"query"`
	Domain    string `json:"domain,omitempty"`
	SortBy    string `json:"sort_by,omitempty"`
	StartPage int    `json:"start_page,omitempty"`
	Pages     int    `json:"pages,omitempty"`
}

// BatchCreateParams creates a new async batch job.
type BatchCreateParams struct {
	Endpoint   string           `json:"endpoint"`
	Items      []map[string]any `json:"items"`
	WebhookURL string           `json:"webhook_url,omitempty"`
}

// BatchCreateResponse is returned from CreateBatch.
type BatchCreateResponse struct {
	ID                      string `json:"id"`
	Status                  string `json:"status"`
	TotalCount              int    `json:"total_count"`
	CreatedAt               string `json:"created_at"`
	WebhookSignatureSecret  string `json:"webhook_signature_secret,omitempty"`
	PollURL                 string `json:"poll_url"`
}

// Product calls GET /v1/amazon/product and returns the raw JSON as a map.
// For strongly-typed fields, unmarshal the returned bytes into your own struct.
func (c *Client) Product(ctx context.Context, p ProductParams) (map[string]any, error) {
	q := url.Values{}
	q.Set("query", p.Query)
	if p.Domain != "" {
		q.Set("domain", p.Domain)
	}
	if p.Language != "" {
		q.Set("language", p.Language)
	}
	if p.AddHTML {
		q.Set("add_html", "true")
	}
	return c.doJSON(ctx, "GET", "/api/v1/amazon/product", q, nil)
}

// Search calls GET /v1/amazon/search.
func (c *Client) Search(ctx context.Context, p SearchParams) (map[string]any, error) {
	q := url.Values{}
	q.Set("query", p.Query)
	if p.Domain != "" {
		q.Set("domain", p.Domain)
	}
	if p.SortBy != "" {
		q.Set("sort_by", p.SortBy)
	}
	if p.StartPage > 0 {
		q.Set("start_page", fmt.Sprintf("%d", p.StartPage))
	}
	if p.Pages > 0 {
		q.Set("pages", fmt.Sprintf("%d", p.Pages))
	}
	return c.doJSON(ctx, "GET", "/api/v1/amazon/search", q, nil)
}

// CreateBatch starts a new async batch job.
func (c *Client) CreateBatch(ctx context.Context, p BatchCreateParams) (*BatchCreateResponse, error) {
	raw, err := c.doJSON(ctx, "POST", "/api/v1/amazon/batch", nil, p)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(raw)
	var out BatchCreateResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetBatch polls the status of a batch.
func (c *Client) GetBatch(ctx context.Context, id string) (map[string]any, error) {
	return c.doJSON(ctx, "GET", "/api/v1/amazon/batch/"+url.PathEscape(id), nil, nil)
}

// VerifyWebhookSignature returns true if the X-ASA-Signature header matches.
func VerifyWebhookSignature(signatureHeader string, rawBody []byte, secret string) bool {
	if signatureHeader == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signatureHeader), []byte(expected))
}

func (c *Client) doJSON(ctx context.Context, method, path string, q url.Values, body any) (map[string]any, error) {
	u, _ := url.Parse(c.baseURL + path)
	if q != nil {
		u.RawQuery = q.Encode()
	}
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "amazonscraperapi-go/0.1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, errors.New("failed to decode response body: " + err.Error())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &Error{StatusCode: resp.StatusCode, Body: out}
	}
	return out, nil
}
