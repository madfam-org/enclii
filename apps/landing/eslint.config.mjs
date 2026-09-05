// Flat config, loaded natively.
//
// This file used to route `next/core-web-vitals`, `next/typescript` and
// `plugin:tailwindcss/recommended` through `FlatCompat` from
// `@eslint/eslintrc`. That translation layer exists to consume *eslintrc*
// shareable configs — and as of eslint-config-next 16 and
// eslint-plugin-tailwindcss 3.18 none of the three is one any more: each
// already exports a flat-config array. Handing a flat config to the eslintrc
// loader fails schema validation (a flat config's `plugins` is an object of
// plugin instances; eslintrc requires an array of names), and eslintrc then
// tries to `JSON.stringify` the offending value to build the error message.
// That value contains `eslint-plugin-react`, whose `configs.flat.plugins.react`
// points back at the plugin, so the *error formatter itself* threw
//
//   TypeError: Converting circular structure to JSON
//     property 'configs' -> 'flat' -> ... -> 'plugins' -> 'react' closes the circle
//
// and `eslint .` never got as far as linting a single file. Importing the flat
// configs directly is both the fix and the supported path; `FlatCompat` is no
// longer needed here at all.
import nextCoreWebVitals from 'eslint-config-next/core-web-vitals'
import nextTypeScript from 'eslint-config-next/typescript'
import tailwindcss from 'eslint-plugin-tailwindcss'

const eslintConfig = [
  {
    ignores: ['.next/**', 'out/**', 'next-env.d.ts'],
  },
  ...nextCoreWebVitals,
  ...nextTypeScript,
  ...tailwindcss.configs['flat/recommended'],
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
