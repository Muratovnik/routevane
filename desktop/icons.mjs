import sharp from 'sharp'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

const assets = new URL('../.cache/desktop/assets/', import.meta.url)
await mkdir(assets, { recursive: true })
const mark = await readFile(
  new URL('../web/public/routevane-logo.svg', import.meta.url),
  'utf8',
)
const whiteMark = await sharp(
  Buffer.from(mark.replaceAll('fill="black"', 'fill="white"')),
)
  .resize(156, 182)
  .png()
  .toBuffer()
const png = await sharp({
  create: { width: 256, height: 256, channels: 4, background: '#163d30' },
})
  .composite([{ input: whiteMark, gravity: 'centre' }])
  .png()
  .toBuffer()
await writeFile(new URL('icon.png', assets), png)
await sharp(Buffer.from(mark))
  .resize(32, 32, { fit: 'contain', background: '#00000000' })
  .png()
  .toFile(fileURLToPath(new URL('trayTemplate.png', assets)))
// ICO supports a PNG-compressed 256px entry; keep the mark's SVG as the source.
const header = Buffer.alloc(22)
header.writeUInt16LE(1, 2)
header.writeUInt16LE(1, 4)
header.writeUInt16LE(1, 10)
header.writeUInt16LE(32, 12)
header.writeUInt32LE(png.length, 14)
header.writeUInt32LE(22, 18)
await writeFile(new URL('icon.ico', assets), Buffer.concat([header, png]))
