import { CircleAlert, CircleX } from "lucide-react";
import type { PlaybackImpact } from "../../../../types/api";

interface PlaybackImpactBadgeProps {
	impact: PlaybackImpact;
}

// PlaybackImpactBadge renders the hole-model playback verdict stored in
// error_details: a "degraded" file still plays (streaming zero-fills the
// missing segments), a "failed" file does not. Clean/unknown render nothing.
export function PlaybackImpactBadge({ impact }: PlaybackImpactBadgeProps) {
	if (impact.verdict === "degraded") {
		const gaps = impact.total_missing ?? 0;
		const label =
			gaps > 0
				? `Playable — ${gaps} small gap${gaps === 1 ? "" : "s"} padded`
				: "Playable with glitches";
		return (
			<span className="badge badge-warning badge-xs gap-1">
				<CircleAlert className="h-3 w-3" aria-hidden="true" />
				{label}
			</span>
		);
	}
	if (impact.verdict === "failed") {
		const gaps = impact.total_missing ?? 0;
		const label = gaps > 0 ? `Unplayable — ${gaps} segments missing` : "Unplayable";
		return (
			<span className="badge badge-error badge-xs gap-1">
				<CircleX className="h-3 w-3" aria-hidden="true" />
				{label}
			</span>
		);
	}
	return null;
}
