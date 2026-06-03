import { defineConfig, globalIgnores } from 'eslint/config'
import nextVitals from 'eslint-config-next/core-web-vitals'
import nextTs from 'eslint-config-next/typescript'
import tailwindcss from 'eslint-plugin-tailwindcss'

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    plugins: {
      tailwindcss,
    },
    settings: {
      tailwindcss: {
        callees: ['cn', 'clsx', 'cva'],
        config: 'tailwind.config.ts',
      },
    },
    rules: {
      'max-lines': ['warn', { max: 600, skipBlankLines: true, skipComments: true }],
      'max-lines-per-function': ['warn', { max: 200, skipBlankLines: true, skipComments: true }],
      'tailwindcss/classnames-order': 'warn',
      'tailwindcss/no-contradicting-classname': 'error',
      'no-restricted-syntax': [
        'warn',
        {
          selector: 'Literal[value=/^#[0-9a-fA-F]{3,8}$/]',
          message:
            'Hardcoded HEX colors are not allowed. Use CSS variables (--foreground, --primary, etc.) or semantic Tailwind classes instead.',
        },
        {
          selector:
            'Literal[value=/(bg|text|border)-(red|blue|green|yellow|gray|slate|zinc|neutral|stone|orange|amber|lime|emerald|teal|cyan|sky|indigo|violet|purple|fuchsia|pink|rose)-\\d{2,3}(?!\\/)(?![a-z])/]',
          message:
            'Use semantic Tailwind colors (bg-background, text-foreground, text-muted-foreground, bg-primary, etc.) instead of raw color values for theme compatibility.',
        },
      ],
      'react/no-unescaped-entities': 'off',
      '@next/next/no-img-element': 'warn',
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-unused-vars': 'warn',
      '@typescript-eslint/no-empty-object-type': 'warn',
      '@typescript-eslint/no-require-imports': 'warn',
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/rules-of-hooks': 'warn',
      'prefer-const': 'warn',
    },
  },
  {
    files: ['**/*.tsx', '**/*.ts'],
    rules: {
      'max-lines': ['error', { max: 800, skipBlankLines: true, skipComments: true }],
    },
  },
  {
    files: ['*.config.js', 'jest.setup.js', 'tailwind.config.js'],
    rules: {
      '@typescript-eslint/no-require-imports': 'off',
      'no-restricted-syntax': 'off',
    },
  },
  globalIgnores(['.next/**', 'out/**', 'build/**', 'node_modules/**', 'next-env.d.ts', '**/*.css']),
])

export default eslintConfig
