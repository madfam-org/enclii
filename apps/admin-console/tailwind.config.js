/** @type {import('tailwindcss').Config} */
module.exports = {
  // Dispatch always uses Solarpunk theme (dark mode forced)
  darkMode: ['class', '[data-theme="solarpunk"]'],
  content: [
    './pages/**/*.{js,ts,jsx,tsx,mdx}',
    './components/**/*.{js,ts,jsx,tsx,mdx}',
    './app/**/*.{js,ts,jsx,tsx,mdx}',
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['var(--font-geist-sans)', 'system-ui', 'sans-serif'],
        mono: ['var(--font-geist-mono)', 'JetBrains Mono', 'monospace'],
        // Dispatch uses mono for display (terminal aesthetic)
        display: ['JetBrains Mono', 'var(--font-geist-mono)', 'monospace'],
      },
      colors: {
        // =================================================================
        // SEMANTIC COLORS (Solarpunk Theme - Forced)
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
        // SOLARPUNK PALETTE (Bioluminescent)
        // =================================================================
        solarpunk: {
          glow: 'hsl(var(--solarpunk-glow))',
          'glow-muted': 'hsl(var(--solarpunk-glow-muted))',
          substrate: 'hsl(var(--solarpunk-substrate))',
          'substrate-elevated': 'hsl(var(--solarpunk-substrate-elevated))',
          chlorophyll: '#00ff9d',
          amber: '#ffb347',
          coral: '#ff6b6b',
          moss: '#2d5a27',
        },

        // =================================================================
        // SEMANTIC STATUS COLORS (Dispatch-specific)
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
      },
      animation: {
        'pulse-glow': 'pulse-glow 2s ease-in-out infinite',
        'terminal-blink': 'terminal-blink 1s step-end infinite',
      },
      keyframes: {
        'pulse-glow': {
          '0%, 100%': { opacity: '1', filter: 'brightness(1)' },
          '50%': { opacity: '0.8', filter: 'brightness(1.2)' },
        },
        'terminal-blink': {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0' },
        },
      },
    },
  },
  plugins: [],
};
