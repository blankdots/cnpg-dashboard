import { useState, useEffect, useRef } from "react";
import { ClustersDataTable } from "./ClustersDataTable";

// ── API config ─────────────────────────────────────────────────────────────
// Backend serves the frontend in-cluster, so relative paths work everywhere.
const API_BASE = "";

const Icon = ({ d, size = 16, className = "" }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill="none"
    stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round"
    className={className}>
    <path d={d} />
  </svg>
);

const P = {
  cluster: "M3 5a2 2 0 012-2h14a2 2 0 012 2v14a2 2 0 01-2 2H5a2 2 0 01-2-2V5zm0 5h18M9 3v18",
  db: "M12 2C6.48 2 2 4.24 2 7v10c0 2.76 4.48 5 10 5s10-2.24 10-5V7c0-2.76-4.48-5-10-5zM2 12c0 2.76 4.48 5 10 5s10-2.24 10-5M2 7c0 2.76 4.48 5 10 5s10-2.24 10-5",
  backup: "M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M7 10l5 5 5-5M12 15V3",
  check: "M20 6L9 17l-5-5",
  warn: "M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0zM12 9v4M12 17h.01",
  refresh: "M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15",
  more: "M12 5h.01M12 12h.01M12 19h.01",
  node: "M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5",
  shield: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z",
  storage: "M22 8.5c0 2.76-4.48 5-10 5S2 11.26 2 8.5M22 8.5C22 5.74 17.52 3 12 3S2 5.74 2 8.5M22 8.5V16c0 2.76-4.48 5-10 5S2 18.76 2 16V8.5",
  clock: "M12 22a10 10 0 100-20 10 10 0 000 20zM12 6v6l4 2",
  x: "M18 6L6 18M6 6l12 12",
  err: "M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0zM15 9l-6 6M9 9l6 6",
  sun: "M12 4V2m0 20v-2m8-8h2M2 12h2m13.66-5.66L19.78 4.22M4.22 19.78 7.34 16.66m10.32 0 3.12 3.12M4.22 4.22 7.34 7.34M12 8a4 4 0 100 8 4 4 0 000-8z",
  moon: "M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z",
};

// Mock data kept as fallback for when the backend is unreachable (demo / dev)
const MOCK_CLUSTERS = [];
const MOCK_BARMAN = [];

const CONN_CONNECTING = "connecting";
const CONN_CONNECTED = "connected";
const CONN_ERROR = "error";
const MSG_EVENT = "event";
const RESOURCE_CLUSTERS = "clusters";
const RESOURCE_OBJECTSTORES = "objectstores";

// Format Kubernetes quantity strings for display (e.g. "3435583n" → "3.4m", "53852Ki" → "53 MiB")
function formatCpuQuantity(s) {
  if (!s || typeof s !== "string") return s;
  const v = s.trim();
  const num = parseFloat(v);
  if (Number.isNaN(num)) return s;
  if (v.endsWith("n")) return (num / 1_000_000).toFixed(1) + "m";
  if (v.endsWith("u")) return (num / 1_000).toFixed(1) + "m";
  if (v.endsWith("m")) return (Math.round(num * 10) / 10) + "m";
  return num >= 1 ? String(Math.round(num * 10) / 10) : String(num);
}
function formatMemQuantity(s) {
  if (!s || typeof s !== "string") return s;
  const v = s.trim();
  const num = parseFloat(v);
  if (Number.isNaN(num)) return s;
  if (v.endsWith("Ki")) return Math.round(num / 1024) + " MiB";
  if (v.endsWith("Mi")) return Math.round(num) + " MiB";
  if (v.endsWith("Gi")) return (Math.round(num * 10) / 10) + " GiB";
  if (v.endsWith("Ti")) return (Math.round(num * 10) / 10) + " TiB";
  if (v.endsWith("K")) return Math.round(num / 1000) + " MB";
  if (v.endsWith("M")) return Math.round(num) + " MB";
  if (v.endsWith("G")) return (Math.round(num * 10) / 10) + " GB";
  return s;
}

// ── WebSocket hook ─────────────────────────────────────────────────────────
// Single persistent connection for live resource events and outbound commands.
// msg shape in:  { type: "event"|"ack"|"error", payload: ... }
// msg shape out: { action: string, payload: object }
function useWS() {
  const [clusters, setClusters] = useState(MOCK_CLUSTERS);
  const [barmans, setBarmans]   = useState(MOCK_BARMAN);
  const [connStatus, setConnStatus] = useState(CONN_CONNECTING);
  const wsRef   = useRef(null);
  const sendRef = useRef(null); // expose send() to callers

  useEffect(() => {
    let retryTimer;

    function connect() {
      // Derive ws:// or wss:// from the page protocol automatically
      const proto = location.protocol === "https:" ? "wss" : "ws";
      const url   = `${proto}://${location.host}${API_BASE}/api/ws`;
      const wsock = new WebSocket(url);
      wsRef.current = wsock;

      wsock.onopen = () => {
        setConnStatus(CONN_CONNECTED);
        clearTimeout(retryTimer);
      };

      wsock.onclose = () => {
        setConnStatus(CONN_ERROR);
        retryTimer = setTimeout(connect, 5000);
      };

      wsock.onerror = () => wsock.close();

      wsock.onmessage = (e) => {
        try {
          const msg = JSON.parse(e.data);
          if (msg.type === MSG_EVENT) {
            const { type, resourceKind, resource } = msg.payload;
            const key = `${resource.namespace}/${resource.name}`;
            if (resourceKind === RESOURCE_CLUSTERS) {
              setClusters(prev => applyEvent(prev, type, key, resource));
            } else if (resourceKind === RESOURCE_OBJECTSTORES) {
              setBarmans(prev => applyEvent(prev, type, key, resource));
            }
          }
          // ack and error frames can be handled here as new actions are added
        } catch (_) {}
      };

      // Attach send so components can dispatch commands
      sendRef.current = (action, payload) => {
        if (wsock.readyState === WebSocket.OPEN) {
          wsock.send(JSON.stringify({ action, payload }));
        }
      };
    }

    connect();
    return () => {
      clearTimeout(retryTimer);
      wsRef.current?.close();
    };
  }, []);

  return { clusters, barmans, connStatus, send: sendRef };
}

// Apply an SSE event to a resource list
function applyEvent(list, type, key, resource) {
  const id = item => `${item.namespace}/${item.name}`;
  switch (type) {
    case "ADDED":
      return list.some(i => id(i) === key) ? list : [...list, resource];
    case "UPDATED":
      return list.map(i => id(i) === key ? resource : i);
    case "DELETED":
      return list.filter(i => id(i) !== key);
    default:
      return list;
  }
}

const STATUS_MAX_LENGTH = 24;

const statusStyles = {
  Healthy:   { dot: "#34d399", text: "#6ee7b7", bg: "rgba(52,211,153,0.1)",  border: "rgba(52,211,153,0.2)" },
  Ready:     { dot: "#34d399", text: "#6ee7b7", bg: "rgba(52,211,153,0.1)",  border: "rgba(52,211,153,0.2)" },
  Completed: { dot: "#34d399", text: "#6ee7b7", bg: "rgba(52,211,153,0.1)",  border: "rgba(52,211,153,0.2)" },
  Degraded:  { dot: "#fbbf24", text: "#fcd34d", bg: "rgba(251,191,36,0.1)",  border: "rgba(251,191,36,0.2)" },
  Creating:  { dot: "#60a5fa", text: "#93c5fd", bg: "rgba(96,165,250,0.1)",  border: "rgba(96,165,250,0.2)" },
  NotReady:  { dot: "#f87171", text: "#fca5a5", bg: "rgba(248,113,113,0.1)", border: "rgba(248,113,113,0.2)" },
  Failed:    { dot: "#f87171", text: "#fca5a5", bg: "rgba(248,113,113,0.1)", border: "rgba(248,113,113,0.2)" },
};

const StatusBadge = ({ status }) => {
  const s = statusStyles[status] || { dot: "#71717a", text: "#a1a1aa", bg: "rgba(39,39,42,0.8)", border: "#3f3f46" };
  const display = typeof status === "string" && status.length > STATUS_MAX_LENGTH ? status.slice(0, STATUS_MAX_LENGTH) + "…" : status;
  const title = typeof status === "string" && status.length > STATUS_MAX_LENGTH ? status : undefined;
  return (
    <span title={title} style={{ display: "inline-flex", alignItems: "center", gap: 6, padding: "2px 8px", borderRadius: 999, fontSize: 12, fontWeight: 500, background: s.bg, border: `1px solid ${s.border}`, color: s.text, maxWidth: 200, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
      <span style={{ width: 6, height: 6, borderRadius: "50%", background: s.dot, flexShrink: 0 }} />
      {display}
    </span>
  );
};

const MiniBar = ({ pct }) => {
  if (pct == null) return <span style={{ color: "#52525b", fontSize: 12 }}>n/a</span>;
  const color = pct > 80 ? "#ef4444" : pct > 60 ? "#f59e0b" : "#10b981";
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
      <div style={{ width: 56, height: 6, borderRadius: 999, background: "#27272a" }}>
        <div style={{ width: `${pct}%`, height: "100%", borderRadius: 999, background: color }} />
      </div>
      <span style={{ fontSize: 12, color: "#a1a1aa", fontVariantNumeric: "tabular-nums" }}>{pct}%</span>
    </div>
  );
};

const ClusterModal = ({ cluster, barmans, onClose, send, connStatus }) => {
  // Request fresh metrics when modal opens (and when WS connects); backend updates store and broadcasts
  useEffect(() => {
    if (!cluster?.namespace || !cluster?.name || connStatus !== "connected" || !send?.current) return;
    send.current("refresh_cluster_metrics", { namespace: cluster.namespace, cluster: cluster.name });
  }, [cluster?.namespace, cluster?.name, connStatus]);

  const barman = barmans.find(b => b.clusterRef === cluster.name);
  return (
    <div onClick={onClose} style={{ position: "fixed", inset: 0, zIndex: 50, display: "flex", alignItems: "center", justifyContent: "center", padding: 16, background: "var(--cnpg-modal-overlay)", backdropFilter: "blur(4px)" }}>
      <div onClick={e => e.stopPropagation()} style={{ width: "100%", maxWidth: 672, borderRadius: 16, overflow: "hidden", background: "var(--cnpg-modal-bg)", border: "1px solid var(--cnpg-border)", boxShadow: "0 25px 60px rgba(0,0,0,0.2)" }}>
        <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", padding: "24px 24px 20px", borderBottom: "1px solid var(--cnpg-border)" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <div style={{ width: 36, height: 36, borderRadius: 12, background: "rgba(139,92,246,0.1)", border: "1px solid rgba(139,92,246,0.2)", display: "flex", alignItems: "center", justifyContent: "center", flexShrink: 0 }}>
              <Icon d={P.db} size={15} className="text-violet-400" />
            </div>
            <div>
              <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
                <span style={{ fontSize: 14, fontWeight: 700, color: "var(--cnpg-text)", fontFamily: "monospace" }}>{cluster.name}</span>
                <StatusBadge status={cluster.status} />
              </div>
              <p style={{ fontSize: 11, color: "var(--cnpg-text-muted)", marginTop: 2 }}>
                ns: <span style={{ color: "var(--cnpg-text-dim)" }}>{cluster.namespace}</span>
                {" · "}pg <span style={{ color: "#a1a1aa" }}>{cluster.postgresVersion}</span>
                {" · "}age <span style={{ color: "#a1a1aa" }}>{cluster.age}</span>
              </p>
            </div>
          </div>
          <button onClick={onClose} style={{ padding: 6, borderRadius: 8, background: "transparent", border: "none", cursor: "pointer", color: "var(--cnpg-text-muted)" }}
            onMouseEnter={e => { e.target.style.background = "var(--cnpg-bg-tab)"; e.target.style.color = "var(--cnpg-text-secondary)"; }}
            onMouseLeave={e => { e.target.style.background = "transparent"; e.target.style.color = "var(--cnpg-text-muted)"; }}>
            <Icon d={P.x} size={14} />
          </button>
        </div>

        <div style={{ padding: 24, overflowY: "auto", maxHeight: "72vh", display: "flex", flexDirection: "column", gap: 24 }}>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 12 }}>
            {[
              { label: "Instances", value: `${cluster.readyInstances}/${cluster.instances}` },
              { label: "Storage", value: cluster.storage },
              { label: "Primary", value: "#" + cluster.primaryNode.split("-").pop() },
              { label: "Backups", value: cluster.backupEnabled ? "Active" : "None" },
            ].map(s => (
              <div key={s.label} style={{ borderRadius: 12, background: "var(--cnpg-bg-card-hover)", border: "1px solid var(--cnpg-border-subtle)", padding: 12, textAlign: "center" }}>
                <div style={{ fontSize: 18, fontWeight: 700, color: "var(--cnpg-text)", fontFamily: "monospace" }}>{s.value}</div>
                <div style={{ fontSize: 10, textTransform: "uppercase", letterSpacing: "0.1em", color: "var(--cnpg-text-muted)", marginTop: 2 }}>{s.label}</div>
              </div>
            ))}
          </div>

          {cluster.pgDataImage && (
            <div style={{ borderRadius: 12, background: "var(--cnpg-bg-card-hover)", border: "1px solid var(--cnpg-border-subtle)", padding: 12 }}>
              <div style={{ fontSize: 10, textTransform: "uppercase", letterSpacing: "0.1em", color: "var(--cnpg-text-muted)", marginBottom: 4 }}>Pg Data Image</div>
              <div style={{ fontSize: 12, fontFamily: "monospace", color: "var(--cnpg-text-dim)", wordBreak: "break-all" }}>{cluster.pgDataImage}</div>
            </div>
          )}

          <div>
            <h3 style={{ fontSize: 10, textTransform: "uppercase", letterSpacing: "0.1em", fontWeight: 600, color: "var(--cnpg-text-muted)", marginBottom: 12 }}>Instances</h3>
            <div style={{ borderRadius: 12, border: "1px solid var(--cnpg-border)", overflow: "hidden" }}>
              <table style={{ width: "100%", fontSize: 12, borderCollapse: "collapse" }}>
                <thead>
                  <tr style={{ borderBottom: "1px solid var(--cnpg-border)" }}>
                    {["Name", "Role", "Status", "CPU", "Memory"].map(h => (
                      <th key={h} style={{ textAlign: "left", padding: "8px 16px", color: "var(--cnpg-text-muted)", fontWeight: 500 }}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {cluster.nodes.map((n, i) => (
                    <tr key={n.name} style={{ borderBottom: i < cluster.nodes.length - 1 ? "1px solid var(--cnpg-border-subtle)" : "none" }}>
                      <td style={{ padding: "10px 16px", fontFamily: "monospace", color: "var(--cnpg-text-secondary)" }}>{n.name}</td>
                      <td style={{ padding: "10px 16px", color: n.role === "Primary" ? "var(--cnpg-accent)" : "var(--cnpg-text-muted)", fontWeight: n.role === "Primary" ? 600 : 400 }}>{n.role}</td>
                      <td style={{ padding: "10px 16px" }}><StatusBadge status={n.status} /></td>
                      <td style={{ padding: "10px 16px", fontFamily: "monospace", color: "var(--cnpg-text-dim)" }}>{n.cpuUsage ? formatCpuQuantity(n.cpuUsage) : "n/a"}</td>
                      <td style={{ padding: "10px 16px", fontFamily: "monospace", color: "var(--cnpg-text-dim)" }}>{n.memUsage ? formatMemQuantity(n.memUsage) : "n/a"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          {barman && (
            <div>
              <h3 style={{ fontSize: 10, textTransform: "uppercase", letterSpacing: "0.1em", fontWeight: 600, color: "var(--cnpg-text-muted)", marginBottom: 12 }}>Backup Configuration</h3>
              <div style={{ borderRadius: 12, border: "1px solid var(--cnpg-border)", background: "var(--cnpg-bg-card-hover)", padding: 16, display: "flex", flexDirection: "column", gap: 8 }}>
                {[
                  ["Barman Store", barman.name],
                  ["Endpoint", barman.endpoint],
                  ["Type", barman.destinationType],
                  ["Retention", barman.retentionPolicy],
                  ["Schedule", barman.scheduledBackup],
                  ["Last Backup", `${barman.lastBackup} — ${barman.lastBackupStatus}`],
                  ["Total", `${barman.totalBackups} backups · ${barman.size}`],
                  ["WAL Archiving", barman.walEnabled ? "Enabled" : "Disabled"],
                  ["Encryption", barman.encryption],
                ].map(([k, v]) => (
                  <div key={k} style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
                    <span style={{ fontSize: 12, color: "var(--cnpg-text-muted)" }}>{k}</span>
                    <span style={{ fontSize: 12, fontFamily: "monospace", color: "var(--cnpg-text-secondary)" }}>{v}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {!cluster.backupEnabled && (
            <div style={{ display: "flex", alignItems: "flex-start", gap: 12, borderRadius: 12, border: "1px solid rgba(245,158,11,0.3)", background: "rgba(245,158,11,0.08)", padding: 16 }}>
              <Icon d={P.warn} size={14} className="text-amber-400" style={{ flexShrink: 0, marginTop: 1 }} />
              <p style={{ fontSize: 12, color: "#fcd34d", margin: 0 }}>No Barman object store associated. Point-in-time recovery is unavailable.</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default function CNPGDashboard() {
  const { clusters, barmans, connStatus, send } = useWS();
  const [selected, setSelected] = useState(null);
  const [tab, setTab] = useState("clusters");
  const [ns, setNs] = useState("");
  const [theme, setTheme] = useState("dark");

  // Initialise theme from localStorage or prefers-color-scheme
  useEffect(() => {
    try {
      const stored = window.localStorage.getItem("cnpg-theme");
      if (stored === "light" || stored === "dark") {
        setTheme(stored);
        return;
      }
      if (window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches) {
        setTheme("light");
      }
    } catch {
      // ignore
    }
  }, []);

  // Apply theme class on body (drives CSS variables for all elements)
  useEffect(() => {
    if (typeof document === "undefined") return;
    document.body.classList.remove("theme-light", "theme-dark");
    document.body.classList.add(theme === "light" ? "theme-light" : "theme-dark");
    try {
      window.localStorage.setItem("cnpg-theme", theme);
    } catch {
      // ignore
    }
  }, [theme]);

  // Keep selected cluster in sync with list (so modal shows live metrics when backend pushes updates)
  useEffect(() => {
    if (!selected) return;
    const key = `${selected.namespace}/${selected.name}`;
    const current = clusters.find(c => `${c.namespace}/${c.name}` === key);
    if (current) setSelected(current);
    else setSelected(null); // cluster was deleted
  }, [clusters, selected]);

  const nsQuery = ns.trim().toLowerCase();
  const filteredClusters = nsQuery ? clusters.filter(c => c.namespace.toLowerCase().includes(nsQuery)) : clusters;
  const filteredBarman = nsQuery ? barmans.filter(b => b.namespace.toLowerCase().includes(nsQuery)) : barmans;

  const healthy = clusters.filter(c => c.status === "Healthy").length;
  const degraded = clusters.filter(c => c.status === "Degraded").length;
  const totalInst = clusters.reduce((a, c) => a + c.instances, 0);
  const readyInst = clusters.reduce((a, c) => a + c.readyInstances, 0);

  const mono = { fontFamily: "'JetBrains Mono', 'Cascadia Code', 'Fira Mono', monospace" };

  return (
    <div style={{ minHeight: "100vh", background: "var(--cnpg-bg-page)", color: "var(--cnpg-text)", ...mono }}>
      {/* Nav */}
      <div style={{ position: "sticky", top: 0, zIndex: 40, borderBottom: "1px solid var(--cnpg-border)", background: "var(--cnpg-bg-nav)", backdropFilter: "blur(8px)" }}>
        <div style={{ maxWidth: 1100, margin: "0 auto", padding: "0 24px", height: 56, display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <div style={{ width: 28, height: 28, borderRadius: 8, background: "var(--cnpg-accent)", display: "flex", alignItems: "center", justifyContent: "center" }}>
              <Icon d={P.cluster} size={13} className="text-white" />
            </div>
            <span style={{ fontSize: 14, fontWeight: 700, letterSpacing: "-0.02em" }}>CNPG Console</span>
            <span style={{ color: "var(--cnpg-text-muted)", fontSize: 18 }}>·</span>
            <span style={{ fontSize: 12, color: "var(--cnpg-text-muted)" }}>CloudNativePG + Barman</span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <button style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, color: "var(--cnpg-text-dim)", background: "transparent", border: "none", cursor: "pointer", padding: "4px 8px", borderRadius: 8 }}>
              <Icon d={P.refresh} size={12} />
              Sync
            </button>
            <button
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
              style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, color: "var(--cnpg-text-dim)", background: "transparent", border: "none", cursor: "pointer", padding: "4px 8px", borderRadius: 8 }}
            >
              <Icon d={theme === "dark" ? P.sun : P.moon} size={12} />
              {theme === "dark" ? "Light" : "Dark"}
            </button>
            <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, color: "var(--cnpg-text-muted)" }}>
              <span style={{ width: 8, height: 8, borderRadius: "50%", display: "inline-block",
                background: connStatus === "connected" ? "#34d399" : connStatus === "error" ? "#f87171" : "#fbbf24",
                boxShadow: connStatus === "connected" ? "0 0 0 3px rgba(52,211,153,0.2)" : connStatus === "error" ? "0 0 0 3px rgba(248,113,113,0.2)" : "0 0 0 3px rgba(251,191,36,0.2)"
              }} />
              {connStatus === "connected" ? "Live" : connStatus === "error" ? "Reconnecting..." : "Connecting..."}
            </div>
          </div>
        </div>
      </div>

      <div style={{ maxWidth: 1100, margin: "0 auto", padding: "32px 24px", display: "flex", flexDirection: "column", gap: 32 }}>

        {/* Summary */}
        <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 16 }}>
          {[
            { label: "Total Clusters", value: clusters.length, icon: P.cluster, iconColor: "#a78bfa", boxBg: "rgba(139,92,246,0.1)", boxBorder: "rgba(139,92,246,0.2)" },
            { label: "Healthy",        value: healthy,          icon: P.check,   iconColor: "#34d399", boxBg: "rgba(52,211,153,0.1)",  boxBorder: "rgba(52,211,153,0.2)" },
            { label: "Degraded",       value: degraded,         icon: P.warn,    iconColor: "#fbbf24", boxBg: "rgba(251,191,36,0.1)",  boxBorder: "rgba(251,191,36,0.2)" },
            { label: "Instances",      value: `${readyInst}/${totalInst}`, icon: P.node, iconColor: "#38bdf8", boxBg: "rgba(56,189,248,0.1)", boxBorder: "rgba(56,189,248,0.2)" },
          ].map(card => (
            <div key={card.label} style={{ borderRadius: 12, padding: 16, display: "flex", alignItems: "center", gap: 16, background: "var(--cnpg-bg-card)", border: "1px solid var(--cnpg-border)" }}>
              <div style={{ width: 40, height: 40, borderRadius: 12, background: card.boxBg, border: `1px solid ${card.boxBorder}`, display: "flex", alignItems: "center", justifyContent: "center", flexShrink: 0 }}>
                <svg width={16} height={16} viewBox="0 0 24 24" fill="none" stroke={card.iconColor} strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
                  <path d={card.icon} />
                </svg>
              </div>
              <div>
                <div style={{ fontSize: 24, fontWeight: 700, color: "var(--cnpg-text)" }}>{card.value}</div>
                <div style={{ fontSize: 10, textTransform: "uppercase", letterSpacing: "0.08em", color: "var(--cnpg-text-muted)" }}>{card.label}</div>
              </div>
            </div>
          ))}
        </div>

        {/* Tabs + NS Filter */}
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: 12 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 4, padding: 4, borderRadius: 12, background: "var(--cnpg-bg-card)", border: "1px solid var(--cnpg-border)" }}>
            {[
              { id: "clusters", icon: P.db,    label: "Clusters" },
              { id: "barman",   icon: P.backup, label: "Barman Stores" },
            ].map(t => (
              <button key={t.id} onClick={() => setTab(t.id)}
                style={{ display: "flex", alignItems: "center", gap: 6, padding: "6px 16px", borderRadius: 8, fontSize: 12, border: "none", cursor: "pointer", transition: "all 0.15s", background: tab === t.id ? "var(--cnpg-bg-tab)" : "transparent", color: tab === t.id ? "var(--cnpg-text)" : "var(--cnpg-text-muted)" }}>
                <svg width={12} height={12} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
                  <path d={t.icon} />
                </svg>
                {t.label}
              </button>
            ))}
          </div>
          <div style={{ position: "relative" }}>
            <svg width={13} height={13} viewBox="0 0 24 24" fill="none" stroke="var(--cnpg-text-muted)" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"
              style={{ position: "absolute", left: 10, top: "50%", transform: "translateY(-50%)", pointerEvents: "none" }}>
              <circle cx={11} cy={11} r={8} /><path d="M21 21l-4.35-4.35" />
            </svg>
            <input
              type="text"
              placeholder="Filter namespace..."
              value={ns}
              onChange={e => setNs(e.target.value)}
              style={{ background: "var(--cnpg-bg-input)", border: "1px solid var(--cnpg-border)", color: "var(--cnpg-text-secondary)", fontSize: 12, padding: "6px 32px 6px 30px", borderRadius: 8, outline: "none", fontFamily: "inherit", width: 180, caretColor: "var(--cnpg-accent)" }}
              onFocus={e => e.target.style.borderColor = "var(--cnpg-accent)"}
              onBlur={e => e.target.style.borderColor = ""}
            />
            {ns !== "" && (
              <button onClick={() => setNs("")}
                style={{ position: "absolute", right: 8, top: "50%", transform: "translateY(-50%)", background: "transparent", border: "none", cursor: "pointer", color: "var(--cnpg-text-muted)", padding: 2, display: "flex", alignItems: "center" }}>
                <svg width={11} height={11} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5} strokeLinecap="round" strokeLinejoin="round">
                  <path d="M18 6L6 18M6 6l12 12" />
                </svg>
              </button>
            )}
          </div>
        </div>

        {/* Clusters Table */}
        {tab === "clusters" && (
          <ClustersDataTable
            data={filteredClusters}
            setSelected={setSelected}
            send={send}
          />
        )}

        {/* Barman Cards */}
        {tab === "barman" && (
          <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
            {filteredBarman.map(b => (
              <div key={b.name} style={{ borderRadius: 12, overflow: "hidden", background: "var(--cnpg-bg-card)", border: "1px solid var(--cnpg-border)" }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "20px 24px", borderBottom: "1px solid var(--cnpg-border)", flexWrap: "wrap", gap: 12 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                    <div style={{ width: 40, height: 40, borderRadius: 12, background: "rgba(139,92,246,0.1)", border: "1px solid rgba(139,92,246,0.2)", display: "flex", alignItems: "center", justifyContent: "center", flexShrink: 0 }}>
                      <Icon d={P.backup} size={16} className="text-violet-400" />
                    </div>
                    <div>
                      <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
                        <span style={{ fontSize: 14, fontWeight: 700, color: "var(--cnpg-text)" }}>{b.name}</span>
                        <StatusBadge status={b.lastBackupStatus} />
                      </div>
                      <div style={{ fontSize: 11, color: "var(--cnpg-text-muted)", fontFamily: "monospace", marginTop: 2 }}>{b.endpoint}</div>
                    </div>
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                    <span style={{ fontSize: 12, color: "var(--cnpg-text-dim)", display: "flex", alignItems: "center", gap: 6 }}>
                      <Icon d={P.db} size={12} className="text-zinc-600" />
                      {b.cluster}
                    </span>
                    <button style={{ fontSize: 12, padding: "6px 12px", borderRadius: 8, border: "1px solid var(--cnpg-border)", background: "var(--cnpg-bg-tab)", color: "var(--cnpg-text-secondary)", cursor: "pointer", display: "flex", alignItems: "center", gap: 6 }}>
                      <Icon d={P.backup} size={11} />
                      Backup Now
                    </button>
                  </div>
                </div>

                <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", borderBottom: "1px solid var(--cnpg-border)" }}>
                  {[
                    { label: "Destination", value: b.destinationType, sub: `ns: ${b.namespace}` },
                    { label: "Last Backup",  value: b.lastBackup,      sub: b.lastBackupStatus },
                    { label: "Retention",    value: b.retentionPolicy,  sub: `${b.totalBackups} backups stored` },
                    { label: "Total Size",   value: b.size,             sub: `WAL: ${b.walEnabled ? "On" : "Off"} · Enc: ${b.encryption}` },
                  ].map((item, idx) => (
                    <div key={item.label} style={{ padding: "16px 24px", borderRight: idx < 3 ? "1px solid var(--cnpg-border)" : "none" }}>
                      <div style={{ fontSize: 10, textTransform: "uppercase", letterSpacing: "0.1em", color: "var(--cnpg-text-muted)", marginBottom: 4 }}>{item.label}</div>
                      <div style={{ fontSize: 14, fontWeight: 700, color: "var(--cnpg-text-secondary)" }}>{item.value}</div>
                      <div style={{ fontSize: 11, color: "var(--cnpg-text-muted)", marginTop: 2 }}>{item.sub}</div>
                    </div>
                  ))}
                </div>

                <div style={{ padding: "12px 24px", background: "var(--cnpg-bg-card-hover)", display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
                  <Icon d={P.clock} size={12} className="text-zinc-600" />
                  <span style={{ fontSize: 11, color: "var(--cnpg-text-muted)" }}>Schedule:</span>
                  <code style={{ fontSize: 11, color: "var(--cnpg-accent)", background: "rgba(139,92,246,0.05)", border: "1px solid rgba(139,92,246,0.1)", padding: "2px 8px", borderRadius: 6 }}>
                    {b.scheduledBackup}
                  </code>
                  {b.lastBackupStatus === "Failed" && (
                    <span style={{ marginLeft: "auto", fontSize: 11, color: "#f87171", display: "flex", alignItems: "center", gap: 6 }}>
                      <Icon d={P.err} size={11} />
                      Last backup failed — check credentials and bucket access
                    </span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}

        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", paddingTop: 8, borderTop: "1px solid var(--cnpg-border-subtle)" }}>
          <span style={{ fontSize: 11, color: "var(--cnpg-text-muted)" }}>CloudNativePG operator · Barman Cloud</span>
          <span style={{ fontSize: 11, color: "var(--cnpg-text-muted)" }}>Last synced: just now</span>
        </div>
      </div>

      {selected && <ClusterModal cluster={selected} barmans={barmans} onClose={() => setSelected(null)} send={send} connStatus={connStatus} />}
    </div>
  );
}
