// main.go — HTTP JSON bridge for the React dashboard
//
// Why not serve the dashboard directly from gRPC?
//   Browsers can't speak raw gRPC (binary HTTP/2 framing). Solutions:
//     1. grpc-gateway (proto annotations → REST proxy) — production choice
//     2. grpc-web + Envoy proxy — another production pattern
//     3. Simple JSON HTTP handler — our choice for the demo (zero extra deps)
//   For an interview, mentioning all three shows you know the ecosystem.

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// AdStatJSON mirrors pb.AdStat for JSON serialization
type AdStatJSON struct {
	AdID           int     `json:"ad_id"`
	PredictedCTR   float64 `json:"predicted_ctr"`
	WeightedScore  float64 `json:"weighted_score"`
	RankPosition   int     `json:"rank_position"`
	ImpressionCount int    `json:"impression_count"`
}

type RankedAdJSON struct {
	AdID            int     `json:"ad_id"`
	PredictedCTR    float64 `json:"predicted_ctr"`
	WeightedScore   float64 `json:"weighted_score"`
	RankPosition    int     `json:"rank_position"`
	ImpressionCount int     `json:"impression_count"`
	FromCache       bool    `json:"from_cache"`
}

type RankResponse struct {
	Ads         []RankedAdJSON `json:"ads"`
	LatencyUs   int64          `json:"latency_us"`
	CacheStatus string         `json:"cache_status"`
}

func startHTTPServer(srv *AdRankingServer) {
	mux := http.NewServeMux()

	// CORS middleware — needed for React dev server (localhost:3000 → :8081)
	handler := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			h(w, r)
		}
	}

	// GET /stats — all ad scores for dashboard
	mux.HandleFunc("/stats", handler(func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil {
				limit = n
			}
		}

		ads, err := srv.fetchRankedAdsFromDB(0, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var stats []AdStatJSON
		for _, a := range ads {
			stats = append(stats, AdStatJSON{
				AdID:            int(a.AdId),
				PredictedCTR:    a.PredictedCtr,
				WeightedScore:   a.WeightedScore,
				RankPosition:    int(a.RankPosition),
				ImpressionCount: int(a.ImpressionCount),
			})
		}
		json.NewEncoder(w).Encode(stats)
	}))

	// GET /rank?top_n=10&campaign_id=0 — ranked ads with cache telemetry
	mux.HandleFunc("/rank", handler(func(w http.ResponseWriter, r *http.Request) {
		topN := 10
		campaignID := int32(0)

		if n, err := strconv.Atoi(r.URL.Query().Get("top_n")); err == nil {
			topN = n
		}
		if c, err := strconv.Atoi(r.URL.Query().Get("campaign_id")); err == nil {
			campaignID = int32(c)
		}

		start := time.Now()
		cacheKey := strconv.Itoa(int(campaignID)) + ":" + strconv.Itoa(topN)

		var jsonAds []RankedAdJSON
		cacheStatus := "MISS"

		if ads, hit := srv.cache.Get(cacheKey); hit {
			cacheStatus = "HIT"
			for _, a := range ads {
				jsonAds = append(jsonAds, RankedAdJSON{
					AdID:            int(a.AdId),
					PredictedCTR:    a.PredictedCtr,
					WeightedScore:   a.WeightedScore,
					RankPosition:    int(a.RankPosition),
					ImpressionCount: int(a.ImpressionCount),
					FromCache:       true,
				})
			}
		} else {
			ads, err := srv.fetchRankedAdsFromDB(campaignID, topN)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			srv.cache.Set(cacheKey, ads)
			for _, a := range ads {
				jsonAds = append(jsonAds, RankedAdJSON{
					AdID:            int(a.AdId),
					PredictedCTR:    a.PredictedCtr,
					WeightedScore:   a.WeightedScore,
					RankPosition:    int(a.RankPosition),
					ImpressionCount: int(a.ImpressionCount),
					FromCache:       false,
				})
			}
		}

		json.NewEncoder(w).Encode(RankResponse{
			Ads:         jsonAds,
			LatencyUs:   time.Since(start).Microseconds(),
			CacheStatus: cacheStatus,
		})
	}))

	// GET /health
	mux.HandleFunc("/health", handler(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))

	log.Println("HTTP JSON server listening on :8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}
