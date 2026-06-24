"""
generate_logs.py — Synthetic ad impression log generator

Why logistic-style click probabilities?
Real ad clicks follow a logistic curve: linear features (position, relevance,
user segment) are combined, then squashed through sigmoid to produce a
probability. We use the same functional form here so our training labels
are generated from a known ground truth — this is standard practice for
simulation-based ranker prototyping.
"""

import random
import math
from datetime import datetime, timedelta
from typing import List, Dict

# Reproducibility — important for demos
random.seed(42)


def sigmoid(x: float) -> float:
    """Logistic function — squashes any real number into (0, 1)."""
    return 1.0 / (1.0 + math.exp(-x))


def generate_ads(n_ads: int = 20) -> List[Dict]:
    """
    Create a pool of ads across campaigns.
    bid_price simulates what an advertiser is willing to pay per click (CPC model).
    Higher bids don't guarantee top ranking — CTR * bid is the auction score.
    """
    campaigns = list(range(1, 6))  # 5 campaigns, each with multiple ads
    ads = []
    for ad_id in range(1, n_ads + 1):
        ads.append({
            "ad_id": ad_id,
            "campaign_id": random.choice(campaigns),
            "ad_text": f"Ad creative #{ad_id} — Campaign {random.choice(campaigns)}",
            "bid_price": round(random.uniform(0.10, 5.00), 4),
        })
    return ads


def generate_impressions(ads: List[Dict], n_impressions: int = 10_000) -> List[Dict]:
    """
    Generate synthetic impression logs with realistic click probabilities.

    Feature effects on click probability (ground truth model):
      - position:        higher slot = lower CTR (position bias is real in ad systems)
      - relevance_score: pre-auction semantic match signal
      - user_segment:    some segments click more (e.g., segment 4 = high-intent)
      - time_of_day:     peak hours (8am-10am, 7pm-9pm) see higher CTR
      - bid_price:       slight positive signal (higher bid = higher quality advertiser?)

    We add Gaussian noise to make the labels imperfect — a perfect signal
    would make the model trivially overfit and be unrealistic.
    """
    impressions = []
    base_time = datetime.utcnow() - timedelta(hours=24)

    for i in range(n_impressions):
        ad = random.choice(ads)
        user_id = random.randint(1, 5000)
        position = random.randint(1, 5)           # 1=top slot, 5=bottom
        user_segment = random.randint(0, 4)
        time_of_day = random.randint(0, 23)
        relevance_score = round(random.uniform(0.1, 1.0), 4)
        timestamp = base_time + timedelta(seconds=i * 8)  # ~8s between events

        # Ground truth logistic model for click probability
        # Coefficients chosen to produce ~5-15% overall CTR (realistic for display ads)
        log_odds = (
            -2.5                                   # base rate intercept
            + (-0.3 * position)                    # position penalty
            + (1.2 * relevance_score)              # relevance boost
            + (0.15 * user_segment)                # segment quality
            + (0.05 * ad["bid_price"])             # advertiser quality signal
            + (0.2 if time_of_day in range(8, 11) else 0)   # morning peak
            + (0.2 if time_of_day in range(19, 22) else 0)  # evening peak
            + random.gauss(0, 0.3)                 # noise
        )

        click_prob = sigmoid(log_odds)
        clicked = random.random() < click_prob

        impressions.append({
            "ad_id": ad["ad_id"],
            "user_id": user_id,
            "timestamp": timestamp.isoformat(),
            "clicked": clicked,
            "time_of_day": time_of_day,
            "user_segment": user_segment,
            "position": position,
            "relevance_score": relevance_score,
        })

    return impressions


if __name__ == "__main__":
    ads = generate_ads(n_ads=20)
    impressions = generate_impressions(ads, n_impressions=10_000)

    print(f"Generated {len(ads)} ads")
    print(f"Generated {len(impressions)} impressions")
    print(f"Overall CTR: {sum(i['clicked'] for i in impressions) / len(impressions):.3%}")
    print(f"Sample impression: {impressions[0]}")
