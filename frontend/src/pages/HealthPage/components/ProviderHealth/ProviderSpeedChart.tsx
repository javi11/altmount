import { useMemo, useState } from "react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis, Legend, PieChart, Pie, Cell } from "recharts";
import { useProviderSpeedHistory, usePoolMetrics } from "../../../../hooks/useApi";
import { LoadingSpinner } from "../../../../components/ui/LoadingSpinner";
import { Activity } from "lucide-react";

const COLORS = [
	"#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#ec4899", "#06b6d4"
];

const CustomTooltip = ({ active, payload, label }: any) => {
	if (!active || !payload) return null;

	const sortedPayload = [...payload].sort((a, b) => b.value - a.value);
	const sum = payload.reduce((acc: number, p: any) => acc + p.value, 0);

	return (
		<div className="z-50 rounded-lg border border-base-200 bg-base-100 p-3 text-sm shadow-xl">
			<p className="mb-2 border-base-200 border-b pb-1 font-bold">{label}</p>
			{sortedPayload.map((p) => (
				<div key={p.dataKey} className="flex justify-between gap-4 py-0.5">
					<span style={{ color: p.stroke }} className="font-medium">{p.dataKey}:</span>
					<span>{p.value.toFixed(2)} Mbps</span>
				</div>
			))}
			<div className="mt-2 flex justify-between border-base-200 border-t pt-1 font-bold">
				<span>Total:</span>
				<span>{sum.toFixed(2)} Mbps</span>
			</div>
		</div>
	);
};

export function ProviderSpeedChart() {
	const [days, setDays] = useState(7);
	const [activeProviders, setActiveProviders] = useState<Record<string, boolean>>({});
	const { data: historyResponse, isLoading: historyLoading } = useProviderSpeedHistory(days);
    const { data: poolData } = usePoolMetrics();

	const { chartData, providers, providerMaxes } = useMemo(() => {
		if (!historyResponse?.history) return { chartData: [], providers: [], providerMaxes: {} };

		const grouped: Record<string, any> = {};
        const maxes: Record<string, number> = {};
		
		historyResponse.history.forEach(stat => {
			const date = new Date(stat.created_at);
            const timestamp = date.toLocaleString(undefined, {
                month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
            });
            
            if (!grouped[timestamp]) {
                grouped[timestamp] = { name: timestamp };
            }
            
            const provider = poolData?.providers.find(p => p.id === stat.provider_id);
            const label = provider ? provider.host : stat.provider_id;
            
            grouped[timestamp][label] = stat.speed_mbps;
            maxes[label] = Math.max(maxes[label] || 0, stat.speed_mbps);
		});

        const sortedProviders = Object.keys(maxes).sort((a, b) => maxes[b] - maxes[a]);

		return { chartData: Object.values(grouped), providers: sortedProviders, providerMaxes: maxes };
	}, [historyResponse, poolData]);

    // Initialize active providers when providers load
	useMemo(() => {
		if (providers.length > 0 && Object.keys(activeProviders).length === 0) {
			const initialActive: Record<string, boolean> = {};
			for (const p of providers) {
				initialActive[p] = true;
			}
			setActiveProviders(initialActive);
		}
	}, [providers, activeProviders]);

	if (historyLoading) return <div className="flex h-64 items-center justify-center"><LoadingSpinner size="lg" /></div>;

    const toggleProvider = (provider: string) => {
		setActiveProviders(prev => ({
			...prev,
			[provider]: !prev[provider]
		}));
	};

	const pieData = providers.map(p => ({
		name: p,
		value: providerMaxes[p]
	})).filter(d => activeProviders[d.name]);

	return (
		<div className="card border border-base-200 bg-base-100 shadow-xl">
			<div className="card-body p-6">
				<div className="mb-6 flex flex-col items-start justify-between gap-4 lg:flex-row lg:items-center">
					<div>
						<h2 className="card-title flex items-center gap-2 text-lg">
							<Activity className="h-5 w-5 text-success" />
							Speed Performance History
						</h2>
						<p className="text-base-content/60 text-xs">Top speed (Mbps) per provider over time (stacked)</p>
					</div>
                    <div className="flex items-center gap-2">
						<span className="text-sm">Days:</span>
						<input 
							type="number" 
							className="input input-bordered input-sm w-20"
							value={days}
							onChange={(e) => setDays(Number(e.target.value))}
						/>
					</div>
				</div>

				<div className="flex h-80 w-full flex-col gap-6 lg:flex-row">
					<div className="h-full w-full flex-grow lg:w-3/4">
						<ResponsiveContainer width="100%" height="100%">
							<AreaChart data={chartData} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
								<defs>
									{providers.map((p, i) => (
										<linearGradient key={`colorSpeed${p}`} id={`colorSpeed${p}`} x1="0" y1="0" x2="0" y2="1">
											<stop offset="5%" stopColor={COLORS[i % COLORS.length]} stopOpacity={0.8}/>
											<stop offset="95%" stopColor={COLORS[i % COLORS.length]} stopOpacity={0.1}/>
										</linearGradient>
									))}
								</defs>
								<CartesianGrid strokeDasharray="3 3" opacity={0.1} vertical={false} />
								<XAxis dataKey="name" tick={{ fontSize: 10 }} axisLine={false} tickLine={false} />
								<YAxis tick={{ fontSize: 10 }} axisLine={false} tickLine={false} unit=" Mbps" />
								<Tooltip content={<CustomTooltip />} />
								<Legend 
                                    onClick={(e: any) => toggleProvider(e.dataKey as string)} 
                                    wrapperStyle={{ cursor: 'pointer', fontSize: '12px' }}
                                    {...({
                                        payload: providers.map((p, i) => ({
                                            value: p,
                                            type: 'rect',
                                            id: p,
                                            color: COLORS[i % COLORS.length],
                                            dataKey: p,
                                            inactive: !activeProviders[p]
                                        }))
                                    } as any)}
                                    formatter={(value, entry: any) => (
                                        <span style={{ color: !entry.inactive ? 'inherit' : '#999', textDecoration: !entry.inactive ? 'none' : 'line-through' }}>
                                            {value}
                                        </span>
                                    )}
                                />
								{[...providers].reverse().map((p) => {
                                    const i = providers.indexOf(p);
                                    const color = COLORS[i % COLORS.length];
                                    return activeProviders[p] && (
                                        <Area 
                                            key={p} 
                                            dataKey={p} 
                                            type="monotone"
                                            stackId="1" 
                                            stroke={color} 
                                            fill={`url(#colorSpeed${p})`} 
                                            strokeWidth={2} 
                                            activeDot={{ r: 6, strokeWidth: 0 }}
                                            connectNulls
                                        />
                                    );
                                })}
							</AreaChart>
						</ResponsiveContainer>
					</div>
                    <div className="flex hidden h-full w-full flex-col items-center justify-center border-base-200/50 border-l pl-4 lg:flex lg:w-1/4">
						<span className="mb-2 font-semibold text-base-content/70 text-xs">Peak Performance</span>
						<ResponsiveContainer width="100%" height="100%">
							<PieChart>
								<Pie
									data={pieData}
									innerRadius={60}
									outerRadius={80}
									paddingAngle={5}
									dataKey="value"
								>
									{pieData.map((entry, index) => (
										<Cell key={`cell-speed-${index}`} fill={COLORS[providers.indexOf(entry.name) % COLORS.length]} />
									))}
								</Pie>
								<Tooltip 
									formatter={(value: number) => `${value.toFixed(2)} Mbps`}
									contentStyle={{ borderRadius: '8px', border: '1px solid hsl(var(--bc) / 0.2)', backgroundColor: 'hsl(var(--b1))', fontSize: '12px' }}
								/>
							</PieChart>
						</ResponsiveContainer>
					</div>
				</div>
			</div>
		</div>
	);
}
