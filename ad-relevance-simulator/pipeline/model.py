"""
model.py — Logistic Regression CTR Scoring + Feature-Weighted Auction Ranking

This module does three things:
  1. Pulls impression data from PostgreSQL
  2. Trains a logistic regression model to predict click probability (CTR)
  3. Computes a weighted auction score = predicted_CTR * bid_price
  4. Ranks ads by that score and writes results to ctr_scores table

WHY LOGISTIC REGRESSION for CTR prediction?
  - CTR prediction is a binary classification problem: clicked (1) or not (0).
  - Logistic regression outputs a calibrated probability — P(click | features).
    "Calibrated" means if the model says 10%, roughly 10% of those cases
    actually click. This matters in auctions where you're multiplying by bid.
  - Industry history: Facebook's 2014 GBDT+LR paper showed LR with good
    features beats complex models for CTR. SimpleML is often better than
    complex ML in production for this task because it's fast and interpretable.

WHY FEATURE-WEIGHTED RANKING (not just CTR rank)?
  - Ad auctions don't rank by CTR alone — they rank by "expected revenue":
      auction_score = predicted_CTR * quality_score (often ≈ bid_price)
  - This is Google's VCG-style auction mechanic in simplified form.
  - An ad with CTR=0.20 and bid=$0.50 scores 0.10
  - An ad with CTR=0.08 and bid=$4.00 scores 0.32 → ranks higher
  - This prevents high-bidding low-quality ads from dominating (spam problem).
"""

import psycopg2
import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.preprocessing import StandardScaler
from sklearn.model_selection import train_test_split
from sklearn.metrics import roc_auc_score, log_loss
import os
from typing import Tuple
import json

DB_CONFIG = {
    "host": os.getenv("DB_HOST", "localhost"),
    "port": int(os.getenv("DB_PORT", "5432")),
    "dbname": os.getenv("DB_NAME", "addb"),
    "user": os.getenv("DB_USER", "aduser"),
    "password": os.getenv("DB_PASSWORD", "adpass"),
}


def fetch_training_data(conn) -> Tuple[np.ndarray, np.ndarray, np.ndarray, np.ndarray]:
    """
    Fetch impressions joined with bid_price from ads table.

    Feature engineering decisions:
      - time_of_day: kept as raw int (0-23). An alternative is cyclical encoding
        (sin/cos) to capture the circular nature of time — worth mentioning.
      - position: raw int. In production, you'd use position-corrected labels
        (Inverse Propensity Scoring) to debias — position 1 gets clicked more
        just because it's shown first, not because it's better.
      - relevance_score: continuous float from the upstream retrieval system.
      - user_segment: treated as ordinal (0-4). Could be one-hot encoded.
      - bid_price: included as a feature (advertiser quality signal).
    """
    query = """
        SELECT
            i.time_of_day,
            i.user_segment,
            i.position,
            i.relevance_score,
            a.bid_price,
            i.clicked::int AS label,
            i.ad_id
        FROM impressions i
        JOIN ads a ON i.ad_id = a.ad_id
        ORDER BY i.impression_id
    """
    with conn.cursor() as cur:
        cur.execute(query)
        rows = cur.fetchall()

    if not rows:
        raise ValueError("No impression data found. Run ingest.py first.")

    data = np.array(rows, dtype=float)
    features = data[:, :5]    # time_of_day, user_segment, position, relevance_score, bid_price
    labels = data[:, 5]       # clicked (0 or 1)
    ad_ids = data[:, 6].astype(int)

    return features, labels, ad_ids


def train_ctr_model(features: np.ndarray, labels: np.ndarray):
    """
    Train logistic regression with standard scaling.

    WHY SCALE FEATURES?
      Logistic regression uses gradient descent on the loss. If features are on
      different scales (bid_price: 0-5, time_of_day: 0-23), the gradient steps
      are uneven and convergence is slow or distorted. StandardScaler normalizes
      each feature to mean=0, std=1.

    WHY C=1.0 (regularization)?
      C is the inverse of regularization strength (1/lambda). C=1.0 is
      scikit-learn's default — moderate L2 regularization. This penalizes large
      coefficients, preventing overfitting on the 10k synthetic samples.

    WHY class_weight='balanced'?
      If CTR is 8%, the dataset is 92% negatives. Without balancing, the model
      learns to just predict "no click" always and gets 92% accuracy (useless).
      'balanced' upweights the positive (click) class inversely proportional
      to its frequency.
    """
    X_train, X_test, y_train, y_test = train_test_split(
        features, labels, test_size=0.2, random_state=42, stratify=labels
    )

    scaler = StandardScaler()
    X_train_scaled = scaler.fit_transform(X_train)
    X_test_scaled = scaler.transform(X_test)  # use train stats — avoid data leakage

    model = LogisticRegression(
        C=1.0,
        class_weight="balanced",
        max_iter=500,
        random_state=42,
    )
    model.fit(X_train_scaled, y_train)

    # Evaluation
    y_proba = model.predict_proba(X_test_scaled)[:, 1]
    auc = roc_auc_score(y_test, y_proba)
    logloss = log_loss(y_test, y_proba)

    print(f"\n  Model evaluation on held-out test set:")
    print(f"    AUC-ROC:  {auc:.4f}  (1.0 = perfect, 0.5 = random)")
    print(f"    Log Loss: {logloss:.4f}  (lower is better)")
    print(f"\n  Feature coefficients (after scaling):")
    feature_names = ["time_of_day", "user_segment", "position", "relevance_score", "bid_price"]
    for name, coef in zip(feature_names, model.coef_[0]):
        print(f"    {name:20s}: {coef:+.4f}")

    return model, scaler


def compute_per_ad_scores(conn, model, scaler) -> list:
    """
    For each ad, compute:
      1. Mean predicted CTR across all its impressions
      2. Auction score = predicted_CTR * bid_price (feature-weighted ranking)

    WHY average CTR across impressions rather than one global prediction?
      Each ad has a distribution of impressions with varying positions, time
      slots, and user segments. Averaging gives a more stable CTR estimate
      than a single-feature-vector prediction — this is similar to how
      production systems estimate "expected CTR" from historical data.
    """
    query = """
        SELECT
            i.ad_id,
            a.bid_price,
            AVG(i.relevance_score)  AS avg_relevance,
            AVG(i.position)         AS avg_position,
            COUNT(*)                AS impression_count,
            i.time_of_day,
            i.user_segment,
            i.relevance_score,
            i.position
        FROM impressions i
        JOIN ads a ON i.ad_id = a.ad_id
        GROUP BY i.ad_id, a.bid_price, i.time_of_day, i.user_segment,
                 i.relevance_score, i.position
        ORDER BY i.ad_id
    """
    # Simpler: get one representative feature vector per ad
    query = """
        SELECT
            i.ad_id,
            a.bid_price,
            AVG(i.time_of_day)      AS avg_time,
            AVG(i.user_segment)     AS avg_segment,
            AVG(i.position)         AS avg_position,
            AVG(i.relevance_score)  AS avg_relevance,
            COUNT(*)                AS impression_count
        FROM impressions i
        JOIN ads a ON i.ad_id = a.ad_id
        GROUP BY i.ad_id, a.bid_price
        ORDER BY i.ad_id
    """
    with conn.cursor() as cur:
        cur.execute(query)
        rows = cur.fetchall()

    results = []
    for row in rows:
        ad_id, bid_price, avg_time, avg_segment, avg_pos, avg_rel, imp_count = row

        # Build feature vector matching training schema
        feature_vec = np.array([[avg_time, avg_segment, avg_pos, avg_rel, bid_price]])
        feature_scaled = scaler.transform(feature_vec)
        predicted_ctr = float(model.predict_proba(feature_scaled)[0, 1])

        # THE AUCTION SCORE — this is the ranking key
        # This mirrors how Google/Meta compute "expected value":
        #   eCPM = predicted_CTR * max_bid * 1000
        weighted_score = predicted_ctr * float(bid_price)

        results.append({
            "ad_id": int(ad_id),
            "predicted_ctr": predicted_ctr,
            "weighted_score": weighted_score,
            "impression_count": int(imp_count),
        })

    # Rank by weighted_score descending — highest expected revenue first
    results.sort(key=lambda x: x["weighted_score"], reverse=True)
    for rank, ad in enumerate(results, start=1):
        ad["rank_position"] = rank

    return results


def write_scores_to_db(conn, scores: list) -> None:
    """
    Upsert scores into ctr_scores table.
    ON CONFLICT (ad_id) DO UPDATE: each pipeline run refreshes scores.
    This is what the Go server reads for ranking — updated_at tracks freshness.
    """
    sql = """
        INSERT INTO ctr_scores
            (ad_id, predicted_ctr, weighted_score, rank_position, impression_count, updated_at)
        VALUES (%s, %s, %s, %s, %s, NOW())
        ON CONFLICT (ad_id) DO UPDATE SET
            predicted_ctr    = EXCLUDED.predicted_ctr,
            weighted_score   = EXCLUDED.weighted_score,
            rank_position    = EXCLUDED.rank_position,
            impression_count = EXCLUDED.impression_count,
            updated_at       = NOW()
    """
    with conn.cursor() as cur:
        for s in scores:
            cur.execute(sql, (
                s["ad_id"], s["predicted_ctr"],
                s["weighted_score"], s["rank_position"], s["impression_count"]
            ))
    conn.commit()
    print(f"\n  Wrote {len(scores)} ad scores to ctr_scores table")


def run_pipeline():
    print("Connecting to PostgreSQL...")
    conn = psycopg2.connect(**DB_CONFIG)

    print("Fetching training data...")
    features, labels, ad_ids = fetch_training_data(conn)
    print(f"  {len(features)} impressions loaded, overall CTR: {labels.mean():.3%}")

    print("\nTraining CTR model...")
    model, scaler = train_ctr_model(features, labels)

    print("\nComputing per-ad auction scores...")
    scores = compute_per_ad_scores(conn, model, scaler)

    print("\nTop 5 ranked ads:")
    for s in scores[:5]:
        print(f"  Rank {s['rank_position']}: ad_id={s['ad_id']} "
              f"CTR={s['predicted_ctr']:.4f} "
              f"score={s['weighted_score']:.4f} "
              f"impressions={s['impression_count']}")

    write_scores_to_db(conn, scores)
    conn.close()
    print("\nPipeline complete.")


if __name__ == "__main__":
    run_pipeline()
