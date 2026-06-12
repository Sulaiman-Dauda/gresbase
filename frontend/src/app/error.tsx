'use client'

import { useEffect } from 'react'

/**
 * Route-segment error boundary. Catches render/runtime errors in the dashboard
 * so a single failing component cannot blank out the whole app, and offers a
 * recovery action.
 */
export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    // Surface the error for debugging; a real deployment can forward this to an
    // error-reporting endpoint.
    // eslint-disable-next-line no-console
    console.error(error)
  }, [error])

  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 p-8 text-center">
      <h2 className="text-xl font-semibold">Something went wrong</h2>
      <p className="max-w-md text-sm text-muted-foreground">
        An unexpected error occurred while rendering this page.
        {error?.digest ? ` (ref: ${error.digest})` : ''}
      </p>
      <button
        onClick={reset}
        className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:opacity-90"
      >
        Try again
      </button>
    </div>
  )
}
