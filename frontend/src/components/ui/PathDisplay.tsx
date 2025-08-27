import { Check, Copy } from "lucide-react";
import { useState } from "react";

interface PathDisplayProps {
	path: string;
	maxLength?: number;
	showFileName?: boolean;
	className?: string;
}

function smartTruncate(text: string, maxLength: number): string {
	if (text.length <= maxLength) return text;

	const start = Math.floor(maxLength * 0.4);
	const end = Math.floor(maxLength * 0.4);

	return `${text.slice(0, start)}...${text.slice(-end)}`;
}

export function PathDisplay({
	path,
	maxLength = 40,
	showFileName = false,
	className = "",
}: PathDisplayProps) {
	const [copied, setCopied] = useState(false);

	const displayText = showFileName ? path.split("/").pop() || "" : path;
	const truncatedText = smartTruncate(displayText, maxLength);
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
		<div className={`flex items-center gap-2 ${className}`}>
			<span className="text-sm">{truncatedText}</span>

			{isTextTruncated && (
				<button
					type="button"
					className="btn btn-ghost btn-xs"
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
