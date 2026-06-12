'use client'

import { useEffect } from 'react'

/**
 * Root error boundary — catches errors thrown in the root layout itself, which
 * the per-segment error.tsx cannot. Must render its own <html>/<body>.
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    // eslint-disable-next-line no-console
    console.error(error)
  }, [error])

  return (
    <html lang="en">
      <body
        style={{
          display: 'flex',
          minHeight: '100vh',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          gap: '1rem',
          fontFamily: 'system-ui, sans-serif',
          textAlign: 'center',
          padding: '2rem',
        }}
      >
        <h2 style={{ fontSize: '1.25rem', fontWeight: 600 }}>Something went wrong</h2>
        <p style={{ maxWidth: '28rem', color: '#666', fontSize: '0.875rem' }}>
          The application encountered a fatal error.
          {error?.digest ? ` (ref: ${error.digest})` : ''}
        </p>
        <button
          onClick={reset}
          style={{
            borderRadius: '0.375rem',
            background: '#111',
            color: '#fff',
            padding: '0.5rem 1rem',
            fontSize: '0.875rem',
            border: 'none',
            cursor: 'pointer',
          }}
        >
          Try again
        </button>
      </body>
    </html>
  )
}
