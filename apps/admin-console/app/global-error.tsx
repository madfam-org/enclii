'use client';

import { useEffect } from 'react';

interface GlobalErrorProps {
  error: Error & { digest?: string };
  reset: () => void;
}

/**
 * Global Error Boundary for Dispatch - catches errors in root layout
 * This is the last line of defense for uncaught errors.
 * Must include its own html/body tags as it replaces the root layout.
 */
export default function GlobalError({ error, reset }: GlobalErrorProps) {
  useEffect(() => {
    // Log critical error for observability
    console.error('Dispatch critical application error:', {
      message: error.message,
      digest: error.digest,
      stack: error.stack,
    });
  }, [error]);

  return (
    <html lang="en">
      <body>
        <div
          style={{
            minHeight: '100vh',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            backgroundColor: '#09090b',
            padding: '1rem',
            fontFamily: 'system-ui, -apple-system, sans-serif',
          }}
        >
          <div
            style={{
              maxWidth: '28rem',
              width: '100%',
              textAlign: 'center',
            }}
          >
            <div
              style={{
                width: '4rem',
                height: '4rem',
                margin: '0 auto 1.5rem',
                borderRadius: '9999px',
                backgroundColor: 'rgba(239, 68, 68, 0.1)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                width="32"
                height="32"
                viewBox="0 0 24 24"
                fill="none"
                stroke="#ef4444"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <circle cx="12" cy="12" r="10" />
                <line x1="12" y1="8" x2="12" y2="12" />
                <line x1="12" y1="16" x2="12.01" y2="16" />
              </svg>
            </div>

            <h1
              style={{
                fontSize: '1.5rem',
                fontWeight: 'bold',
                color: '#ffffff',
                marginBottom: '0.5rem',
              }}
            >
              Application Error
            </h1>

            <p
              style={{
                color: '#a1a1aa',
                marginBottom: '1.5rem',
              }}
            >
              A critical error occurred. Please refresh the page or try again later.
            </p>

            {error.digest && (
              <p
                style={{
                  fontSize: '0.75rem',
                  color: '#52525b',
                  marginBottom: '1.5rem',
                  fontFamily: 'monospace',
                }}
              >
                Error ID: {error.digest}
              </p>
            )}

            <div
              style={{
                display: 'flex',
                gap: '0.75rem',
                justifyContent: 'center',
                flexWrap: 'wrap',
              }}
            >
              <button
                onClick={reset}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  gap: '0.5rem',
                  padding: '0.625rem 1rem',
                  fontSize: '0.875rem',
                  fontWeight: '500',
                  color: 'white',
                  backgroundColor: '#2563eb',
                  borderRadius: '0.5rem',
                  border: 'none',
                  cursor: 'pointer',
                }}
              >
                Try again
              </button>

              <button
                onClick={() => (window.location.href = '/')}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  gap: '0.5rem',
                  padding: '0.625rem 1rem',
                  fontSize: '0.875rem',
                  fontWeight: '500',
                  color: '#d4d4d8',
                  backgroundColor: '#27272a',
                  borderRadius: '0.5rem',
                  border: 'none',
                  cursor: 'pointer',
                }}
              >
                Go home
              </button>
            </div>
          </div>
        </div>
      </body>
    </html>
  );
}
