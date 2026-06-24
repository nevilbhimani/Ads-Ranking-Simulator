"""
ingest.py — Ingests synthetic ad logs into PostgreSQL

Design decisions:
  - psycopg2 with executemany() for batch inserts: much faster than
    row-by-row inserts (single round-trip per batch vs. N round-trips).
  - BATCH_SIZE=500: balances memory usage and insert throughput.
    At 10k rows, this is 20 batches — fine for a demo pipeline.
  - ON CONFLICT DO NOTHING for ads: idempotent re-runs won't duplicate ads.
    Impressions always append (they're events, not entities).
"""

import psycopg2
from psycopg2.extras import execute_values
from typing import List, Dict
import os

# Connection config — reads from env vars or falls back to docker-compose defaults
DB_CONFIG = {
    "host": os.getenv("DB_HOST", "localhost"),
    "port": int(os.getenv("DB_PORT", "5432")),
    "dbname": os.getenv("DB_NAME", "addb"),
    "user": os.getenv("DB_USER", "aduser"),
    "password": os.getenv("DB_PASSWORD", "adpass"),
}

BATCH_SIZE = 500


def get_connection():
    return psycopg2.connect(**DB_CONFIG)


def ingest_ads(conn, ads: List[Dict]) -> None:
    """
    Insert ads into the master registry.
    ON CONFLICT DO NOTHING: safe to re-run without duplicating ads.
    In production, you'd use ON CONFLICT DO UPDATE for price changes.
    """
    sql = """
        INSERT INTO ads (ad_id, campaign_id, ad_text, bid_price)
        VALUES %s
        ON CONFLICT (ad_id) DO NOTHING
    """
    rows = [(a["ad_id"], a["campaign_id"], a["ad_text"], a["bid_price"]) for a in ads]

    with conn.cursor() as cur:
        execute_values(cur, sql, rows)
    conn.commit()
    print(f"  Ingested {len(ads)} ads")


def ingest_impressions(conn, impressions: List[Dict]) -> None:
    """
    Batch-insert impression events.
    execute_values() sends all rows in a single multi-row INSERT — far more
    efficient than a loop of single inserts for bulk data.
    """
    sql = """
        INSERT INTO impressions
            (ad_id, user_id, timestamp, clicked,
             time_of_day, user_segment, position, relevance_score)
        VALUES %s
    """

    # Split into batches to avoid massive single transactions
    for batch_start in range(0, len(impressions), BATCH_SIZE):
        batch = impressions[batch_start: batch_start + BATCH_SIZE]
        rows = [
            (
                i["ad_id"], i["user_id"], i["timestamp"], i["clicked"],
                i["time_of_day"], i["user_segment"], i["position"], i["relevance_score"],
            )
            for i in batch
        ]
        with conn.cursor() as cur:
            execute_values(cur, sql, rows)
        conn.commit()

    print(f"  Ingested {len(impressions)} impressions in batches of {BATCH_SIZE}")


def run_ingestion(ads: List[Dict], impressions: List[Dict]) -> None:
    print("Connecting to PostgreSQL...")
    conn = get_connection()
    print("Connected.\n")

    print("Ingesting ads...")
    ingest_ads(conn, ads)

    print("Ingesting impressions...")
    ingest_impressions(conn, impressions)

    conn.close()
    print("\nIngestion complete.")


if __name__ == "__main__":
    from generate_logs import generate_ads, generate_impressions

    ads = generate_ads(n_ads=20)
    impressions = generate_impressions(ads, n_impressions=10_000)
    run_ingestion(ads, impressions)
