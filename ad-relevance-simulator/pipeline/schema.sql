-- Master ad/campaign registry
CREATE TABLE IF NOT EXISTS ads (
    ad_id       SERIAL PRIMARY KEY,
    campaign_id INT NOT NULL,
    ad_text     TEXT NOT NULL,
    bid_price   NUMERIC(10, 4) NOT NULL,  -- advertiser's max bid in $
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Raw impression event log — one row per ad shown to a user
CREATE TABLE IF NOT EXISTS impressions (
    impression_id   SERIAL PRIMARY KEY,
    ad_id           INT NOT NULL REFERENCES ads(ad_id),
    user_id         INT NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL,
    clicked         BOOLEAN NOT NULL,       -- ground truth label
    -- Features we'll use for logistic regression:
    time_of_day     INT NOT NULL,           -- hour 0-23
    user_segment    INT NOT NULL,           -- bucketed user type 0-4
    position        INT NOT NULL,           -- slot position (1=top)
    relevance_score NUMERIC(6, 4) NOT NULL  -- pre-auction relevance hint
);

-- Model output: computed CTR scores per ad (updated each pipeline run)
CREATE TABLE IF NOT EXISTS ctr_scores (
    ad_id           INT PRIMARY KEY REFERENCES ads(ad_id),
    predicted_ctr   NUMERIC(8, 6) NOT NULL,   -- logistic regression output
    weighted_score  NUMERIC(12, 6) NOT NULL,   -- CTR * bid_price (auction score)
    rank_position   INT NOT NULL,              -- final ranked position
    impression_count INT NOT NULL,
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Index for the Go server's hot query path
CREATE INDEX IF NOT EXISTS idx_ctr_scores_rank ON ctr_scores(rank_position ASC);
CREATE INDEX IF NOT EXISTS idx_impressions_ad_id ON impressions(ad_id);
CREATE INDEX IF NOT EXISTS idx_impressions_timestamp ON impressions(timestamp DESC);
