import { Home } from "lucide-react";
import type { BreadcrumbItem } from "../../types/webdav";

interface BreadcrumbNavProps {
	path: string;
	onNavigate: (path: string) => void;
}

export function BreadcrumbNav({ path, onNavigate }: BreadcrumbNavProps) {
	const generateBreadcrumbs = (currentPath: string): BreadcrumbItem[] => {
		if (currentPath === "/" || currentPath === "") {
			return [{ name: "Root", path: "/" }];
		}

		const parts = currentPath.split("/").filter(Boolean);
		const breadcrumbs: BreadcrumbItem[] = [{ name: "Root", path: "/" }];

		let accumulatedPath = "";
		for (const part of parts) {
			accumulatedPath += `/${part}`;
			breadcrumbs.push({
				name: part,
				path: accumulatedPath,
			});
		}

		return breadcrumbs;
	};

	const breadcrumbs = generateBreadcrumbs(path);

	return (
		<div className="breadcrumbs text-sm">
			<ul>
				{breadcrumbs.map((crumb, index) => (
					<li key={crumb.path}>
						<button
							type="button"
							onClick={() => onNavigate(crumb.path)}
							className="flex items-center space-x-1 transition-colors hover:text-primary"
						>
							{index === 0 && <Home className="h-4 w-4" />}
							<span>{crumb.name}</span>
						</button>
					</li>
				))}
			</ul>
		</div>
	);
}
