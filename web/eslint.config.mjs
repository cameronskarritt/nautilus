import js from "@eslint/js"
import globals from "globals"
import reactHooks from "eslint-plugin-react-hooks"
import reactRefresh from "eslint-plugin-react-refresh"
import tseslint from "typescript-eslint"
import { defineConfig, globalIgnores } from "eslint/config"

export default defineConfig([
  globalIgnores([
    "**/dist/**",
    "**/node_modules/**",
    "**/.turbo/**",
    "**/routeTree.gen.ts",
  ]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
    ],
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
  },
  {
    files: ["apps/*/src/**/*.{ts,tsx}"],
    extends: [reactRefresh.configs.vite],
  },
  {
    // TanStack's Vite plugin manages refresh boundaries for route modules.
    files: ["apps/*/src/routes/**/*.tsx"],
    rules: { "react-refresh/only-export-components": "off" },
  },
])
