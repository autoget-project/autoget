# Autoget Frontend

A web application for searching and managing resources, downloads for AutoGet. Built with Lit, TypeScript, and Tailwind CSS.

## Features

- **Resource Search**: Search for resources across multiple indexers with advanced filtering
- **Category Navigation**: Hierarchical category browsing with multi-level support
- **Indexer Management**: Switch between different resource indexers
- **Responsive Design**: Mobile-friendly interface built with DaisyUI and Tailwind CSS
- **Modern Architecture**: Built with web components using Lit framework

## Tech Stack

- **Frontend Framework**: [Lit](https://lit.dev/) - Lightweight web component library
- **Language**: TypeScript
- **Styling**: Tailwind CSS with DaisyUI components
- **Build Tool**: Vite
- **Testing**: Vitest with jsdom
- **Code Quality**: oxlint + oxfmt
- **Icons**: Iconify
- **Date Handling**: Luxon
- **Routing**: in-repo `src/utils/router.ts` on native URLPattern (`urlpattern-polyfill` is loaded on demand)

## Prerequisites

- Node.js 26 or higher
- pnpm 12 (see [CONTRIBUTING](../CONTRIBUTING.md) for the exact version)

### Development

```bash
# Install dependencies and start the development server
pnpm install
pnpm dev

# The application will be available at http://localhost:8889
```

### Building for Production

```bash
# Build the application
pnpm run build
```

### Testing

```bash
pnpm run test:run
```

See [CONTRIBUTING](../CONTRIBUTING.md) for the full workflow.
