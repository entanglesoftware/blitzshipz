# blitzshipz

A Go client for the [Blitzshipz Open API](https://www.app.blitzshipz.com/open-api/documentation/?api=getting-started), an Indian shipping aggregator that prices a parcel across several couriers and books it with the one you choose.

- **Zero dependencies**: it uses the standard library only.
- **Safe to ship with**: lookups and prices are retried on transient failures. Creating an order, generating an AWB and cancelling are **never** retried, so a lost answer cannot create or ship anything twice.
- **Honest about failure**: `AsAPIError` separates "Blitzshipz refused" from "the answer was lost". Only the first one is safe to treat as "nothing happened".
- **Webhooks**: `VerifySignature` checks the HMAC-SHA256 signature on a delivery, and `ParseEvent` decodes it.
- **Typed**: every documented request and response is a Go type.
- **Tested**: every endpoint and the retry rules are covered by `httptest`-backed tests.

## Install

```sh
go get github.com/entanglesoftware/blitzshipz/pkg/blitzshipz
```

Requires Go 1.23 or newer.

## Project layout

```
pkg/blitzshipz/     # the public SDK (import this)
cmd/blitzshipz/     # read-only CLI: check a key, see live answers
internal/retry/     # private backoff/jitter helpers
docs/api.md         # the API as documented, and where it is unclear
```

## Shipping an order

A shipment takes three steps. Only the last one spends money.

```go
client, err := blitzshipz.New(blitzshipz.Config{APIKey: os.Getenv("BLITZSHIPZ_API_KEY")})
if err != nil {
	log.Fatal(err)
}

// 1. Record the order. No courier yet, nothing spent.
created, err := client.Orders.Create(ctx, &blitzshipz.Order{
	WarehouseName: "USR0076 - Home",
	SellerName:    "Acme", SellerAddress: "Mumbai",
	DropName: "Asha Rao", DropAddress: "12 MG Road", DropCity: "Bengaluru",
	DropState: "Karnataka", DropMobile: "9876543210", DropPincode: 560038,
	TotalAmount: 739, PaymentMode: blitzshipz.PaymentPrepaid,
	Items: []blitzshipz.OrderItem{{SKU: "NM10", Name: "Dry Fruit Powder",
		ItemName: "Dry Fruit Powder", Quantity: 1, UnitPrice: 739}},
	Length: 20, Breadth: 15, Height: 10, ActualWeight: 0.5,
})

// 2. Price it with every courier. Still nothing spent.
rates, err := client.Rates.Get(ctx, created.Reference())
choice := rates.Data.Rates.All()[0] // cheapest

// 3. Ship it. This spends wallet balance.
result, err := client.Orders.GenerateAWB(ctx, created.Reference(), &blitzshipz.GenerateAWBRequest{
	VendorCode: choice.VendorCode, Weight: 0.5, Amount: choice.TotalAmount,
})
if err != nil {
	if _, answered := blitzshipz.AsAPIError(err); !answered {
		// The answer was lost. The order may have shipped: look it up with
		// client.Orders.Track before trying again.
	}
	return err
}
awb, reason := result.For(created.Reference())
```

## Receiving status updates

```go
http.HandleFunc("/blitzshipz/hook", func(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body) // the raw bytes, not re-encoded JSON
	if !blitzshipz.VerifySignature(secret, body, r.Header.Get(blitzshipz.HeaderSignature)) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK) // answer within 5 seconds, then process
	if r.Header.Get(blitzshipz.HeaderTest) == "true" {
		return
	}
	event, err := blitzshipz.ParseEvent(body)
	// Deduplicate on event.Data.EventID. Events can arrive out of order:
	// compare event.Data.StatusDateTime with what you already hold.
})
```

Register the endpoint with `client.Webhooks.Register`, or from Settings → Webhooks in the Blitzshipz dashboard.

## Checking a key from the command line

```sh
export BLITZSHIPZ_API_KEY=...
go run ./cmd/blitzshipz track CX221367
go run ./cmd/blitzshipz serviceability 110001 400001
go run ./cmd/blitzshipz rates CX123456
go run ./cmd/blitzshipz webhooks
```

The CLI cannot create, ship or cancel anything.

## Development

```sh
make check   # gofmt, go vet, go test -race
```
