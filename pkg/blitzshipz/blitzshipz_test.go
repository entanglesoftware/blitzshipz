package blitzshipz

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// served records what one request carried, so a test can assert on the wire.
type served struct {
	method, path, apiKey, contentType string
	body                              map[string]any
}

// fake starts a server answering every request with handler, and a client
// pointed at it.
func fake(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, got served)) (*Client, *[]served) {
	t.Helper()
	var calls []served
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := served{
			method: r.Method, path: r.URL.EscapedPath(),
			apiKey: r.Header.Get("x-api-key"), contentType: r.Header.Get("Content-Type"),
		}
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &got.body); err != nil {
				t.Errorf("the request body is not a JSON object: %s", raw)
			}
		}
		calls = append(calls, got)
		handler(w, r, got)
	}))
	t.Cleanup(srv.Close)

	client, err := New(Config{APIKey: "test-key", BaseURL: srv.URL + "/api/v1/", MaxRetries: 2})
	if err != nil {
		t.Fatal(err)
	}
	return client, &calls
}

func answer(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestNewNeedsAnAPIKey(t *testing.T) {
	if _, err := New(Config{APIKey: "  "}); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("New without a key = %v, want ErrMissingAPIKey", err)
	}
	c, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("base URL = %q, want the production API", c.baseURL)
	}
}

func TestCreateOrderSendsTheAPIsOwnFieldNames(t *testing.T) {
	client, calls := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
		answer(w, 200, `{"success": true, "awb": "ATPRE137180", "message": "Order created successfully"}`)
	})

	got, err := client.Orders.Create(context.Background(), &Order{
		WarehouseName: "USR0076 - Home", DropName: "Ayush", DropMobile: "9958109872",
		DropAlternativeMobile: "9958109873", DropPincode: 201318, TotalAmount: 2200,
		PaymentMode: PaymentPrepaid, Length: 10, Breadth: 38, Height: 10,
		VolumetricWeight: 0.76, ActualWeight: 1,
		Items: []OrderItem{{SKU: "098", Name: "Wax", ItemName: "Wax", Quantity: 1, UnitPrice: 2100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if call.method != http.MethodPost || call.path != "/api/v1/orders" {
		t.Fatalf("sent %s %s", call.method, call.path)
	}
	if call.apiKey != "test-key" || call.contentType != "application/json" {
		t.Fatalf("headers: key %q, content type %q", call.apiKey, call.contentType)
	}
	// The misspellings are the API's, and a corrected spelling is ignored.
	for _, field := range []string{"volumentricWeight", "dropAlternative_mobile", "l", "b", "h", "warehouseName"} {
		if _, ok := call.body[field]; !ok {
			t.Errorf("the body has no %q: %v", field, call.body)
		}
	}
	if call.body["dropPincode"] != float64(201318) {
		t.Errorf("dropPincode = %v, want the number", call.body["dropPincode"])
	}
	if got.Reference() != "ATPRE137180" {
		t.Fatalf("Reference = %q, want the awb the answer named", got.Reference())
	}
	if len(got.Raw) == 0 {
		t.Fatal("the raw answer was not kept")
	}
}

func TestReferencePrefersAnOrderIDOverAnAWB(t *testing.T) {
	var r CreateOrderResponse
	if err := json.Unmarshal([]byte(`{"success":true,"awb":"ATPRE1","data":{"orderId":"CX123456"}}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Reference() != "CX123456" {
		t.Fatalf("Reference = %q, want the order id", r.Reference())
	}
}

// The first live Create Order answer named the order in none of the modelled
// fields; the reference is found by its documented shape wherever it sits.
func TestReferenceIsFoundByItsShape(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"status":200,"message":"Order created","data":{"order_id":"CX221367"}}`, "CX221367"},
		{`{"status":200,"data":[{"orderNumber":"CD100200"}]}`, "CD100200"},
		// Under a key mentioning "order" beats one that does not.
		{`{"status":200,"data":{"a":"CX111111","orderRef":"CX222222"}}`, "CX222222"},
		// The awb stays the last resort.
		{`{"success":true,"awb":"ATPRE137180","data":{"ref":"CX333333"}}`, "CX333333"},
		{`{"success":true,"awb":"ATPRE137180"}`, "ATPRE137180"},
		{`{"status":200,"message":"Order created"}`, ""},
	} {
		client, _ := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
			answer(w, 200, tc.body)
		})
		got, err := client.Orders.Create(context.Background(), &Order{})
		if err != nil {
			t.Fatalf("%s: %v", tc.body, err)
		}
		if got.Reference() != tc.want {
			t.Errorf("%s: Reference = %q, want %q", tc.body, got.Reference(), tc.want)
		}
	}
}

func TestAStatusWordOfFailureInsideA200IsARefusal(t *testing.T) {
	client, _ := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
		answer(w, 200, `{"status":"error","message":"Warehouse Not Found"}`)
	})
	_, err := client.Orders.Create(context.Background(), &Order{})
	apiErr, answered := AsAPIError(err)
	if !answered || apiErr.Message != "Warehouse Not Found" {
		t.Fatalf("err = %v, want the refusal with its message", err)
	}
}

// ⚠️ The property that matters most in the package: an order is never created
// twice because a server had a bad moment.
func TestCreatingAnOrderIsNeverRetried(t *testing.T) {
	client, calls := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
		answer(w, 503, `{"success": false, "message": "try later"}`)
	})
	_, err := client.Orders.Create(context.Background(), &Order{})
	apiErr, answered := AsAPIError(err)
	if !answered || !apiErr.Temporary() {
		t.Fatalf("err = %v, want a temporary APIError", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("Create was sent %d times, want once", len(*calls))
	}
}

func TestGeneratingAnAWBIsNeverRetried(t *testing.T) {
	client, calls := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
		answer(w, 502, `bad gateway`)
	})
	_, err := client.Orders.GenerateAWB(context.Background(), "CX1",
		&GenerateAWBRequest{VendorCode: "DL", Weight: 1, Amount: 60})
	if _, answered := AsAPIError(err); !answered {
		t.Fatalf("err = %v, want an APIError", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("GenerateAWB was sent %d times, want once", len(*calls))
	}
}

// A lost answer is not a refusal: the caller must not read it as "Blitzshipz
// said no", because the order may have shipped.
func TestALostAnswerIsNotAnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	client, _ := New(Config{APIKey: "k", BaseURL: srv.URL})

	_, err := client.Orders.GenerateAWB(context.Background(), "CX1",
		&GenerateAWBRequest{VendorCode: "DL", Weight: 1, Amount: 60})
	if err == nil {
		t.Fatal("a dropped connection returned no error")
	}
	if _, answered := AsAPIError(err); answered {
		t.Fatalf("a dropped connection read as an answer: %v", err)
	}
}

func TestGenerateAWBReadsTheOrdersOutcome(t *testing.T) {
	client, calls := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
		answer(w, 200, `{"status": 200, "message": "AWB generated successfully",
			"data": {"awbs": {"CX/1": "AWB123"}, "failed": [{"orderId": "CX2", "reason": "Insufficient Balance! Please Recharge"}],
			"summary": {"total": 2, "success": 1, "failed": 1, "totalAmount": 60.5}}}`)
	})
	got, err := client.Orders.GenerateAWB(context.Background(), "CX/1",
		&GenerateAWBRequest{VendorCode: "DL", Weight: 1.5, Amount: 60.5})
	if err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if call.path != "/api/v1/orders/CX%2F1/generate-awb" {
		t.Fatalf("path = %q, want the id escaped as one segment", call.path)
	}
	if call.body["vendorCode"] != "DL" || call.body["weight"] != 1.5 || call.body["amount"] != 60.5 {
		t.Fatalf("body = %v", call.body)
	}
	if awb, reason := got.For("CX/1"); awb != "AWB123" || reason != "" {
		t.Fatalf("For(CX/1) = %q, %q", awb, reason)
	}
	if awb, reason := got.For("CX2"); awb != "" || !strings.Contains(reason, "Insufficient") {
		t.Fatalf("For(CX2) = %q, %q", awb, reason)
	}
	if awb, reason := got.For("CX9"); awb != "" || reason != "" {
		t.Fatalf("For(an order not mentioned) = %q, %q", awb, reason)
	}
}

// Some refusals arrive as a 200 whose body says otherwise, in either dialect.
func TestARefusalInsideA200IsAnAPIError(t *testing.T) {
	for name, body := range map[string]string{
		"success false": `{"success": false, "message": "Invalid pincode"}`,
		"status 400":    `{"status": 400, "message": "Insufficient Balance! Please Recharge"}`,
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
				answer(w, 200, body)
			})
			_, err := client.Orders.Create(context.Background(), &Order{})
			apiErr, answered := AsAPIError(err)
			if !answered {
				t.Fatalf("err = %v, want an APIError", err)
			}
			if apiErr.HTTPStatus != 200 || apiErr.Message == "" {
				t.Fatalf("APIError = %+v", apiErr)
			}
			if apiErr.Temporary() {
				t.Fatal("a refusal about the order read as temporary")
			}
		})
	}

	var e APIError
	e.Message = "Insufficient Balance! Please Recharge"
	if !e.Insufficient() {
		t.Fatal("the wallet refusal was not recognised")
	}
}

func TestRatesAreRetriedAndSorted(t *testing.T) {
	var hits atomic.Int32
	client, calls := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
		if hits.Add(1) == 1 {
			answer(w, 503, `{"status": 503, "message": "busy"}`)
			return
		}
		answer(w, 200, `{"status": 200, "message": "Rates retrieved successfully", "data": {"orderId": "CX1",
			"rates": {"basePricing": [{"vendorCode": "XB", "vendorName": "Xpressbees", "totalAmount": 64.5},
			{"vendorCode": "DL", "vendorName": "Delhivery", "totalAmount": 60}], "advancePricing": []}}}`)
	})
	got, err := client.Rates.Get(context.Background(), " CX1 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Fatalf("Rates was sent %d times, want a retry after the 503", len(*calls))
	}
	if (*calls)[1].body["orderId"] != "CX1" {
		t.Fatalf("body = %v", (*calls)[1].body)
	}
	all := got.Data.Rates.All()
	if len(all) != 2 || all[0].VendorCode != "DL" {
		t.Fatalf("All = %+v, want the cheapest first", all)
	}
	if r, ok := got.Data.Rates.Find("xb"); !ok || r.TotalAmount != 64.5 {
		t.Fatalf("Find(xb) = %+v, %v", r, ok)
	}
}

func TestRetriesCanBeSwitchedOff(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		answer(w, 503, `{}`)
	}))
	t.Cleanup(srv.Close)
	client, _ := New(Config{APIKey: "k", BaseURL: srv.URL, MaxRetries: -1})
	if _, err := client.Rates.Get(context.Background(), "CX1"); err == nil {
		t.Fatal("a 503 returned no error")
	}
	if hits.Load() != 1 {
		t.Fatalf("sent %d times with retries off", hits.Load())
	}
}

func TestBulkRatesAreBounded(t *testing.T) {
	client, calls := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
		answer(w, 200, `{"status": 200, "data": {"individualRates": {"CX1": {"basePricing": [{"vendorCode": "DL", "totalAmount": 60}]}},
			"vendorTotalStrategy": {"DL": 135, "XB": 141.5}}}`)
	})
	if _, err := client.Rates.Bulk(context.Background(), make([]string, MaxBulk+1)); err == nil {
		t.Fatal("a batch over the limit was sent")
	}
	got, err := client.Rates.Bulk(context.Background(), []string{"CX1", "CX2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].path != "/api/v1/rates/bulk" {
		t.Fatalf("calls = %+v", *calls)
	}
	if got.Data.VendorTotalStrategy["XB"] != 141.5 || got.Data.IndividualRates["CX1"].BasePricing[0].VendorCode != "DL" {
		t.Fatalf("answer = %+v", got.Data)
	}
}

func TestTrackCancelAndNDRPaths(t *testing.T) {
	client, calls := fake(t, func(w http.ResponseWriter, r *http.Request, _ served) {
		if strings.HasSuffix(r.URL.Path, "/track") {
			answer(w, 200, `{"success": true, "awb": "CX221367", "currentStatus": "In Transit", "scans": []}`)
			return
		}
		answer(w, 200, `{"success": true, "message": "done"}`)
	})
	ctx := context.Background()

	track, err := client.Orders.Track(ctx, "CX221367")
	if err != nil {
		t.Fatal(err)
	}
	if track.CurrentStatus != "In Transit" || !strings.Contains(string(track.Raw), "scans") {
		t.Fatalf("Track = %+v", track)
	}
	if _, err := client.Orders.Cancel(ctx, "4477"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Orders.NDRAction(ctx, "4478"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"GET /api/v1/orders/CX221367/track",
		"POST /api/v1/orders/open/cancel/4477",
		"POST /api/v1/orders/open/ndr/4478",
	}
	for i, w := range want {
		if got := (*calls)[i].method + " " + (*calls)[i].path; got != w {
			t.Errorf("call %d = %q, want %q", i, got, w)
		}
	}
	if _, err := client.Orders.Cancel(ctx, " "); err == nil {
		t.Fatal("a cancellation with no AWB was sent")
	}
}

func TestServiceability(t *testing.T) {
	client, calls := fake(t, func(w http.ResponseWriter, _ *http.Request, _ served) {
		answer(w, 200, `{"success": true, "serviceable": true, "message": "Route is serviceable"}`)
	})
	got, err := client.Serviceability.Check(context.Background(), &ServiceabilityRequest{
		OriginPincode: 110001, DestinationPincode: 400001, PaymentMode: PaymentCOD, Weight: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Serviceable {
		t.Fatal("the route read as unserviceable")
	}
	body := (*calls)[0].body
	if body["originPincode"] != float64(110001) || body["paymentMode"] != "COD" {
		t.Fatalf("body = %v", body)
	}
	if _, present := body["length"]; present {
		t.Fatal("an unset dimension was sent as zero")
	}
}

func TestWebhookManagement(t *testing.T) {
	client, calls := fake(t, func(w http.ResponseWriter, r *http.Request, _ served) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/register"):
			answer(w, 200, `{"status": 200, "data": {"_id": "665f", "event": "ORDER_STATUS_UPDATE",
				"url": "https://shop.example/hook", "isActive": true, "failureCount": 0}}`)
		case r.Method == http.MethodGet:
			answer(w, 200, `{"status": 200, "data": [{"id": "665f", "url": "https://shop.example/hook",
				"isActive": true, "hasSecret": true, "lastTriggeredAt": "2026-09-19T08:15:00.000Z"}]}`)
		case r.Method == http.MethodPatch:
			answer(w, 200, `{"status": 200, "data": {"id": "665f", "isActive": false}}`)
		case strings.HasSuffix(r.URL.Path, "/test"):
			answer(w, 200, `{"status": 200, "message": "Test event could not be delivered",
				"data": {"delivered": false, "responseStatus": 500, "error": "Request failed with status code 500"}}`)
		default:
			answer(w, 200, `{"status": 200, "data": {"id": "665f"}}`)
		}
	})
	ctx := context.Background()

	if _, err := client.Webhooks.Register(ctx, &RegisterWebhookRequest{URL: "http://shop.example/hook"}); err == nil {
		t.Fatal("a plain-http webhook was registered")
	}
	hook, err := client.Webhooks.Register(ctx, &RegisterWebhookRequest{URL: "https://shop.example/hook", Secret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if hook.ID != "665f" {
		t.Fatalf("the id from _id was lost: %+v", hook)
	}
	if (*calls)[0].body["event"] != EventOrderStatusUpdate {
		t.Fatalf("register body = %v", (*calls)[0].body)
	}

	hooks, err := client.Webhooks.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 || !hooks[0].HasSecret || hooks[0].LastTriggeredAt == nil {
		t.Fatalf("List = %+v", hooks)
	}

	off := false
	if _, err := client.Webhooks.Update(ctx, "665f", &UpdateWebhookRequest{IsActive: &off}); err != nil {
		t.Fatal(err)
	}
	patch := (*calls)[2].body
	if patch["isActive"] != false || len(patch) != 1 {
		t.Fatalf("an update sent more than it changed: %v", patch)
	}

	result, err := client.Webhooks.Test(ctx, "665f")
	if err != nil {
		t.Fatal(err)
	}
	if result.Delivered || result.ResponseStatus != 500 {
		t.Fatalf("Test = %+v", result)
	}
	if err := client.Webhooks.Delete(ctx, "665f"); err != nil {
		t.Fatal(err)
	}
	if last := (*calls)[len(*calls)-1]; last.method != http.MethodDelete || last.path != "/api/v1/webhooks/665f" {
		t.Fatalf("delete sent %s %s", last.method, last.path)
	}
}

func TestVerifyingADelivery(t *testing.T) {
	body := []byte(`{"event":"ORDER_STATUS_UPDATE","time":1758240000000,"data":{"eventId":"665f",` +
		`"orderId":"CX123456","awb":"AWB123456789","vendor":"DL","status":"In Transit",` +
		`"statusLocation":"Delhi Hub","statusDateTime":"2026-09-19T08:15:00.000Z"}}`)
	signature := Sign("whsec", body)

	if !VerifySignature("whsec", body, signature) {
		t.Fatal("the right signature did not verify")
	}
	if !VerifySignature("whsec", body, strings.ToUpper(signature)) {
		t.Fatal("hex case decided the answer")
	}
	if VerifySignature("other", body, signature) {
		t.Fatal("the wrong secret verified")
	}
	if VerifySignature("whsec", append(body, ' '), signature) {
		t.Fatal("a changed body verified")
	}
	if VerifySignature("", body, Sign("", body)) {
		t.Fatal("an empty secret verified")
	}

	event, err := ParseEvent(body)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 19, 8, 15, 0, 0, time.UTC)
	if event.Data.EventID != "665f" || event.Data.AWB != "AWB123456789" || !event.Data.StatusDateTime.Equal(want) {
		t.Fatalf("event = %+v", event.Data)
	}
}

func TestACancelledContextIsNotRetried(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(200 * time.Millisecond)
		answer(w, 200, `{}`)
	}))
	t.Cleanup(srv.Close)
	client, _ := New(Config{APIKey: "k", BaseURL: srv.URL})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.Orders.Track(ctx, "CX1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("sent %d times after the context ended", hits.Load())
	}
}
