interface LogoProps {
	width?: number;
	height?: number;
	className?: string;
}

export function Logo({ width, height, className }: LogoProps) {
	// If className is provided, use it directly; otherwise use width/height props
	const containerClass = className
		? `flex items-center justify-center overflow-hidden ${className}`
		: `flex items-center justify-center overflow-hidden ${width ? `w-${width}` : "w-12"} ${height ? `h-${height}` : "h-12"}`;

	return (
		<div className={containerClass}>
			<img src="/logo.jpg" alt="AltMount Logo" className="object-contain" />
		</div>
	);
}
