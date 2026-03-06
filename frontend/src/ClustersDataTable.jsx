"use client";

import { useState, useEffect, useMemo } from "react";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  arrayMove,
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  useReactTable,
} from "@tanstack/react-table";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { createClusterColumns } from "./clusterColumns";

function clusterKey(c) {
  return `${c.namespace}/${c.name}`;
}

const Icon = ({ d, size = 16 }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill="none"
    stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
    <path d={d} />
  </svg>
);

const P = {
  arrowUp: "M18 15l-6-6-6 6",
  arrowDown: "M6 9l6 6 6-6",
  columns: "M3 5h18M3 12h18M3 19h18",
  settings: "M12 15a3 3 0 100-6 3 3 0 000 6z M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2h-2a2 2 0 01-2-2v-1.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2v-2a2 2 0 012-2h1.09a1.65 1.65 0 001-1.51 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 012-2h2a2 2 0 012 2v1.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 012 2v2a2 2 0 01-2 2h-1.09a1.65 1.65 0 00-1.51 1z",
  check: "M20 6L9 17l-5-5",
  gripVertical: "M8 6h.01M8 12h.01M8 18h.01M16 6h.01M16 12h.01M16 18h.01",
};

function DragHandle({ id }) {
  const { attributes, listeners } = useSortable({ id });
  return (
    <button
      type="button"
      {...attributes}
      {...listeners}
      style={{
        width: 28,
        height: 28,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "transparent",
        border: "none",
        borderRadius: 6,
        cursor: "grab",
        color: "#52525b",
      }}
      onPointerDown={(e) => e.stopPropagation()}
      onClick={(e) => e.stopPropagation()}>
      <Icon d={P.gripVertical} size={14} />
      <span style={{ position: "absolute", width: 1, height: 1, overflow: "hidden", clip: "rect(0,0,0,0)" }}>Drag to reorder</span>
    </button>
  );
}

function ColumnCheckbox({ checked }) {
  return (
    <span
      style={{
        width: 16,
        height: 16,
        flexShrink: 0,
        borderRadius: 4,
        border: "1.5px solid",
        borderColor: checked ? "#a78bfa" : "#52525b",
        background: checked ? "#a78bfa" : "transparent",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
      }}>
      {checked && (
        <svg width={12} height={12} viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth={2.5} strokeLinecap="round" strokeLinejoin="round">
          <path d={P.check} />
        </svg>
      )}
    </span>
  );
}

function DraggableRow({ row, setSelected, isLast }) {
  const id = row.id;
  const { transform, transition, setNodeRef, isDragging } = useSortable({ id });
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    borderBottom: isLast ? "none" : "1px solid rgba(39,39,42,0.6)",
    cursor: "pointer",
    opacity: isDragging ? 0.8 : 1,
    zIndex: isDragging ? 10 : 0,
    position: isDragging ? "relative" : undefined,
  };
  return (
    <tr
      ref={setNodeRef}
      style={style}
      onMouseEnter={(e) => { if (!isDragging) e.currentTarget.style.background = "rgba(39,39,42,0.5)"; }}
      onMouseLeave={(e) => { e.currentTarget.style.background = "transparent"; }}
      onClick={() => setSelected(row.original)}>
      {row.getVisibleCells().map((cell) => (
        <td
          key={cell.id}
          style={{ padding: "14px 20px" }}
          onClick={cell.column.id === "actions" ? (e) => e.stopPropagation() : undefined}>
          {flexRender(cell.column.columnDef.cell, cell.getContext())}
        </td>
      ))}
    </tr>
  );
}

export function ClustersDataTable({ data, setSelected, send }) {
  const [sorting, setSorting] = useState([]);
  const [columnVisibility, setColumnVisibility] = useState({});
  const [orderedData, setOrderedData] = useState([]);

  useEffect(() => {
    if (!data?.length) {
      setOrderedData([]);
      return;
    }
    setOrderedData((prev) => {
      const key = clusterKey;
      const dataByKey = new Map(data.map((c) => [key(c), c]));
      if (prev.length === 0) return [...data];
      const kept = prev.map((c) => dataByKey.get(key(c))).filter(Boolean);
      const added = data.filter((c) => !prev.some((p) => key(p) === key(c)));
      return [...kept, ...added];
    });
  }, [data]);

  const baseColumns = createClusterColumns({ setSelected, send });
  const dragColumn = {
    id: "drag",
    header: () => null,
    cell: ({ row }) => <DragHandle id={row.id} />,
    enableSorting: false,
    enableHiding: false,
  };
  const columns = [dragColumn, ...baseColumns];

  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 5 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 100, tolerance: 5 } }),
    useSensor(KeyboardSensor)
  );

  const rowIds = useMemo(() => orderedData.map((c) => clusterKey(c)), [orderedData]);

  const table = useReactTable({
    data: orderedData,
    columns,
    getRowId: (row) => clusterKey(row),
    state: {
      sorting,
      columnVisibility,
    },
    onSortingChange: setSorting,
    onColumnVisibilityChange: (updater) => {
      const next = typeof updater === "function" ? updater(columnVisibility) : updater;
      setColumnVisibility(next);
    },
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
  });

  function handleDragEnd(event) {
    const { active, over } = event;
    if (active && over && active.id !== over.id) {
      setOrderedData((prev) => {
        const oldIndex = rowIds.indexOf(active.id);
        const newIndex = rowIds.indexOf(over.id);
        if (oldIndex === -1 || newIndex === -1) return prev;
        return arrayMove(prev, oldIndex, newIndex);
      });
    }
  }

  return (
    <div style={{ borderRadius: 12, overflow: "visible", background: "#18181b", border: "1px solid #27272a" }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "flex-end", padding: "8px 12px", borderBottom: "1px solid #27272a", gap: 8 }}>
        <DropdownMenu.Root>
          <DropdownMenu.Trigger asChild>
            <button
              style={{ display: "flex", alignItems: "center", gap: 6, padding: "6px 12px", borderRadius: 8, fontSize: 12, border: "1px solid #3f3f46", background: "#27272a", color: "#d4d4d8", cursor: "pointer" }}>
              <Icon d={P.settings} size={14} />
              View
            </button>
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content
              side="bottom"
              sideOffset={8}
              align="end"
              style={{ minWidth: 180, borderRadius: 12, overflow: "hidden", background: "#27272a", border: "1px solid #3f3f46", boxShadow: "0 8px 24px rgba(0,0,0,0.5)", padding: 4 }}>
              <DropdownMenu.Label style={{ fontSize: 11, color: "#71717a", padding: "8px 12px" }}>Toggle columns</DropdownMenu.Label>
              <DropdownMenu.Separator style={{ height: 1, margin: "4px 0", background: "#3f3f46" }} />
              {table.getAllColumns()
                .filter((col) => typeof col.accessorFn !== "undefined" && col.getCanHide())
                .map((col) => (
                  <DropdownMenu.CheckboxItem
                    key={col.id}
                    checked={col.getIsVisible()}
                    onCheckedChange={(v) => col.toggleVisibility(!!v)}
                    style={{ display: "flex", alignItems: "center", gap: 10, padding: "8px 12px", fontSize: 12, color: "#d4d4d8", cursor: "pointer", outline: "none", background: "transparent", border: "none", borderRadius: 6 }}
                    onSelect={(e) => e.preventDefault()}>
                    <ColumnCheckbox checked={col.getIsVisible()} />
                    {col.columnDef.header}
                  </DropdownMenu.CheckboxItem>
                ))}
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      </div>
      <div style={{ overflowX: "auto" }}>
        <DndContext
          collisionDetection={closestCenter}
          modifiers={[restrictToVerticalAxis]}
          onDragEnd={handleDragEnd}
          sensors={sensors}>
          <table style={{ width: "100%", borderCollapse: "collapse", tableLayout: "fixed" }}>
            <colgroup>
              {table.getVisibleLeafColumns().map((col) => (
                <col
                  key={col.id}
                  style={{
                    width: col.id === "drag" ? 48 : col.id === "cluster" ? "28%" : col.id === "actions" ? 56 : undefined,
                  }}
                />
              ))}
            </colgroup>
            <thead>
              {table.getHeaderGroups().map((headerGroup) => (
                <tr key={headerGroup.id} style={{ borderBottom: "1px solid #27272a" }}>
                  {headerGroup.headers.map((header) => (
                    <th
                      key={header.id}
                      style={{
                        textAlign: "left",
                        padding: "12px 20px",
                        fontSize: 11,
                        textTransform: "uppercase",
                        letterSpacing: "0.08em",
                        color: "#71717a",
                        fontWeight: 500,
                        cursor: header.column.getCanSort() ? "pointer" : "default",
                        userSelect: header.column.getCanSort() ? "none" : "auto",
                      }}
                      onClick={header.column.getToggleSortingHandler()}>
                      <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                        {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                        {header.column.getCanSort() && (
                          <span style={{ color: "#52525b", fontSize: 10 }}>
                            {header.column.getIsSorted() === "asc" ? " ↑" : header.column.getIsSorted() === "desc" ? " ↓" : " ⇅"}
                          </span>
                        )}
                      </div>
                    </th>
                  ))}
                </tr>
              ))}
            </thead>
            <tbody>
              {table.getRowModel().rows?.length ? (
                <SortableContext items={rowIds} strategy={verticalListSortingStrategy}>
                  {table.getRowModel().rows.map((row, i) => (
                    <DraggableRow
                      key={row.id}
                      row={row}
                      setSelected={setSelected}
                      isLast={i === table.getRowModel().rows.length - 1}
                    />
                  ))}
                </SortableContext>
              ) : (
                <tr>
                  <td colSpan={table.getVisibleLeafColumns().length} style={{ padding: 24, textAlign: "center", fontSize: 12, color: "#71717a" }}>
                    No clusters found.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </DndContext>
      </div>
    </div>
  );
}
