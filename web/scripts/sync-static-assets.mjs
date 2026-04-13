import { cpSync, existsSync, mkdirSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const webRoot = path.resolve(__dirname, '..')
const publicRoot = path.join(webRoot, 'public')

const copies = [
  {
    source: path.join(webRoot, 'node_modules', '@semi-bot', 'semi-theme-a11y', 'semi.min.css'),
    target: path.join(publicRoot, 'semi-themes', 'a11y.css'),
  },
  {
    source: path.join(webRoot, 'node_modules', '@semi-bot', 'semi-theme-doucreator', 'semi.min.css'),
    target: path.join(publicRoot, 'semi-themes', 'doucreator.css'),
  },
  {
    source: path.join(webRoot, 'node_modules', '@semi-bot', 'semi-theme-douyin', 'semi.min.css'),
    target: path.join(publicRoot, 'semi-themes', 'douyin.css'),
  },
  {
    source: path.join(webRoot, 'node_modules', '@semi-bot', 'semi-theme-feishu', 'semi.min.css'),
    target: path.join(publicRoot, 'semi-themes', 'feishu.css'),
  },
  {
    source: path.join(webRoot, 'node_modules', '@semi-bot', 'semi-theme-volcengine', 'semi.min.css'),
    target: path.join(publicRoot, 'semi-themes', 'volcengine.css'),
  },
]

for (const item of copies) {
  if (!existsSync(item.source)) {
    throw new Error(`missing asset: ${path.relative(webRoot, item.source)}`)
  }
  mkdirSync(path.dirname(item.target), { recursive: true })
  cpSync(item.source, item.target)
}
