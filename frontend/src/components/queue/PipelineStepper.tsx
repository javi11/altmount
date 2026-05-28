import { Download, FileText, Search, ShieldCheck } from "lucide-react";
import { memo } from "react";
import { cn } from "../../lib/utils";

const STAGE_ALIASES: Record<string, number> = {
	"Parsing NZB": 0,
	"Identifying files": 1,
	"Validating segments": 2,
	"Verifying archive": 3,
	"Analyzing archive": 2,
};

const PIPELINE_STEPS = [
	{ icon: FileText, label: "Parsing NZB" },
	{ icon: Search, label: "Identifying" },
	{ icon: Download, label: "Downloading" },
	{ icon: ShieldCheck, label: "Verifying" },
];

export function resolveStageIndex(stage?: string): number {
	if (!stage) return -1;
	return STAGE_ALIASES[stage] ?? -1;
}

interface PipelineStepperProps {
	stage?: string;
	percentage: number;
	className?: string;
}

export const PipelineStepper = memo(function PipelineStepper({
	stage,
	percentage,
	className,
}: PipelineStepperProps) {
	const activeIndex = resolveStageIndex(stage);

	return (
		<div className={cn("space-y-2", className)}>
			<ul className="steps-horizontal w-full">
				{PIPELINE_STEPS.map((step, index) => {
					const Icon = step.icon;
					const isCompleted = activeIndex > index;
					const isActive = activeIndex === index;
					const isPending = activeIndex < index || activeIndex === -1;

					return (
						<li
							key={step.label}
							className={cn("step", {
								"step-primary": isCompleted || isActive,
							})}
							data-content={isCompleted ? "\u2713" : undefined}
						>
							<div className="flex flex-col items-center gap-0.5">
								<Icon
									className={cn("h-3.5 w-3.5", {
										"text-primary": isCompleted || isActive,
										"text-base-content/30": isPending,
									})}
									aria-hidden={true}
								/>
								<span
									className={cn("font-medium text-[10px]", {
										"text-primary": isCompleted || isActive,
										"text-base-content/30": isPending,
									})}
								>
									{step.label}
								</span>
							</div>
						</li>
					);
				})}
			</ul>

			<progress className="progress progress-primary h-1 w-full" value={percentage} max={100} />
		</div>
	);
});
