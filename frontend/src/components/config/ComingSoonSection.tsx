interface ComingSoonSectionProps {
	sectionName: string;
}

export function ComingSoonSection({ sectionName }: ComingSoonSectionProps) {
	return (
		<div className="py-8 text-center">
			<div className="mb-4 text-4xl">🚧</div>
			<h3 className="mb-2 font-semibold text-lg">Coming Soon</h3>
			<p className="text-base-content/70">
				The {sectionName} configuration section is not yet implemented.
			</p>
		</div>
	);
}
