import { Check, Copy } from "lucide-react";
import { useState } from "react";

interface PathDisplayProps {
	path: string;
	maxLength?: number;
	showFileName?: boolean;
	className?: string;
}

export function PathDisplay({
	path,
	maxLength = 40,
	showFileName = false,
	className = "",
}: PathDisplayProps) {
	const [copied, setCopied] = useState(false);

	const displayText = showFileName ? path.split("/").pop() || "" : path;
	const isTextTruncated = displayText.length > maxLength;

	const handleCopy = async () => {
		try {
			await navigator.clipboard.writeText(path);
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		} catch (error) {
			console.error("Failed to copy path:", error);
		}
	};

	return (
		<div
			className={`flex w-full min-w-0 max-w-full items-center gap-2 overflow-hidden ${className}`}
		>
			<span className="block min-w-0 flex-1 break-all text-sm">{displayText}</span>

			{isTextTruncated && (
				<button
					type="button"
					className="btn btn-ghost btn-sm shrink-0"
					onClick={handleCopy}
					aria-label={`Copy ${showFileName ? "file path" : "path"} to clipboard`}
					title={copied ? "Copied!" : "Copy to clipboard"}
				>
					{copied ? <Check className="h-3 w-3 text-success" /> : <Copy className="h-3 w-3" />}
				</button>
			)}
		</div>
	);
}
