import js from "@eslint/js";
import eslintConfigPrettier from "eslint-config-prettier";
import globals from "globals";
import svelte from "eslint-plugin-svelte";
import tseslint from "typescript-eslint";

const localeFormattingRule = [
  "error",
  {
    selector: "CallExpression[callee.property.name='toLocaleDateString']",
    message:
      "Use localDateLabel() from src/utils/time.ts so timestamp localization stays in the presentation helper.",
  },
  {
    selector: "CallExpression[callee.property.name='toLocaleString']",
    message:
      "Use shared presentation-layer time helpers instead of inline locale formatting.",
  },
  {
    selector: "CallExpression[callee.property.name='toLocaleTimeString']",
    message:
      "Use shared presentation-layer time helpers instead of inline locale formatting.",
  },
];

export default tseslint.config(
  {
    ignores: ["dist/", "node_modules/", "eslint.config.mjs"],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...svelte.configs["flat/recommended"],
  {
    files: ["**/*.ts"],
    languageOptions: {
      parser: tseslint.parser,
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    rules: {
      "svelte/prefer-svelte-reactivity": "off",
      "@typescript-eslint/ban-ts-comment": [
        "error",
        {
          "ts-check": false,
          "ts-expect-error": "allow-with-description",
          "ts-ignore": true,
          "ts-nocheck": true,
          minimumDescriptionLength: 10,
        },
      ],
      "@typescript-eslint/no-explicit-any": ["error", { fixToUnknown: true }],
      "no-restricted-syntax": localeFormattingRule,
    },
  },
  {
    files: ["**/*.svelte"],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node,
      },
      parserOptions: {
        parser: tseslint.parser,
        extraFileExtensions: [".svelte"],
      },
    },
    rules: {
      "svelte/no-at-html-tags": "off",
      "svelte/prefer-svelte-reactivity": "off",
      "svelte/require-each-key": "off",
      "svelte/no-unused-svelte-ignore": "off",
      "@typescript-eslint/ban-ts-comment": [
        "error",
        {
          "ts-check": false,
          "ts-expect-error": "allow-with-description",
          "ts-ignore": true,
          "ts-nocheck": true,
          minimumDescriptionLength: 10,
        },
      ],
      "@typescript-eslint/no-explicit-any": ["error", { fixToUnknown: true }],
      "no-restricted-syntax": localeFormattingRule,
    },
  },
  {
    files: ["src/utils/time.ts"],
    rules: {
      "no-restricted-syntax": "off",
    },
  },
  eslintConfigPrettier,
);
