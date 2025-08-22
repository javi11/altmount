import { useEffect, useState } from "react";
import { useUpdateConfigSection } from "../../hooks/useConfig";
import type { WorkersConfig, WorkersFormData } from "../../types/config";
import { ConfigSection } from "./ConfigSection";

interface WorkersFormProps {
	config: WorkersConfig;
	onUpdate?: (config: WorkersConfig) => void;
}

export function WorkersForm({ config, onUpdate }: WorkersFormProps) {
	const updateSection = useUpdateConfigSection();
	const [formData, setFormData] = useState<WorkersFormData>({
		download: config.download,
		processor: config.processor,
	});

	// Check if form has changes
	const hasChanges =
		formData.download !== config.download ||
		formData.processor !== config.processor;

	// Update form when config changes
	useEffect(() => {
		setFormData({
			download: config.download,
			processor: config.processor,
		});
	}, [config]);

	const handleInputChange = (field: keyof WorkersFormData, value: number) => {
		setFormData((prev) => ({
			...prev,
			[field]: value,
		}));
	};

	const handleSave = async () => {
		try {
			await updateSection.mutateAsync({
				section: "workers",
				config: {
					workers: {
						download: formData.download,
						processor: formData.processor,
					},
				},
			});
			onUpdate?.({
				download: formData.download,
				processor: formData.processor,
			});
		} catch (error) {
			console.error("Failed to update workers configuration:", error);
		}
	};

	const handleReset = () => {
		setFormData({
			download: config.download,
			processor: config.processor,
		});
	};

	return (
		<ConfigSection
			title="Worker Processes"
			description="Configure download and processor worker threads"
			icon="⚙️"
			canEdit={true}
			hasChanges={hasChanges}
			isLoading={updateSection.isPending}
			error={updateSection.error?.message}
			onSave={handleSave}
			onReset={handleReset}
		>
			<div className="space-y-4">
				<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
					<div className="form-control">
						<label className="label">
							<span className="label-text">Download Workers</span>
						</label>
						<input
							type="number"
							className="input input-bordered"
							value={formData.download}
							min={1}
							max={100}
							onChange={(e) =>
								handleInputChange("download", parseInt(e.target.value) || 1)
							}
						/>
						<label className="label">
							<span className="label-text-alt">
								Number of concurrent download threads (1-100)
							</span>
						</label>
					</div>
					<div className="form-control">
						<label className="label">
							<span className="label-text">Processor Workers</span>
						</label>
						<input
							type="number"
							className="input input-bordered"
							value={formData.processor}
							min={1}
							max={20}
							onChange={(e) =>
								handleInputChange("processor", parseInt(e.target.value) || 1)
							}
						/>
						<label className="label">
							<span className="label-text-alt">
								Number of NZB processing threads (1-20)
							</span>
						</label>
					</div>
				</div>

				<div className="alert alert-info">
					<div>
						<div className="font-bold">Performance Tips</div>
						<ul className="text-sm mt-2 space-y-1">
							<li>
								• More download workers = faster downloads but higher resource
								usage
							</li>
							<li>• Set based on your provider limits and system resources</li>
							<li>
								• Typical range: 10-50 download workers, 2-8 processor workers
							</li>
						</ul>
					</div>
				</div>
			</div>
		</ConfigSection>
	);
}
