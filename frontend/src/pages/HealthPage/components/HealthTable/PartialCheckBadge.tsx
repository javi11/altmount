import { CircleSlash } from "lucide-react";
import type { HealthErrorDetails } from "../../../../types/api";

interface PartialCheckBadgeProps {
	details: HealthErrorDetails;
}

// PartialCheckBadge marks a result produced by a check that stopped once the
// file's failure threshold was irreversibly exceeded. The verdict is final,
// but the segment counts are partial and the missing-segment map is
// incomplete by design — so the badge exists to stop the numbers reading as a
// full survey of the damage.
export function PartialCheckBadge({ details }: PartialCheckBadgeProps) {
	if (!details.terminated_early) {
		return null;
	}

	const checked = details.sampled ?? 0;
	const total = details.total_articles ?? 0;
	const missing = details.missing_articles ?? 0;

	const missingText =
		missing > 0
			? `Failed after ${missing} confirmed missing segment${missing === 1 ? "" : "s"}`
			: "Check stopped early";
	const scopeText =
		total > 0
			? ` — stopped after ${checked.toLocaleString()} of ${total.toLocaleString()} segments`
			: "";

	return (
		<span
			className="badge badge-ghost badge-xs gap-1"
			title={details.termination_reason || `${missingText}${scopeText}`}
		>
			<CircleSlash className="h-3 w-3" aria-hidden="true" />
			{`${missingText}${scopeText}`}
		</span>
	);
}
