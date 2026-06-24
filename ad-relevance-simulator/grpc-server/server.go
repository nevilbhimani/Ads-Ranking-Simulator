// server.go — Ad Ranking gRPC Server
//
// Architecture:
//   Client → gRPC → AdRankingServer → Cache (5s TTL) → PostgreSQL
//
// The cache layer is the key latency design:
//   - Without cache: each RPC hits PostgreSQL → ~5-15ms per query
//   - With cache: in-memory read → ~0.05ms per query
//   - Tradeoff: scores are stale for up to 5 seconds after the Python
//     pipeline updates them. For an ad ranker, 5s staleness is acceptable
//     because CTR models are typically retrained every minutes-to-hours,
//     not sub-second. We'd only need shorter TTL if bid prices changed
//     real-time (which we'd handle differently, e.g., a separate bid stream).

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	_ "github.com/lib/pq"
	pb "adranker/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// ─── Cache ──────────────────────────────────────────────────────────────────

// CacheEntry holds a snapshot of ranked ads and a TTL expiry timestamp.
type CacheEntry struct {
	ads       []*pb.RankedAd
	expiresAt time.Time
}

// AdCache is a simple in-memory cache with a mutex for concurrent safety.
// WHY sync.RWMutex and not sync.Mutex?
//   RWMutex allows multiple concurrent readers (RLock) as long as there's
//   no writer. Since 500 RPS means many concurrent goroutines reading the
//   same cache entry, RWMutex is dramatically better than a full Mutex
//   (which would serialize all readers). Writers (cache refresh) are rare.
type AdCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry // key: "campaign_id:top_n"
	ttl     time.Duration
}

func NewAdCache(ttl time.Duration) *AdCache {
	return &AdCache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
	}
}

func (c *AdCache) Get(key string) ([]*pb.RankedAd, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false // cache miss or expired
	}
	return entry.ads, true
}

func (c *AdCache) Set(key string, ads []*pb.RankedAd) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &CacheEntry{
		ads:       ads,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// ─── gRPC Server ────────────────────────────────────────────────────────────

type AdRankingServer struct {
	pb.UnimplementedAdRankingServiceServer
	db    *sql.DB
	cache *AdCache
}

func NewAdRankingServer(db *sql.DB) *AdRankingServer {
	return &AdRankingServer{
		db:    db,
		cache: NewAdCache(5 * time.Second), // 5s TTL — the resume claim
	}
}

// GetRankedAds is the hot-path RPC. Cache-first, then DB fallback.
func (s *AdRankingServer) GetRankedAds(
	ctx context.Context,
	req *pb.RankAdsRequest,
) (*pb.RankAdsResponse, error) {

	start := time.Now()
	topN := int(req.TopN)
	if topN <= 0 {
		topN = 10
	}

	cacheKey := fmt.Sprintf("%d:%d", req.CampaignId, topN)

	// ── Cache lookup ──
	if ads, hit := s.cache.Get(cacheKey); hit {
		latency := time.Since(start).Microseconds()
		return &pb.RankAdsResponse{
			Ads:         ads,
			LatencyUs:   latency,
			CacheStatus: "HIT",
		}, nil
	}

	// ── Cache miss — query PostgreSQL ──
	ads, err := s.fetchRankedAdsFromDB(req.CampaignId, topN)
	if err != nil {
		return nil, fmt.Errorf("db fetch failed: %w", err)
	}

	s.cache.Set(cacheKey, ads)

	latency := time.Since(start).Microseconds()
	return &pb.RankAdsResponse{
		Ads:         ads,
		LatencyUs:   latency,
		CacheStatus: "MISS",
	}, nil
}

func (s *AdRankingServer) fetchRankedAdsFromDB(campaignID int32, topN int) ([]*pb.RankedAd, error) {
	// WHY LEFT JOIN with ads?
	//   We need impression_count from ctr_scores, but want to verify the ad
	//   exists. In production you'd join more tables for bid floors, targeting,
	//   budget caps, etc.
	var query string
	var rows *sql.Rows
	var err error

	if campaignID > 0 {
		query = `
			SELECT cs.ad_id, cs.predicted_ctr, cs.weighted_score,
			       cs.rank_position, cs.impression_count
			FROM ctr_scores cs
			JOIN ads a ON cs.ad_id = a.ad_id
			WHERE a.campaign_id = $1
			ORDER BY cs.rank_position ASC
			LIMIT $2
		`
		rows, err = s.db.Query(query, campaignID, topN)
	} else {
		query = `
			SELECT ad_id, predicted_ctr, weighted_score, rank_position, impression_count
			FROM ctr_scores
			ORDER BY rank_position ASC
			LIMIT $1
		`
		rows, err = s.db.Query(query, topN)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ads []*pb.RankedAd
	for rows.Next() {
		ad := &pb.RankedAd{}
		err := rows.Scan(
			&ad.AdId, &ad.PredictedCtr, &ad.WeightedScore,
			&ad.RankPosition, &ad.ImpressionCount,
		)
		if err != nil {
			return nil, err
		}
		ads = append(ads, ad)
	}
	return ads, rows.Err()
}

// GetAdStats serves the React dashboard — returns all ad stats.
func (s *AdRankingServer) GetAdStats(
	ctx context.Context,
	req *pb.StatsRequest,
) (*pb.StatsResponse, error) {

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT ad_id, predicted_ctr, weighted_score, rank_position, impression_count
		FROM ctr_scores
		ORDER BY rank_position ASC
		LIMIT $1
	`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*pb.AdStat
	for rows.Next() {
		s := &pb.AdStat{}
		if err := rows.Scan(&s.AdId, &s.PredictedCtr, &s.WeightedScore,
			&s.RankPosition, &s.ImpressionCount); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return &pb.StatsResponse{Stats: stats}, rows.Err()
}

// ─── HTTP shim for dashboard (avoids needing a gRPC-Web proxy) ──────────────
// In production you'd use grpc-gateway or Envoy. For the dashboard demo,
// we expose a plain JSON HTTP endpoint on :8081 alongside gRPC on :50051.

// ─── Main ───────────────────────────────────────────────────────────────────

func main() {
	// Database connection
	dsn := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_NAME", "addb"),
		getEnv("DB_USER", "aduser"),
		getEnv("DB_PASSWORD", "adpass"),
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Connection pool config
	// WHY these numbers?
	//   MaxOpenConns=10: prevents overwhelming PostgreSQL (which has a default
	//     max_connections of 100). At 500 RPS with 5s cache TTL, most requests
	//     hit cache — only ~1-2 RPS actually touch the DB.
	//   MaxIdleConns=5: keeps idle connections warm so cache-miss requests
	//     don't pay TCP handshake cost.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("cannot reach PostgreSQL: %v", err)
	}
	log.Println("Connected to PostgreSQL")

	// Start gRPC server
	grpcAddr := ":50052"
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := NewAdRankingServer(db)

	// WHY grpc.MaxConcurrentStreams?
	//   Limits how many goroutines the server spawns concurrently. Without this,
	//   a traffic spike could create thousands of goroutines and OOM the server.
	grpcSrv := grpc.NewServer(
		grpc.MaxConcurrentStreams(500),
	)
	pb.RegisterAdRankingServiceServer(grpcSrv, srv)
	reflection.Register(grpcSrv) // enables grpcurl for debugging

	// Start HTTP JSON shim for dashboard on :8081
	go startHTTPServer(srv)

	log.Printf("gRPC server listening on %s", grpcAddr)
	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
