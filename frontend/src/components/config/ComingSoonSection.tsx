interface ComingSoonSectionProps {
	sectionName: string;
}

export function ComingSoonSection({ sectionName }: ComingSoonSectionProps) {
	return (
		<div className="text-center py-8">
			<div className="text-4xl mb-4">🚧</div>
			<h3 className="text-lg font-semibold mb-2">Coming Soon</h3>
			<p className="text-base-content/70">
				The {sectionName} configuration section is not yet implemented.
			</p>
		</div>
	);
}
