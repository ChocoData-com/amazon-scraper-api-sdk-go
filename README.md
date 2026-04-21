# amazon-scraper-api-sdk-go

[![Go Reference](https://pkg.go.dev/badge/github.com/ChocoData-com/amazon-scraper-api-sdk-go.svg)](https://pkg.go.dev/github.com/ChocoData-com/amazon-scraper-api-sdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/ChocoData-com/amazon-scraper-api-sdk-go)](https://goreportcard.com/report/github.com/ChocoData-com/amazon-scraper-api-sdk-go)
[![license](https://img.shields.io/github/license/ChocoData-com/amazon-scraper-api-sdk-go)](./LICENSE)

Official Go client for **[Amazon Scraper API](https://www.amazonscraperapi.com/)**. Flat-priced at $0.50 per 1,000 successful requests, no credits system, pay only for 2xx responses. Idiomatic Go types, context-aware cancellation, works with the standard `net/http` client under the hood.

## Benchmark (live production, 2026-04)

Measured on our own infrastructure against a 30-query mixed international set:

| Metric | Value |
|---|---|
| Median latency (product, US) | **~2.6 s** |
| P95 latency | **~6 s** |
| P99 latency | ~10.5 s |
| Price / 1,000 Amazon products | **$0.50** flat |
| Concurrent threads (entry paid plan) | **50** |
| Marketplaces supported | **20+** |
| Billing unit | per successful (2xx) response |

---

## Install

```bash
go get github.com/ChocoData-com/amazon-scraper-api-sdk-go
```

Requires Go >= 1.21.

## Quick start - single product

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    amz "github.com/ChocoData-com/amazon-scraper-api-sdk-go"
)

func main() {
    client := amz.New(os.Getenv("ASA_API_KEY"))
    ctx := context.Background()

    product, err := client.Product(ctx, amz.ProductParams{
        Query:  "B09HN3Q81F",
        Domain: "com",
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(product["title"])
    // Apple AirPods Pro (2nd Generation)...
    fmt.Println(product["price"])
    // map[current:199 currency:USD was:249]
}
```

### Example output

```go
map[string]any{
    "asin":         "B09HN3Q81F",
    "title":        "Apple AirPods Pro (2nd Generation)...",
    "brand":        "Apple",
    "price":        map[string]any{"current": 199.0, "currency": "USD", "was": 249.0},
    "rating":       map[string]any{"average": 4.7, "count": 58214},
    "availability": "In Stock",
    "buybox":       map[string]any{"seller": "Amazon.com", "prime": true},
    "images":       []any{"https://m.media-amazon.com/images/I/...jpg"},
    "bullets":      []any{"Active Noise Cancellation...", "Adaptive Audio..."},
    "variants":     []any{},
}
```

Access fields by casting, or use `encoding/json` with a typed struct if you prefer strong typing.

## Keyword search

```go
results, err := client.Search(ctx, amz.SearchParams{
    Query:  "wireless headphones",
    Domain: "co.uk",
    SortBy: "avg_customer_review",
    Pages:  1,
})
if err != nil {
    log.Fatal(err)
}
for _, r := range results.Results {
    fmt.Printf("%d. %s - %v\n", r.Position, r.Title, r.Price)
}
```

## Async batch (up to 1,000 ASINs with webhook callback)

```go
batch, err := client.CreateBatch(ctx, amz.BatchCreateParams{
    Endpoint: "amazon.product",
    Items: []map[string]any{
        {"query": "B09HN3Q81F", "domain": "com"},
        {"query": "B000ALVUM6", "domain": "de", "language": "de_DE"},
    },
    WebhookURL: "https://your.server/webhooks/asa",
})
if err != nil {
    log.Fatal(err)
}
fmt.Println("batch id:", batch.ID)
// SAVE THIS. Returned only once.
fmt.Println("webhook secret:", batch.WebhookSignatureSecret)

// Alternative: poll
status, _ := client.GetBatch(ctx, batch.ID)
fmt.Printf("%d / %d processed\n", status.ProcessedCount, status.TotalCount)
```

## Verifying webhook signatures

Webhooks carry `X-ASA-Signature: sha256=<hmac-hex>` over the raw body:

```go
func asaWebhookHandler(w http.ResponseWriter, r *http.Request) {
    raw, _ := io.ReadAll(r.Body)
    sig := r.Header.Get("X-ASA-Signature")
    if !amz.VerifyWebhookSignature(sig, raw, os.Getenv("WEBHOOK_SECRET")) {
        http.Error(w, "invalid signature", http.StatusUnauthorized)
        return
    }
    var payload amz.BatchPayload
    json.Unmarshal(raw, &payload)
    // process payload.Results
    w.WriteHeader(http.StatusOK)
}
```

## What the API solves for you

Building a production-grade Amazon scraper in-house is a 2-4 week engineering project plus permanent maintenance. This SDK wraps [Amazon Scraper API](https://www.amazonscraperapi.com/), which has already solved:

| Pain point | What we handle |
|---|---|
| **Amazon CAPTCHAs / robot pages** | Auto-retried through a heavier proxy tier (datacenter, residential, premium) |
| **Brittle CSS selectors** | Extractors update as Amazon changes layouts; your code doesn't |
| **20+ marketplaces** | `com`, `co.uk`, `de`, `co.jp`, `com.br`, and more. Marketplace-specific parsing quirks handled. |
| **Country-matched residential IPs** | Auto-routed by TLD; override with `Country: "DE"` |
| **Rotating proxies + anti-fingerprinting** | Handled server-side |
| **Rate-limit retries** | Transparent |
| **Structured JSON output** | Title, price, rating, reviews, variants, seller, images. Parsed. |
| **Batch/async jobs** | 1,000 ASINs submitted, webhook-delivered on completion |

**Time saved:** a greenfield Go Amazon scraper built to this spec takes roughly 80 engineer-hours. This client is 5 minutes.

## Error handling

```go
product, err := client.Product(ctx, amz.ProductParams{Query: "INVALID_ASIN"})
if err != nil {
    var ae *amz.APIError
    if errors.As(err, &ae) {
        switch ae.Code {
        case "INSUFFICIENT_CREDITS":
            // top up
        case "RATE_LIMITED":
            time.Sleep(ae.RetryAfter)
        default:
            return err
        }
    }
}
```

| HTTP | Code | When | Action |
|---|---|---|---|
| 400 | `INVALID_PARAMS` | Bad `query`/`domain`/`sort_by` | Fix request |
| 401 | `INVALID_API_KEY` | Missing or revoked key | Rotate key |
| 402 | `INSUFFICIENT_CREDITS` | Balance empty | Top up |
| 429 | `RATE_LIMITED` | Over 120 req/60s | Honor `Retry-After` |
| 429 | `CONCURRENCY_LIMIT` | Over plan cap | Drop parallelism |
| 502 | `target_unreachable` | Amazon / all tiers blocked | Retry 30s later. Not charged |
| 502 | `amazon-robot-or-human` | Challenge unresolvable | Retry. Not charged |
| 502 | `extraction_failed` | Unknown layout | Report `X-Request-Id`. Not charged |
| 503 | `SERVICE_OVERLOADED` | Global circuit breaker | Honor `Retry-After: 60` |
| 500 | `INTERNAL_ERROR` | Our bug | Report `X-Request-Id` |

**Flat-credit promise:** non-2xx responses are free. `X-Request-Id` on every response.

## Get an API key

[app.amazonscraperapi.com](https://app.amazonscraperapi.com). **1,000 free requests on signup, no credit card required.**

## Links

- **Website:** https://www.amazonscraperapi.com/
- **Docs:** https://amazonscraperapi.com/docs
- **Status:** https://amazonscraperapi.com/status
- **Pricing:** https://amazonscraperapi.com/pricing
- **Node SDK:** [amazon-scraper-api-sdk](https://www.npmjs.com/package/amazon-scraper-api-sdk) · **Python SDK:** [amazonscraperapi-sdk](https://pypi.org/project/amazonscraperapi-sdk/) · **CLI:** [amazon-scraper-api-cli](https://www.npmjs.com/package/amazon-scraper-api-cli) · **MCP server:** [amazon-scraper-api-mcp](https://www.npmjs.com/package/amazon-scraper-api-mcp)

## License

MIT
