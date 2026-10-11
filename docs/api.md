# The Blitzshipz Open API, as documented

This was transcribed on 2026-10-11 from
https://www.app.blitzshipz.com/open-api/documentation/. That page is a
single-page app and its content lives in the app's script, so this copy keeps
it readable and diffable. The last section lists where the documentation
contradicts itself, and what this SDK does about each case.

- **Base URL:** `https://open.blitzshipz.com/api/v1`
- **Authentication:** an `x-api-key: <key>` header on every request.

## Orders

| SDK | Request | Body | Answer (as documented) |
|---|---|---|---|
| `Orders.Create` | `POST /orders` | the order (below) | `{"success": true, "awb": "ATPRE137180", "message": "Order created successfully"}` |
| `Orders.Cancel` | `POST /orders/open/cancel/{AWB}` | none | `{"success": true, "message": "Order cancelled successfully"}` |
| `Orders.NDRAction` | `POST /orders/open/ndr/{AWB}` | none | `{"success": true, "message": "NDR action submitted"}` |
| `Orders.Track` | `GET /orders/{trackingNumber}/track` | none | `{"success": true, "awb": "CX221367", "currentStatus": "In Transit"}` |
| `Serviceability.Check` | `POST /serviceability` | `originPincode`, `destinationPincode` (numbers, required); `paymentMode`, `weight` (kg), `declaredAmount`, `length`, `breadth`, `height` (cm) | `{"success": true, "serviceable": true, "message": "Route is serviceable"}` |

### The Create Order body

Required fields:
- `warehouseName`, `sellerName`, `sellerAddress`
- `dropName`, `dropAddress`, `dropCity`, `dropState`, `dropMobile`, `dropPincode` (a number)
- `totalAmount`
- `paymentMode` ("COD" or "Prepaid")
- `items`
- `l`, `b`, `h` (cm)
- `actualWeight` (kg)

Optional fields:
- `dropEmail`, `dropAlternative_mobile`, `dropLandmark`, `referenceNumber`
- `totalTaxes`, `fragile`, `volumentricWeight`, `discountAmount`, `shippingCost`

Each item has `sku`, `name`, `itemName`, `quantity`, `unitPrice`, `taxAmount`, `length`, `breadth`, `height` and `weight`.

The misspellings `volumentricWeight` and `dropAlternative_mobile` are the API's own, and the SDK sends them as written.

## Rates and shipping

| SDK | Request | Body | Answer |
|---|---|---|---|
| `Rates.Get` | `POST /rates` | `{"orderId": "CX123456"}` | `data.rates.basePricing[]` / `advancePricing[]` of `{vendorCode, vendorName, totalAmount}` |
| `Rates.Bulk` | `POST /rates/bulk` | `{"orderIds": [...]}`, 1 to 50 | `data.individualRates{orderId: {basePricing}}`, `data.vendorTotalStrategy{vendorCode: total}` |
| `Orders.GenerateAWB` | `POST /orders/{orderId}/generate-awb` | `{"vendorCode", "weight" (kg, > 0), "amount" (> 0)}` | `data.awbs{orderId: awb}`, `data.failed[{orderId, reason}]`, `data.summary` |
| `Orders.GenerateAWBBulk` | `POST /orders/generate-awb/bulk` | `{"vendorCode", "orders": [{orderId, weight, amount}], "strategyType"?}` | same as above |

- **Rates:** "Nothing is shipped and no balance is spent."
- **Generate AWB:** "This is the call that makes a shipment live and spends wallet balance. An order that is already live is rejected rather than shipped twice."
- **Bulk:** "Best-effort by design … never re-send the whole batch on a partial failure." `strategyType` left out means `SINGLE_VENDOR`. `OPTIMIZED_MIX` returns 400.

## Webhooks

| SDK | Request | Body |
|---|---|---|
| `Webhooks.Register` | `POST /webhooks/register` | `{"event": "ORDER_STATUS_UPDATE", "url": "https://…", "secret"?}` |
| `Webhooks.List` | `GET /webhooks` | none |
| `Webhooks.Update` | `PATCH /webhooks/{id}` | any of `isActive`, `url`, `secret` (`""` removes the secret) |
| `Webhooks.Test` | `POST /webhooks/{id}/test` | none. Always answers 200; read `data.delivered` |
| `Webhooks.Delete` | `DELETE /webhooks/{id}` | none |

### A delivery

```json
{
  "event": "ORDER_STATUS_UPDATE",
  "time": 1758240000000,
  "data": {
    "eventId": "665f1c2e9a4b7d0012ab34cd",
    "orderId": "CX123456",
    "awb": "AWB123456789",
    "vendor": "DL",
    "status": "In Transit",
    "statusLocation": "Delhi Hub",
    "statusDateTime": "2026-09-19T08:15:00.000Z"
  }
}
```

Headers:
- `X-Webhook-Event: ORDER_STATUS_UPDATE`
- `X-Webhook-Signature`: the hex HMAC-SHA256 of the raw body, keyed by the secret
- `X-Webhook-Test: true`: present on test events only

What the endpoint has to do:
- Answer 2xx within 5 seconds.
- Deduplicate on `eventId`.
- Expect events out of order, so compare `statusDateTime`.
- Drop test events.

After 5 consecutive failures the webhook is switched off. `isActive: true` turns it back on and clears the count.

## Where the documentation is unclear

1. **Two answer dialects.** The order endpoints answer `"success": true`, while rates, AWB and webhooks answer `"status": 200`. The SDK reads both. A 2xx whose body says `success: false`, `status >= 400`, or a `status` word such as `"error"` or `"failed"` is treated as a refusal.
2. **What names an order.** Create Order's example answer carries `awb: "ATPRE137180"`. Get Rates asks for "the order reference returned by Create Order (CX…/CD…)". The getting-started example tracks `ATPRE123456`. `CreateOrderResponse.Reference()` picks `orderId`, then `data.orderId`, then any `CX…`/`CD…` token anywhere in the answer, even inside a sentence (one under a key mentioning "order" first), then `awb`. `data` may be any shape without failing the decode.

   **The live answer (2026-10-11)** names the order only inside a sentence, nests its own verdict under an outer "synced" 200, and carries no AWB. The AWB comes from Generate AWB.

   ```json
   {"status": 200, "message": "Order created and synced successfully",
    "data": {"statusCode": 200, "message": "Order created successfully: CD465228", "additionInformation": null}}
   ```

   The dashboard lists `CD465228` as the ORDERID. A `data.statusCode` of 400 or more under that outer 200 is treated as a refusal, with `data.message` as the reason.
3. **Cancel and NDR.** The path placeholder is `{orderId}`, but the documented parameter is the AWB, and the examples use a 14-digit number. The SDK names the parameter `awb`.
4. **The "What this API does" text** on Cancel, NDR and Serviceability is Create Order's, copied over. It describes nothing about those endpoints.
5. **Example JSON** for the bulk answers and List Webhooks is missing closing braces. The shapes above are read from the parts that are present.
