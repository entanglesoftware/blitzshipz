package blitzshipz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ServiceabilityService asks whether a route can be served.
type ServiceabilityService struct{ client *Client }

// ServiceabilityRequest is a route and, optionally, the parcel on it.
type ServiceabilityRequest struct {
	OriginPincode      int `json:"originPincode"`
	DestinationPincode int `json:"destinationPincode"`
	// PaymentMode is PaymentPrepaid or PaymentCOD.
	PaymentMode    string  `json:"paymentMode,omitempty"`
	Weight         float64 `json:"weight,omitempty"` // kilograms
	DeclaredAmount float64 `json:"declaredAmount,omitempty"`
	Length         float64 `json:"length,omitempty"` // centimetres
	Breadth        float64 `json:"breadth,omitempty"`
	Height         float64 `json:"height,omitempty"`
}

// Serviceability is what Check Serviceability answers.
type Serviceability struct {
	Success     bool   `json:"success"`
	Serviceable bool   `json:"serviceable"`
	Message     string `json:"message"`
	// Raw is the whole answer. The API describes "rate/estimate details" that
	// its example does not show.
	Raw json.RawMessage `json:"-"`
}

// Check asks whether a parcel can travel from one pincode to another. Read
// only, so it is safe to repeat.
func (s *ServiceabilityService) Check(ctx context.Context, req *ServiceabilityRequest) (*Serviceability, error) {
	if req == nil || req.OriginPincode == 0 || req.DestinationPincode == 0 {
		return nil, fmt.Errorf("blitzshipz: an origin and a destination pincode are required")
	}
	data, err := s.client.doRequest(ctx, requestSpec{
		method: http.MethodPost, path: "/serviceability", body: req, repeatable: true,
	})
	if err != nil {
		return nil, err
	}
	out := &Serviceability{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("blitzshipz: decoding the serviceability answer: %w", err)
	}
	out.Raw = append(json.RawMessage(nil), data...)
	return out, nil
}
