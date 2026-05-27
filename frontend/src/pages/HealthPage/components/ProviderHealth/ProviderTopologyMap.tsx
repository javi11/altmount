import { AlertCircle, Cpu, ShieldCheck, Target, Terminal, Network } from "lucide-react";
import { useMemo, useState } from "react";
import { formatBytes, getProviderBrandName } from "../../../../lib/utils";
import type { ProviderStatus } from "../../../../types/api";

// Calculate health score similar to calculateHealthScore helper
const getScore = (provider: ProviderStatus) => {
	let score = 100;
	if (provider.state !== "connected" && provider.state !== "active") return 0;
	if (provider.ping_ms > 1000) score -= 40;
	else if (provider.ping_ms > 500) score -= 25;
	else if (provider.ping_ms > 200) score -= 10;
	else if (provider.ping_ms > 100) score -= 5;
	score -= Math.min(30, provider.error_count * 5);
	if (provider.missing_warning) score -= 20;
	if (provider.missing_count > 5000) score -= 15;
	else if (provider.missing_count > 1000) score -= 10;
	return Math.max(0, score);
};

interface ProviderTopologyMapProps {
	providers: ProviderStatus[];
	totalSpeed: number;
}

export function ProviderTopologyMap({ providers, totalSpeed }: ProviderTopologyMapProps) {
	const count = providers.length;
	const [selectedId, setSelectedId] = useState<string | null>(null);
	const [hoveredId, setHoveredId] = useState<string | null>(null);

	// Determine active targeted node (hovered takes priority over selected)
	const activeTargetId = hoveredId || selectedId;

	const nodes = useMemo(() => {
		const cx = 55;   // System Core Center X
		const cy = 150;  // System Core Center Y
		const splitX = 95; // Point where lines split vertically
		const targetX = 310; // Target X where provider cards start

		return providers.map((provider, i) => {
			// Distribute Y coordinates vertically
			const padding = 35;
			const availableHeight = 240;
			const y = count > 1 ? padding + i * (availableHeight / (count - 1)) : 150;
			const score = getScore(provider);

			// Premium HSL neon color scheme
			let statusColor = "stroke-slate-500 fill-slate-500/10";
			let glowId = "glow-slate";
			let textColor = "text-slate-400";
			let badgeBg = "bg-slate-500/10 border-slate-500/20 text-slate-400";
			let neonFill = "fill-slate-500";
			let neonStroke = "stroke-slate-500";

			if (score >= 85) {
				statusColor = "stroke-teal-400 fill-teal-400/10";
				glowId = "glow-teal";
				textColor = "text-teal-400";
				badgeBg = "bg-teal-500/10 border-teal-500/20 text-teal-400";
				neonFill = "fill-teal-400";
				neonStroke = "stroke-teal-400";
			} else if (score >= 50) {
				statusColor = "stroke-amber-400 fill-amber-400/10";
				glowId = "glow-amber";
				textColor = "text-amber-400";
				badgeBg = "bg-amber-500/10 border-amber-500/20 text-amber-400";
				neonFill = "fill-amber-400";
				neonStroke = "stroke-amber-400";
			}

			// Speed-scaled flowing data packet calculations
			const speedBytes = provider.current_speed_bytes_per_sec || 0;
			let animDuration = "0s";
			let packetCount = 0;
			let packetColor = "fill-cyan-400";
			let packetGlow = "drop-shadow-[0_0_5px_#22d3ee]";

			if (speedBytes > 0) {
				packetCount = 2;
				animDuration = "3.2s";
				if (speedBytes > 10 * 1024 * 1024) {
					packetCount = 3;
					animDuration = "1.8s";
				}
				if (speedBytes > 50 * 1024 * 1024) {
					packetCount = 4;
					animDuration = "0.85s";
					packetColor = "fill-cyan-300";
					packetGlow = "drop-shadow-[0_0_8px_#22d3ee]";
				}
			}

			// Horizontal pipeline path calculation
			// Gateway (55, 150) -> Split (95, 150) -> Split (95, y) -> Target (310, y)
			const path = `M 55 150 L ${splitX} 150 L ${splitX} ${y} L ${targetX} ${y}`;

			return {
				provider,
				cx,
				cy,
				splitX,
				targetX,
				y,
				path,
				score,
				statusColor,
				glowId,
				textColor,
				badgeBg,
				neonFill,
				neonStroke,
				animDuration,
				packetCount,
				packetColor,
				packetGlow,
			};
		});
	}, [providers, count]);

	// Extract details for the targeted HUD panel
	const targetedNode = useMemo(() => {
		if (!activeTargetId) return null;
		return nodes.find((n) => n.provider.id === activeTargetId);
	}, [nodes, activeTargetId]);

	const totalActiveConnections = useMemo(() => {
		return providers.reduce((sum, p) => sum + (p.used_connections || 0), 0);
	}, [providers]);

	return (
		<div className="card overflow-hidden border border-white/10 bg-white/5 shadow-2xl backdrop-blur-md transition-all duration-300">
			{/* HUD Header */}
			<div className="flex items-center justify-between border-white/5 border-b p-4">
				<div>
					<h3 className="flex items-center gap-2 font-extrabold text-base text-white tracking-tight">
						<Network className="h-4 w-4 animate-pulse text-teal-400" />
						Live Connection Topology Map
					</h3>
					<p className="text-[11px] text-base-content/50 font-medium">
						Real-time cyber-telemetry of Usenet network providers, connection lanes, and line latency
					</p>
				</div>
				<div className="flex items-center gap-2 text-xs">
					{selectedId && (
						<button
							type="button"
							onClick={() => setSelectedId(null)}
							className="rounded-lg border border-teal-500/20 bg-teal-500/10 px-2.5 py-1 font-bold font-mono text-[9px] text-teal-400 transition-all hover:bg-teal-500/20 active:scale-95"
						>
							Clear Lock
						</button>
					)}
					<span className="flex items-center gap-1.5 rounded-lg border border-teal-500/20 bg-teal-500/10 px-2.5 py-1 font-bold font-mono text-[9px] text-teal-400 shadow-[0_0_8px_rgba(20,184,166,0.15)]">
						<span className="h-1.5 w-1.5 animate-ping rounded-full bg-teal-400" />
						Active:{" "}
						{providers.filter((p) => p.state === "connected" || p.state === "active").length}
					</span>
					<span className="rounded-lg border border-cyan-500/20 bg-cyan-500/10 px-2.5 py-1 font-bold font-mono text-[9px] text-cyan-400 shadow-[0_0_8px_rgba(34,211,238,0.15)]">
						Total Speed: {formatBytes(totalSpeed)}/s
					</span>
				</div>
			</div>

			<div className="relative flex min-h-[350px] w-full select-none items-center justify-center overflow-hidden bg-[#06080e] py-4">
				<svg
					className="h-[310px] w-full max-w-[800px]"
					viewBox="0 0 800 310"
					xmlns="http://www.w3.org/2000/svg"
				>
					<title>Usenet Connection Topology</title>
					<defs>
						<style>{`
							@keyframes rotate-clockwise {
								from { transform: rotate(0deg); }
								to { transform: rotate(360deg); }
							}
							@keyframes rotate-counter {
								from { transform: rotate(0deg); }
								to { transform: rotate(-360deg); }
							}
							@keyframes target-pulse {
								0%, 100% { transform: scale(1); opacity: 0.9; }
								50% { transform: scale(1.03); opacity: 0.45; }
							}
							@keyframes tech-pulse {
								0%, 100% { opacity: 0.3; }
								50% { opacity: 0.85; }
							}
							.spin-cw {
								animation: rotate-clockwise 25s linear infinite;
							}
							.spin-ccw {
								animation: rotate-counter 15s linear infinite;
							}
							.pulse-slow {
								animation: tech-pulse 3.5s ease-in-out infinite;
							}
						`}</style>

						{/* Premium Matrix Cyber-Grid Pattern */}
						<pattern id="hud-grid" width="20" height="20" patternUnits="userSpaceOnUse">
							<path d="M 20 0 L 0 0 0 20" fill="none" stroke="rgba(20, 184, 166, 0.015)" strokeWidth="0.8" />
							<circle cx="0" cy="0" r="0.8" fill="rgba(20, 184, 166, 0.05)" />
						</pattern>

						{/* Glow Filters */}
						<filter id="glow-teal" x="-45%" y="-45%" width="190%" height="190%">
							<feGaussianBlur stdDeviation="6" result="blur" />
							<feMerge>
								<feMergeNode in="blur" />
								<feMergeNode in="SourceGraphic" />
							</feMerge>
						</filter>
						<filter id="glow-amber" x="-45%" y="-45%" width="190%" height="190%">
							<feGaussianBlur stdDeviation="6" result="blur" />
							<feMerge>
								<feMergeNode in="blur" />
								<feMergeNode in="SourceGraphic" />
							</feMerge>
						</filter>
						<filter id="glow-slate" x="-45%" y="-45%" width="190%" height="190%">
							<feGaussianBlur stdDeviation="6" result="blur" />
							<feMerge>
								<feMergeNode in="blur" />
								<feMergeNode in="SourceGraphic" />
							</feMerge>
						</filter>
						<filter id="glow-primary" x="-45%" y="-45%" width="190%" height="190%">
							<feGaussianBlur stdDeviation="9" result="blur" />
							<feMerge>
								<feMergeNode in="blur" />
								<feMergeNode in="SourceGraphic" />
							</feMerge>
						</filter>
					</defs>

					{/* 1. Tactical Grid Backdrop */}
					<rect width="800" height="310" fill="url(#hud-grid)" rx="12" />

					{/* Cyber HUD Corner Brackets */}
					<path d="M 12 30 L 12 12 L 30 12" stroke="rgba(20, 184, 166, 0.35)" strokeWidth="1.5" fill="none" />
					<path d="M 788 30 L 788 12 L 770 12" stroke="rgba(20, 184, 166, 0.35)" strokeWidth="1.5" fill="none" />
					<path d="M 12 280 L 12 298 L 30 298" stroke="rgba(20, 184, 166, 0.35)" strokeWidth="1.5" fill="none" />
					<path d="M 788 280 L 788 298 L 770 298" stroke="rgba(20, 184, 166, 0.35)" strokeWidth="1.5" fill="none" />

					{/* Cyber Labels */}
					<text x="40" y="24" className="fill-teal-500/40 font-bold font-mono text-[9px] uppercase tracking-wider select-none">
						REAL-TIME BUS TOPOLOGY
					</text>
					<text x="40" y="294" className="fill-teal-500/30 font-mono text-[9px] tracking-wider select-none">
						ALTMOUNT TELEMETRY HUD v0.3.0
					</text>

					{/* 2. Connection Lanes & Neon Flows */}
					{nodes.map((node) => {
						const isStreaming = node.animDuration !== "0s";
						const isTargeted = activeTargetId === node.provider.id;
						const dimOpacity = activeTargetId && !isTargeted ? "opacity-10" : "opacity-100";

						return (
							<g key={node.provider.id} className={`transition-opacity duration-300 ${dimOpacity}`}>
								{/* Thick energy tube neon glow */}
								<path
									d={node.path}
									className={`stroke-[3.5] fill-none transition-all duration-300 ${
										isTargeted ? `${node.neonStroke} opacity-35 stroke-[4.5]` : "stroke-white/5 opacity-15"
									}`}
								/>

								{/* Primary fiber vector line */}
								<path
									d={node.path}
									className={`stroke-[1.2] fill-none transition-all duration-300 ${
										isTargeted ? `${node.neonStroke} opacity-80` : isStreaming ? "stroke-teal-500/40" : "stroke-white/10"
									}`}
									strokeDasharray={isTargeted ? "none" : "4, 4"}
								/>

								{/* Marching Neon Fiber-Optic Laser Particles (Speed Scaled) */}
								{isStreaming && node.packetCount > 0 && (
									<>
										{/* Laser Particle 1 */}
										<circle r="3" className={`${node.packetColor} ${node.packetGlow}`}>
											<animateMotion path={node.path} dur={node.animDuration} repeatCount="indefinite" />
										</circle>

										{/* Laser Particle 2 */}
										{node.packetCount >= 2 && (
											<circle r="2.5" className={`${node.packetColor} ${node.packetGlow} opacity-80`}>
												<animateMotion path={node.path} dur={node.animDuration} begin={`${parseFloat(node.animDuration) * 0.33}s`} repeatCount="indefinite" />
											</circle>
										)}

										{/* Laser Particle 3 */}
										{node.packetCount >= 3 && (
											<circle r="2" className={`${node.packetColor} ${node.packetGlow} opacity-60`}>
												<animateMotion path={node.path} dur={node.animDuration} begin={`${parseFloat(node.animDuration) * 0.66}s`} repeatCount="indefinite" />
											</circle>
										)}
									</>
								)}
							</g>
						);
					})}

					{/* 3. Central System Core (Gateway Hub) */}
					<g className="cursor-pointer">
						<circle
							cx="55"
							cy="150"
							r="32"
							className="fill-none stroke-teal-500/10 stroke-[1]"
						/>
						{/* Outer Rotating HUD Gear */}
						<circle
							cx="55"
							cy="150"
							r="28"
							className="spin-cw fill-none stroke-teal-500/35 stroke-[1.5]"
							strokeDasharray="6, 10"
							style={{ transformOrigin: "55px 150px" }}
						/>
						{/* Inner Rotating HUD Gear */}
						<circle
							cx="55"
							cy="150"
							r="22"
							className="spin-ccw fill-none stroke-cyan-400/40 stroke-[1]"
							strokeDasharray="4, 5"
							style={{ transformOrigin: "55px 150px" }}
						/>
						{/* Central Glow Orb */}
						<circle
							cx="55"
							cy="150"
							r="16"
							className="fill-[#080b11] stroke-[2] stroke-teal-500"
							filter="url(#glow-primary)"
						/>
						<g transform="translate(46, 141)" className="pointer-events-none text-teal-400 pulse-slow">
							<Cpu className="h-[18px] w-[18px] stroke-[1.5] stroke-teal-400" />
						</g>
						<text
							x="55"
							y="194"
							textAnchor="middle"
							className="fill-teal-400/80 font-bold font-mono text-[8px] uppercase tracking-widest select-none"
						>
							GATEWAY
						</text>
					</g>

					{/* 4. Elegant Rectangular Pipeline Nodes */}
					{nodes.map((node) => {
						const isTargeted = activeTargetId === node.provider.id;
						const isSelected = selectedId === node.provider.id;
						const dimOpacity = activeTargetId && !isTargeted ? "opacity-20" : "opacity-100";

						// Dimensions for the card module
						const cardWidth = 215;
						const cardHeight = 36;
						const cardX = node.targetX;
						const cardY = node.y - cardHeight / 2;

						return (
							<g
								key={node.provider.id}
								className={`group cursor-pointer transition-all duration-300 ${dimOpacity}`}
								onMouseEnter={() => setHoveredId(node.provider.id)}
								onMouseLeave={() => setHoveredId(null)}
								onClick={() => setSelectedId(isSelected ? null : node.provider.id)}
							>
								{/* Target Bracket Overlay */}
								{isTargeted && (
									<rect
										x={cardX - 4}
										y={cardY - 4}
										width={cardWidth + 8}
										height={cardHeight + 8}
										rx="6"
										className="fill-none stroke-cyan-400/60 stroke-[1]"
										style={{
											animation: "target-pulse 2s ease-in-out infinite",
										}}
									/>
								)}

								{/* Glassmorphic Node Card Background */}
								<rect
									x={cardX}
									y={cardY}
									width={cardWidth}
									height={cardHeight}
									rx="4"
									className={`transition-all duration-300 ${
										isTargeted
											? "fill-[#0c1424] stroke-cyan-500/40 stroke-[1]"
											: "fill-[#080d15]/85 stroke-white/5 stroke-[0.8]"
									}`}
								/>

								{/* Interactive Ambient Glow for healthy active paths */}
								{isTargeted && (
									<rect
										x={cardX}
										y={cardY}
										width={cardWidth}
										height={cardHeight}
										rx="4"
										className={`fill-none stroke-[1.5] ${node.neonStroke}`}
										filter={`url(#${node.glowId})`}
										style={{ opacity: 0.15 }}
									/>
								)}

								{/* Left Brand Active Status Indicator Dot */}
								<circle
									cx={cardX + 14}
									cy={node.y}
									r="3.5"
									className={`transition-colors duration-300 ${
										node.score >= 85
											? "fill-teal-400 drop-shadow-[0_0_3px_#14b8a6]"
											: node.score >= 50
												? "fill-amber-400 drop-shadow-[0_0_3px_#f59e0b]"
												: "fill-slate-500"
									}`}
								/>

								{/* Brand Name Label */}
								<text
									x={cardX + 28}
									y={node.y - 2}
									className="fill-white font-extrabold font-mono text-[9.5px] tracking-wide"
								>
									{getProviderBrandName(node.provider.host).toUpperCase()}
								</text>

								{/* Provider Telemetry Stats Line */}
								<text
									x={cardX + 28}
									y={node.y + 9}
									className="fill-slate-400 font-semibold font-mono text-[7.5px]"
								>
									{node.provider.ping_ms > 0 ? `${node.provider.ping_ms}ms` : "down"} // {node.provider.used_connections}/{node.provider.max_connections} lanes
								</text>

								{/* Micro Connection Thread Indicators */}
								<g transform={`translate(${cardX + 160}, ${node.y - 7})`}>
									{[0, 1, 2, 3, 4].map((idx) => {
										const maxC = node.provider.max_connections || 1;
										const ratio = (node.provider.used_connections || 0) / maxC;
										const activeIdx = Math.ceil(ratio * 5);
										const isThreadActive = idx < activeIdx && node.provider.used_connections > 0;

										return (
											<rect
												key={`thread-${node.provider.id}-${idx}`}
												x={idx * 6}
												y="0"
												width="3.5"
												height="14"
												rx="0.5"
												className={`transition-colors duration-300 ${
													isThreadActive
														? "fill-cyan-400 drop-shadow-[0_0_2px_rgba(34,211,238,0.7)]"
														: "fill-white/10"
												}`}
											/>
										);
									})}
								</g>
							</g>
						);
					})}

					{/* 5. Holographic Sidebar Telemetry HUD Panel */}
					<g transform="translate(0, 0)">
						{/* HUD translucent panel glass */}
						<rect
							x="545"
							y="20"
							width="235"
							height="265"
							fill="rgba(8, 12, 20, 0.92)"
							stroke="rgba(20, 184, 166, 0.3)"
							strokeWidth="1.2"
							rx="8"
							className="backdrop-blur-md shadow-2xl"
						/>
						{/* HUD Lines */}
						<line x1="545" y1="46" x2="780" y2="46" stroke="rgba(20, 184, 166, 0.2)" strokeWidth="1" />
						<line x1="545" y1="245" x2="780" y2="245" stroke="rgba(20, 184, 166, 0.2)" strokeWidth="0.8" strokeDasharray="3, 3" />

						{/* Header */}
						<g transform="translate(557, 34)">
							<Target className={`h-3.5 w-3.5 ${targetedNode ? "text-cyan-400 animate-pulse" : "text-teal-500/50"}`} />
							<text x="18" y="10" className="fill-cyan-400 font-bold font-mono text-[10px] uppercase tracking-widest">
								{targetedNode ? "TARGET LOCKED" : "SYSTEM DIAGNOSTICS"}
							</text>
						</g>

						{/* Interactive HUD Readouts */}
						{!targetedNode ? (
							/* CORE SYSTEM READOUT (System Monitor Mode) */
							<g transform="translate(557, 62)">
								<g transform="translate(0, 0)">
									<text x="0" y="10" className="fill-slate-500 font-bold font-mono text-[10px] uppercase tracking-wider">
										SYSTEM:
									</text>
									<text x="75" y="10" className="fill-teal-400 font-bold font-mono text-[10px] tracking-wide">
										ONLINE // ACTIVE
									</text>

									<text x="0" y="24" className="fill-slate-500 font-mono text-[10px]">
										PROVIDERS:
									</text>
									<text x="75" y="24" className="fill-slate-300 font-mono text-[10px]">
										{nodes.length} INDEXED HUBS
									</text>

									<text x="0" y="38" className="fill-slate-500 font-mono text-[10px]">
										CONNS:
									</text>
									<text x="75" y="38" className="fill-cyan-400 font-bold font-mono text-[10px]">
										{totalActiveConnections} THREADS
									</text>

									<text x="0" y="52" className="fill-slate-500 font-mono text-[10px]">
										SECURITY:
									</text>
									<text x="75" y="52" className="fill-teal-500/80 font-bold font-mono text-[10px]">
										TLSv1.3 STABLE
									</text>
								</g>

								{/* Combined speed throughput visualization */}
								<g transform="translate(0, 68)">
									<text x="0" y="10" className="fill-slate-500 font-bold font-mono text-[10px] uppercase tracking-wider">
										COMBINED FLOW:
									</text>
									<text x="0" y="24" className="fill-cyan-400 font-black font-mono text-[14px]">
										{formatBytes(totalSpeed)}/s
									</text>

									{/* Animated Wave visualizer matrix */}
									<g transform="translate(0, 36)">
										{[0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19].map((idx) => {
											const hVal = 4 + Math.sin(idx * 0.6 + (totalSpeed > 0 ? Date.now() / 250 : 0)) * 6 + 7;
											return (
												<rect
													key={`wave-${idx}`}
													x={idx * 11}
													y={25 - hVal}
													width="4"
													height={hVal}
													className="fill-teal-500/25 stroke-none"
												/>
											);
										})}
									</g>
								</g>

								{/* Interactive tooltip */}
								<g transform="translate(0, 138)">
									<rect x="0" y="0" width="211" height="34" rx="4" className="fill-teal-950/20 stroke-[0.8] stroke-teal-500/10" />
									<Terminal className="absolute x-2.5 y-2.5 h-3.5 w-3.5 text-teal-400/60" />
									<text x="24" y="20" className="fill-teal-400/80 font-semibold font-mono text-[9px] tracking-wide">
										SELECT A LANE FOR TELEMETRY
									</text>
								</g>
							</g>
						) : (
							/* TARGET SPECIFIC NODE DATA (Target Details Mode) */
							<g transform="translate(557, 60)">
								<g transform="translate(0, 4)">
									<text x="0" y="8" className="fill-slate-500 font-mono text-[10px]">
										PROVIDER:
									</text>
									<text x="75" y="8" className="fill-white font-black font-mono text-[10px] tracking-widest">
										{getProviderBrandName(targetedNode.provider.host).toUpperCase()}
									</text>

									<text x="0" y="21" className="fill-slate-500 font-mono text-[10px]">
										HOST:
									</text>
									<text x="75" y="21" className="fill-slate-300 font-mono text-[9px] truncate">
										{targetedNode.provider.host}
									</text>
								</g>

								<line x1="0" y1="31" x2="211" y2="31" stroke="rgba(20, 184, 166, 0.15)" strokeWidth="0.8" />

								{/* Host diagnostics metrics */}
								<g transform="translate(0, 44)">
									<text x="0" y="8" className="fill-slate-500 font-mono text-[10px]">
										STATUS:
									</text>
									<text x="80" y="8" className={`font-black font-mono text-[10px] ${targetedNode.textColor}`}>
										{targetedNode.score >= 85 ? "EXCELLENT" : targetedNode.score >= 50 ? "GOOD" : "OPERATIONAL"}
									</text>

									<text x="0" y="21" className="fill-slate-500 font-mono text-[10px]">
										LATENCY:
									</text>
									<text x="80" y="21" className="fill-slate-200 font-bold font-mono text-[10px]">
										{targetedNode.provider.ping_ms > 0 ? `${targetedNode.provider.ping_ms} ms` : "OFFLINE"}
									</text>

									<text x="0" y="34" className="fill-slate-500 font-mono text-[10px]">
										LANES:
									</text>
									<text x="80" y="34" className="fill-cyan-400 font-bold font-mono text-[10px]">
										{targetedNode.provider.used_connections} / {targetedNode.provider.max_connections} ACTIVE
									</text>

									<text x="0" y="47" className="fill-slate-500 font-mono text-[10px]">
										SPEED:
									</text>
									<text x="80" y="47" className="fill-cyan-300 font-bold font-mono text-[10px]">
										{formatBytes(targetedNode.provider.current_speed_bytes_per_sec)}/s
									</text>

									<text x="0" y="60" className="fill-slate-500 font-mono text-[10px]">
										24H TOTAL:
									</text>
									<text x="80" y="60" className="fill-slate-200 font-mono text-[10px]">
										{formatBytes(targetedNode.provider.byte_count_24h || 0)}
									</text>

									<text x="0" y="73" className="fill-slate-500 font-mono text-[10px]">
										FAILURES:
									</text>
									<text x="80" y="73" className={`font-bold font-mono text-[10px] ${targetedNode.provider.error_count > 0 ? "text-amber-500 animate-pulse" : "text-slate-500"}`}>
										{targetedNode.provider.error_count} FAILURES
									</text>
								</g>

								<line x1="0" y1="126" x2="211" y2="126" stroke="rgba(20, 184, 166, 0.15)" strokeWidth="0.8" />

								{/* Action lane health overlay */}
								<g transform="translate(0, 134)">
									{targetedNode.provider.state === "connected" || targetedNode.provider.state === "active" ? (
										<g>
											<rect x="0" y="0" width="211" height="34" rx="4" className="fill-teal-950/20 stroke-[0.8] stroke-teal-500/15" />
											<ShieldCheck className="absolute x-2.5 y-2.5 h-3.5 w-3.5 text-teal-400" />
											<text x="26" y="14" className="fill-teal-400 font-bold font-mono text-[7.5px] tracking-wide animate-pulse">
												SECURE LANE ACTIVE
											</text>
											<text x="26" y="24" className="fill-teal-400/70 font-mono text-[7px]">
												SIGNAL FLOW STABLE // NO DATA LOSS
											</text>
										</g>
									) : (
										<g>
											<rect x="0" y="0" width="211" height="34" rx="4" className="fill-slate-950/40 stroke-[0.8] stroke-slate-500/15" />
											<AlertCircle className="absolute x-2.5 y-2.5 h-3.5 w-3.5 text-slate-400" />
											<text x="26" y="14" className="fill-slate-400 font-bold font-mono text-[7.5px] tracking-wide">
												LINK CONNECTION STANDBY
											</text>
											<text x="26" y="24" className="fill-slate-400/60 font-mono text-[7px] truncate max-w-[170px]">
												{targetedNode.provider.failure_reason || "STANDBY LANES WAITING DISPATCH"}
											</text>
										</g>
									)}
								</g>
							</g>
						)}

						{/* Footer status ticks */}
						<g transform="translate(557, 252)">
							<rect x="0" y="0" width="6" height="6" className="fill-teal-500/30" />
							<rect x="10" y="0" width="6" height="6" className="fill-teal-500/30" />
							<rect x="20" y="0" width="6" height="6" className="fill-teal-500/30" />
							<text x="35" y="6" className="fill-slate-500 font-mono text-[9px] uppercase tracking-wide">
								{targetedNode ? `STATUS: ${targetedNode.provider.state.toUpperCase()}` : "HUD ONLINE..."}
							</text>
						</g>
					</g>
				</svg>

				{/* Floating high-tech legends */}
				<div className="absolute bottom-3 left-4 flex flex-wrap gap-3 font-mono text-[9px] text-slate-400 select-none">
					<div className="flex items-center gap-1">
						<span className="h-1.5 w-1.5 rounded-full bg-teal-400 shadow-[0_0_6px_rgba(20,184,166,0.7)]" />
						<span>Excellent (85%+)</span>
					</div>
					<div className="flex items-center gap-1">
						<span className="h-1.5 w-1.5 rounded-full bg-amber-400 shadow-[0_0_6px_rgba(245,158,11,0.7)]" />
						<span>Good (50-84%)</span>
					</div>
					<div className="flex items-center gap-1">
						<span className="h-1.5 w-1.5 rounded-full bg-slate-500 shadow-[0_0_6px_rgba(148,163,184,0.7)]" />
						<span>Operational (&lt;50%)</span>
					</div>
				</div>
			</div>
		</div>
	);
}
