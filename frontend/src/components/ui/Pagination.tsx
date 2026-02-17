interface PaginationProps {
	currentPage: number;
	totalPages: number;
	onPageChange: (page: number) => void;
	totalItems?: number;
	itemsPerPage?: number;
	showSummary?: boolean;
}

export function Pagination({
	currentPage,
	totalPages,
	onPageChange,
	totalItems,
	itemsPerPage,
	showSummary = true,
}: PaginationProps) {
	// Don't render if there's only one page or no pages
	if (totalPages <= 1) {
		return null;
	}

	const getVisiblePages = () => {
		const pages: (number | string)[] = [];
		const maxVisiblePages = 7; // Show up to 7 page buttons

		if (totalPages <= maxVisiblePages) {
			// Show all pages if total pages is small
			for (let i = 1; i <= totalPages; i++) {
				pages.push(i);
			}
		} else {
			// Complex logic for showing relevant pages with ellipsis
			if (currentPage <= 4) {
				// Current page is near the beginning
				for (let i = 1; i <= 5; i++) {
					pages.push(i);
				}
				pages.push("...");
				pages.push(totalPages);
			} else if (currentPage >= totalPages - 3) {
				// Current page is near the end
				pages.push(1);
				pages.push("...");
				for (let i = totalPages - 4; i <= totalPages; i++) {
					pages.push(i);
				}
			} else {
				// Current page is in the middle
				pages.push(1);
				pages.push("...");
				for (let i = currentPage - 1; i <= currentPage + 1; i++) {
					pages.push(i);
				}
				pages.push("...");
				pages.push(totalPages);
			}
		}

		return pages;
	};

	const visiblePages = getVisiblePages();

	const getSummaryText = () => {
		if (!totalItems || !itemsPerPage) return "";

		const startItem = (currentPage - 1) * itemsPerPage + 1;
		const endItem = Math.min(currentPage * itemsPerPage, totalItems);

		return `Showing ${startItem}-${endItem} of ${totalItems} results`;
	};

	return (
		<div className="flex flex-col items-center justify-between gap-4 sm:flex-row">
			{/* Results Summary */}
			{showSummary && totalItems && itemsPerPage && (
				<div className="text-base-content/70 text-sm">{getSummaryText()}</div>
			)}

			{/* Pagination Controls */}
			<div className="join">
				{/* First Page */}
				<button
					type="button"
					className="join-item btn btn-sm"
					onClick={() => onPageChange(1)}
					disabled={currentPage === 1}
					aria-label="Go to first page"
				>
					«
				</button>

				{/* Previous Page */}
				<button
					type="button"
					className="join-item btn btn-sm"
					onClick={() => onPageChange(currentPage - 1)}
					disabled={currentPage === 1}
					aria-label="Go to previous page"
				>
					‹
				</button>

				{/* Page Numbers */}
				{visiblePages.map((page, index) => {
					if (page === "...") {
						return (
							<button
								key={`ellipsis-${index}`}
								type="button"
								className="join-item btn btn-sm btn-disabled"
								disabled
								aria-label="More pages available"
							>
								...
							</button>
						);
					}

					const pageNum = page as number;
					return (
						<button
							key={pageNum}
							type="button"
							className={`join-item btn btn-sm ${pageNum === currentPage ? "btn-active" : ""}`}
							onClick={() => onPageChange(pageNum)}
							aria-label={`Page ${pageNum}`}
							aria-current={pageNum === currentPage ? "page" : undefined}
						>
							{pageNum}
						</button>
					);
				})}

				{/* Next Page */}
				<button
					type="button"
					className="join-item btn btn-sm"
					onClick={() => onPageChange(currentPage + 1)}
					disabled={currentPage === totalPages}
					aria-label="Go to next page"
				>
					›
				</button>

				{/* Last Page */}
				<button
					type="button"
					className="join-item btn btn-sm"
					onClick={() => onPageChange(totalPages)}
					disabled={currentPage === totalPages}
					aria-label="Go to last page"
				>
					»
				</button>
			</div>
		</div>
	);
}
