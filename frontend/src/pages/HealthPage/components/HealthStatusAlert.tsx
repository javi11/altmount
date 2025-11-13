import { AlertTriangle } from "lucide-react";

interface HealthStats {
	corrupted: number;
}

interface HealthStatusAlertProps {
	stats: HealthStats | undefined;
}

export function HealthStatusAlert({ stats }: HealthStatusAlertProps) {
	if (!stats || stats.corrupted === 0) {
		return null;
	}

	return (
		<div className="alert alert-error">
			<AlertTriangle className="h-6 w-6" />
			<div>
				<div className="font-bold">File Integrity Issues Detected</div>
				<div className="text-sm">
					{stats.corrupted} corrupted files require immediate attention.
				</div>
			</div>
		</div>
	);
}
