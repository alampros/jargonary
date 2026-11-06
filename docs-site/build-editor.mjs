import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const url = new URL(process.env.PAGES_BASE_URL || 'https://alampros.github.io/jargonary')
const base = `${url.pathname.replace(/\/$/, '')}/editor/`
const result = spawnSync(
  process.execPath,
  [
    fileURLToPath(new URL('../ui/node_modules/vite/bin/vite.js', import.meta.url)),
    'build',
    '--mode',
    'editor',
    '--base',
    base,
  ],
  { cwd: fileURLToPath(new URL('../ui', import.meta.url)), stdio: 'inherit' },
)
if (result.error) throw result.error
process.exit(result.status ?? 1)
