import type { GlobalProvider } from "@ladle/react";

/**
 * Global decorator for Ladle stories.
 *
 * Wraps every story with the base Tailwind CSS variables and font
 * so that shadcn/ui utility classes resolve correctly in the
 * Ladle development environment.
 */
export const Provider: GlobalProvider = ({ children }) => (
  <div
    className="font-sans antialiased"
    style={{
      /* shadcn/ui light theme CSS custom properties */
      "--background": "0 0% 100%",
      "--foreground": "222.2 84% 4.9%",
      "--card": "0 0% 100%",
      "--card-foreground": "222.2 84% 4.9%",
      "--popover": "0 0% 100%",
      "--popover-foreground": "222.2 84% 4.9%",
      "--primary": "222.2 47.4% 11.2%",
      "--primary-foreground": "210 40% 98%",
      "--secondary": "210 40% 96.1%",
      "--secondary-foreground": "222.2 47.4% 11.2%",
      "--muted": "210 40% 96.1%",
      "--muted-foreground": "215.4 16.3% 46.9%",
      "--accent": "210 40% 96.1%",
      "--accent-foreground": "222.2 47.4% 11.2%",
      "--destructive": "0 84.2% 60.2%",
      "--destructive-foreground": "210 40% 98%",
      "--border": "214.3 31.8% 91.4%",
      "--input": "214.3 31.8% 91.4%",
      "--ring": "222.2 84% 4.9%",
      "--radius": "0.5rem",
      fontFamily:
        'ui-sans-serif, system-ui, sans-serif, "Apple Color Emoji", "Segoe UI Emoji"',
      color: "hsl(222.2, 84%, 4.9%)",
      backgroundColor: "hsl(0, 0%, 100%)",
      padding: "1rem",
    } as React.CSSProperties}
  >
    {children}
  </div>
);
