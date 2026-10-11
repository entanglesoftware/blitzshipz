package blitzshipz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Payment modes, as Create Order and Check Serviceability spell them.
const (
	PaymentPrepaid = "Prepaid"
	PaymentCOD     = "COD"
)

// OrdersService creates, ships, tracks and cancels orders.
type OrdersService struct{ client *Client }

// Order is the Create Order payload.
//
// Field names follow the API exactly, misspellings included —
// `volumentricWeight` and `dropAlternative_mobile` are what Blitzshipz reads.
// Weights are kilograms and dimensions centimetres; amounts are rupees.
type Order struct {
	// WarehouseName is the pickup location as it is named in the Blitzshipz
	// dashboard, such as "USR0076 - Home". Required.
	WarehouseName string `json:"warehouseName"`
	SellerName    string `json:"sellerName"`
	SellerAddress string `json:"sellerAddress"`

	DropName              string `json:"dropName"`
	DropAddress           string `json:"dropAddress"`
	DropCity              string `json:"dropCity"`
	DropState             string `json:"dropState"`
	DropEmail             string `json:"dropEmail,omitempty"`
	DropMobile            string `json:"dropMobile"`
	DropAlternativeMobile string `json:"dropAlternative_mobile,omitempty"`
	DropPincode           int    `json:"dropPincode"`
	DropLandmark          string `json:"dropLandmark,omitempty"`

	// ReferenceNumber is the caller's own reference for the order — an order
	// number — and the handle a person searches by in the dashboard.
	ReferenceNumber string `json:"referenceNumber,omitempty"`

	TotalTaxes  float64 `json:"totalTaxes,omitempty"`
	TotalAmount float64 `json:"totalAmount"`
	Fragile     bool    `json:"fragile,omitempty"`
	// PaymentMode is PaymentPrepaid or PaymentCOD.
	PaymentMode string      `json:"paymentMode"`
	Items       []OrderItem `json:"items"`

	Length           float64 `json:"l"`
	Breadth          float64 `json:"b"`
	Height           float64 `json:"h"`
	VolumetricWeight float64 `json:"volumentricWeight,omitempty"`
	ActualWeight     float64 `json:"actualWeight"`
	DiscountAmount   float64 `json:"discountAmount,omitempty"`
	ShippingCost     float64 `json:"shippingCost,omitempty"`
}

// OrderItem is one line of an Order.
type OrderItem struct {
	SKU       string  `json:"sku"`
	Name      string  `json:"name"`
	ItemName  string  `json:"itemName"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unitPrice"`
	TaxAmount float64 `json:"taxAmount"`
	Length    float64 `json:"length"`
	Breadth   float64 `json:"breadth"`
	Height    float64 `json:"height"`
	Weight    float64 `json:"weight"`
}

// CreateOrderResponse is what Create Order answers.
//
// ⚠️ **The documentation is not consistent about the field that names the
// order.** Create Order's example answers `{"awb": "ATPRE137180"}`, while Get
// Rates and Generate AWB take "the order reference returned by Create Order
// (CX…/CD…)". So every plausible place is read, and Reference picks the one
// that is there.
type CreateOrderResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	OrderID string `json:"orderId"`
	AWB     string `json:"awb"`
	Data    *struct {
		OrderID string `json:"orderId"`
		AWB     string `json:"awb"`
	} `json:"data"`
	// Raw is the whole answer, for a reference this type does not model.
	Raw json.RawMessage `json:"-"`
}

// Reference is the handle to pass to Get Rates, Generate AWB and Track: the
// order id when the answer names one, otherwise the `awb` it returned.
func (r *CreateOrderResponse) Reference() string {
	if r == nil {
		return ""
	}
	for _, candidate := range []string{r.OrderID, r.dataField(true), r.AWB, r.dataField(false)} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return ""
}

func (r *CreateOrderResponse) dataField(orderID bool) string {
	if r.Data == nil {
		return ""
	}
	if orderID {
		return r.Data.OrderID
	}
	return r.Data.AWB
}

// Create creates an order in Blitzshipz.
//
// No courier is chosen and no balance is spent: the order waits to be priced
// with Rates.Get and shipped with GenerateAWB.
//
// ⚠️ **Never retried.** A request whose answer is lost may still have created
// the order, and a second attempt would create another. Nothing is shipped
// either way, but the duplicate sits in the dashboard waiting for somebody to
// ship it by mistake.
func (s *OrdersService) Create(ctx context.Context, order *Order) (*CreateOrderResponse, error) {
	if order == nil {
		return nil, fmt.Errorf("blitzshipz: an order is required")
	}
	data, err := s.client.doRequest(ctx, requestSpec{
		method: http.MethodPost, path: "/orders", body: order,
	})
	if err != nil {
		return nil, err
	}
	out := &CreateOrderResponse{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("blitzshipz: decoding the Create Order answer: %w", err)
	}
	out.Raw = append(json.RawMessage(nil), data...)
	return out, nil
}

// GenerateAWBRequest ships one order with a chosen courier.
type GenerateAWBRequest struct {
	// VendorCode is the courier, taken from a Rates answer. There is no default.
	VendorCode string `json:"vendorCode"`
	// Weight is the billable weight in kilograms, greater than zero.
	Weight float64 `json:"weight"`
	// Amount is the price accepted for that courier, greater than zero.
	Amount float64 `json:"amount"`
}

// AWBResult is what Generate AWB answers, for one order or a batch.
type AWBResult struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    struct {
		// AWBs maps each shipped order to its AWB.
		AWBs    map[string]string `json:"awbs"`
		Failed  []AWBFailure      `json:"failed"`
		Summary struct {
			Total       int     `json:"total"`
			Success     int     `json:"success"`
			Failed      int     `json:"failed"`
			TotalAmount float64 `json:"totalAmount"`
		} `json:"summary"`
	} `json:"data"`
}

// AWBFailure is an order in a batch that was not shipped, and why.
type AWBFailure struct {
	OrderID string `json:"orderId"`
	Reason  string `json:"reason"`
}

// For reports what happened to one order: its AWB, or the reason it failed.
// Both are empty when the answer does not mention the order at all.
func (r *AWBResult) For(orderID string) (awb, reason string) {
	if r == nil {
		return "", ""
	}
	if awb := strings.TrimSpace(r.Data.AWBs[orderID]); awb != "" {
		return awb, ""
	}
	for _, f := range r.Data.Failed {
		if f.OrderID == orderID {
			return "", strings.TrimSpace(f.Reason)
		}
	}
	return "", ""
}

// GenerateAWB ships one order with the courier chosen from its rates, and
// spends wallet balance.
//
// ⚠️ **Never retried, and a lost answer is not a failure.** This is the call
// that makes a shipment live. A timeout may hide a shipment that went through,
// so a caller must treat an error that is not an *APIError as an unknown
// outcome and look the order up with Track before trying again. Blitzshipz
// rejects an order that is already live rather than shipping it twice, which
// makes a second attempt safe to *send* — but not a substitute for finding out.
func (s *OrdersService) GenerateAWB(ctx context.Context, orderID string, req *GenerateAWBRequest) (*AWBResult, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, fmt.Errorf("blitzshipz: an order id is required")
	}
	if req == nil {
		return nil, fmt.Errorf("blitzshipz: a courier, weight and amount are required")
	}
	out := &AWBResult{}
	err := s.client.callJSON(ctx, requestSpec{
		method: http.MethodPost,
		path:   "/orders/" + escapePath(orderID) + "/generate-awb",
		body:   req,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// BulkAWBOrder is one order in a GenerateAWBBulk batch.
type BulkAWBOrder struct {
	OrderID string  `json:"orderId"`
	Weight  float64 `json:"weight"`
	Amount  float64 `json:"amount"`
}

// BulkAWBRequest ships up to 50 orders with one courier.
type BulkAWBRequest struct {
	VendorCode string         `json:"vendorCode"`
	Orders     []BulkAWBOrder `json:"orders"`
	// StrategyType is optional; omitted means SINGLE_VENDOR, the only value
	// Blitzshipz accepts today.
	StrategyType string `json:"strategyType,omitempty"`
}

// GenerateAWBBulk ships a batch with one courier. Best effort by design: the
// orders that shipped stay shipped and the rest are listed in Data.Failed, so
// retry only those — never the whole batch. Never retried automatically, for
// the reason GenerateAWB gives.
func (s *OrdersService) GenerateAWBBulk(ctx context.Context, req *BulkAWBRequest) (*AWBResult, error) {
	if req == nil || len(req.Orders) == 0 {
		return nil, fmt.Errorf("blitzshipz: at least one order is required")
	}
	out := &AWBResult{}
	err := s.client.callJSON(ctx, requestSpec{
		method: http.MethodPost, path: "/orders/generate-awb/bulk", body: req,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Tracking is what Track Order answers.
type Tracking struct {
	Success       bool   `json:"success"`
	AWB           string `json:"awb"`
	OrderID       string `json:"orderId"`
	CurrentStatus string `json:"currentStatus"`
	// Raw is the whole answer. The documentation shows only the three fields
	// above, and a live answer is likely to carry the scan history too.
	Raw json.RawMessage `json:"-"`
}

// Track looks a shipment up by its AWB or its Blitzshipz order id.
func (s *OrdersService) Track(ctx context.Context, trackingNumber string) (*Tracking, error) {
	if strings.TrimSpace(trackingNumber) == "" {
		return nil, fmt.Errorf("blitzshipz: a tracking number is required")
	}
	data, err := s.client.doRequest(ctx, requestSpec{
		method:     http.MethodGet,
		path:       "/orders/" + escapePath(trackingNumber) + "/track",
		repeatable: true,
	})
	if err != nil {
		return nil, err
	}
	out := &Tracking{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("blitzshipz: decoding the Track Order answer: %w", err)
	}
	out.Raw = append(json.RawMessage(nil), data...)
	return out, nil
}

// Ack is the answer to an action with nothing to return.
type Ack struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Cancel cancels a shipment by its AWB. Never retried: a lost answer may hide
// a cancellation that went through, and Track will say.
func (s *OrdersService) Cancel(ctx context.Context, awb string) (*Ack, error) {
	return s.action(ctx, "cancel", awb)
}

// NDRAction submits an action on a shipment the courier could not deliver.
// Never retried, for the reason Cancel gives.
func (s *OrdersService) NDRAction(ctx context.Context, awb string) (*Ack, error) {
	return s.action(ctx, "ndr", awb)
}

func (s *OrdersService) action(ctx context.Context, verb, awb string) (*Ack, error) {
	if strings.TrimSpace(awb) == "" {
		return nil, fmt.Errorf("blitzshipz: an AWB is required")
	}
	out := &Ack{}
	err := s.client.callJSON(ctx, requestSpec{
		method: http.MethodPost, path: "/orders/open/" + verb + "/" + escapePath(awb),
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
