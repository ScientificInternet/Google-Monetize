# Google-Monetize

AI-driven Google Ads management & automation toolkit. A microservice backend for
operating Google Ads campaigns at scale: account management, campaign execution,
site/keyword ranking signals, billing, and a Next.js frontend.

> Configuration is environment-driven. No project IDs, domains, or credentials are
> hardcoded — supply them via environment variables / your secret manager.

## Services

| Service | Responsibility |
|---|---|
| `adscenter` | Google Ads API integration (accounts, campaigns, execution) |
| `siterank` | Site / keyword ranking signals |
| `billing` | Usage metering and billing |
| `user` | User accounts and auth |
| `console` | Admin/console backend |
| `recommendations` | Optimization recommendations |
| `batchopen` | Batch operations |
| `bff` | Backend-for-frontend aggregation |
| `gateway-middleware` | Edge auth / routing middleware |
| `projector` | Event projections / read models |
| `useractivity` | Activity tracking |
| `apps/frontend` | Next.js web UI |

Shared Go libraries live under `pkg/` and are wired via the Go workspace (`go.work`).

## Stack

- Backend: Go (workspace of independent modules, one per service)
- Frontend: Next.js + TypeScript (pnpm / turbo monorepo)
- Data: PostgreSQL (Supabase-compatible), Redis
- Infra: Docker / Cloud Run; config via environment variables

## Configuration

```bash
cp .env.example .env
```

| Variable | Description |
|---|---|
| `GCP_PROJECT_ID` | Your Google Cloud project ID |
| `APP_DOMAIN` | Your application domain |
| `DATABASE_URL` | PostgreSQL connection string |
| `REDIS_URL` | Redis connection string |

## Build

Backend (requires Go 1.25.1+):

```bash
go work sync
go build ./...
```

Frontend:

```bash
pnpm install
pnpm build
```

## Notes

Provider credentials, project IDs, and domains are intentionally not committed —
provide them at deploy time. The repository ships service skeletons and shared
libraries; some peripheral integrations are left to be wired to your own
infrastructure and accounts.

## License

TBD.
