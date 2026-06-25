import { type FormEvent, useEffect, useRef, useState } from "react";

interface PromptModalProps {
	isOpen: boolean;
	title: string;
	label?: string;
	initialValue?: string;
	placeholder?: string;
	confirmText?: string;
	isPending?: boolean;
	/** Whether to pre-select the initial value (handy for rename). */
	selectOnOpen?: boolean;
	onConfirm: (value: string) => void;
	onCancel: () => void;
}

/**
 * A small single-input modal used for naming operations (create folder, rename).
 * The global ModalContext only supports confirmation dialogs, so this fills the gap.
 */
export function PromptModal({
	isOpen,
	title,
	label,
	initialValue = "",
	placeholder,
	confirmText = "Confirm",
	isPending = false,
	selectOnOpen = false,
	onConfirm,
	onCancel,
}: PromptModalProps) {
	const [value, setValue] = useState(initialValue);
	const inputRef = useRef<HTMLInputElement>(null);

	// Reset the field whenever the modal opens, then focus (and optionally select).
	useEffect(() => {
		if (!isOpen) {
			return;
		}
		setValue(initialValue);
		const id = window.setTimeout(() => {
			const input = inputRef.current;
			if (!input) return;
			input.focus();
			if (selectOnOpen) {
				// Select the basename but leave any extension highlighted too — simplest is select all.
				input.select();
			}
		}, 50);
		return () => window.clearTimeout(id);
	}, [isOpen, initialValue, selectOnOpen]);

	if (!isOpen) {
		return null;
	}

	const trimmed = value.trim();
	const canSubmit = trimmed.length > 0 && !isPending;

	const handleSubmit = (e: FormEvent) => {
		e.preventDefault();
		if (canSubmit) {
			onConfirm(trimmed);
		}
	};

	return (
		<div className="modal modal-open" role="dialog">
			<div className="modal-box">
				<h3 className="font-bold text-lg">{title}</h3>
				<form onSubmit={handleSubmit}>
					<fieldset className="fieldset mt-4">
						{label && <legend className="fieldset-legend">{label}</legend>}
						<input
							ref={inputRef}
							type="text"
							className="input w-full"
							value={value}
							placeholder={placeholder}
							onChange={(e) => setValue(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Escape") {
									e.preventDefault();
									onCancel();
								}
							}}
						/>
					</fieldset>
					<div className="modal-action">
						<button type="button" className="btn btn-ghost" onClick={onCancel} disabled={isPending}>
							Cancel
						</button>
						<button type="submit" className="btn btn-primary" disabled={!canSubmit}>
							{isPending ? <span className="loading loading-spinner loading-xs" /> : confirmText}
						</button>
					</div>
				</form>
			</div>
			<button
				type="button"
				className="modal-backdrop"
				aria-label="Close"
				onClick={onCancel}
				disabled={isPending}
			/>
		</div>
	);
}
