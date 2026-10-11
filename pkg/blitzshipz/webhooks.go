package blitzshipz

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// EventOrderStatusUpdate is the one event Blitzshipz emits. Registering any
// other value creates a webhook that never fires.
const EventOrderStatusUpdate = "ORDER_STATUS_UPDATE"

// The headers on every delivery.
const (
	HeaderEvent     = "X-Webhook-Event"
	HeaderSignature = "X-Webhook-Signature"
	// HeaderTest is "true" on a Send Test Event delivery and absent otherwise.
	HeaderTest = "X-Webhook-Test"
)

// WebhooksService manages the endpoints Blitzshipz pushes status changes to.
type WebhooksService struct{ client *Client }

// Webhook is one registered endpoint.
type Webhook struct {
	ID              string     `json:"id"`
	Event           string     `json:"event"`
	URL             string     `json:"url"`
	IsActive        bool       `json:"isActive"`
	HasSecret       bool       `json:"hasSecret"`
	FailureCount    int        `json:"failureCount"`
	LastTriggeredAt *time.Time `json:"lastTriggeredAt,omitempty"`
	CreatedAt       *time.Time `json:"createdAt,omitempty"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
}

// UnmarshalJSON accepts the id as `id` or `_id`: Register answers with the
// second and every other endpoint with the first.
func (w *Webhook) UnmarshalJSON(data []byte) error {
	type plain Webhook
	var v struct {
		plain
		MongoID string `json:"_id"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*w = Webhook(v.plain)
	if w.ID == "" {
		w.ID = v.MongoID
	}
	return nil
}

// RegisterWebhookRequest asks for status changes to be POSTed to URL.
type RegisterWebhookRequest struct {
	// Event is EventOrderStatusUpdate. Left empty, Register fills it in.
	Event string `json:"event"`
	// URL must be https.
	URL string `json:"url"`
	// Secret signs every delivery. Optional, and strongly recommended: without
	// it nothing proves a delivery came from Blitzshipz. It is never shown
	// again, so keep a copy.
	Secret string `json:"secret,omitempty"`
}

type webhookAnswer struct {
	Status  int     `json:"status"`
	Message string  `json:"message"`
	Data    Webhook `json:"data"`
}

// Register adds an endpoint. Never retried: a lost answer may have registered
// it, and List will say.
func (s *WebhooksService) Register(ctx context.Context, req *RegisterWebhookRequest) (*Webhook, error) {
	if req == nil || !strings.HasPrefix(strings.TrimSpace(req.URL), "https://") {
		return nil, fmt.Errorf("blitzshipz: a webhook needs an https:// URL")
	}
	body := *req
	if body.Event == "" {
		body.Event = EventOrderStatusUpdate
	}
	out := &webhookAnswer{}
	if err := s.client.callJSON(ctx, requestSpec{
		method: http.MethodPost, path: "/webhooks/register", body: body,
	}, out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// List returns every registered endpoint and its delivery health. The stored
// secret is never returned, only whether there is one.
func (s *WebhooksService) List(ctx context.Context) ([]Webhook, error) {
	var out struct {
		Data []Webhook `json:"data"`
	}
	if err := s.client.callJSON(ctx, requestSpec{
		method: http.MethodGet, path: "/webhooks", repeatable: true,
	}, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// UpdateWebhookRequest changes an endpoint. Every field is optional: nil
// leaves it as it is.
type UpdateWebhookRequest struct {
	// IsActive false pauses deliveries; true resumes them and clears the
	// failure count.
	IsActive *bool   `json:"isActive,omitempty"`
	URL      *string `json:"url,omitempty"`
	// Secret replaces the signing secret; a pointer to "" removes it and stops
	// signing.
	Secret *string `json:"secret,omitempty"`
}

// Update pauses, resumes, moves or re-keys an endpoint. Setting a state is
// idempotent, so it is retried like a lookup.
func (s *WebhooksService) Update(ctx context.Context, id string, req *UpdateWebhookRequest) (*Webhook, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("blitzshipz: a webhook id is required")
	}
	if req == nil {
		req = &UpdateWebhookRequest{}
	}
	out := &webhookAnswer{}
	if err := s.client.callJSON(ctx, requestSpec{
		method: http.MethodPatch, path: "/webhooks/" + escapePath(id), body: req, repeatable: true,
	}, out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// TestResult is what Send Test Event answers.
type TestResult struct {
	Delivered      bool   `json:"delivered"`
	ResponseStatus int    `json:"responseStatus"`
	Error          string `json:"error"`
}

// Test delivers a sample event to an endpoint, signed like a real one and
// marked with HeaderTest. The call itself always succeeds; Delivered says
// whether the endpoint accepted it.
func (s *WebhooksService) Test(ctx context.Context, id string) (*TestResult, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("blitzshipz: a webhook id is required")
	}
	var out struct {
		Data TestResult `json:"data"`
	}
	if err := s.client.callJSON(ctx, requestSpec{
		method: http.MethodPost, path: "/webhooks/" + escapePath(id) + "/test",
	}, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// Delete removes an endpoint permanently. Deleting is idempotent, so it is
// retried like a lookup.
func (s *WebhooksService) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("blitzshipz: a webhook id is required")
	}
	return s.client.callJSON(ctx, requestSpec{
		method: http.MethodDelete, path: "/webhooks/" + escapePath(id), repeatable: true,
	}, nil)
}

// Event is one delivery to a webhook endpoint.
type Event struct {
	Event string `json:"event"`
	// Time is when Blitzshipz sent it, in milliseconds since the epoch.
	Time int64       `json:"time"`
	Data StatusEvent `json:"data"`
}

// StatusEvent is a status change on one order.
type StatusEvent struct {
	// EventID is stable per status row, so a redelivery carries the same one.
	// Store it and ignore duplicates.
	EventID        string `json:"eventId"`
	OrderID        string `json:"orderId"`
	AWB            string `json:"awb"`
	Vendor         string `json:"vendor"`
	Status         string `json:"status"`
	StatusLocation string `json:"statusLocation"`
	// StatusDateTime is when the courier recorded the status. Deliveries can
	// arrive out of order, so compare this, not arrival time.
	StatusDateTime time.Time `json:"statusDateTime"`
}

// ParseEvent decodes a delivery body. It does not verify it: call
// VerifySignature on the same bytes first.
func ParseEvent(body []byte) (*Event, error) {
	out := &Event{}
	if err := json.Unmarshal(bytes.TrimSpace(body), out); err != nil {
		return nil, fmt.Errorf("blitzshipz: decoding a webhook delivery: %w", err)
	}
	return out, nil
}

// Sign is the signature Blitzshipz puts on a delivery: the HMAC-SHA256 of the
// raw body under the secret, in lowercase hex.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature reports whether signature is the one Blitzshipz would put on
// body under secret, in constant time.
//
// ⚠️ **Pass the raw request body.** A body decoded and encoded again can come
// back with its keys reordered or its numbers rewritten, and then no
// signature matches. An empty secret or signature never verifies.
func VerifySignature(secret string, body []byte, signature string) bool {
	signature = strings.ToLower(strings.TrimSpace(signature))
	if secret == "" || signature == "" {
		return false
	}
	want := Sign(secret, body)
	return hmac.Equal([]byte(want), []byte(signature))
}
