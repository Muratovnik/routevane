import { spawnSync } from 'node:child_process'
import { once } from 'node:events'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { ownedFiles } from './owned-files'

import AxeBuilder from '@axe-core/playwright'
import {
  expect,
  test as base,
  type Page,
  type WebSocketRoute,
} from '@playwright/test'

import {
  assertPortBindable,
  delay,
  reserveLoopbackPort,
  resolvesWithin,
  spawnProduct,
  type SpawnedProduct,
} from '../e2e/support/product'

const ROOT = resolve(import.meta.dirname, '../../..')

/** How long a supervisor has to confirm its exit before it is terminated. */
const EXIT_TIMEOUT_MILLISECONDS = 20_000

const processIsRunning = (pid: number): boolean => {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}

/** The servers a supervisor announced by process id as it started them. */
const reportedPids = (output: string): number[] =>
  [...output.matchAll(/PID (\d+)/g)].map((match) => Number(match[1]))

/**
 * Stands between the development client and its server, so a test can take that
 * connection away at a moment of its choosing while the server keeps running
 * for the claims that follow. Every socket the page opens from here on is
 * forwarded, and the returned call closes them.
 *
 * It announces `beforeunload` first, and that is the load-bearing part. A client
 * that loses its socket pings for the server and reloads the page the moment it
 * answers — measured here at about 30 ms, which is not a state anything can
 * observe. Its own rule is that a document already on its way out is not
 * reloaded, so the page says it is leaving and the loss is then reported and
 * stays reported. Nothing is unloaded and no server is touched.
 */
const holdOpenDevSocket = async (page: Page): Promise<() => Promise<void>> => {
  const forwarded: WebSocketRoute[] = []
  await page.routeWebSocket(/.*/, (socket) => {
    forwarded.push(socket)
    socket.connectToServer()
  })
  return async () => {
    await page.evaluate(() => window.dispatchEvent(new Event('beforeunload')))
    for (const socket of forwarded) await socket.close()
  }
}

interface DevSession {
  /** Starts the development supervisor with the arguments one test needs. */
  start: (args: string[]) => SpawnedProduct
  /** Creates a new probe and claims cleanup only once creation succeeds. */
  plant: (path: string, contents: string) => Promise<void>
}

/**
 * What a dev test owns: the supervisor it starts, and the probe files it plants
 * in the working tree for the watcher to pick up.
 *
 * Teardown stops whatever is still running, reclaims the servers the supervisor
 * printed, keeps its output beside the test's other artifacts and removes the
 * probes. It runs whether the test passed or failed, which is what keeps the
 * session's own claims — that closing stdin stops it, that it exits cleanly and
 * that its ports come back — in the test body instead of in a `finally` block
 * that would report a cleanup failure in place of the real one.
 */
const test = base.extend<{ session: DevSession }>({
  // Playwright reads a fixture's own dependencies off this parameter, and this
  // one has none: it owns processes and files rather than another fixture.
  session: async ({}, use, testInfo) => {
    const started: SpawnedProduct[] = []
    const planted = ownedFiles()
    await use({
      plant: planted.create,
      start: (args) => {
        const owner = spawnProduct('python', args, {
          cwd: ROOT,
          stdio: 'pipe',
          windowsHide: true,
        })
        started.push(owner)
        return owner
      },
    })
    try {
      for (const owner of started) {
        if (owner.isAlive()) {
          const exited = once(owner.process, 'exit')
          owner.process.stdin?.end()
          if (!(await resolvesWithin(exited, EXIT_TIMEOUT_MILLISECONDS)))
            owner.process.kill()
        }
        for (const pid of reportedPids(owner.output()).filter(processIsRunning))
          spawnSync('taskkill', ['/PID', String(pid), '/T', '/F'], {
            stdio: 'ignore',
            windowsHide: true,
          })
        await mkdir(testInfo.outputDir, { recursive: true })
        await writeFile(testInfo.outputPath('dev-session.log'), owner.output())
      }
    } finally {
      await planted.cleanup()
    }
  },
})

test('one dev session: real API, HMR, Go recovery and owned cleanup', async ({
  page,
  request,
  session,
}, testInfo) => {
  const port = await reserveLoopbackPort()
  const apiPort = await reserveLoopbackPort()
  const origin = `http://127.0.0.1:${port}`
  const api = `http://127.0.0.1:${apiPort}`
  const data = testInfo.outputPath('data')
  const probeName = `dev-hmr-probe-${process.pid}`
  const probe = resolve(ROOT, 'web/src/pages', `${probeName}.vue`)
  const goProbe = resolve(ROOT, 'cmd/routevane', `dev_probe_${process.pid}.go`)
  // The probe reports what a reload would have thrown away, so it is a named
  // live region rather than an anonymous paragraph: the test reads it by role
  // and name while its own text is what the edit changes.
  const source = `<script setup lang="ts">
import { ref } from 'vue'
const draft = ref('')
</script>
<template>
  <main><label>Draft<input v-model="draft" /></label><output id="dev-probe" aria-label="HMR probe">before-hmr</output></main>
</template>
<style>#dev-probe { color: rgb(1, 2, 3); }</style>
`
  const devProbe = page.getByRole('status', { name: 'HMR probe' })
  await session.plant(probe, source)
  const owner = session.start([
    'tools/dev_server.py',
    '--port',
    String(port),
    '--api-port',
    String(apiPort),
    '--no-browser',
    '--stop-on-stdin-close',
    '--data-dir',
    data,
    '--catalog-dir',
    'testdata/expiry/browser-catalog',
  ])
  await expect
    .poll(
      () => {
        owner.assertAlive()
        return owner.output()
      },
      { timeout: 90_000 },
    )
    .toContain('[dev] Ready:')

  const health = await request.get(`${origin}/health`)
  expect(await health.json()).toEqual({ status: 'ok' })
  expect(health.headers()['access-control-allow-origin']).toBeUndefined()
  expect((await request.get(`${origin}/v1/lists`)).ok()).toBe(true)
  const headers = { Origin: origin, 'X-Routevane-Request': '1' }
  const created = await request.post(`${origin}/v1/profiles`, {
    headers,
    data: { name: 'HMR profile', lists: ['youtube'] },
  })
  expect(created.ok(), await created.text()).toBe(true)
  const profilesBefore = await (
    await request.get(`${origin}/v1/profiles`)
  ).json()
  for (const foreign of ['http://evil.example', 'null', api]) {
    const refused = await request.post(`${origin}/v1/profiles`, {
      headers: { ...headers, Origin: foreign },
      data: { name: 'must not exist', lists: ['youtube'] },
    })
    expect(refused.status()).toBe(404)
  }
  expect(
    (
      await request.get(`${origin}/health`, {
        headers: { Host: 'evil.example' },
      })
    ).ok(),
  ).toBe(false)
  expect(
    (
      await request.post(`${origin}/v1/profiles`, {
        headers: { Origin: origin },
        data: { name: 'no marker', lists: ['youtube'] },
      })
    ).status(),
  ).toBe(403)
  expect(
    (
      await request.post(`${api}/v1/profiles`, {
        headers,
        data: { name: 'direct foreign origin', lists: ['youtube'] },
      })
    ).status(),
  ).toBe(403)
  expect(await (await request.get(`${origin}/v1/profiles`)).json()).toEqual(
    profilesBefore,
  )

  await page.goto(origin)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Profiles')
  await expect(page.getByText('HMR profile', { exact: true })).toBeVisible()
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    await page.screenshot({ path: testInfo.outputPath(`dev-${width}.png`) })
    const audit = await new AxeBuilder({ page }).analyze()
    expect(
      audit.violations.filter(({ impact }) =>
        ['serious', 'critical'].includes(impact ?? ''),
      ),
    ).toEqual([])
  }
  await page.keyboard.press('Tab')
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe(
    'BODY',
  )

  await page.goto(`${origin}/${probeName}`)
  await page.getByRole('textbox', { name: 'Draft' }).fill('keep my draft')
  await expect(devProbe).toHaveText('before-hmr')
  await page.evaluate(() => {
    document.documentElement.dataset.hmrSentinel = 'same-document'
  })
  const templateEdit = source.replace('before-hmr', 'after-hmr')
  await writeFile(probe, templateEdit)
  await expect(devProbe).toHaveText('after-hmr')
  await expect(page.getByRole('textbox', { name: 'Draft' })).toHaveValue(
    'keep my draft',
  )
  // The pinned watcher coalesces same-path change events for 50 ms. Model
  // two editor saves, not two writes inside the same coalescing window.
  await delay(100)
  await writeFile(probe, templateEdit.replace('rgb(1, 2, 3)', 'rgb(4, 5, 6)'))
  await expect(devProbe).toHaveCSS('color', 'rgb(4, 5, 6)')
  await expect(page.getByRole('textbox', { name: 'Draft' })).toHaveValue(
    'keep my draft',
  )
  // The sentinel lives on the document element, which no role names; the
  // module replacement above has already settled, so one read is enough.
  expect(
    await page.evaluate(() => document.documentElement.dataset.hmrSentinel),
  ).toBe('same-document')
  expect(owner.output()).not.toContain('build 2)')

  await session.plant(goProbe, 'package main\nthis is not valid Go\n')
  await expect
    .poll(() => owner.output())
    .toContain('Go build failed; last working backend stays up')
  expect((await request.get(`${origin}/health`)).ok()).toBe(true)
  await writeFile(goProbe, 'package main\nconst devProbe = "recovered"\n')
  await expect.poll(() => owner.output()).toContain('build 2)')
  expect(await (await request.get(`${origin}/v1/profiles`)).json()).toEqual(
    profilesBefore,
  )
  await page.reload()
  await expect(devProbe).toHaveText('after-hmr')

  // The development server's socket is the only thing telling an open page that
  // the code it is running can still be rebuilt, and its client reports the
  // loss to the console alone. The surface says it instead. The page is used
  // for nothing else after this, so its socket is routed through the test only
  // now — every claim above ran on the client's own connection.
  const dropDevSocket = await holdOpenDevSocket(page)
  await page.goto(origin)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Profiles')
  await dropDevSocket()
  await expect(
    page
      .getByRole('status')
      .filter({ hasText: 'Development server disconnected' }),
  ).toBeVisible()

  // Owned cleanup is this session's last claim, and it is asserted here rather
  // than in teardown: closing stdin stops the supervisor, it reports a clean
  // exit, and both ports it held are bindable again.
  const exited = once(owner.process, 'exit')
  owner.process.stdin?.end()
  expect(
    await resolvesWithin(exited, EXIT_TIMEOUT_MILLISECONDS),
    owner.output(),
  ).toBe(true)
  expect(owner.process.exitCode, owner.output()).toBe(0)
  await assertPortBindable(port)
  await assertPortBindable(apiPort)
})

test('abrupt Windows supervisor termination releases its servers', async ({
  request,
  session,
}, testInfo) => {
  test.skip(process.platform !== 'win32', 'Windows Job Object behavior')
  const port = await reserveLoopbackPort()
  const apiPort = await reserveLoopbackPort()
  const origin = `http://127.0.0.1:${port}`
  const owner = session.start([
    'tools/dev_server.py',
    '--port',
    String(port),
    '--api-port',
    String(apiPort),
    '--no-browser',
    '--data-dir',
    testInfo.outputPath('abrupt-data'),
    '--catalog-dir',
    'testdata/expiry/browser-catalog',
  ])
  await expect
    .poll(
      () => {
        owner.assertAlive()
        return owner.output()
      },
      { timeout: 90_000 },
    )
    .toContain('[dev] Ready:')
  expect((await request.get(`${origin}/health`)).ok()).toBe(true)
  const childPids = reportedPids(owner.output())
  expect(childPids).toHaveLength(2)
  expect(childPids.every(processIsRunning)).toBe(true)

  const exited = once(owner.process, 'exit')
  // On Windows ChildProcess.kill calls TerminateProcess for this PID; it does
  // not ask the supervisor to perform its normal stdin/CTRL_BREAK cleanup.
  expect(owner.process.kill()).toBe(true)
  expect(await resolvesWithin(exited, 10_000), owner.output()).toBe(true)
  await expect
    .poll(() => childPids.every((pid) => !processIsRunning(pid)))
    .toBe(true)
  await expect
    .poll(async () => {
      try {
        await assertPortBindable(port)
        await assertPortBindable(apiPort)
        return true
      } catch {
        return false
      }
    })
    .toBe(true)
})
