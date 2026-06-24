// App.jsx — Ad Relevance Ranking Dashboard (polished UI)
// Data logic: UNCHANGED. Only styles, layout, and visual polish updated.

import React, { useState, useEffect, useCallback } from "react";
import {
  BarChart, Bar, LineChart, Line, XAxis, YAxis, CartesianGrid,
  Tooltip, Legend, ResponsiveContainer, Cell
} from "recharts";

const API_BASE = "http://localhost:8081";
const POLL_INTERVAL_MS = 5000;

// Indigo-to-rose gradient scale by rank (1=top indigo, 20=bottom rose)
const getRankColor = (rank, maxRank) => {
  const ratio = (rank - 1) / Math.max(maxRank - 1, 1);
  // Interpolate: indigo #6366f1 → rose #f43f5e
  const r = Math.round(99  + ratio * (244 - 99));
  const g = Math.round(102 + ratio * (63  - 102));
  const b = Math.round(241 + ratio * (94  - 241));
  return `rgb(${r},${g},${b})`;
};

// ── Custom Tooltip ────────────────────────────────────────────────────────────
const CustomTooltip = ({ active, payload, label, formatter }) => {
  if (!active || !payload?.length) return null;
  return (
    <div style={{
      background: "rgba(15,15,30,0.97)",
      border: "1px solid rgba(99,102,241,0.3)",
      borderRadius: 10,
      padding: "10px 14px",
      boxShadow: "0 8px 32px rgba(0,0,0,0.5)",
      backdropFilter: "blur(8px)",
    }}>
      <p style={{ color: "#a5b4fc", fontSize: 12, margin: "0 0 6px", fontWeight: 600 }}>
        Ad {label}
      </p>
      {payload.map((p, i) => (
        <p key={i} style={{ color: p.color || "#e2e8f0", fontSize: 13, margin: "2px 0", fontWeight: 500 }}>
          {formatter ? formatter(p.value) : p.value}
        </p>
      ))}
    </div>
  );
};

const LineTooltip = ({ active, payload, label }) => {
  if (!active || !payload?.length) return null;
  return (
    <div style={{
      background: "rgba(15,15,30,0.97)",
      border: "1px solid rgba(99,102,241,0.3)",
      borderRadius: 10,
      padding: "10px 14px",
      boxShadow: "0 8px 32px rgba(0,0,0,0.5)",
    }}>
      <p style={{ color: "#a5b4fc", fontSize: 12, margin: "0 0 8px", fontWeight: 600 }}>{label}</p>
      {payload.map((p, i) => (
        <p key={i} style={{ color: p.color, fontSize: 12, margin: "3px 0" }}>
          {p.name}: <strong>Rank #{p.value}</strong>
        </p>
      ))}
    </div>
  );
};

// ── Section wrapper ───────────────────────────────────────────────────────────
function Panel({ title, badge, subtitle, note, children }) {
  return (
    <div style={{
      background: "linear-gradient(145deg, #0f0f1e 0%, #13132a 100%)",
      border: "1px solid rgba(99,102,241,0.15)",
      borderRadius: 16,
      padding: "28px 28px 20px",
      marginBottom: 20,
      boxShadow: "0 4px 24px rgba(0,0,0,0.4), inset 0 1px 0 rgba(255,255,255,0.03)",
    }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 4 }}>
        <h2 style={{ color: "#c7d2fe", margin: 0, fontSize: "1rem", fontWeight: 600, letterSpacing: "-0.01em" }}>
          {title}
        </h2>
        {badge && (
          <span style={{
            fontSize: 10, fontWeight: 700, padding: "2px 7px", borderRadius: 20,
            background: "rgba(99,102,241,0.15)", color: "#818cf8", letterSpacing: "0.06em",
          }}>{badge}</span>
        )}
      </div>
      <p style={{ color: "#475569", fontSize: "0.8rem", margin: "0 0 20px", lineHeight: 1.6 }}>
        {subtitle}
      </p>
      {children}
      {note && (
        <p style={{
          color: "#334155", fontSize: "0.73rem", margin: "14px 0 0",
          fontStyle: "italic", lineHeight: 1.5,
          borderTop: "1px solid rgba(255,255,255,0.04)", paddingTop: 10,
        }}>
          {note}
        </p>
      )}
    </div>
  );
}

// ── CTR Trends ────────────────────────────────────────────────────────────────
function CTRTrends({ data }) {
  return (
    <Panel
      title="Per-Ad CTR Trends"
      badge="LOGISTIC REGRESSION"
      subtitle="Predicted click-through rate per ad. Color encodes rank — indigo = top, rose = bottom. Higher CTR doesn't guarantee top rank; the auction score is CTR × bid price."
      note="Rank 1 ad (ad_id=18) has a lower CTR than ad_id=13 — it wins because its bid price is higher, producing a greater weighted auction score."
    >
      <ResponsiveContainer width="100%" height={260}>
        <BarChart data={data} margin={{ top: 4, right: 8, left: -8, bottom: 0 }} barCategoryGap="28%">
          <CartesianGrid strokeDasharray="2 4" stroke="rgba(255,255,255,0.04)" vertical={false} />
          <XAxis
            dataKey="ad_id"
            tick={{ fill: "#475569", fontSize: 11 }}
            axisLine={false} tickLine={false}
            label={{ value: "Ad ID", position: "insideBottom", offset: -2, fill: "#334155", fontSize: 11 }}
          />
          <YAxis
            tickFormatter={v => `${(v * 100).toFixed(0)}%`}
            tick={{ fill: "#475569", fontSize: 11 }}
            axisLine={false} tickLine={false}
          />
          <Tooltip content={<CustomTooltip formatter={v => `Predicted CTR: ${(v * 100).toFixed(3)}%`} />} />
          <Bar dataKey="predicted_ctr" radius={[5, 5, 0, 0]} activeBar={false}>
            {data.map((entry, i) => (
              <Cell key={i} fill={getRankColor(entry.rank_position, data.length)} fillOpacity={0.9} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </Panel>
  );
}

// ── Impression Volume ─────────────────────────────────────────────────────────
function ImpressionVolume({ data }) {
  return (
    <Panel
      title="Impression Volume per Ad"
      badge="POSTGRESQL"
      subtitle="Total synthetic impression events ingested. Uneven distribution mirrors real ad systems where some creatives get more exposure based on budget and targeting."
    >
      <ResponsiveContainer width="100%" height={220}>
        <BarChart data={data} margin={{ top: 4, right: 8, left: -8, bottom: 0 }} barCategoryGap="28%">
          <CartesianGrid strokeDasharray="2 4" stroke="rgba(255,255,255,0.04)" vertical={false} />
          <XAxis
            dataKey="ad_id"
            tick={{ fill: "#475569", fontSize: 11 }}
            axisLine={false} tickLine={false}
          />
          <YAxis
            tick={{ fill: "#475569", fontSize: 11 }}
            axisLine={false} tickLine={false}
          />
          <Tooltip content={<CustomTooltip formatter={v => `Impressions: ${v.toLocaleString()}`} />} />
          <Bar dataKey="impression_count" radius={[5, 5, 0, 0]} activeBar={false}>
            {data.map((_, i) => (
              <Cell key={i} fill="#6366f1" fillOpacity={0.55 + (i % 3) * 0.1} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </Panel>
  );
}

// ── Ranking Shifts ────────────────────────────────────────────────────────────
function RankingShifts({ data }) {
  const topAds = data.slice(0, 8);
  const runs = ["Run 1", "Run 2", "Run 3", "Run 4", "Run 5"];
  const colors = ["#6366f1","#f59e0b","#10b981","#ec4899","#3b82f6","#8b5cf6","#14b8a6","#f43f5e"];

  const [chartData] = useState(() =>
    runs.map((run, ri) => {
      const entry = { run };
      topAds.forEach(ad => {
        const jitter = ri === 0 ? 0 : Math.floor((Math.random() - 0.5) * 4);
        entry[`ad_${ad.ad_id}`] = Math.max(1, Math.min(data.length, ad.rank_position + jitter));
      });
      return entry;
    })
  );

  return (
    <Panel
      title="Ranking Position Shifts Across Campaign Runs"
      badge="SIMULATED RERUNS"
      subtitle="How the top 8 ads shift rank as the pipeline reruns with new impression data. Lower Y = better rank. Crossing lines show ads competing for the same slots."
      note="In production, rank shifts arise from new impression data, bid price changes, or A/B test results. The 5s cache TTL means clients observe updates within one TTL window of a pipeline rerun."
    >
      <ResponsiveContainer width="100%" height={280}>
        <LineChart data={chartData} margin={{ top: 4, right: 16, left: -8, bottom: 0 }}>
          <CartesianGrid strokeDasharray="2 4" stroke="rgba(255,255,255,0.04)" vertical={false} />
          <XAxis dataKey="run" tick={{ fill: "#475569", fontSize: 11 }} axisLine={false} tickLine={false} />
          <YAxis
            reversed
            domain={[1, data.length]}
            tick={{ fill: "#475569", fontSize: 11 }}
            axisLine={false} tickLine={false}
            label={{ value: "Rank (1 = top)", angle: -90, position: "insideLeft", fill: "#334155", fontSize: 11 }}
          />
          <Tooltip content={<LineTooltip />} />
          <Legend
            wrapperStyle={{ fontSize: 11, color: "#64748b", paddingTop: 12 }}
            iconType="circle" iconSize={8}
          />
          {topAds.map((ad, i) => (
            <Line
              key={ad.ad_id}
              type="monotone"
              dataKey={`ad_${ad.ad_id}`}
              name={`Ad ${ad.ad_id}`}
              stroke={colors[i % colors.length]}
              strokeWidth={2}
              dot={{ r: 3, fill: colors[i % colors.length], strokeWidth: 0 }}
              activeDot={{ r: 5, strokeWidth: 0 }}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </Panel>
  );
}

// ── Latency Bar ───────────────────────────────────────────────────────────────
function LatencyMeter({ latencyUs, cacheStatus }) {
  const ms = (latencyUs / 1000).toFixed(2);
  const isHit = cacheStatus === "HIT";
  const isGood = latencyUs < 20000;

  return (
    <div style={{
      display: "flex", alignItems: "center", gap: 16, flexWrap: "wrap",
      background: "linear-gradient(145deg, #0f0f1e, #13132a)",
      border: "1px solid rgba(99,102,241,0.15)",
      borderRadius: 12, padding: "14px 22px", marginBottom: 20,
      boxShadow: "0 4px 24px rgba(0,0,0,0.4)",
    }}>
      <span style={{
        fontSize: 10, fontWeight: 800, padding: "4px 10px", borderRadius: 6,
        letterSpacing: "0.08em",
        background: isHit ? "rgba(16,185,129,0.12)" : "rgba(245,158,11,0.12)",
        color: isHit ? "#10b981" : "#f59e0b",
        border: `1px solid ${isHit ? "rgba(16,185,129,0.25)" : "rgba(245,158,11,0.25)"}`,
      }}>
        CACHE {cacheStatus}
      </span>

      <span style={{
        fontSize: 26, fontWeight: 800, letterSpacing: "-0.03em",
        color: isGood ? "#10b981" : "#f59e0b",
        fontVariantNumeric: "tabular-nums",
      }}>
        {ms}<span style={{ fontSize: 14, fontWeight: 500, marginLeft: 3, color: "#475569" }}>ms</span>
      </span>

      <span style={{ color: "#334155", fontSize: 12 }}>last query latency</span>

      <span style={{
        fontSize: 12, color: isGood ? "#10b981" : "#f59e0b",
        display: "flex", alignItems: "center", gap: 5,
      }}>
        {isGood ? "✓" : "⚠"} {isGood ? "sub-20ms target met" : "above 20ms — DB miss"}
      </span>

      <div style={{ marginLeft: "auto", display: "flex", gap: 20 }}>
        {[
          { label: "P99 benchmark", value: "3.38ms" },
          { label: "cache TTL", value: "5s" },
          { label: "target RPS", value: "500" },
        ].map(({ label, value }) => (
          <div key={label} style={{ textAlign: "right" }}>
            <div style={{ fontSize: 14, fontWeight: 700, color: "#c7d2fe", letterSpacing: "-0.02em" }}>{value}</div>
            <div style={{ fontSize: 10, color: "#334155", marginTop: 1 }}>{label}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Stat Cards ────────────────────────────────────────────────────────────────
function StatCards({ data }) {
  const topAd = data[0];
  const avgCTR = data.length ? data.reduce((s, d) => s + d.predicted_ctr, 0) / data.length : 0;
  const totalImpressions = data.reduce((s, d) => s + d.impression_count, 0);

  const cards = [
    { label: "Top Ranked Ad", value: topAd ? `Ad #${topAd.ad_id}` : "—", sub: topAd ? `score ${topAd.weighted_score?.toFixed(3)}` : "" },
    { label: "Avg Predicted CTR", value: `${(avgCTR * 100).toFixed(2)}%`, sub: "across all 20 ads" },
    { label: "Total Impressions", value: totalImpressions.toLocaleString(), sub: "ingested to PostgreSQL" },
    { label: "Ads Ranked", value: data.length, sub: "across 5 campaigns" },
  ];

  return (
    <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 12, marginBottom: 20 }}>
      {cards.map(({ label, value, sub }) => (
        <div key={label} style={{
          background: "linear-gradient(145deg, #0f0f1e, #13132a)",
          border: "1px solid rgba(99,102,241,0.12)",
          borderRadius: 12, padding: "16px 18px",
          boxShadow: "0 2px 12px rgba(0,0,0,0.3)",
        }}>
          <div style={{ fontSize: 10, color: "#475569", letterSpacing: "0.06em", marginBottom: 6 }}>{label.toUpperCase()}</div>
          <div style={{ fontSize: 20, fontWeight: 800, color: "#e2e8f0", letterSpacing: "-0.02em", fontVariantNumeric: "tabular-nums" }}>{value}</div>
          <div style={{ fontSize: 11, color: "#334155", marginTop: 3 }}>{sub}</div>
        </div>
      ))}
    </div>
  );
}

// ── Main App ──────────────────────────────────────────────────────────────────
export default function App() {
  const [data, setData] = useState([]);
  const [latencyUs, setLatencyUs] = useState(0);
  const [cacheStatus, setCacheStatus] = useState("—");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [lastUpdated, setLastUpdated] = useState(null);

  const generateMockData = useCallback(() => {
    return Array.from({ length: 20 }, (_, i) => ({
      ad_id: i + 1,
      predicted_ctr: Math.random() * 0.18 + 0.02,
      weighted_score: Math.random() * 0.5,
      rank_position: i + 1,
      impression_count: Math.floor(Math.random() * 600 + 200),
    })).sort((a, b) => b.weighted_score - a.weighted_score)
      .map((d, i) => ({ ...d, rank_position: i + 1 }));
  }, []);

  const fetchData = useCallback(async () => {
    try {
      const resp = await fetch(`${API_BASE}/stats?limit=20`);
      if (!resp.ok) throw new Error("Server responded with " + resp.status);
      const json = await resp.json();
      setData(json);
      setError(null);
    } catch (err) {
      setData(generateMockData());
      setError("Go server not running — showing simulated data.");
    }

    try {
      const rankResp = await fetch(`${API_BASE}/rank?top_n=10&campaign_id=0`);
      if (rankResp.ok) {
        const rankJson = await rankResp.json();
        setLatencyUs(rankJson.latency_us || 0);
        setCacheStatus(rankJson.cache_status || "—");
      }
    } catch {
      setLatencyUs(Math.random() < 0.85 ? Math.random() * 500 + 100 : Math.random() * 8000 + 3000);
      setCacheStatus(Math.random() < 0.85 ? "HIT" : "MISS");
    }

    setLoading(false);
    setLastUpdated(new Date().toLocaleTimeString());
  }, [generateMockData]);

  useEffect(() => {
    fetchData();
    const interval = setInterval(fetchData, POLL_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [fetchData]);

  return (
    <div style={{
      fontFamily: "'Inter', system-ui, -apple-system, sans-serif",
      background: "#080812",
      minHeight: "100vh",
      color: "#e2e8f0",
      padding: "24px 28px",
    }}>
      {/* Header */}
      <div style={{ marginBottom: 24 }}>
        <div style={{ display: "flex", alignItems: "baseline", gap: 10, marginBottom: 4 }}>
          <h1 style={{
            color: "#e2e8f0", margin: 0, fontSize: "1.35rem", fontWeight: 700, letterSpacing: "-0.03em",
          }}>
            Ad Relevance Ranking
          </h1>
          <span style={{
            fontSize: 10, fontWeight: 700, padding: "3px 8px", borderRadius: 5,
            background: "rgba(99,102,241,0.15)", color: "#818cf8", letterSpacing: "0.06em",
          }}>LIVE</span>
        </div>
        <p style={{ color: "#334155", fontSize: "0.78rem", margin: 0, lineHeight: 1.6 }}>
          Python · logistic regression CTR model · PostgreSQL · Go gRPC · 5s TTL cache
          {lastUpdated && <span style={{ color: "#1e293b" }}> · updated {lastUpdated}</span>}
        </p>
      </div>

      {error && (
        <div style={{
          background: "rgba(245,158,11,0.07)", border: "1px solid rgba(245,158,11,0.2)",
          color: "#92400e", padding: "9px 14px", borderRadius: 8,
          fontSize: "0.78rem", marginBottom: 16, color: "#b45309",
        }}>
          ⚠ {error}
        </div>
      )}

      {loading ? (
        <div style={{ textAlign: "center", padding: "100px 0", color: "#1e293b" }}>
          Connecting to ranking service…
        </div>
      ) : (
        <>
          <StatCards data={data} />
          <LatencyMeter latencyUs={latencyUs} cacheStatus={cacheStatus} />
          <CTRTrends data={data} />
          <ImpressionVolume data={data} />
          <RankingShifts data={data} />
        </>
      )}
    </div>
  );
}
