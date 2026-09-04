import { DateTime } from "luxon";

export function formatBytes(bytes: number, decimals: number = 2): string {
  if (bytes === 0) return "0 Bytes";

  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ["Bytes", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"];

  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + " " + sizes[i];
}

export function formatCreatedDate(timestamp: number): string {
  const createdDate = DateTime.fromSeconds(timestamp);
  const now = DateTime.now();
  const diff = now.diff(createdDate, ["minutes", "hours", "days", "weeks"]).toObject();

  if (diff.weeks && diff.weeks >= 1) {
    return createdDate.toFormat("yyyy-MM-dd");
  } else if (diff.days && diff.days >= 1) {
    return `${Math.floor(diff.days)} day${Math.floor(diff.days) === 1 ? "" : "s"} ago`;
  } else if (diff.hours && diff.hours >= 1) {
    return `${Math.floor(diff.hours)} hour${Math.floor(diff.hours) === 1 ? "" : "s"} ago`;
  } else if (diff.minutes && diff.minutes >= 1) {
    return `${Math.floor(diff.minutes)} min${Math.floor(diff.minutes) === 1 ? "" : "s"} ago`;
  } else {
    return "just now";
  }
}
