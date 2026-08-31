import { Check, Copy, QrCode, Tv, X } from "lucide-react";
import { useMemo, useState } from "react";
import { copyToClipboard } from "../../../lib/utils";
import { generateQrMatrix } from "./qrGenerator";

interface QrCodeModalProps {
	isOpen: boolean;
	onClose: () => void;
	manifestUrl: string;
}

export function QrCodeModal({ isOpen, onClose, manifestUrl }: QrCodeModalProps) {
	const [copied, setCopied] = useState(false);

	const matrix = useMemo(() => {
		if (!manifestUrl) return [];
		try {
			return generateQrMatrix(manifestUrl);
		} catch (e) {
			console.error("QR Code generation error:", e);
			return [];
		}
	}, [manifestUrl]);

	const handleCopy = async () => {
		const ok = await copyToClipboard(manifestUrl);
		if (ok) {
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		}
	};

	if (!isOpen) return null;

	const size = matrix.length;
	const padding = 3;
	const totalSize = size + padding * 2;

	return (
		<div className="modal modal-open z-50">
			<div className="modal-box relative max-w-md border border-base-300 bg-base-100 p-6 shadow-2xl">
				<button
					type="button"
					className="btn btn-circle btn-ghost btn-sm absolute top-4 right-4"
					onClick={onClose}
					aria-label="Close"
				>
					<X className="h-4 w-4" />
				</button>

				<div className="flex items-center gap-3">
					<div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary">
						<QrCode className="h-5 w-5" />
					</div>
					<div>
						<h3 className="font-bold text-lg">Stremio Addon QR Setup</h3>
						<p className="text-base-content/60 text-xs">
							Scan with your phone or Smart TV camera to install
						</p>
					</div>
				</div>

				{/* QR Code Container */}
				<div className="my-6 flex flex-col items-center justify-center">
					<div className="rounded-2xl border border-base-300 bg-white p-4 shadow-inner">
						{size > 0 ? (
							<svg
								viewBox={`0 0 ${totalSize} ${totalSize}`}
								className="shape-rendering-crispEdges h-56 w-56"
								role="img"
								aria-label="Stremio Manifest QR Code"
							>
								{/* White background */}
								<rect width={totalSize} height={totalSize} fill="#ffffff" />
								{/* QR modules */}
								{matrix.map((row, r) =>
									row.map((cell, c) =>
										cell ? (
											<rect
												key={`${r}-${c}`}
												x={c + padding}
												y={r + padding}
												width="1.02"
												height="1.02"
												fill="#0f172a"
											/>
										) : null,
									),
								)}
							</svg>
						) : (
							<div className="flex h-56 w-56 items-center justify-center text-error text-xs">
								Unable to render QR code
							</div>
						)}
					</div>

					<div className="mt-4 flex items-center gap-2 text-base-content/70 text-xs">
						<Tv className="h-4 w-4 text-primary" />
						<span>Compatible with Android TV, Fire TV, WebOS, iOS & Android</span>
					</div>
				</div>

				{/* Manifest URL Box */}
				<div className="space-y-2">
					<span className="font-semibold text-base-content/70 text-xs uppercase tracking-wider">
						Manifest URL
					</span>
					<div className="flex items-center gap-2 rounded-xl border border-base-300 bg-base-200/70 p-1.5">
						<input
							type="text"
							readOnly
							value={manifestUrl}
							className="input input-ghost input-xs min-w-0 flex-1 font-mono text-[11px] focus:outline-none"
						/>
						<button
							type="button"
							onClick={handleCopy}
							className="btn btn-ghost btn-xs shrink-0 gap-1"
							title="Copy Manifest URL"
						>
							{copied ? (
								<>
									<Check className="h-3.5 w-3.5 text-success" />
									<span className="text-success text-xs">Copied</span>
								</>
							) : (
								<>
									<Copy className="h-3.5 w-3.5" />
									<span className="text-xs">Copy</span>
								</>
							)}
						</button>
					</div>
				</div>

				<div className="modal-action mt-6">
					<button type="button" className="btn btn-block btn-ghost" onClick={onClose}>
						Done
					</button>
				</div>
			</div>
			<button type="button" className="modal-backdrop" onClick={onClose} aria-label="Close modal">
				Close
			</button>
		</div>
	);
}
