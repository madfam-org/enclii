'use client'

import { useEffect, useState, useSyncExternalStore } from 'react'
import { Sun, Moon } from 'lucide-react'

type Theme = 'dark' | 'light'

function readTheme(): Theme {
  try {
    const stored = localStorage.getItem('theme')
    if (stored === 'dark' || stored === 'light') return stored
  } catch { /* Browser storage can be unavailable. */ }
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function subscribeTheme(notify: () => void) {
  window.addEventListener('storage', notify)
  return () => window.removeEventListener('storage', notify)
}

const serverTheme = () => null

export function ThemeToggle() {
  const storedTheme = useSyncExternalStore<Theme | null>(subscribeTheme, readTheme, serverTheme)
  const [selectedTheme, setTheme] = useState<Theme | null>(null)
  const theme = selectedTheme ?? storedTheme

  useEffect(() => {
    if (theme !== null) document.documentElement.setAttribute('data-theme', theme)
  }, [theme])

  function toggle() {
    const next: Theme = theme === 'dark' ? 'light' : 'dark'
    setTheme(next)
    document.documentElement.setAttribute('data-theme', next)
    try { localStorage.setItem('theme', next) } catch { /* Keep the in-memory selection. */ }
  }

  // Render placeholder to avoid layout shift before hydration
  if (theme === null) {
    return (
      <div className="p-2 rounded-md size-9 flex items-center justify-center text-muted-foreground" aria-hidden="true">
        <Sun className="size-4 opacity-50" />
      </div>
    )
  }

  const Icon = theme === 'dark' ? Sun : Moon
  const label = `Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`

  return (
    <button
      onClick={toggle}
      className="p-2 rounded-md hover:bg-muted transition-colors text-muted-foreground hover:text-foreground"
      aria-label={label}
      title={label}
    >
      <Icon className="size-4" />
    </button>
  )
}
