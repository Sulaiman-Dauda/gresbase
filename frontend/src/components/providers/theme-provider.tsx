'use client'

import * as React from 'react'
import { ThemeProvider as NextThemesProvider } from 'next-themes'

// next-themes 0.3 stopped publishing the 'next-themes/dist/types' subpath and
// does not export ThemeProviderProps from the package root either. Deriving the
// props from the component itself is version-independent: it keeps working
// whatever the package chooses to export.
type ThemeProviderProps = React.ComponentProps<typeof NextThemesProvider>

export function ThemeProvider({ children, ...props }: ThemeProviderProps) {
  return <NextThemesProvider {...props}>{children}</NextThemesProvider>
}
