import React from "react";
import BrowserOnly from "@docusaurus/BrowserOnly";
import Layout from "@theme/Layout";

function SwaggerUI() {
	return (
		<BrowserOnly fallback={<div>Loading API Explorer...</div>}>
			{() => {
				const SwaggerUIComponent =
					// eslint-disable-next-line @typescript-eslint/no-require-imports
					require("swagger-ui-react").default;
				// eslint-disable-next-line @typescript-eslint/no-require-imports
				require("swagger-ui-react/swagger-ui.css");
				return (
					<SwaggerUIComponent
						url="/openapi.yaml"
						docExpansion="list"
						defaultModelsExpandDepth={1}
						filter
						tryItOutEnabled
					/>
				);
			}}
		</BrowserOnly>
	);
}

export default function ApiExplorerPage() {
	return (
		<Layout
			title="API Reference"
			description="Interactive AltMount REST API reference — explore endpoints, schemas, and try requests live."
		>
			<main>
				<SwaggerUI />
			</main>
		</Layout>
	);
}
