import { spawnSync } from 'node:child_process'
import { once } from 'node:events'
import { mkdir, unlink, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

import {
  assertPortBindable,
  delay,
  reserveLoopbackPort,
  resolvesWithin,
  spawnProduct,
} from '../e2e/support/product'

test('one dev session: real API, HMR, Go recovery and owned cleanup', async ({
  page,
  request,
}, testInfo) => {
  const root = resolve(import.meta.dirname, '../../..')
  const port = await reserveLoopbackPort()
  const apiPort = await reserveLoopbackPort()
  const origin = `http://127.0.0.1:${port}`
  const api = `http://127.0.0.1:${apiPort}`
  const data = testInfo.outputPath('data')
  const probeName = `dev-hmr-probe-${process.pid}`
  const probe = resolve(root, 'web/src/pages', `${probeName}.vue`)
  const goProbe = resolve(root, 'cmd/routevane', `dev_probe_${process.pid}.go`)
  const source = `<script setup lang="ts">
import { ref } from 'vue'
const draft = ref('')
</script>
<template>
  <main><label>Draft<input v-model="draft" /></label><p id="dev-probe">before-hmr</p></main>
</template>
<style>#dev-probe { color: rgb(1, 2, 3); }</style>
`
  await writeFile(probe, source, { flag: 'wx' })
  const owner = spawnProduct(
    'python',
    [
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
    ],
    { cwd: root, stdio: 'pipe', windowsHide: true },
  )
  let goProbeCreated = false
  try {
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
    await expect(page.locator('#dev-probe')).toHaveText('before-hmr')
    await page.evaluate(() => {
      document.documentElement.dataset.hmrSentinel = 'same-document'
    })
    const templateEdit = source.replace('before-hmr', 'after-hmr')
    await writeFile(probe, templateEdit)
    await expect(page.locator('#dev-probe')).toHaveText('after-hmr')
    await expect(page.getByRole('textbox', { name: 'Draft' })).toHaveValue(
      'keep my draft',
    )
    // The pinned watcher coalesces same-path change events for 50 ms. Model
    // two editor saves, not two writes inside the same coalescing window.
    await delay(100)
    await writeFile(probe, templateEdit.replace('rgb(1, 2, 3)', 'rgb(4, 5, 6)'))
    await expect(page.locator('#dev-probe')).toHaveCSS('color', 'rgb(4, 5, 6)')
    await expect(page.getByRole('textbox', { name: 'Draft' })).toHaveValue(
      'keep my draft',
    )
    await expect(page.locator('html')).toHaveAttribute(
      'data-hmr-sentinel',
      'same-document',
    )
    expect(owner.output()).not.toContain('build 2)')

    await writeFile(goProbe, 'package main\nthis is not valid Go\n', {
      flag: 'wx',
    })
    goProbeCreated = true
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
    await expect(page.locator('#dev-probe')).toHaveText('after-hmr')
  } finally {
    try {
      if (owner.process.exitCode === null) {
        const exited = once(owner.process, 'exit')
        owner.process.stdin?.end()
        expect(await resolvesWithin(exited, 20_000), owner.output()).toBe(true)
      }
    } finally {
      await unlink(probe)
      if (goProbeCreated) await unlink(goProbe)
      await mkdir(testInfo.outputDir, { recursive: true })
      await writeFile(testInfo.outputPath('dev-session.log'), owner.output())
    }
    expect(owner.process.exitCode, owner.output()).toBe(0)
    await assertPortBindable(port)
    await assertPortBindable(apiPort)
  }
})

test('abrupt Windows supervisor termination releases its servers', async ({
  request,
}, testInfo) => {
  test.skip(process.platform !== 'win32', 'Windows Job Object behavior')
  const root = resolve(import.meta.dirname, '../../..')
  const port = await reserveLoopbackPort()
  const apiPort = await reserveLoopbackPort()
  const origin = `http://127.0.0.1:${port}`
  const owner = spawnProduct(
    'python',
    [
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
    ],
    { cwd: root, stdio: 'pipe', windowsHide: true },
  )
  let childPids: number[] = []
  try {
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
    childPids = [...owner.output().matchAll(/PID (\d+)/g)].map((match) =>
      Number(match[1]),
    )
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
  } finally {
    if (owner.isAlive()) {
      const exited = once(owner.process, 'exit')
      owner.process.stdin?.end()
      if (!(await resolvesWithin(exited, 20_000))) owner.process.kill()
    }
    for (const pid of childPids.filter(processIsRunning)) {
      spawnSync('taskkill', ['/PID', String(pid), '/T', '/F'], {
        stdio: 'ignore',
        windowsHide: true,
      })
    }
    await mkdir(testInfo.outputDir, { recursive: true })
    await writeFile(testInfo.outputPath('abrupt-session.log'), owner.output())
  }
})

function processIsRunning(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch {
    return false
  }
}
