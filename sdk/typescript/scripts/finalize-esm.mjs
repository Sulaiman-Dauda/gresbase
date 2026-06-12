// Renames the ESM build output (dist/esm/index.js) to dist/index.mjs so the
// package exports map ("import": "./dist/index.mjs") resolves correctly.
import { renameSync, rmSync, existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const esmOut = join(root, 'dist', 'esm', 'index.js')
const target = join(root, 'dist', 'index.mjs')

if (!existsSync(esmOut)) {
  console.error('finalize-esm: expected ESM output at dist/esm/index.js — did the esm tsc pass run?')
  process.exit(1)
}

renameSync(esmOut, target)
rmSync(join(root, 'dist', 'esm'), { recursive: true, force: true })
console.log('finalize-esm: wrote dist/index.mjs')
