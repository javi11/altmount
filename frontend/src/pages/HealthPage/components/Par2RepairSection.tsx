import { Ban, Clock, Loader2, Wrench } from "lucide-react";
import { Fragment } from "react";
import { formatDuration, formatRelativeTime } from "../../../lib/utils";
import type { Par2RepairJob, Par2RepairStatus } from "../../../types/api";
import { parseRepairReason } from "./par2RepairReason";

interface Par2RepairSectionProps {
	jobs: Par2RepairJob[] | undefined;
	onCancel: (id: number) => void;
	onCancelAll: () => void;
	/** Disables the controls while a cancel request is in flight. */
	isCancelling?: boolean;
}

const STATUS_BADGES: Record<Par2RepairStatus, { className: string; label: string }> = {
	pending: { className: "badge-warning", label: "Pending" },
	running: { className: "badge-info", label: "Repairing" },
};

function StatusBadge({ status }: { status: Par2RepairStatus }) {
	const badge = STATUS_BADGES[status] ?? { className: "badge-ghost", label: status };
	return <span className={`badge badge-sm ${badge.className}`}>{badge.label}</span>;
}

function StatusIcon({ status }: { status: Par2RepairStatus }) {
	if (status === "running") {
		return <Loader2 className="h-4 w-4 animate-spin text-info" aria-hidden="true" />;
	}
	return <Clock className="h-4 w-4 text-warning" aria-hidden="true" />;
}

function fileName(path: string) {
	const parts = path.split("/");
	return parts[parts.length - 1] || path;
}

function SweepProgress({ job }: { job: Par2RepairJob }) {
	if (job.status !== "running") {
		return null;
	}
	const total = job.progress_total ?? 0;
	const done = job.progress_done ?? 0;
	if (total <= 0) {
		// Running but the sweep has not started yet (planning / fetching PAR2 index).
		return (
			<div className="flex items-center gap-2 text-base-content/60 text-xs">
				<span className="loading loading-spinner loading-xs" aria-hidden="true" />
				Planning…
			</div>
		);
	}
	const percent = Math.min(100, Math.round((done / total) * 100));
	return (
		<div className="flex items-center gap-2">
			<progress className="progress progress-info w-32" value={done} max={total} />
			<span className="whitespace-nowrap text-base-content/60 text-xs">
				{percent}% ({done}/{total} articles)
			</span>
		</div>
	);
}

/**
 * Remaining time for a running sweep, extrapolated from the rate so far.
 *
 * A repair streams the entire release, so it runs for minutes to hours; a bare
 * percentage does not tell the user whether to wait for it. Only shown once
 * enough articles are done for the rate to mean anything.
 */
function estimateRemaining(job: Par2RepairJob): number | null {
	const done = job.progress_done ?? 0;
	const total = job.progress_total ?? 0;
	const elapsed = job.duration_seconds ?? 0;
	if (done < 5 || total <= done || elapsed <= 0) {
		return null;
	}
	return (elapsed / done) * (total - done);
}

function RunTime({ job }: { job: Par2RepairJob }) {
	if (job.duration_seconds === undefined) {
		return <span className="text-base-content/40">—</span>;
	}
	const elapsed = formatDuration(Math.round(job.duration_seconds));
	const remaining = job.status === "running" ? estimateRemaining(job) : null;
	return (
		<div className="whitespace-nowrap">
			<div>{elapsed}</div>
			{remaining !== null && (
				<div className="text-base-content/50 text-xs">
					~{formatDuration(Math.round(remaining))} left
				</div>
			)}
		</div>
	);
}

/**
 * Why the last attempt of a retrying job failed, on its own full-width row.
 * Unrepairable verdicts do not appear here: they are recorded on the file's
 * health entry (or the failed import) and the job row is deleted.
 */
function ReasonRow({ job, columns }: { job: Par2RepairJob; columns: number }) {
	if (!job.last_error) {
		return null;
	}
	const reason = parseRepairReason(job.last_error);
	return (
		<tr className="border-none">
			<td colSpan={columns} className="pt-0">
				<div className="rounded-md border-warning border-l-4 bg-warning/10 px-3 py-2 text-xs">
					<div className="flex items-start gap-2">
						<Clock className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
						<div className="min-w-0 space-y-1">
							<p className="font-medium">
								Last attempt failed: <span className="font-normal">{reason.summary}</span>
							</p>
							{reason.hint && <p className="text-base-content/70">{reason.hint}</p>}
							{reason.detail !== reason.summary && (
								<details>
									<summary className="cursor-pointer text-base-content/50 hover:text-base-content/80">
										Technical details
									</summary>
									<code className="mt-1 block break-all text-base-content/70">{reason.detail}</code>
								</details>
							)}
						</div>
					</div>
				</div>
			</td>
		</tr>
	);
}

export function Par2RepairSection({
	jobs,
	onCancel,
	onCancelAll,
	isCancelling = false,
}: Par2RepairSectionProps) {
	if (!jobs || jobs.length === 0) {
		// Always rendered, even with no jobs: an invisible card is
		// indistinguishable from a broken one.
		return (
			<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
				<div className="card-body p-4 sm:p-6">
					<h3 className="card-title text-base">
						<Wrench className="h-5 w-5 text-primary" aria-hidden="true" />
						PAR2 Repairs
					</h3>
					<p className="text-base-content/60 text-sm">
						No repairs queued. When a stream hits a missing article — or a health check finds a
						degraded file — AltMount rebuilds the missing bytes here from the release's PAR2
						recovery data. You can also start one from a file's actions menu below.
					</p>
				</div>
			</div>
		);
	}

	return (
		<div className="card border-2 border-base-300/50 bg-base-100 shadow-md">
			<div className="card-body p-4 sm:p-6">
				<h3 className="card-title text-base">
					<Wrench className="h-5 w-5 text-primary" aria-hidden="true" />
					PAR2 Repairs
					{jobs.some((job) => job.status === "running") && (
						<span className="badge badge-info badge-sm">active</span>
					)}
					<button
						type="button"
						className="btn btn-ghost btn-xs ml-auto font-normal"
						onClick={onCancelAll}
						disabled={isCancelling}
					>
						<Ban className="h-3.5 w-3.5" aria-hidden="true" />
						Cancel all
					</button>
				</h3>
				<p className="text-base-content/60 text-sm">
					Missing articles reconstructed from the release's PAR2 recovery data. A repair streams the
					whole release once in the background; repaired bytes are served permanently.
				</p>
				<div className="overflow-x-auto">
					<table className="table-sm table">
						<thead>
							<tr>
								<th>File</th>
								<th>Status</th>
								<th>Progress</th>
								<th>Time</th>
								<th>Attempts</th>
								<th>Updated</th>
								<th>Actions</th>
							</tr>
						</thead>
						<tbody>
							{jobs.map((job) => (
								<Fragment key={job.id}>
									<tr className={job.last_error ? "border-none" : undefined}>
										<td className="max-w-md">
											<div className="flex items-center gap-2">
												<StatusIcon status={job.status} />
												<span className="truncate font-medium" title={job.file_path}>
													{fileName(job.file_path)}
												</span>
											</div>
										</td>
										<td>
											<StatusBadge status={job.status} />
										</td>
										<td>
											<SweepProgress job={job} />
											{job.status === "pending" && job.next_attempt_at && (
												<span className="text-base-content/60 text-xs">
													retry {formatRelativeTime(job.next_attempt_at)}
												</span>
											)}
										</td>
										<td className="text-sm">
											<RunTime job={job} />
										</td>
										<td>{job.attempts}</td>
										<td className="whitespace-nowrap text-base-content/60 text-sm">
											{formatRelativeTime(job.updated_at)}
										</td>
										<td>
											<button
												type="button"
												className="btn btn-ghost btn-xs text-error"
												aria-label={`Cancel repair of ${fileName(job.file_path)}`}
												title="Cancel repair"
												onClick={() => onCancel(job.id)}
												disabled={isCancelling}
											>
												<Ban className="h-4 w-4" aria-hidden="true" />
											</button>
										</td>
									</tr>
									<ReasonRow job={job} columns={7} />
								</Fragment>
							))}
						</tbody>
					</table>
				</div>
			</div>
		</div>
	);
}
