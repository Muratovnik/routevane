import builder from 'electron-builder'
import { execFileSync } from 'node:child_process'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname, join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('..', import.meta.url))
const manifest = JSON.parse(
  await readFile(new URL('package.json', import.meta.url), 'utf8'),
)
const args = process.argv.slice(2)
const installer = args.includes('--installer')
const version = args.includes('--version')
  ? args[args.indexOf('--version') + 1]?.replace(/^v/, '')
  : manifest.version
if (!/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version ?? ''))
  throw new Error('invalid version')
if (installer && version === '0.0.0')
  throw new Error('installer requires --version')
if (installer && (process.platform !== 'win32' || process.arch !== 'x64'))
  throw new Error('Installer requires Windows x64 acceptance')
let publish = {
  provider: 'github',
  owner: 'Muratovnik',
  repo: 'routevane',
  releaseType: 'release',
}
let fixtureID
// Only explicit fixture builds use a local feed. The installed production app
// exposes no API/env setting that can redirect its updater to another server.
if (args.includes('--fixture-feed')) {
  fixtureID = args[args.indexOf('--fixture-id') + 1]
  if (
    !args.includes('--fixture-id') ||
    !/^test-[a-z0-9]+$/.test(fixtureID ?? '')
  )
    throw new Error('fixture requires isolated identity')
  const url = new URL(args[args.indexOf('--fixture-feed') + 1])
  if (
    url.protocol !== 'http:' ||
    url.hostname !== '127.0.0.1' ||
    !url.port ||
    url.username ||
    url.password
  )
    throw new Error('invalid fixture feed')
  publish = { provider: 'generic', url: url.href }
}
const out = args.includes('--output')
  ? args[args.indexOf('--output') + 1]
  : join(root, '.cache/desktop')
let fixtureEntry
if (fixtureID) {
  const scratch = resolve(args[args.indexOf('--fixture-root') + 1] ?? '')
  if (
    !args.includes('--fixture-root') ||
    !scratch.startsWith(resolve(root, 'tmp/update-acceptance') + sep)
  )
    throw new Error('fixture data must stay under tmp/update-acceptance')
  if (!resolve(out).startsWith(scratch + sep))
    throw new Error('fixture output must stay with fixture data')
  await mkdir(out, { recursive: true })
  fixtureEntry = join(scratch, 'fixture-entry.mjs')
  // Explorer may restart an installed app without the launcher's environment.
  // Fixture-only bootstrap keeps every launch isolated from real user data.
  await writeFile(
    fixtureEntry,
    `process.env.ROUTEVANE_DESKTOP_PROFILE = ${JSON.stringify(join(scratch, 'profile'))};
process.env.ROUTEVANE_DESKTOP_DATA = ${JSON.stringify(join(scratch, 'data'))};
await import('./src/main.mjs');
`,
  )
}
const coreMetadata = join(out, 'core-metadata')
if (installer) {
  const created = execFileSync('git', ['show', '-s', '--format=%cI', 'HEAD'], {
    cwd: root,
    encoding: 'utf8',
    windowsHide: true,
  }).trim()
  execFileSync(
    'python',
    [
      join(root, 'tools/release_metadata.py'),
      '--version',
      version,
      '--created',
      created,
      '--notices',
      join(coreMetadata, 'CORE-THIRD-PARTY-NOTICES.txt'),
      '--sbom',
      join(coreMetadata, 'CORE-SBOM.spdx.json'),
    ],
    { cwd: root, stdio: 'inherit', windowsHide: true },
  )
}
const binary =
  process.platform === 'win32' ? 'routing-agent.exe' : 'routing-agent'
await builder.build({
  projectDir: fileURLToPath(new URL('.', import.meta.url)),
  targets: builder.Platform.current().createTarget(installer ? 'nsis' : 'dir'),
  publish: 'never',
  config: {
    appId: fixtureID ? `io.routevane.${fixtureID}` : 'io.routevane.desktop',
    productName: 'Routevane',
    executableName: 'Routevane',
    electronVersion: manifest.devDependencies.electron,
    extraMetadata: {
      version,
      ...(fixtureID
        ? { name: `routevane-${fixtureID}`, main: 'fixture-entry.mjs' }
        : {}),
    },
    directories: {
      output: out,
      buildResources: join(root, '.cache/desktop/assets'),
    },
    files: [
      'src/**',
      ...(fixtureEntry
        ? [
            {
              from: dirname(fixtureEntry),
              to: '.',
              filter: ['fixture-entry.mjs'],
            },
          ]
        : []),
      'package.json',
      { from: '../.cache/desktop/assets', to: 'assets' },
    ],
    extraResources: [
      ...(installer ? [{ from: coreMetadata, to: '.' }] : []),
      { from: join(root, '.cache/build', binary), to: binary },
      { from: join(root, 'catalog'), to: 'catalog' },
      { from: join(root, 'LICENSE'), to: 'LICENSE' },
    ],
    asar: true,
    npmRebuild: false,
    publish,
    win: { icon: join(root, '.cache/desktop/assets/icon.ico'), target: 'nsis' },
    nsis: {
      oneClick: true,
      perMachine: false,
      allowElevation: false,
      runAfterFinish: true,
      deleteAppDataOnUninstall: false,
      include: fileURLToPath(new URL('installer.nsh', import.meta.url)),
      ...(fixtureID
        ? {
            createDesktopShortcut: false,
            createStartMenuShortcut: false,
            shortcutName: fixtureID,
          }
        : {}),
      artifactName: 'Routevane-${version}-${arch}-setup.${ext}',
    },
  },
})
