import js from "@eslint/js";
import { defineConfig } from "eslint/config";

export default defineConfig([
    {
        ignores: ["out/**", ".vscode-test/**", "node_modules/**"],
    },
    {
        files: ["**/*.mjs"],
        extends: [js.configs.recommended],
    },
]);
