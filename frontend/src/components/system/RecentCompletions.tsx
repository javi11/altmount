import { CheckCircle2, History } from "lucide-react";
import { useImportHistory } from "../../hooks/useApi";
import { formatRelativeTime } from "../../lib/utils";
import { LoadingSpinner } from "../ui/LoadingSpinner";

export function RecentCompletions() {
	// Use persistent history instead of transient queue
	const { data: history, isLoading } = useImportHistory(10, 10000);

	if (isLoading) return <LoadingSpinner size="sm" />;
	if (!history || history.length === 0) return null;

	return (
		<div className="card bg-base-100 shadow-lg">
			<div className="card-body p-4">
				<h3 className="mb-3 flex items-center gap-2 font-bold text-base-content/50 text-xs uppercase tracking-wider">
					<History className="h-3 w-3" />
					Recent Successes
				</h3>
				<div className="space-y-2">
					{history.map((item) => (
						<div key={item.id} className="flex items-center justify-between gap-4 text-sm">
							<div className="flex min-w-0 items-center gap-2 truncate">
								<CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
								<span className="truncate" title={`${item.nzb_name} -> ${item.file_name}`}>
									{item.file_name}
								</span>
							</div>
							<span className="whitespace-nowrap text-[10px] text-base-content/40">
								{formatRelativeTime(item.completed_at)}
							</span>
						</div>
					))}
				</div>
			</div>
		</div>
	);
}
