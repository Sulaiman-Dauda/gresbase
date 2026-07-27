'use client'

import * as React from 'react'
// next-themes 0.3 moved ThemeProviderProps to the package root; the old
// 'next-themes/dist/types' subpath is no longer published, so importing it
// fails type-checking against the version in package.json.
import { ThemeProvider as NextThemesProvider, type ThemeProviderProps } from 'next-themes'

export function ThemeProvider({ children, ...props }: ThemeProviderProps) {
  return <NextThemesProvider {...props}>{children}</NextThemesProvider>
}
