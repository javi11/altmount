import { CheckCircle, Clock, Loader2, Wrench, XCircle } from "lucide-react";
import { formatRelativeTime } from "../../../lib/utils";
import type { Par2RepairJob, Par2RepairStatus } from "../../../types/api";

interface Par2RepairSectionProps {
	jobs: Par2RepairJob[] | undefined;
}

const STATUS_BADGES: Record<Par2RepairStatus, { className: string; label: string }> = {
	pending: { className: "badge-warning", label: "Pending" },
	running: { className: "badge-info", label: "Repairing" },
	repaired: { className: "badge-success", label: "Repaired" },
	unrepairable: { className: "badge-error", label: "Unrepairable" },
};

function StatusBadge({ status }: { status: Par2RepairStatus }) {
	const badge = STATUS_BADGES[status] ?? { className: "badge-ghost", label: status };
	return <span className={`badge badge-sm ${badge.className}`}>{badge.label}</span>;
}

function StatusIcon({ status }: { status: Par2RepairStatus }) {
	switch (status) {
		case "running":
			return <Loader2 className="h-4 w-4 animate-spin text-info" aria-hidden="true" />;
		case "repaired":
			return <CheckCircle className="h-4 w-4 text-success" aria-hidden="true" />;
		case "unrepairable":
			return <XCircle className="h-4 w-4 text-error" aria-hidden="true" />;
		default:
			return <Clock className="h-4 w-4 text-warning" aria-hidden="true" />;
	}
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

export function Par2RepairSection({ jobs }: Par2RepairSectionProps) {
	if (!jobs || jobs.length === 0) {
		return null;
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
								<th>Attempts</th>
								<th>Updated</th>
							</tr>
						</thead>
						<tbody>
							{jobs.map((job) => (
								<tr key={job.id}>
									<td className="max-w-md">
										<div className="flex items-center gap-2">
											<StatusIcon status={job.status} />
											<span className="truncate font-medium" title={job.file_path}>
												{fileName(job.file_path)}
											</span>
										</div>
										{job.last_error && job.status === "unrepairable" && (
											<div className="mt-1 truncate text-error text-xs" title={job.last_error}>
												{job.last_error}
											</div>
										)}
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
									<td>{job.attempts}</td>
									<td className="whitespace-nowrap text-base-content/60 text-sm">
										{formatRelativeTime(job.updated_at)}
									</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
			</div>
		</div>
	);
}
