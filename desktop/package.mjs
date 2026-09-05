import { packager } from '@electron/packager'
import { cp, mkdir, mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('..', import.meta.url))
const out = join(root, '.cache/desktop')
await mkdir(out, { recursive: true })
const stage = await mkdtemp(join(out, 'stage-'))
const manifest = JSON.parse(
  await readFile(new URL('package.json', import.meta.url), 'utf8'),
)
await cp(new URL('src', import.meta.url), join(stage, 'src'), {
  recursive: true,
})
await cp(join(out, 'assets'), join(stage, 'assets'), { recursive: true })
await writeFile(
  join(stage, 'package.json'),
  JSON.stringify({
    name: manifest.name,
    productName: manifest.productName,
    version: manifest.version,
    description: manifest.description,
    license: manifest.license,
    type: 'module',
    main: manifest.main,
  }),
)
const executable =
  process.platform === 'win32' ? 'routing-agent.exe' : 'routing-agent'
const result = await packager({
  dir: stage,
  out,
  name: 'Routevane',
  executableName: 'Routevane',
  appBundleId: 'io.routevane.desktop',
  asar: true,
  overwrite: true,
  ...(process.platform === 'win32'
    ? { icon: join(out, 'assets/icon.ico') }
    : {}),
  electronVersion: manifest.devDependencies.electron,
  platform: process.platform,
  arch: process.arch,
  extraResource: [
    resolve(root, '.cache/build', executable),
    join(root, 'catalog'),
    join(root, 'LICENSE'),
  ],
})
console.log(result.join('\n'))
