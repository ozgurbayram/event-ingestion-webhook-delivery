# Event Ingestion & Webhook Delivery

An internal service that ingests events from REST API and Kafka, matches them against registered webhooks, and reliably delivers payloads with HMAC-SHA256 signatures and exponential backoff retries.

## Architecture

```
                  ┌──────────┐
                  │ REST API │──────┐
                  └──────────┘      │
                                    ▼
                  ┌──────────┐   ┌─────────┐   ┌────────────┐   ┌────────────┐
                  │  Kafka   │──▶│ Ingestor│──▶│ PostgreSQL │◀──│   Poller   │
                  └──────────┘   └─────────┘   └────────────┘   └────────────┘
                                                                      │
                                                                      ▼
                                                                ┌────────────┐
                                                                │ Dispatcher │──▶ Webhook URLs
                                                                └────────────┘
```

### Flow

1. Events arrive via **REST** (`POST /events`) or **Kafka** consumer
2. **Ingestor** validates the event, checks idempotency, persists it, and creates delivery records for all matching webhooks
3. **Poller** fetches pending deliveries in batches using `SELECT ... FOR UPDATE SKIP LOCKED`
4. **Dispatcher** sends HTTP POST to each webhook URL with HMAC-SHA256 signature
5. Failed deliveries are retried with exponential backoff (1m → 5m → 15m → 1h → 4h)

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.23 |
| Database | PostgreSQL 16 |
| ORM | GORM |
| HTTP Router | Gorilla Mux |
| Message Broker | Apache Kafka (IBM Sarama) |
| Containerization | Docker / Docker Compose |

## Project Structure

```
├── cmd/
│   └── main.go                  # Entry point, wiring, graceful shutdown
├── internal/
│   ├── config/
│   │   └── config.go            # Environment-based configuration
│   ├── db/
│   │   └── postgres.go          # GORM connection & auto-migration
│   ├── entities/
│   │   ├── event.go             # Event model (JSONB payload, idempotency key)
│   │   ├── webhook.go           # Webhook model (event type filtering, wildcard)
│   │   └── delivery.go          # Delivery model (status machine, retry logic)
│   ├── handlers/
│   │   ├── rest.go              # REST API endpoints
│   │   └── kafka.go             # Kafka consumer group handler
│   ├── ingestor/
│   │   └── service.go           # Event ingestion + delivery fan-out
│   ├── repositories/
│   │   ├── event.go             # Event CRUD + duplicate detection
│   │   ├── webhook.go           # Webhook CRUD
│   │   └── delivery.go          # Delivery CRUD + SKIP LOCKED fetch
│   └── worker/
│       ├── dispatcher.go        # HTTP dispatch with signature headers
│       ├── poller.go            # Batch polling + retry scheduling
│       └── signer.go            # HMAC-SHA256 payload signing
├── tests/
│   ├── integration_test.go      # API integration tests
│   └── signer_test.go           # Signer unit tests
├── docker-compose.yml           # Full stack (app, postgres, kafka, zookeeper, echo server)
├── docker-compose.test.yml      # Test environment
├── Dockerfile                   # Multi-stage production build
├── Dockerfile.test              # Test runner image
├── Makefile                     # Build, run, test, docker commands
└── go.mod
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/events` | Ingest a new event |
| `POST` | `/webhooks` | Register a webhook |
| `GET` | `/webhooks/{id}` | Get webhook by ID |
| `PUT` | `/webhooks/{id}` | Update a webhook |
| `DELETE` | `/webhooks/{id}` | Delete a webhook |
| `GET` | `/health` | Health check |

### Ingest Event

```bash
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '{
    "application_id": "app-123",
    "event_type": "order.created",
    "idempotency_key": "evt-abc-001",
    "payload": {"order_id": "ord-456", "amount": 99.99}
  }'
```

### Register Webhook

```bash
curl -X POST http://localhost:8080/webhooks \
  -H "Content-Type: application/json" \
  -d '{
    "application_id": "app-123",
    "url": "https://example.com/webhook",
    "secret": "whsec_my_secret_key",
    "event_types": ["order.created", "order.updated"]
  }'
```

Use `"event_types": ["*"]` to subscribe to all event types.

## Database Schema

**events** — Ingested events with idempotency

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | Primary key |
| application_id | VARCHAR(255) | Composite unique with idempotency_key |
| event_type | VARCHAR(255) | Indexed |
| idempotency_key | VARCHAR(255) | Composite unique with application_id |
| payload | JSONB | Event data |
| source | VARCHAR(50) | `rest` or `kafka` |
| occurred_at | TIMESTAMP | When the event occurred |
| created_at | TIMESTAMP | Auto-generated |

**webhooks** — Registered webhook endpoints

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | Primary key |
| application_id | VARCHAR(255) | Composite index with active |
| url | VARCHAR(2048) | Target endpoint |
| secret | VARCHAR(255) | HMAC signing secret |
| event_types | TEXT[] | PostgreSQL array, supports `*` wildcard |
| active | BOOLEAN | Default true |
| created_at | TIMESTAMP | Auto-generated |
| updated_at | TIMESTAMP | Auto-generated |

**deliveries** — Webhook delivery attempts

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | Primary key |
| event_id | UUID | FK → events |
| webhook_id | UUID | FK → webhooks |
| status | VARCHAR(50) | `pending` → `in_progress` → `success` / `failed` / `exhausted` |
| attempt_count | INT | Default 0 |
| next_retry_at | TIMESTAMP | Indexed with status |
| last_error | TEXT | Last failure reason |
| last_attempt_at | TIMESTAMP | Nullable |
| created_at | TIMESTAMP | Auto-generated |

## Webhook Signature

Each delivery includes an `X-Webhook-Signature` header using HMAC-SHA256:

```
t=1708617600,v1=5257a869e7ecebeda32affa62cdca3fa51cad7e77a0e56ff536d0ce8e108d8f9
```

Verification: compute `HMAC-SHA256(secret, "{timestamp}.{raw_body}")` and compare against `v1`.

## Delivery & Retry

| Attempt | Delay |
|---------|-------|
| 1st retry | ~1 minute |
| 2nd retry | ~5 minutes |
| 3rd retry | ~15 minutes |
| 4th retry | ~1 hour |
| 5th retry | ~4 hours |

Each delay includes ±25% jitter. After exhausting all retries, status is set to `exhausted`.

Concurrency is handled via `SELECT ... FOR UPDATE SKIP LOCKED` to allow parallel pollers.

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `8080` | HTTP server port |
| `SERVER_READ_TIMEOUT` | `10s` | HTTP read timeout |
| `SERVER_WRITE_TIMEOUT` | `10s` | HTTP write timeout |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL user |
| `DB_PASSWORD` | `postgres` | PostgreSQL password |
| `DB_NAME` | `event_ingestion` | PostgreSQL database name |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `KAFKA_BROKERS` | `localhost:9092` | Kafka broker addresses |
| `KAFKA_TOPIC` | `events` | Kafka topic to consume |
| `KAFKA_GROUP_ID` | `event-ingestion` | Kafka consumer group ID |
| `WORKER_POLLING_INTERVAL` | `1s` | Delivery polling interval |
| `WORKER_BATCH_SIZE` | `10` | Deliveries per poll batch |
| `WORKER_MAX_RETRIES` | `5` | Max delivery attempts |
| `WORKER_REQUEST_TIMEOUT` | `10s` | HTTP dispatch timeout |

## Getting Started

### Prerequisites

- Go 1.23+
- Docker & Docker Compose

### Run with Docker

```bash
make docker-up
```

This starts the app, PostgreSQL, Kafka, Zookeeper, and an echo server (for testing) on:
- App: `http://localhost:8080`
- Echo server: `http://localhost:8081`

### Run Locally

```bash
make run
```

Requires PostgreSQL and Kafka running locally with default config.

### Run Tests

```bash
make test                # all tests
make test-unit           # unit tests only
make test-integration    # integration tests via Docker
```
