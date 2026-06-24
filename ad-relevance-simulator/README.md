# Ad Relevance Ranking Simulator

A full-stack ad auction ranking system that simulates how platforms like Google and Meta decide which ads to show. Built end-to-end: a Python ML pipeline, a Go gRPC serving layer, and a React dashboard.

## What it does

When a user loads a page, an ad auction runs in milliseconds. This system simulates that pipeline:

1. Ad impression logs are ingested into PostgreSQL
2. A logistic regression model predicts click-through rate (CTR) per ad
3. Ads are ranked by `predicted_CTR × bid_price` — the auction score
4. A Go gRPC server serves ranked results with a 5s TTL cache
5. A React dashboard visualizes CTR trends, impression volume, and rank shifts in real time

## Architecture

```
[Python Pipeline]
  generate_logs.py  →  Synthetic impression logs (10k events)
  ingest.py         →  PostgreSQL batch ingestion
  model.py          →  Logistic regression CTR model + auction ranking
        ↓
[PostgreSQL]
  impressions       →  Raw event log
  ctr_scores        →  Model output (predicted CTR, weighted score, rank)
        ↓
[Go Server :50052]
  gRPC service      →  Ranked results with 5s in-memory TTL cache
  HTTP shim :8081   →  JSON endpoints for React dashboard
        ↓
[React Dashboard :3000]
  CTR trends · Impression volume · Ranking position shifts
```

## Tech Stack

- **Python** — data generation, PostgreSQL ingestion, scikit-learn logistic regression
- **Go** — gRPC + HTTP JSON server, `sync.RWMutex` cache
- **PostgreSQL 15** — impression log storage, model score persistence
- **React + Recharts** — live polling dashboard
- **Protocol Buffers / gRPC** — typed service contract, binary serialization
- **Docker** — PostgreSQL via docker-compose

## Running Locally

### Prerequisites

- Docker Desktop
- Python 3.10+
- Go 1.22+
- Node.js 18+
- `protoc` + Go proto plugins (`brew install protobuf`)

### 1. Start PostgreSQL

```bash
docker-compose up -d
```

### 2. Python pipeline

```bash
cd pipeline
python3 -m venv venv && source venv/bin/activate
pip install -r requirements.txt
python3 ingest.py
python3 model.py
```

### 3. Go server

```bash
cd grpc-server

# First time only — generate proto files
brew install protobuf
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
export PATH="$PATH:$(go env GOPATH)/bin"
protoc --go_out=. --go_opt=module=adranker \
       --go-grpc_out=. --go-grpc_opt=module=adranker \
       proto/ads.proto

go mod tidy
go build -o adranker . && ./adranker
```

Test it:

```bash
curl "http://localhost:8081/rank?top_n=5&campaign_id=0" | python3 -m json.tool
```

### 4. Load benchmark

```bash
cd grpc-server
go run benchmark.go --rps=500 --duration=30s --addr=localhost:8081
```

### 5. React dashboard

```bash
cd dashboard
npm install && npm start
# Opens http://localhost:3000
```

## Benchmark Results

```
Total requests:    14,574
Errors:            0 (0.00%)
Actual RPS:        485.8

Latency:
  P50:   0.31ms
  P90:   0.63ms
  P95:   0.94ms
  P99:   3.38ms
  P99.9: 20.33ms
  Max:   51.24ms
```

P50 of 0.31ms reflects cache hits (in-memory reads). P99 of 3.38ms reflects the occasional cache miss hitting PostgreSQL. The gap between them is the cache tradeoff made visible in numbers.

## Model Output

```
Overall CTR:  10.57%
AUC-ROC:      0.6603
Log Loss:     0.6579

Feature coefficients:
  position        : -0.4194  (position bias — higher slot = fewer clicks)
  relevance_score : +0.3187  (semantic match drives CTR)
  user_segment    : +0.2520  (high-intent segments click more)
  time_of_day     : -0.0056
  bid_price       : -0.0053
```

Top ranked ad (ad_id=18) has a lower CTR than ad_id=13 but wins because its bid price produces a higher weighted auction score — the auction mechanic working as intended.

## Design Decisions

**Why logistic regression for CTR?**
CTR prediction is binary classification. Logistic regression outputs a calibrated probability — P(click | features) — which is essential because we multiply it by bid price. Calibration means if the model predicts 10%, roughly 10% of those impressions result in clicks. It's also fast to serve and easy to inspect via coefficients.

**Why CTR × bid_price for ranking?**
Ranking by CTR alone lets high-quality-low-bid ads dominate. Ranking by bid alone lets spammy high-bidding ads dominate. The product balances both — an ad has to be clickable and backed by real budget to rank well. This mirrors how Google's VCG auction works.

**Why a 5s TTL cache?**
Without cache, every one of ~500 requests/second would hit PostgreSQL — unsustainable. With cache, only ~1-2 requests/second touch the DB. Score staleness up to 5 seconds is acceptable because the CTR model retrains on a minutes-level cadence anyway.

**Why `sync.RWMutex`?**
At high RPS, many goroutines read the same cache entry concurrently. RWMutex allows unlimited concurrent readers as long as there's no active writer. A plain Mutex would serialize all readers — a meaningful throughput bottleneck at 500 RPS.

**What would change in production?**
- Position-debiased training labels (Inverse Propensity Scoring)
- GBDT + LR for feature interactions
- Redis for distributed caching with pub/sub invalidation
- Real-time bid stream instead of DB reads for bid prices
- grpc-gateway + Envoy instead of the custom HTTP shim

## Project Structure

```
ad-relevance-simulator/
├── docker-compose.yml
├── pipeline/
│   ├── generate_logs.py    # Synthetic impression log generator
│   ├── ingest.py           # PostgreSQL batch ingestion
│   ├── model.py            # Logistic regression CTR model + ranker
│   └── schema.sql          # DB schema (ads, impressions, ctr_scores)
├── grpc-server/
│   ├── server.go           # gRPC service + RWMutex cache
│   ├── main.go             # HTTP JSON shim for dashboard
│   ├── benchmark.go        # Load benchmarking tool
│   └── proto/ads.proto     # gRPC service definition
└── dashboard/
    └── src/App.jsx         # React dashboard (Recharts)
```
