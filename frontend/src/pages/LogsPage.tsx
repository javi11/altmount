import { useQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, Download } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { LogEntry } from "../api/client";
import { apiClient } from "../api/client";
import { useLogsStream } from "../hooks/useLogsStream";

type LevelFilter = "all" | "debug" | "info" | "warn" | "error";

const LEVEL_COLORS: Record<string, string> = {
	error: "text-error",
	warn: "text-warning",
	info: "text-info",
	debug: "text-base-content/50",
};

function formatTime(timeStr: string): string {
	try {
		const d = new Date(timeStr);
		const hh = d.getHours().toString().padStart(2, "0");
		const mm = d.getMinutes().toString().padStart(2, "0");
		const ss = d.getSeconds().toString().padStart(2, "0");
		const ms = d.getMilliseconds().toString().padStart(3, "0");
		return `${hh}:${mm}:${ss}.${ms}`;
	} catch {
		return timeStr;
	}
}

function attrsString(entry: LogEntry): string {
	const attrs = entry.attrs;
	if (!attrs || Object.keys(attrs).length === 0) return "";
	try {
		return JSON.stringify(attrs);
	} catch {
		return "";
	}
}

function levelBadgeClass(level: string): string {
	switch (level?.toLowerCase()) {
		case "error":
			return "badge-error";
		case "warn":
		case "warning":
			return "badge-warning";
		case "info":
			return "badge-info";
		default:
			return "badge-ghost";
	}
}

interface LogRowProps {
	entry: LogEntry;
}

function LogRow({ entry }: LogRowProps) {
	const level = (entry.level ?? "").toLowerCase();
	const colorClass = LEVEL_COLORS[level] ?? "text-base-content";
	const extra = attrsString(entry);

	return (
		<div className={`font-mono text-xs leading-relaxed ${colorClass}`}>
			<span className="text-base-content/40">[{formatTime(entry.time)}]</span>{" "}
			<span className={`badge badge-xs ${levelBadgeClass(level)} mr-1`}>
				{entry.level?.toUpperCase()}
			</span>{" "}
			<span>{entry.msg}</span>
			{extra && <span className="ml-2 text-base-content/40">{extra}</span>}
		</div>
	);
}

export function LogsPage() {
	const [levelFilter, setLevelFilter] = useState<LevelFilter>("all");
	const [search, setSearch] = useState("");
	const [liveEnabled, setLiveEnabled] = useState(true);
	const [isFollowing, setIsFollowing] = useState(true);
	const [displayEntries, setDisplayEntries] = useState<LogEntry[]>([]);
	const scrollRef = useRef<HTMLDivElement>(null);
	const virtualizerRef = useRef<ReturnType<typeof useVirtualizer<HTMLDivElement, Element>> | null>(
		null,
	);

	// Historical data from REST endpoint.
	const { data: historicalEntries } = useQuery({
		queryKey: ["logs", "history"],
		queryFn: () => apiClient.getLogs({ limit: 200 }),
		staleTime: Number.POSITIVE_INFINITY,
	});

	// Live stream.
	const { entries: streamEntries } = useLogsStream();

	// Merge historical + stream entries with deduplication by time+msg.
	useEffect(() => {
		const seen = new Set<string>();
		const merged: LogEntry[] = [];

		const dedup = (entry: LogEntry) => {
			const key = `${entry.time}|${entry.msg}`;
			if (!seen.has(key)) {
				seen.add(key);
				merged.push(entry);
			}
		};

		// Stream entries are newest-first; historical are also newest-first.
		for (const e of streamEntries) dedup(e);
		for (const e of historicalEntries ?? []) dedup(e);

		setDisplayEntries(merged.reverse());
	}, [streamEntries, historicalEntries]);

	const filteredEntries = useMemo(() => {
		return displayEntries.filter((e) => {
			if (levelFilter !== "all") {
				const el = (e.level ?? "").toLowerCase();
				const fl = levelFilter === "warn" ? ["warn", "warning"] : [levelFilter];
				if (!fl.includes(el)) return false;
			}
			if (search) {
				const q = search.toLowerCase();
				const inMsg = e.msg?.toLowerCase().includes(q);
				const inAttrs = attrsString(e).toLowerCase().includes(q);
				if (!inMsg && !inAttrs) return false;
			}
			return true;
		});
	}, [displayEntries, levelFilter, search]);

	// Auto-scroll to bottom when following and new entries arrive.
	useEffect(() => {
		if (isFollowing && virtualizerRef.current && filteredEntries.length > 0) {
			virtualizerRef.current.scrollToIndex(filteredEntries.length - 1);
		}
	}, [isFollowing, filteredEntries]);

	// Detect when the user scrolls away from the bottom.
	const handleScroll = useCallback(() => {
		const el = scrollRef.current;
		if (!el) return;
		const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 50;
		setIsFollowing(atBottom);
	}, []);

	const handleFollowClick = useCallback(() => {
		setIsFollowing(true);
		if (virtualizerRef.current && filteredEntries.length > 0) {
			virtualizerRef.current.scrollToIndex(filteredEntries.length - 1);
		}
	}, [filteredEntries]);

	const rowVirtualizer = useVirtualizer({
		count: filteredEntries.length,
		getScrollElement: () => scrollRef.current,
		estimateSize: () => 32,
		overscan: 10,
		measureElement: (el) => el.getBoundingClientRect().height,
	});
	virtualizerRef.current = rowVirtualizer;

	const handleClear = useCallback(() => {
		setDisplayEntries([]);
	}, []);

	const handleDownload = useCallback(() => {
		const lines = filteredEntries.map((e) => {
			const time = formatTime(e.time);
			const level = (e.level ?? "").toUpperCase().padEnd(5);
			const extra = attrsString(e);
			return extra ? `[${time}] ${level} ${e.msg}  ${extra}` : `[${time}] ${level} ${e.msg}`;
		});
		const blob = new Blob([lines.join("\n")], { type: "text/plain" });
		const url = URL.createObjectURL(blob);
		const a = document.createElement("a");
		a.href = url;
		a.download = `altmount-logs-${new Date().toISOString().slice(0, 19).replace(/:/g, "-")}.txt`;
		a.click();
		URL.revokeObjectURL(url);
	}, [filteredEntries]);

	const levels: { label: string; value: LevelFilter }[] = [
		{ label: "All", value: "all" },
		{ label: "Debug", value: "debug" },
		{ label: "Info", value: "info" },
		{ label: "Warn", value: "warn" },
		{ label: "Error", value: "error" },
	];

	return (
		<div className="space-y-4 p-4">
			<div className="flex flex-wrap items-center justify-between gap-3">
				<h1 className="font-bold text-xl">Logs</h1>
			</div>

			{/* Controls */}
			<div className="flex flex-wrap items-center gap-3">
				{/* Level filter tabs */}
				<div className="tabs tabs-boxed" role="tablist" aria-label="Log level filter">
					{levels.map(({ label, value }) => (
						<button
							key={value}
							type="button"
							role="tab"
							aria-selected={levelFilter === value}
							className={`tab tab-sm ${levelFilter === value ? "tab-active" : ""}`}
							onClick={() => setLevelFilter(value)}
						>
							{label}
						</button>
					))}
				</div>

				{/* Search */}
				<fieldset className="fieldset py-0">
					<input
						type="search"
						className="input input-sm w-48"
						placeholder="Search logs…"
						value={search}
						onChange={(e) => setSearch(e.target.value)}
						aria-label="Search log messages"
					/>
				</fieldset>

				{/* Live toggle */}
				<button
					type="button"
					className={`btn btn-sm ${liveEnabled ? "btn-primary" : "btn-outline"}`}
					onClick={() => setLiveEnabled((v) => !v)}
					aria-pressed={liveEnabled}
				>
					{liveEnabled ? "⏸ Pause" : "▶ Live"}
				</button>

				{/* Clear */}
				<button
					type="button"
					className="btn btn-sm btn-ghost"
					onClick={handleClear}
					aria-label="Clear displayed logs"
				>
					Clear
				</button>

				{/* Download */}
				<button
					type="button"
					className="btn btn-sm btn-ghost"
					onClick={handleDownload}
					aria-label="Download displayed logs as text file"
					disabled={filteredEntries.length === 0}
				>
					<Download className="h-4 w-4" />
					Download
				</button>
			</div>

			{/* Log output */}
			<div className="card bg-base-200 shadow">
				<div className="relative">
				<div
					ref={scrollRef}
					role="log"
					className="card-body h-[65vh] overflow-y-auto p-3"
					aria-live={liveEnabled ? "polite" : "off"}
					aria-atomic="false"
					onScroll={handleScroll}
				>
					{filteredEntries.length === 0 ? (
						<p className="text-base-content/50 text-sm">No log entries to display.</p>
					) : (
						<div style={{ height: `${rowVirtualizer.getTotalSize()}px`, position: "relative" }}>
							{rowVirtualizer.getVirtualItems().map((virtualRow) => (
								<div
									key={virtualRow.key}
									data-index={virtualRow.index}
									ref={rowVirtualizer.measureElement}
									style={{
										position: "absolute",
										top: 0,
										left: 0,
										width: "100%",
										transform: `translateY(${virtualRow.start}px)`,
									}}
								>
									<LogRow entry={filteredEntries[virtualRow.index]} />
								</div>
							))}
						</div>
					)}
				</div>
				{!isFollowing && (
					<button
						type="button"
						className="absolute right-4 bottom-4 btn btn-sm btn-primary gap-1 shadow-lg"
						onClick={handleFollowClick}
						aria-label="Scroll to latest log entry"
					>
						<ArrowDown className="h-4 w-4" />
						Follow
					</button>
				)}
				</div>
			</div>

			<p className="text-base-content/40 text-xs">
				Showing {filteredEntries.length} entries
				{displayEntries.length !== filteredEntries.length &&
					` (filtered from ${displayEntries.length})`}
			</p>
		</div>
	);
}
