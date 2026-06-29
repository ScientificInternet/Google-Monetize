module github.com/ScientificInternet/Google-Monetize/pkg/http

go 1.25

require (
	github.com/ScientificInternet/Google-Monetize/pkg/httpclient v0.0.0-20251024001746-c36c54440544
	github.com/ScientificInternet/Google-Monetize/pkg/idempotency v0.0.0-00010101000000-000000000000
)

replace github.com/ScientificInternet/Google-Monetize/pkg/idempotency => ../idempotency
