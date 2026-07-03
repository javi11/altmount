import { CircleAlert, CircleX } from "lucide-react";
import type { PlaybackImpact } from "../../../../types/api";

interface PlaybackImpactBadgeProps {
	impact: PlaybackImpact;
}

function formatPlaybackTime(totalSeconds: number): string {
	const s = Math.max(0, Math.floor(totalSeconds));
	const hours = Math.floor(s / 3600);
	const minutes = Math.floor((s % 3600) / 60);
	const seconds = s % 60;
	if (hours > 0) {
		return `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
	}
	return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

// PlaybackImpactBadge renders the playback-impact verdict stored in
// error_details: "still playable with a glitch" vs "unplayable".
// Unknown verdicts render nothing (no information to convey).
export function PlaybackImpactBadge({ impact }: PlaybackImpactBadgeProps) {
	if (impact.verdict === "degraded") {
		const window = impact.affected_time?.[0];
		const label = window
			? `Playable (glitch ~${formatPlaybackTime(window.from_sec)}–${formatPlaybackTime(window.to_sec)})`
			: "Playable with glitches";
		return (
			<span className="badge badge-warning badge-xs gap-1" title={impact.reason}>
				<CircleAlert className="h-3 w-3" aria-hidden="true" />
				{label}
			</span>
		);
	}
	if (impact.verdict === "fatal") {
		return (
			<span className="badge badge-error badge-xs gap-1" title={impact.reason}>
				<CircleX className="h-3 w-3" aria-hidden="true" />
				Unplayable
			</span>
		);
	}
	return null;
}
