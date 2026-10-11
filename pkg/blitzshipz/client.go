package blitzshipz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/entanglesoftware/blitzshipz/internal/retry"
)

// maxBody caps a response read. Every Blitzshipz answer is a small JSON
// document; anything larger is a misbehaving server.
const maxBody = 4 << 20

// maxRawBody caps what an APIError carries, so a refusal written to a database
// column or a log line stays readable.
const maxRawBody = 2000

// requestSpec describes a single HTTP call to the API.
type requestSpec struct {
	method string
	path   string
	body   any // marshalled as JSON when non-nil

	// repeatable marks a call that may be sent again after a failure: a lookup
	// or a price. ⚠️ Anything that creates, ships or cancels must leave this
	// false — see the package documentation.
	repeatable bool
}

// callJSON performs the request and decodes the response body into out, which
// may be nil when the body is not needed.
func (c *Client) callJSON(ctx context.Context, spec requestSpec, out any) error {
	data, err := c.doRequest(ctx, spec)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("blitzshipz: decoding the response to %s %s: %w", spec.method, spec.path, err)
	}
	return nil
}

// doRequest executes a request, retrying it only when spec.repeatable, and
// returns the body of a successful answer or a typed error.
func (c *Client) doRequest(ctx context.Context, spec requestSpec) ([]byte, error) {
	var bodyBytes []byte
	if spec.body != nil {
		b, err := json.Marshal(spec.body)
		if err != nil {
			return nil, fmt.Errorf("blitzshipz: encoding the request body: %w", err)
		}
		bodyBytes = b
	}

	retries := 0
	if spec.repeatable {
		retries = c.maxRetries
	}

	endpoint := c.baseURL + spec.path
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := retry.Sleep(ctx, retry.Backoff(attempt)); err != nil {
				return nil, err
			}
		}

		req, err := c.buildRequest(ctx, spec.method, endpoint, bodyBytes)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < retries {
				continue
			}
			return nil, fmt.Errorf("blitzshipz: %s %s: %w", spec.method, spec.path, err)
		}

		data, err := readAndClose(resp)
		if err != nil {
			if attempt < retries {
				continue
			}
			return nil, fmt.Errorf("blitzshipz: reading the response to %s %s: %w", spec.method, spec.path, err)
		}

		if apiErr := refusal(resp.StatusCode, data); apiErr != nil {
			if apiErr.Temporary() && attempt < retries {
				continue
			}
			return nil, apiErr
		}
		return data, nil
	}
}

// buildRequest constructs an *http.Request for a single attempt.
func (c *Client) buildRequest(ctx context.Context, method, endpoint string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("blitzshipz: building the request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func readAndClose(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

// envelope is the part of every Blitzshipz answer that says whether it worked.
//
// The API answers in two dialects. The order endpoints say `"success": true`;
// the rates, AWB and webhook endpoints say `"status": 200`. Either may carry a
// `message`. Both are read, so a refusal is recognised in whichever it uses.
type envelope struct {
	Success *bool           `json:"success"`
	Status  json.RawMessage `json:"status"`
	Message string          `json:"message"`
	Error   string          `json:"error"`
}

// refusal reads a response and returns the APIError it amounts to, or nil.
//
// A non-2xx is always a refusal. A 2xx is one when its body says so: an
// explicit `"success": false`, or a numeric `"status"` of 400 or more.
func refusal(httpStatus int, data []byte) *APIError {
	trimmed := bytes.TrimSpace(data)

	var env envelope
	decoded := len(trimmed) > 0 && trimmed[0] == '{' && json.Unmarshal(trimmed, &env) == nil

	bodyStatus := 0
	if decoded && len(env.Status) > 0 {
		_ = json.Unmarshal(env.Status, &bodyStatus) // a string status is ignored
	}

	ok := httpStatus >= 200 && httpStatus < 300
	if ok && decoded {
		if env.Success != nil && !*env.Success {
			ok = false
		}
		if bodyStatus >= 400 {
			ok = false
		}
	}
	if ok {
		return nil
	}

	apiErr := &APIError{HTTPStatus: httpStatus, Status: bodyStatus}
	if decoded {
		apiErr.Message = strings.TrimSpace(env.Message)
		if apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(env.Error)
		}
	}
	raw := string(trimmed)
	if len(raw) > maxRawBody {
		raw = raw[:maxRawBody] + "…"
	}
	apiErr.RawBody = raw
	return apiErr
}

// escapePath escapes a single path segment for safe interpolation.
func escapePath(segment string) string {
	return url.PathEscape(strings.TrimSpace(segment))
}
