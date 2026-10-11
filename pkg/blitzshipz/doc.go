// Package blitzshipz is a Go client for the Blitzshipz Open API
// (https://open.blitzshipz.com/api/v1), an Indian shipping aggregator that
// prices a parcel across several couriers and books it with the one chosen.
//
// # Getting started
//
//	client, err := blitzshipz.New(blitzshipz.Config{APIKey: "your-api-key"})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	created, err := client.Orders.Create(ctx, &blitzshipz.Order{ ... })
//	rates, err := client.Rates.Get(ctx, created.Reference())
//	cheapest := rates.Data.Rates.All()[0]
//	result, err := client.Orders.GenerateAWB(ctx, created.Reference(),
//		&blitzshipz.GenerateAWBRequest{
//			VendorCode: cheapest.VendorCode, Weight: 0.5, Amount: cheapest.TotalAmount,
//		})
//	awb, reason := result.For(created.Reference())
//
// # The three steps of a shipment
//
// Blitzshipz separates what an order is from who carries it:
//
//  1. Orders.Create records the order. No courier is chosen and nothing is
//     spent.
//  2. Rates.Get prices it with every courier on the account. Still nothing
//     is spent.
//  3. Orders.GenerateAWB ships it with one of them, at the price quoted, and
//     spends wallet balance. This is the step with a physical consequence.
//
// # What is retried, and what never is
//
// Lookups and prices — Track, Rates, Serviceability, listing webhooks — are
// retried on a network error, a 429 or a 5xx, with exponential backoff and
// full jitter.
//
// ⚠️ **Creating an order, generating an AWB, cancelling, an NDR action and
// registering a webhook are never retried**, whatever Config.MaxRetries says.
// A request whose answer was lost may well have been carried out, and a second
// one would create a second order or act twice. So the client returns the
// error and leaves the decision to the caller, and AsAPIError tells the two
// cases apart: an *APIError means Blitzshipz answered and refused; any other
// error means the outcome is unknown, and Orders.Track is how to find out.
//
// # Webhooks
//
// Blitzshipz POSTs every status change to a registered https endpoint. Verify
// the raw body with VerifySignature before trusting it, decode it with
// ParseEvent, deduplicate on StatusEvent.EventID, and compare
// StatusEvent.StatusDateTime with what you hold, because deliveries can arrive
// out of order. A delivery carrying HeaderTest is a test and should not be
// recorded. Answer 2xx within five seconds; five consecutive failures switch a
// webhook off.
//
// # Where the documentation is unclear
//
// The answers come in two dialects — `"success": true` on the order
// endpoints, `"status": 200` on the rest — and a refusal may arrive as a 2xx
// with either saying otherwise; both are recognised and returned as an
// *APIError. Create Order's example names the order `awb` while Get Rates
// expects an order id it says Create Order returned; CreateOrderResponse
// keeps both and Reference chooses.
package blitzshipz
