package blitzshipz

import (
	"errors"
	"fmt"
	"strings"
)

// APIError is Blitzshipz answering and refusing.
//
// It is returned for a non-2xx response, and also for a 2xx whose body says it
// failed — `"success": false`, or a `"status"` of 400 or more — because the API
// reports some refusals that way. Either way the request reached Blitzshipz
// and was turned down, which is a different fact from a request whose answer
// was lost: see AsAPIError.
type APIError struct {
	// HTTPStatus is the status line's code.
	HTTPStatus int
	// Status is the `status` the body reported, when it carried one. It
	// usually matches HTTPStatus, and is the only signal on a refusal that
	// arrived as a 200.
	Status int
	// Message is the body's `message` — Blitzshipz's own sentence, such as
	// "Insufficient Balance! Please Recharge".
	Message string
	// RawBody is the response body, trimmed and capped, for whatever the
	// structured fields above did not capture.
	RawBody string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	code := e.code()
	switch {
	case e.Message != "":
		return fmt.Sprintf("blitzshipz: %d: %s", code, e.Message)
	case e.RawBody != "":
		return fmt.Sprintf("blitzshipz: unexpected response (status %d): %s", code, e.RawBody)
	default:
		return fmt.Sprintf("blitzshipz: unexpected response (status %d)", code)
	}
}

// code is the most specific status known: the body's, then the status line's.
func (e *APIError) code() int {
	if e.Status >= 400 {
		return e.Status
	}
	return e.HTTPStatus
}

// Temporary reports whether the refusal is likely to pass if asked again
// later: a rate limit or a server error.
//
// It says nothing about whether asking again is *safe*. For a lookup it
// always is; for Generate AWB a 5xx may still have shipped the order — see
// the package documentation.
func (e *APIError) Temporary() bool {
	code := e.code()
	return code == 429 || code >= 500
}

// Insufficient reports whether the refusal is about the wallet balance.
//
// Matched on Blitzshipz's sentence, which is the only signal it gives. A
// caller that cannot act on a recharge can ignore this; one that can tell an
// operator to top up should, because the order is otherwise fine.
func (e *APIError) Insufficient() bool {
	return strings.Contains(strings.ToLower(e.Message+" "+e.RawBody), "insufficient balance")
}

// AsAPIError extracts an *APIError from err, if one is present in its chain.
//
// The boolean is the distinction that matters most to a caller that ships
// parcels: true means Blitzshipz answered, false means the request may or may
// not have been received — a timeout, a dropped connection, a cancelled
// context — and its outcome is unknown.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// ErrMissingAPIKey is returned by New when no API key is supplied.
var ErrMissingAPIKey = errors.New("blitzshipz: an API key is required")
