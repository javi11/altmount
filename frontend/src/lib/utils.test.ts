import { describe, expect, it } from "vitest";
import { formatBytes, formatDuration, formatSpeed } from "./utils";

describe("formatBytes", () => {
	it("formats zero", () => {
		expect(formatBytes(0)).toBe("0 Bytes");
	});
	it("formats KB/MB/GB with default precision", () => {
		expect(formatBytes(1024)).toBe("1 KB");
		expect(formatBytes(1024 * 1024)).toBe("1 MB");
		expect(formatBytes(1536)).toBe("1.5 KB");
		expect(formatBytes(5 * 1024 * 1024 * 1024)).toBe("5 GB");
	});
	it("respects the decimals argument", () => {
		expect(formatBytes(1536, 0)).toBe("2 KB");
	});
});

describe("formatSpeed", () => {
	it("formats zero", () => {
		expect(formatSpeed(0)).toBe("0 B/s");
	});
	it("scales by unit", () => {
		expect(formatSpeed(1024)).toBe("1.0 KB/s");
		expect(formatSpeed(1024 * 1024)).toBe("1.0 MB/s");
	});
});

describe("formatDuration", () => {
	it("formats sub-minute, minutes, hours and days", () => {
		expect(formatDuration(45)).toContain("45");
		expect(formatDuration(3600)).toBe("1h");
		expect(formatDuration(3660)).toBe("1h 1m");
		expect(formatDuration(90000)).toBe("1d 1h");
	});
});
