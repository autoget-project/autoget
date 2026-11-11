import type { DownloadItem, DownloaderState } from "./api.ts";

export interface StateLabel {
  label: string;
  color: string;
}

export function downloadItemsEqual(a: DownloadItem[], b: DownloadItem[]): boolean {
  if (a.length !== b.length) return false;

  const aMap = new Map(a.map((item) => [item.id, item]));
  const bMap = new Map(b.map((item) => [item.id, item]));

  if (aMap.size !== bMap.size) return false;

  for (const [id, aItem] of aMap) {
    const bItem = bMap.get(id);
    if (!bItem) return false;

    // Compare key properties that affect display
    if (
      aItem.download_progress !== bItem.download_progress ||
      aItem.move_state !== bItem.move_state ||
      aItem.organize_state !== bItem.organize_state ||
      aItem.state !== bItem.state ||
      aItem.updated_at !== bItem.updated_at ||
      JSON.stringify(aItem.organize_plans) !== JSON.stringify(bItem.organize_plans)
    ) {
      return false;
    }
  }

  return true;
}

export function downloaderStatesEqual(
  a: DownloaderState | null,
  b: DownloaderState | null,
): boolean {
  if (!a && !b) return true;
  if (!a || !b) return false;
  return (
    a.count_of_downloading === b.count_of_downloading &&
    a.count_of_planned === b.count_of_planned &&
    a.count_of_failed === b.count_of_failed
  );
}

export function getTabCount(state: DownloaderState | null, tabId: string): number {
  if (!state) return 0;

  switch (tabId) {
    case "downloading":
      return state.count_of_downloading;
    case "planned":
      return state.count_of_planned;
    case "failed":
      return state.count_of_failed;
    default:
      return 0;
  }
}

export function getTabBadgeColor(tabId: string): string {
  switch (tabId) {
    case "downloading":
      return "badge-success";
    case "planned":
      return "badge-info";
    case "failed":
      return "badge-error";
    default:
      return "badge-neutral";
  }
}

export function getMoveStateLabel(moveState: number): StateLabel {
  switch (moveState) {
    case 0: // UnMoved
      return { label: "Unmoved", color: "warning" };
    case 1: // Moved
      return { label: "Moved", color: "success" };
    default:
      return { label: "Unknown", color: "neutral" };
  }
}

export function getOrganizeStateLabel(organizeState: number): StateLabel {
  switch (organizeState) {
    case 0: // Unplanned
      return { label: "Unplanned", color: "neutral" };
    case 1: // Planned
      return { label: "Planned", color: "info" };
    case 2: // Organized
      return { label: "Organized", color: "success" };
    case 3: // CreatePlanFailed
      return { label: "Create Failed", color: "error" };
    case 4: // ExecutePlanFailed
      return { label: "Execute Failed", color: "error" };
    default:
      return { label: "Unknown", color: "neutral" };
  }
}
