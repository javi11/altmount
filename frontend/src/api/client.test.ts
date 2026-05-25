import { afterEach, describe, expect, it, vi } from "vitest";
import { APIError, apiClient } from "./client";

function mockFetch(body: unknown, init: { ok?: boolean; status?: number; type?: string } = {}) {
	const { ok = true, status = 200, type = "basic" } = init;
	global.fetch = vi.fn().mockResolvedValue({
		ok,
		status,
		statusText: "",
		type,
		json: async () => body,
	} as unknown as Response);
}

afterEach(() => {
	vi.restoreAllMocks();
});

describe("APIClient.request", () => {
	it("unwraps the data field on a success envelope", async () => {
		mockFetch({ success: true, data: { total: 5 } });
		await expect(apiClient.getQueueStats()).resolves.toEqual({ total: 5 });
	});

	it("throws APIError when success is false", async () => {
		mockFetch({ success: false, error: { message: "boom", details: "context" } });
		await expect(apiClient.getQueueStats()).rejects.toMatchObject({
			name: "APIError",
			message: "boom",
			details: "context",
		});
	});

	it("maps a non-ok HTTP response to APIError", async () => {
		mockFetch({ error: { message: "server fell over" } }, { ok: false, status: 500 });
		await expect(apiClient.getQueueStats()).rejects.toMatchObject({
			status: 500,
			message: "server fell over",
		});
	});

	it("dispatches api:unauthorized on a 401", async () => {
		const handler = vi.fn();
		window.addEventListener("api:unauthorized", handler);
		mockFetch({ error: { message: "nope" } }, { ok: false, status: 401 });
		await expect(apiClient.getQueueStats()).rejects.toBeInstanceOf(APIError);
		expect(handler).toHaveBeenCalledTimes(1);
		window.removeEventListener("api:unauthorized", handler);
	});

	it("wraps a network failure as APIError with status 0", async () => {
		global.fetch = vi.fn().mockRejectedValue(new Error("ECONNREFUSED"));
		await expect(apiClient.getQueueStats()).rejects.toMatchObject({ status: 0 });
	});
});
