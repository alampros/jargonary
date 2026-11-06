import { createRoot } from 'react-dom/client'
import '@fontsource/source-serif-4/latin-600.css'
import '@fontsource/source-serif-4/latin-700.css'
import '@fontsource/dm-sans/latin-400.css'
import '@fontsource/dm-sans/latin-500.css'
import '@fontsource/dm-sans/latin-400-italic.css'
import './styles.css'
import '../../internal/themes/default.css'
import EditorApp from './EditorApp'

const root = document.getElementById('root')
if (!root) throw new Error('Missing application root')

// The editor is one level below the docs, including on project Pages sites.
const base = import.meta.env.BASE_URL.replace(/\/editor\/$/, '')
createRoot(root).render(<EditorApp siteName="Jargonary" base={base} home="documentation" />)
