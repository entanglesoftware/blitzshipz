package blitzshipz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// RatesService prices orders across the couriers Blitzshipz works with.
type RatesService struct{ client *Client }

// VendorRate is one courier's price for an order.
//
// ⚠️ **The live answer is not the documented one.** The documentation lists
// `{vendorCode, vendorName, totalAmount}`. Blitzshipz sends
//
//	{"standered":"DIAMOND","weightCategory":"FEATHER","vendorCode":"DL",
//	 "amount":97.35,"zone":"ZONED","rate":60.18,"additionalRate":55.46,
//	 "codCharges":37.17,"weight":0.5,"rto":60.18,"qcCharge":0}
//
// with no name and the price in `amount` (here rate + COD charges). The
// decoder copies `amount` into TotalAmount when `totalAmount` is absent, so
// every caller reads one field whichever spelling arrived.
type VendorRate struct {
	// VendorCode is what GenerateAWB takes, such as "DL".
	VendorCode string `json:"vendorCode"`
	// VendorName is the courier's name, such as "Delhivery". Neither the live
	// answer nor the bulk one carries it.
	VendorName string `json:"vendorName"`
	// TotalAmount is the price for this order, in rupees: `totalAmount` as
	// documented, or `amount` as sent.
	TotalAmount float64 `json:"totalAmount"`
	// Weight is the billable weight the price was worked out for, in
	// kilograms — the weight Generate AWB takes.
	Weight float64 `json:"weight"`

	// The breakdown, as the live answer sends it. Standard keeps the API's
	// spelling, `standered`.
	Rate           float64 `json:"rate"`
	AdditionalRate float64 `json:"additionalRate"`
	CODCharges     float64 `json:"codCharges"`
	RTO            float64 `json:"rto"`
	QCCharge       float64 `json:"qcCharge"`
	Standard       string  `json:"standered"`
	WeightCategory string  `json:"weightCategory"`
	Zone           string  `json:"zone"`
}

// UnmarshalJSON reads a rate in either spelling of its price.
func (r *VendorRate) UnmarshalJSON(b []byte) error {
	type plain VendorRate
	var v struct {
		plain
		Amount float64 `json:"amount"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*r = VendorRate(v.plain)
	if r.TotalAmount == 0 {
		r.TotalAmount = v.Amount
	}
	return nil
}

// Pricing is the rates for one order.
type Pricing struct {
	BasePricing    []VendorRate `json:"basePricing"`
	AdvancePricing []VendorRate `json:"advancePricing"`
}

// All is every rate on offer, base pricing first, cheapest first within each.
func (p Pricing) All() []VendorRate {
	out := make([]VendorRate, 0, len(p.BasePricing)+len(p.AdvancePricing))
	for _, group := range [][]VendorRate{p.BasePricing, p.AdvancePricing} {
		sorted := append([]VendorRate(nil), group...)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TotalAmount < sorted[j].TotalAmount })
		out = append(out, sorted...)
	}
	return out
}

// Find returns the rate a courier quoted, if it quoted one.
func (p Pricing) Find(vendorCode string) (VendorRate, bool) {
	for _, r := range p.All() {
		if strings.EqualFold(r.VendorCode, vendorCode) {
			return r, true
		}
	}
	return VendorRate{}, false
}

// Rates is what Get Rates answers.
type Rates struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    struct {
		OrderID string  `json:"orderId"`
		Rates   Pricing `json:"rates"`
	} `json:"data"`
	// Raw is the whole answer. The documented shapes have been wrong before
	// (Create Order's was), so a caller that finds no courier can show what
	// Blitzshipz actually said.
	Raw json.RawMessage `json:"-"`
}

// Get prices one order across every available courier. Nothing is shipped and
// no balance is spent, so it is safe to repeat.
func (s *RatesService) Get(ctx context.Context, orderID string) (*Rates, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, fmt.Errorf("blitzshipz: an order id is required")
	}
	data, err := s.client.doRequest(ctx, requestSpec{
		method:     http.MethodPost,
		path:       "/rates",
		body:       map[string]string{"orderId": strings.TrimSpace(orderID)},
		repeatable: true,
	})
	if err != nil {
		return nil, err
	}
	out := &Rates{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("blitzshipz: decoding the Get Rates answer: %w", err)
		}
	}
	out.Raw = append(json.RawMessage(nil), data...)
	return out, nil
}

// BulkRates is what Get Rates (Bulk) answers.
type BulkRates struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    struct {
		IndividualRates map[string]Pricing `json:"individualRates"`
		// VendorTotalStrategy is what each courier would charge for the whole
		// batch together, which the individual rates do not add up to.
		VendorTotalStrategy map[string]float64 `json:"vendorTotalStrategy"`
	} `json:"data"`
}

// MaxBulk is the most orders one bulk call accepts.
const MaxBulk = 50

// Bulk prices up to MaxBulk orders in one call.
func (s *RatesService) Bulk(ctx context.Context, orderIDs []string) (*BulkRates, error) {
	if len(orderIDs) == 0 || len(orderIDs) > MaxBulk {
		return nil, fmt.Errorf("blitzshipz: between 1 and %d order ids are required, not %d", MaxBulk, len(orderIDs))
	}
	out := &BulkRates{}
	err := s.client.callJSON(ctx, requestSpec{
		method:     http.MethodPost,
		path:       "/rates/bulk",
		body:       map[string][]string{"orderIds": orderIDs},
		repeatable: true,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
