import type { LucideIcon } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import {
	Area,
	AreaChart,
	CartesianGrid,
	Cell,
	DefaultLegendContent,
	Legend,
	type LegendPayload,
	Pie,
	PieChart,
	ResponsiveContainer,
	Tooltip,
	XAxis,
	YAxis,
} from "recharts";

// Multi-series palette sourced from the active daisyUI theme so chart colors
// follow the selected theme instead of fixed hex values.
const CHART_COLORS = [
	"var(--color-primary)",
	"var(--color-success)",
	"var(--color-warning)",
	"var(--color-error)",
	"var(--color-secondary)",
	"var(--color-accent)",
	"var(--color-info)",
];

export type ChartDatum = Record<string, string | number>;

export interface TimeRangeTab {
	label: string;
	value: number;
}

interface TooltipPayloadItem {
	value: number;
	dataKey: string;
	stroke: string;
}

interface CustomTooltipProps {
	active?: boolean;
	payload?: TooltipPayloadItem[];
	label?: string;
	formatValue: (value: number) => string;
	totalClassName: string;
}

function CustomTooltip({
	active,
	payload,
	label,
	formatValue,
	totalClassName,
}: CustomTooltipProps) {
	if (!active || !payload || payload.length === 0) return null;

	const sortedPayload = [...payload].sort((a, b) => b.value - a.value);
	const sum = payload.reduce((acc, p) => acc + p.value, 0);

	return (
		<div className="z-50 min-w-[220px] rounded-xl border border-base-200/50 bg-base-100 p-4 text-xs shadow-2xl">
			<p className="mb-2 border-base-200/30 border-b pb-1.5 font-bold text-base-content/80">
				{label}
			</p>
			<div className="max-h-48 space-y-1.5 overflow-y-auto pr-1">
				{sortedPayload.map((p) => (
					<div key={p.dataKey} className="flex items-center justify-between gap-4 py-0.5">
						<div className="flex items-center gap-1.5">
							<span className="h-2 w-2 rounded-full" style={{ backgroundColor: p.stroke }} />
							<span className="font-medium text-base-content/75">{p.dataKey}:</span>
						</div>
						<span className="font-mono font-semibold text-base-content">
							{formatValue(p.value)}
						</span>
					</div>
				))}
			</div>
			<div className="mt-3 flex justify-between border-base-200/30 border-t pt-2 font-bold text-sm">
				<span className="text-base-content/70">Total:</span>
				<span className={`font-mono ${totalClassName}`}>{formatValue(sum)}</span>
			</div>
		</div>
	);
}

interface PieTooltipProps {
	active?: boolean;
	payload?: Array<{ name?: string; value?: number; payload?: { fill?: string } }>;
	formatValue: (value: number) => string;
}

/** Breakdown pie tooltip — text tinted with the hovered slice's color. */
function PieTooltip({ active, payload, formatValue }: PieTooltipProps) {
	const item = payload?.[0];
	if (!active || !item || typeof item.value !== "number") return null;

	return (
		<div
			className="shadow-2xl"
			style={{
				borderRadius: "12px",
				border: "1px solid color-mix(in oklch, var(--color-base-content) 10%, transparent)",
				backgroundColor: "color-mix(in oklch, var(--color-base-100) 95%, transparent)",
				fontSize: "11px",
				backdropFilter: "blur(8px)",
				padding: "8px 12px",
			}}
		>
			<span style={{ color: item.payload?.fill ?? "var(--color-base-content)" }}>
				{item.name} : {formatValue(item.value)}
			</span>
		</div>
	);
}

/**
 * Keeps an active/inactive toggle map in sync with the available providers,
 * defaulting newly seen providers to active.
 */
function useActiveProviders(providers: string[]) {
	const [activeProviders, setActiveProviders] = useState<Record<string, boolean>>({});

	useEffect(() => {
		if (providers.length > 0) {
			setActiveProviders((prev) => {
				const next = { ...prev };
				let changed = false;
				for (const p of providers) {
					if (next[p] === undefined) {
						next[p] = true;
						changed = true;
					}
				}
				return changed ? next : prev;
			});
		}
	}, [providers]);

	const toggleProvider = (provider: string) => {
		setActiveProviders((prev) => ({
			...prev,
			[provider]: !prev[provider],
		}));
	};

	return { activeProviders, toggleProvider };
}

interface TimeRangeTabsProps {
	tabs: TimeRangeTab[];
	value: number;
	onChange: (value: number) => void;
	activeClassName: string;
}

function TimeRangeTabs({ tabs, value, onChange, activeClassName }: TimeRangeTabsProps) {
	return (
		<div className="join rounded-xl border border-base-200/40 bg-base-200/50 p-0.5">
			{tabs.map((tab) => (
				<button
					type="button"
					key={tab.label}
					className={`join-item btn btn-sm btn-ghost rounded-lg px-3.5 font-bold text-xs transition-all ${
						value === tab.value ? activeClassName : "text-base-content/60 hover:text-base-content"
					}`}
					onClick={() => onChange(tab.value)}
				>
					{tab.label}
				</button>
			))}
		</div>
	);
}

interface ProviderAreaChartProps {
	/** Icon shown next to the title. */
	icon: LucideIcon;
	iconClassName: string;
	title: string;
	subtitle: ReactNode;
	/** Time-range tabs configuration. */
	tabs: TimeRangeTab[];
	days: number;
	onDaysChange: (value: number) => void;
	tabActiveClassName: string;
	/** Chart data rows, each keyed by provider host. */
	chartData: ChartDatum[];
	/** Providers sorted by magnitude (controls color + draw order). */
	providers: string[];
	/** Per-provider value used for the breakdown pie. */
	providerValues: Record<string, number>;
	/** Unique prefix for the gradient ids (avoids collisions between charts). */
	gradientPrefix: string;
	/** Tooltip / breakdown value formatter. */
	formatValue: (value: number) => string;
	tooltipTotalClassName: string;
	/** Tailwind class for the tooltip total accent (passed through to CustomTooltip). */
	yAxisTickFormatter?: (value: number) => string;
	yAxisUnit?: string;
	/** Connect area segments across null gaps (used by the speed chart). */
	connectNulls?: boolean;
	breakdownLabel: string;
}

export function ProviderAreaChart({
	icon: Icon,
	iconClassName,
	title,
	subtitle,
	tabs,
	days,
	onDaysChange,
	tabActiveClassName,
	chartData,
	providers,
	providerValues,
	gradientPrefix,
	formatValue,
	tooltipTotalClassName,
	yAxisTickFormatter,
	yAxisUnit,
	connectNulls,
	breakdownLabel,
}: ProviderAreaChartProps) {
	const { activeProviders, toggleProvider } = useActiveProviders(providers);

	const pieData = providers
		.map((p) => ({
			name: p,
			value: providerValues[p],
		}))
		.filter((d) => activeProviders[d.name]);

	return (
		<div className="card rounded-2xl border border-base-200/60 bg-base-100/40 shadow-xl backdrop-blur-sm">
			<div className="card-body p-6">
				<div className="mb-6 flex flex-col items-start justify-between gap-4 lg:flex-row lg:items-center">
					<div>
						<h2 className="card-title flex items-center gap-2 font-bold text-lg">
							<Icon className={`h-5 w-5 animate-pulse ${iconClassName}`} />
							{title}
						</h2>
						<p className="mt-0.5 text-base-content/60 text-xs">{subtitle}</p>
					</div>

					{/* Premium Segmented Timeline Controller */}
					<TimeRangeTabs
						tabs={tabs}
						value={days}
						onChange={onDaysChange}
						activeClassName={tabActiveClassName}
					/>
				</div>

				<div className="flex h-80 w-full flex-col gap-6 lg:flex-row">
					<div className="h-full w-full flex-grow lg:w-3/4">
						<ResponsiveContainer width="100%" height="100%">
							<AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
								<defs>
									{providers.map((p, i) => (
										<linearGradient
											key={`${gradientPrefix}${p}`}
											id={`${gradientPrefix}${p}`}
											x1="0"
											y1="0"
											x2="0"
											y2="1"
										>
											<stop
												offset="5%"
												stopColor={CHART_COLORS[i % CHART_COLORS.length]}
												stopOpacity={0.45}
											/>
											<stop
												offset="95%"
												stopColor={CHART_COLORS[i % CHART_COLORS.length]}
												stopOpacity={0.02}
											/>
										</linearGradient>
									))}
								</defs>
								<CartesianGrid strokeDasharray="3 3" opacity={0.04} vertical={false} />
								<XAxis
									dataKey="name"
									tick={{ fontSize: 10, fill: "currentColor", opacity: 0.7 }}
									axisLine={false}
									tickLine={false}
								/>
								<YAxis
									tick={{ fontSize: 10, fill: "currentColor", opacity: 0.7 }}
									axisLine={false}
									tickLine={false}
									tickFormatter={yAxisTickFormatter}
									unit={yAxisUnit}
								/>
								<Tooltip
									content={
										<CustomTooltip
											formatValue={formatValue}
											totalClassName={tooltipTotalClassName}
										/>
									}
									cursor={{
										stroke: "var(--color-base-content)",
										strokeOpacity: 0.1,
										strokeWidth: 1,
									}}
									isAnimationActive={false}
									// z-index must live on the recharts wrapper: it's positioned
									// with transform (own stacking context), so the legend would
									// otherwise paint over the tooltip and clip the Total row.
									wrapperStyle={{ zIndex: 50 }}
								/>
								<Legend
									onClick={(e) => {
										if (typeof e.dataKey === "string") {
											toggleProvider(e.dataKey);
										}
									}}
									wrapperStyle={{ cursor: "pointer", fontSize: "11px", paddingTop: "15px" }}
									content={
										<DefaultLegendContent
											payload={providers.map<LegendPayload>((p, i) => ({
												value: p,
												type: "circle",
												color: CHART_COLORS[i % CHART_COLORS.length],
												dataKey: p,
												inactive: !activeProviders[p],
											}))}
											formatter={(value, entry) => (
												<span
													style={{
														color: "inherit",
														opacity: entry?.inactive ? 0.4 : 1,
														textDecoration: !entry?.inactive ? "none" : "line-through",
														paddingRight: "8px",
													}}
												>
													{value}
												</span>
											)}
										/>
									}
								/>
								{[...providers].reverse().map((p) => {
									const i = providers.indexOf(p);
									const color = CHART_COLORS[i % CHART_COLORS.length];
									return (
										activeProviders[p] && (
											<Area
												key={p}
												dataKey={p}
												type="monotone"
												stroke={color}
												fill={`url(#${gradientPrefix}${p})`}
												strokeWidth={2}
												activeDot={{ r: 5, strokeWidth: 0, fill: color }}
												connectNulls={connectNulls}
											/>
										)
									);
								})}
							</AreaChart>
						</ResponsiveContainer>
					</div>
					<div className="flex hidden h-full w-full flex-col items-center justify-center border-base-200/40 border-l pl-4 lg:flex lg:w-1/4">
						<span className="mb-2 font-bold text-base-content/70 text-xs uppercase tracking-wider">
							{breakdownLabel}
						</span>
						<ResponsiveContainer width="100%" height="100%">
							<PieChart>
								<Pie
									data={pieData}
									innerRadius={55}
									outerRadius={75}
									paddingAngle={4}
									dataKey="value"
								>
									{pieData.map((entry, index) => (
										<Cell
											key={`cell-${gradientPrefix}-${index}`}
											fill={CHART_COLORS[providers.indexOf(entry.name) % CHART_COLORS.length]}
										/>
									))}
								</Pie>
								<Tooltip
									content={<PieTooltip formatValue={formatValue} />}
									isAnimationActive={false}
									wrapperStyle={{ zIndex: 50 }}
								/>
							</PieChart>
						</ResponsiveContainer>
					</div>
				</div>
			</div>
		</div>
	);
}
