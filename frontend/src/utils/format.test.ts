import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { formatBytes, formatCreatedDate } from "./format";
import { DateTime } from "luxon";

describe("formatBytes", () => {
  it("should return 0 Bytes for 0", () => {
    expect(formatBytes(0)).toBe("0 Bytes");
  });

  it("should format bytes correctly", () => {
    expect(formatBytes(500)).toBe("500 Bytes");
  });

  it("should format kilobytes correctly", () => {
    expect(formatBytes(1024)).toBe("1 KB");
    expect(formatBytes(2500)).toBe("2.44 KB");
  });

  it("should format megabytes correctly", () => {
    expect(formatBytes(1024 * 1024)).toBe("1 MB");
    expect(formatBytes(1572864)).toBe("1.5 MB");
  });

  it("should format gigabytes correctly", () => {
    expect(formatBytes(1024 * 1024 * 1024)).toBe("1 GB");
  });

  it("should handle different decimals", () => {
    expect(formatBytes(1572864, 0)).toBe("2 MB");
    expect(formatBytes(1573864, 3)).toBe("1.501 MB");
  });
});

describe("formatCreatedDate", () => {
  const mockNow = DateTime.fromISO("2025-09-05T10:00:00.000Z").toUTC();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(mockNow.toJSDate());
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('should return "just now" for times less than a minute ago', () => {
    const timestamp = mockNow.minus({ seconds: 30 }).toSeconds();
    expect(formatCreatedDate(timestamp)).toBe("just now");
  });

  it('should return "1 min ago" for times 1 minute ago', () => {
    const timestamp = mockNow.minus({ minutes: 1 }).toSeconds();
    expect(formatCreatedDate(timestamp)).toBe("1 min ago");
  });

  it('should return "X mins ago" for times less than an hour ago', () => {
    const timestamp = mockNow.minus({ minutes: 30 }).toSeconds();
    expect(formatCreatedDate(timestamp)).toBe("30 mins ago");
  });

  it('should return "1 hour ago" for times 1 hour ago', () => {
    const timestamp = mockNow.minus({ hours: 1 }).toSeconds();
    expect(formatCreatedDate(timestamp)).toBe("1 hour ago");
  });

  it('should return "X hours ago" for times less than a day ago', () => {
    const timestamp = mockNow.minus({ hours: 12 }).toSeconds();
    expect(formatCreatedDate(timestamp)).toBe("12 hours ago");
  });

  it('should return "1 day ago" for times 1 day ago', () => {
    const timestamp = mockNow.minus({ days: 1 }).toSeconds();
    expect(formatCreatedDate(timestamp)).toBe("1 day ago");
  });

  it('should return "X days ago" for times less than a week ago', () => {
    const timestamp = mockNow.minus({ days: 4 }).toSeconds();
    expect(formatCreatedDate(timestamp)).toBe("4 days ago");
  });

  it("should return date for times a week or more ago", () => {
    const timestamp = mockNow.minus({ weeks: 1 }).toSeconds();
    const expectedDate = DateTime.fromSeconds(timestamp, { zone: "utc" }).toFormat("yyyy-MM-dd");
    expect(formatCreatedDate(timestamp)).toBe(expectedDate);
  });

  it("should return date for times more than a week ago", () => {
    const timestamp = mockNow.minus({ months: 2 }).toSeconds();
    const expectedDate = DateTime.fromSeconds(timestamp, { zone: "utc" }).toFormat("yyyy-MM-dd");
    expect(formatCreatedDate(timestamp)).toBe(expectedDate);
  });
});
