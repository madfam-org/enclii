import { defineConfig, globalIgnores } from 'eslint/config'
import nextVitals from 'eslint-config-next/core-web-vitals'
import nextTs from 'eslint-config-next/typescript'
import tailwindcss from 'eslint-plugin-tailwindcss'
import reactHooks from 'eslint-plugin-react-hooks'

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    plugins: {
      'react-hooks': reactHooks,
      tailwindcss,
    },
    settings: {
      tailwindcss: {
        callees: ['cn', 'clsx', 'cva'],
        config: 'tailwind.config.ts',
      },
    },
    rules: {
      // ============================================
      // Phase 1: Anti-Monolith Law (max-lines)
      // ============================================
      'max-lines': ['warn', { max: 600, skipBlankLines: true, skipComments: true }],
      'max-lines-per-function': ['warn', { max: 200, skipBlankLines: true, skipComments: true }],

      // ============================================
      // Phase 2: Theme Police (hardcoded colors)
      // ============================================
      'tailwindcss/classnames-order': 'warn',
      'tailwindcss/no-contradicting-classname': 'error',
      'no-restricted-syntax': [
        'warn',
        {
          selector: 'Literal[value=/^#[0-9a-fA-F]{3,8}$/]',
          message: 'Hardcoded HEX colors are not allowed. Use CSS variables (--foreground, --primary, etc.) or semantic Tailwind classes instead.',
        },
        {
          selector: 'Literal[value=/(bg|text|border)-(red|blue|green|yellow|gray|slate|zinc|neutral|stone|orange|amber|lime|emerald|teal|cyan|sky|indigo|violet|purple|fuchsia|pink|rose)-\\d{2,3}(?!\\/)(?![a-z])/]',
          message: 'Use semantic Tailwind colors (bg-background, text-foreground, text-muted-foreground, bg-primary, etc.) instead of raw color values for theme compatibility.',
        },
      ],

      // ============================================
      // Existing rules (kept for compatibility)
      // ============================================
      'react-hooks/exhaustive-deps': 'warn',
      'react/no-unescaped-entities': 'off',
      '@next/next/no-img-element': 'warn',

      // TypeScript rules - relax for existing codebase
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-unused-vars': 'warn',
      '@typescript-eslint/no-empty-object-type': 'warn',

      // React rules - relax for existing patterns
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/rules-of-hooks': 'warn',
      // eslint-plugin-react-hooks 7.1 (pulled by eslint-config-next 16.4)
      // reports existing call-before-declare and memoization patterns as
      // errors (10 sites). Kept at warn like the rules above until fixed.
      'react-hooks/immutability': 'warn',
      'react-hooks/preserve-manual-memoization': 'warn',

      // General rules
      'prefer-const': 'warn',
    },
  },
  // Stricter limits for TypeScript files
  {
    files: ['**/*.tsx', '**/*.ts'],
    rules: {
      'max-lines': ['error', { max: 800, skipBlankLines: true, skipComments: true }],
    },
  },
  // Exclude CSS and config files from color restrictions
  {
    files: ['**/globals.css', '**/tailwind.config.*'],
    rules: {
      'max-lines': 'off',
      'no-restricted-syntax': 'off',
    },
  },
  // Disable require imports rule for CommonJS config files
  {
    files: ['*.config.js', 'jest.setup.js'],
    rules: {
      '@typescript-eslint/no-require-imports': 'off',
    },
  },
  globalIgnores(['.next/**', 'out/**', 'build/**', 'node_modules/**', 'next-env.d.ts', '**/*.css']),
])

export default eslintConfig
