module github.com/ScientificInternet/Google-Monetize/services/siterank

go 1.25.1

replace github.com/ScientificInternet/Google-Monetize/pkg/httpclient => ../../../pkg/httpclient
replace github.com/ScientificInternet/Google-Monetize/pkg/http => ../../../pkg/http

replace github.com/ScientificInternet/Google-Monetize/pkg/errors => ../../../pkg/errors

replace github.com/ScientificInternet/Google-Monetize/pkg/auth => ../../../pkg/auth

replace github.com/ScientificInternet/Google-Monetize/pkg/logger => ../../../pkg/logger

replace github.com/ScientificInternet/Google-Monetize/pkg/events => ../../../pkg/events

replace github.com/ScientificInternet/Google-Monetize/pkg/telemetry => ../../../pkg/telemetry

replace github.com/ScientificInternet/Google-Monetize/pkg/middleware => ../../../pkg/middleware
replace github.com/ScientificInternet/Google-Monetize/pkg/eventstore => ../../../pkg/eventstore
replace github.com/ScientificInternet/Google-Monetize/pkg/idempotency => ../../../pkg/idempotency
replace github.com/ScientificInternet/Google-Monetize/pkg/cache => ../../../pkg/cache
replace github.com/ScientificInternet/Google-Monetize/pkg/dburl => ../../../pkg/dburl
replace github.com/ScientificInternet/Google-Monetize/pkg/supabaseauth => ../../../pkg/supabaseauth

	require (
	cloud.google.com/go/pubsub v1.50.1
	cloud.google.com/go/secretmanager v1.15.0
	github.com/go-chi/chi/v5 v5.2.3
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.10.9
	github.com/oapi-codegen/runtime v1.1.2
	github.com/redis/go-redis/v9 v9.7.0
	github.com/ScientificInternet/Google-Monetize/pkg/auth v0.0.1
	github.com/ScientificInternet/Google-Monetize/pkg/config v0.0.0-20250921095352-ef8078c06b83
	github.com/ScientificInternet/Google-Monetize/pkg/errors v0.0.1
	github.com/ScientificInternet/Google-Monetize/pkg/events v0.0.0-00010101000000-000000000000
	github.com/ScientificInternet/Google-Monetize/pkg/httpclient v0.0.1
	github.com/ScientificInternet/Google-Monetize/pkg/http v0.0.0-00010101000000-000000000000
	github.com/ScientificInternet/Google-Monetize/pkg/logger v0.0.1
	github.com/ScientificInternet/Google-Monetize/pkg/middleware v0.0.0-20250921095352-ef8078c06b83
	github.com/ScientificInternet/Google-Monetize/pkg/telemetry v0.0.0-00010101000000-000000000000
	github.com/ScientificInternet/Google-Monetize/pkg/eventstore v0.0.0-00010101000000-000000000000
)

require (
	cloud.google.com/go v0.121.6 // indirect
	cloud.google.com/go/auth v0.16.4 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.8.0 // indirect
	cloud.google.com/go/iam v1.5.2 // indirect
	cloud.google.com/go/longrunning v0.6.7 // indirect
	cloud.google.com/go/pubsub/v2 v2.0.0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.6 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/prometheus/client_golang v1.19.1 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.48.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/ScientificInternet/Google-Monetize/pkg/idempotency v0.0.0-20250925113750-f38a0734af10 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.61.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.61.0 // indirect
	go.opentelemetry.io/otel v1.36.0 // indirect
	go.opentelemetry.io/otel/metric v1.36.0 // indirect
	go.opentelemetry.io/otel/trace v1.36.0 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	google.golang.org/api v0.247.0 // indirect
	google.golang.org/genproto v0.0.0-20250603155806-513f23925822 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250818200422-3122310a409c // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250811230008-5f3141c8851a // indirect
	google.golang.org/grpc v1.74.2 // indirect
	google.golang.org/protobuf v1.36.7 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
