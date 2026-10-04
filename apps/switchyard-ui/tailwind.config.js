/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: ['class', '[data-theme="solarpunk"]'],
  content: [
    './pages/**/*.{js,ts,jsx,tsx,mdx}',
    './components/**/*.{js,ts,jsx,tsx,mdx}',
    './app/**/*.{js,ts,jsx,tsx,mdx}',
    '../../packages/ui-components/src/**/*.{ts,tsx}',
    // @madfam/ui from solarpunk-foundry (workspace link or node_modules)
    './node_modules/@madfam/ui/src/**/*.{ts,tsx}',
    './node_modules/@madfam/ui/dist/**/*.{js,mjs}',
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['var(--font-geist-sans)', 'system-ui', 'sans-serif'],
        mono: ['var(--font-geist-mono)', 'monospace'],
        // Solarpunk-specific: JetBrains Mono for headers
        display: ['var(--font-display)', 'var(--font-geist-mono)', 'monospace'],
      },
      colors: {
        // =================================================================
        // SEMANTIC COLORS (Theme-Aware via CSS Variables)
        // =================================================================
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))',
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))',
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))',
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))',
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))',
        },
        popover: {
          DEFAULT: 'hsl(var(--popover))',
          foreground: 'hsl(var(--popover-foreground))',
        },
        card: {
          DEFAULT: 'hsl(var(--card))',
          foreground: 'hsl(var(--card-foreground))',
        },

        // =================================================================
        // BRAND COLORS (Static, not theme-dependent)
        // =================================================================
        'enclii-blue': '#0070f3',
        'enclii-green': '#00b894',
        'enclii-orange': '#e17055',
        'enclii-red': '#d63031',

        // =================================================================
        // SOLARPUNK PALETTE (Available globally, activated via theme)
        // =================================================================
        solarpunk: {
          // Bioluminescent accent
          glow: 'hsl(var(--solarpunk-glow))',
          'glow-muted': 'hsl(var(--solarpunk-glow-muted))',
          // Deep organic backgrounds
          substrate: 'hsl(var(--solarpunk-substrate))',
          'substrate-elevated': 'hsl(var(--solarpunk-substrate-elevated))',
          // Living system colors
          chlorophyll: '#00ff9d',
          amber: '#ffb347',
          coral: '#ff6b6b',
          moss: '#2d5a27',
        },

        // =================================================================
        // SEMANTIC STATUS COLORS (Theme-aware)
        // Use these for all status indicators instead of raw colors
        // Examples: bg-status-success, text-status-error-foreground
        // =================================================================
        status: {
          success: {
            DEFAULT: 'hsl(var(--status-success))',
            foreground: 'hsl(var(--status-success-foreground))',
            muted: 'hsl(var(--status-success-muted))',
            'muted-foreground': 'hsl(var(--status-success-muted-foreground))',
          },
          warning: {
            DEFAULT: 'hsl(var(--status-warning))',
            foreground: 'hsl(var(--status-warning-foreground))',
            muted: 'hsl(var(--status-warning-muted))',
            'muted-foreground': 'hsl(var(--status-warning-muted-foreground))',
          },
          error: {
            DEFAULT: 'hsl(var(--status-error))',
            foreground: 'hsl(var(--status-error-foreground))',
            muted: 'hsl(var(--status-error-muted))',
            'muted-foreground': 'hsl(var(--status-error-muted-foreground))',
          },
          info: {
            DEFAULT: 'hsl(var(--status-info))',
            foreground: 'hsl(var(--status-info-foreground))',
            muted: 'hsl(var(--status-info-muted))',
            'muted-foreground': 'hsl(var(--status-info-muted-foreground))',
          },
          neutral: {
            DEFAULT: 'hsl(var(--status-neutral))',
            foreground: 'hsl(var(--status-neutral-foreground))',
            muted: 'hsl(var(--status-neutral-muted))',
            'muted-foreground': 'hsl(var(--status-neutral-muted-foreground))',
          },
        },

        // =================================================================
        // TRELLIS VISUALIZATION COLORS
        // =================================================================
        trellis: {
          node: 'hsl(var(--trellis-node))',
          'node-hover': 'hsl(var(--trellis-node-hover))',
          connector: 'hsl(var(--trellis-connector))',
          'connector-active': 'hsl(var(--trellis-connector-active))',
        },
      },
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)',
      },
      boxShadow: {
        // Solarpunk bioluminescent glow effects
        'glow-sm': '0 0 10px hsl(var(--solarpunk-glow) / 0.3)',
        glow: '0 0 20px hsl(var(--solarpunk-glow) / 0.4)',
        'glow-lg': '0 0 40px hsl(var(--solarpunk-glow) / 0.5)',
        // Enterprise subtle shadows
        'enterprise-sm': '0 1px 2px rgba(0, 0, 0, 0.05)',
        enterprise: '0 1px 3px rgba(0, 0, 0, 0.1), 0 1px 2px rgba(0, 0, 0, 0.06)',
        'enterprise-lg': '0 4px 6px rgba(0, 0, 0, 0.1), 0 2px 4px rgba(0, 0, 0, 0.06)',
      },
      animation: {
        'pulse-glow': 'pulse-glow 2s ease-in-out infinite',
        'trellis-grow': 'trellis-grow 0.5s ease-out forwards',
        'node-appear': 'node-appear 0.3s ease-out forwards',
      },
      keyframes: {
        'pulse-glow': {
          '0%, 100%': { opacity: '1', filter: 'brightness(1)' },
          '50%': { opacity: '0.8', filter: 'brightness(1.2)' },
        },
        'trellis-grow': {
          '0%': { strokeDashoffset: '100%', opacity: '0' },
          '100%': { strokeDashoffset: '0%', opacity: '1' },
        },
        'node-appear': {
          '0%': { transform: 'scale(0.8)', opacity: '0' },
          '100%': { transform: 'scale(1)', opacity: '1' },
        },
      },
    },
  },
  plugins: [],
};
