package blitzshipz

import (
	"net/http"
	"strings"
	"time"
)

// Version is the semantic version of this client library.
const Version = "0.1.0"

const (
	// DefaultBaseURL is the production Open API, including its version prefix.
	DefaultBaseURL = "https://open.blitzshipz.com/api/v1"

	defaultUserAgent  = "blitzshipz-go/" + Version
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 3
)

// Config holds the settings used to construct a Client.
type Config struct {
	// APIKey is the key from the Blitzshipz dashboard. It is sent on every
	// request as the x-api-key header. Required.
	APIKey string

	// BaseURL overrides the API base URL, version prefix included. Defaults to
	// DefaultBaseURL.
	BaseURL string

	// HTTPClient lets callers supply a customised *http.Client (proxies,
	// transports, timeouts). If nil, a client with a 30-second timeout is used.
	HTTPClient *http.Client

	// MaxRetries is how many times a transient failure (a network error, HTTP
	// 429 or a 5xx) is retried with exponential backoff. Defaults to 3; -1
	// disables retries.
	//
	// It applies only to calls that are safe to repeat — lookups and pricing.
	// See the package documentation for why creating an order, generating an
	// AWB and cancelling are never retried, whatever this says.
	MaxRetries int

	// UserAgent overrides the User-Agent header sent with each request.
	UserAgent string
}

// Client is the entry point to the Blitzshipz Open API. It is safe for
// concurrent use by multiple goroutines. Construct one with New.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	userAgent  string
	maxRetries int

	// Services grouping the API surface.
	Orders         *OrdersService
	Rates          *RatesService
	Serviceability *ServiceabilityService
	Webhooks       *WebhooksService
}

// New constructs a Client from the given Config.
func New(cfg Config) (*Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, ErrMissingAPIKey
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = defaultMaxRetries
	}
	if maxRetries < 0 {
		maxRetries = 0
	}

	c := &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
		userAgent:  userAgent,
		maxRetries: maxRetries,
	}
	c.Orders = &OrdersService{client: c}
	c.Rates = &RatesService{client: c}
	c.Serviceability = &ServiceabilityService{client: c}
	c.Webhooks = &WebhooksService{client: c}
	return c, nil
}
