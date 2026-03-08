import { dirname } from 'path'
import { fileURLToPath } from 'url'
import { FlatCompat } from '@eslint/eslintrc'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

const compat = new FlatCompat({
  baseDirectory: __dirname,
})

const eslintConfig = [
  ...compat.extends('next/core-web-vitals', 'next/typescript'),
  ...compat.extends('plugin:tailwindcss/recommended'),
  {
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
    },
  },
  {
    files: ['**/*.tsx', '**/*.ts'],
    rules: {
      'max-lines': ['error', { max: 800, skipBlankLines: true, skipComments: true }],
    },
  },
]

export default eslintConfig
