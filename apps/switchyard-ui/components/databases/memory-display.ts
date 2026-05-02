/**
 * Pure helper for rendering the database addon `memory` field on the
 * /databases page (audit finding DB-1). Lives in its own module so unit
 * tests can import it without dragging the React tree (which depends on
 * @enclii/ui-components — pre-existing module-resolution issue under
 * jest in this app).
 *
 * The API returns the literal token "shared" for addons whose memory
 * comes from a cluster pool rather than a per-addon allocation. The
 * bare word is opaque (audit DB-1), so we expose it as a friendly
 * label + tooltip.
 */

export function memoryDisplay(memory: string | undefined | null): {
  label: string;
  tooltip: string | null;
} {
  if (!memory) return { label: "—", tooltip: null };
  if (memory === "shared") {
    return {
      label: "Shared (cluster pool)",
      tooltip:
        "Allocated from a shared pool — actual usage varies. Use `enclii admin databases discover` to see live consumption.",
    };
  }
  // Concrete numeric values (e.g. "256Mi", "1Gi") render as-is.
  return { label: memory, tooltip: null };
}
