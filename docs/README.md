# AltMount Documentation

This documentation site is built using [Docusaurus](https://docusaurus.io/), a modern static website generator.

## Installation

```bash
npm install
```

## Local Development

```bash
npm start
```

This command starts a local development server and opens up a browser window. Most changes are reflected live without having to restart the server.

## Build

```bash
npm run build
```

This command generates static content into the `build` directory and can be served using any static contents hosting service.

## Deployment

Using SSH:

```bash
USE_SSH=true npm run deploy
```

Not using SSH:

```bash
GIT_USER=<Your GitHub username> npm run deploy
```

If you are using GitHub pages for hosting, this command is a convenient way to build the website and push to the `gh-pages` branch.

## Adding Images

The documentation includes many image placeholders marked with:

```
*[Screenshot placeholder: Description of what image should show]*
```

To add actual images:

1. Create screenshots or images as described in the placeholder text
2. Save images to `static/img/` directory
3. Replace the placeholder text with:
   ```markdown
   ![Alt text](path/to/image.png "Optional title")
   ```

For more information, see the [Docusaurus documentation](https://docusaurus.io/).
