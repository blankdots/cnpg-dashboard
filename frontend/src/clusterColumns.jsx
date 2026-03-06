import { flexRender } from "@tanstack/react-table";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";

const Icon = ({ d, size = 16 }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill="none"
    stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
    <path d={d} />
  </svg>
);

const P = {
  db: "M12 2C6.48 2 2 4.24 2 7v10c0 2.76 4.48 5 10 5s10-2.24 10-5V7c0-2.76-4.48-5-10-5zM2 12c0 2.76 4.48 5 10 5s10-2.24 10-5M2 7c0 2.76 4.48 5 10 5s10-2.24 10-5",
  storage: "M22 8.5c0 2.76-4.48 5-10 5S2 11.26 2 8.5M22 8.5C22 5.74 17.52 3 12 3S2 5.74 2 8.5M22 8.5V16c0 2.76-4.48 5-10 5S2 18.76 2 16V8.5",
  shield: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z",
  more: "M12 5h.01M12 12h.01M12 19h.01",
  arrowUp: "M18 15l-6-6-6 6",
  arrowDown: "M6 9l6 6 6-6",
};

const statusStyles = {
  Healthy: { dot: "#34d399", text: "#6ee7b7", bg: "rgba(52,211,153,0.1)", border: "rgba(52,211,153,0.2)" },
  Degraded: { dot: "#fbbf24", text: "#fcd34d", bg: "rgba(251,191,36,0.1)", border: "rgba(251,191,36,0.2)" },
  Creating: { dot: "#60a5fa", text: "#93c5fd", bg: "rgba(96,165,250,0.1)", border: "rgba(96,165,250,0.2)" },
};

const StatusBadge = ({ status }) => {
  const s = statusStyles[status] || { dot: "#71717a", text: "#a1a1aa", bg: "rgba(39,39,42,0.8)", border: "#3f3f46" };
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, padding: "2px 8px", borderRadius: 999, fontSize: 12, fontWeight: 500, background: s.bg, border: `1px solid ${s.border}`, color: s.text }}>
      <span style={{ width: 6, height: 6, borderRadius: "50%", background: s.dot, flexShrink: 0 }} />
      {status}
    </span>
  );
};

export function createClusterColumns({ setSelected, send }) {
  const I = Icon;
  const paths = P;
  const SB = StatusBadge;
  return [
    {
      id: "cluster",
      accessorKey: "name",
      header: "Cluster",
      cell: ({ row }) => {
        const c = row.original;
        return (
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <div style={{ width: 32, height: 32, borderRadius: 8, background: "#27272a", border: "1px solid #3f3f46", display: "flex", alignItems: "center", justifyContent: "center", flexShrink: 0 }}>
              <I d={paths.db} size={13} />
            </div>
            <div>
              <div style={{ fontSize: 13, fontWeight: 600, color: "#e4e4e7" }}>{c.name}</div>
              <div style={{ fontSize: 11, color: "#71717a" }}>{c.namespace}</div>
            </div>
          </div>
        );
      },
      enableSorting: true,
    },
    {
      id: "namespace",
      accessorKey: "namespace",
      header: "Namespace",
      cell: ({ getValue }) => (
        <span style={{ fontSize: 12, color: "#a1a1aa", background: "#27272a", border: "1px solid #3f3f46", padding: "2px 8px", borderRadius: 6 }}>{getValue()}</span>
      ),
      enableSorting: true,
    },
    {
      id: "status",
      accessorKey: "status",
      header: "Status",
      cell: ({ getValue }) => <SB status={getValue()} />,
      enableSorting: true,
    },
    {
      id: "instances",
      accessorFn: (row) => `${row.readyInstances}/${row.instances}`,
      header: "Instances",
      cell: ({ row }) => {
        const c = row.original;
        return (
          <>
            <span style={{ fontSize: 15, fontWeight: 700, fontFamily: "monospace", color: c.readyInstances < c.instances ? "#fbbf24" : "#e4e4e7" }}>{c.readyInstances}</span>
            <span style={{ fontSize: 12, color: "#52525b" }}> / {c.instances}</span>
          </>
        );
      },
      enableSorting: true,
      sortingFn: (rowA, rowB) => {
        const a = rowA.original.readyInstances / rowA.original.instances;
        const b = rowB.original.readyInstances / rowB.original.instances;
        return a - b;
      },
    },
    {
      id: "postgresVersion",
      accessorKey: "postgresVersion",
      header: "PG Version",
      cell: ({ getValue }) => <span style={{ fontSize: 12, fontFamily: "monospace", color: "#a1a1aa" }}>v{getValue()}</span>,
      enableSorting: true,
    },
    {
      id: "storage",
      accessorKey: "storage",
      header: "Storage",
      cell: ({ getValue }) => (
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <I d={paths.storage} size={12} />
          <span style={{ fontSize: 12, color: "#a1a1aa" }}>{getValue()}</span>
        </div>
      ),
      enableSorting: true,
    },
    {
      id: "backups",
      accessorKey: "backupEnabled",
      header: "Backups",
      cell: ({ getValue }) =>
        getValue() ? (
          <span style={{ display: "flex", alignItems: "center", gap: 4, fontSize: 12, color: "#34d399" }}><I d={paths.shield} size={11} />Active</span>
        ) : (
          <span style={{ color: "#52525b", fontSize: 12 }}>—</span>
        ),
      enableSorting: true,
    },
    {
      id: "age",
      accessorKey: "age",
      header: "Age",
      cell: ({ getValue }) => <span style={{ fontSize: 12, color: "#71717a" }}>{getValue()}</span>,
      enableSorting: true,
    },
    {
      id: "actions",
      header: "",
      cell: ({ row }) => {
        const c = row.original;
        return (
          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild>
              <button
                onClick={(e) => e.stopPropagation()}
                style={{ width: 28, height: 28, borderRadius: 8, display: "flex", alignItems: "center", justifyContent: "center", background: "transparent", border: "none", cursor: "pointer", color: "#71717a" }}>
                <I d={paths.more} size={14} />
              </button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content
                side="bottom"
                sideOffset={8}
                align="end"
                style={{ minWidth: 160, borderRadius: 12, overflow: "hidden", background: "#27272a", border: "1px solid #3f3f46", boxShadow: "0 8px 24px rgba(0,0,0,0.5)", padding: 4 }}>
                <DropdownMenu.Item onSelect={() => setSelected(c)} style={{ padding: "8px 12px", fontSize: 12, color: "#d4d4d8", cursor: "pointer", outline: "none", borderRadius: 8 }}>View Details</DropdownMenu.Item>
                <DropdownMenu.Item onSelect={() => send?.current?.("trigger_backup", { namespace: c.namespace, cluster: c.name })} style={{ padding: "8px 12px", fontSize: 12, color: "#d4d4d8", cursor: "pointer", outline: "none", borderRadius: 8 }}>Trigger Backup</DropdownMenu.Item>
                <DropdownMenu.Item
                  onSelect={() => {
                    const standby = c.nodes?.find(n => (n.role || n.Role) === "Standby");
                    if (standby) send?.current?.("switchover", { namespace: c.namespace, cluster: c.name, targetNode: standby.name });
                  }}
                  style={{ padding: "8px 12px", fontSize: 12, color: "#d4d4d8", cursor: "pointer", outline: "none", borderRadius: 8 }}>Switchover</DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        );
      },
      enableSorting: false,
      enableHiding: false,
    },
  ];
}

export { StatusBadge, Icon, P };
