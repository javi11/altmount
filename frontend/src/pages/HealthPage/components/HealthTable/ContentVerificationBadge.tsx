import { ShieldAlert, ShieldCheck, ShieldQuestion } from "lucide-react";
import type { HealthErrorDetails } from "../../../../types/api";

interface ContentVerificationBadgeProps {
	details: HealthErrorDetails;
}

// ContentVerificationBadge reports the media-container header probe. It exists
// because a health result that says nothing about content is ambiguous: a file
// verified clean and one never probed both look identical otherwise. An
// "unavailable" probe is the important case — the file is unproven, not good.
export function ContentVerificationBadge({ details }: ContentVerificationBadgeProps) {
	switch (details.content_verification) {
		case "passed":
			return (
				<span
					className="badge badge-ghost badge-xs gap-1"
					title="The file's header carries a recognized media container signature"
				>
					<ShieldCheck className="h-3 w-3" aria-hidden="true" />
					Content verified
				</span>
			);
		case "failed":
			return (
				<span
					className="badge badge-error badge-xs gap-1"
					title={details.message || "Content verification failed"}
				>
					<ShieldAlert className="h-3 w-3" aria-hidden="true" />
					Content invalid
				</span>
			);
		case "unavailable":
			return (
				<span
					className="badge badge-warning badge-xs gap-1"
					title={details.message || "The content probe could not complete"}
				>
					<ShieldQuestion className="h-3 w-3" aria-hidden="true" />
					Content unverified
				</span>
			);
		default:
			return null;
	}
}
