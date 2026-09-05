import {
  access,
  copyFile,
  mkdir,
  readFile,
  rm,
  writeFile,
} from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

import { dictionaries } from '../../src/shared/i18n/messages'

import {
  assertPortBindable,
  reserveLoopbackPort,
  spawnProduct,
  stopOwnedProduct,
  type SpawnedProduct,
} from './support/product'

const testDirectory = dirname(fileURLToPath(import.meta.url))
const repositoryRoot = resolve(testDirectory, '..', '..', '..')
const dataRoot = join(repositoryRoot, '.cache', 'deploy-browser-data')
const singBoxRoot = join(repositoryRoot, '.cache', 'deploy-browser-singbox')
const catalogRoot = join(repositoryRoot, '.cache', 'deploy-browser-catalog')
const sourceCatalog = join(
  repositoryRoot,
  'testdata',
  'expiry',
  'browser-catalog',
)
const configPath = join(singBoxRoot, 'config.json')
const ruleSetPath = join(singBoxRoot, 'routevane.json')
const binary = join(
  repositoryRoot,
  '.cache',
  'build',
  process.platform === 'win32' ? 'routing-agent.exe' : 'routing-agent',
)

let origin = ''
let port = 0
let managedProduct: SpawnedProduct | undefined

// The surface picks its language from the browser. English is the product's
// primary language, so the walkthrough runs against the English dictionary.
test.use({ locale: 'en-US' })

// A file URL always uses forward slashes, including on Windows.
function fileURL(path: string): string {
  const normalized = path.replaceAll('\\', '/')
  return `file://${normalized.startsWith('/') ? '' : '/'}${normalized}`
}

/**
 * publishList walks the surface the way an operator does — a route holding
 * YouTube, published for the chosen consumer — and answers with the route and
 * output
 * identities. The send screen only exists for an output that was really
 * published, so the test never fabricates an artifact.
 */
// The format is chosen from a searchable list of names, so an identity a
// caller names is resolved to the words that list actually shows.
const formatNames: Record<string, RegExp> = {
  keenetic: /Keenetic/,
  mikrotik: /MikroTik/,
  singbox: /sing-box/,
}

async function publishList(
  page: Page,
  targetID: string,
  language: 'en' | 'ru' = 'en',
): Promise<{ listId: string; outputId: string }> {
  await page.goto(`${origin}/lists/new`)
  await expect(
    page.getByRole('heading', {
      level: 1,
      name: message(language, 'create.title'),
    }),
  ).toBeVisible()
  await page
    .getByRole('searchbox', { name: message(language, 'create.search') })
    .fill('youtube')
  await page.locator('input[value="youtube"]').check()
  await page.locator('.rv-search-select__trigger--field').click()
  await page.getByRole('option', { name: formatNames[targetID] }).click()
  await expect(page.locator('#create-target')).not.toHaveText(
    'Choose a device or application',
  )
  const outputsResponse = page.waitForResponse(
    (candidate) =>
      candidate.request().method() === 'POST' &&
      /\/v1\/lists\/[a-f0-9]{32}\/outputs$/.test(
        new URL(candidate.url()).pathname,
      ),
  )
  await page
    .getByRole('button', { name: message(language, 'create.submit') })
    .click()
  await page.waitForURL(/\/lists\/[a-f0-9]{32}(?:[#?].*)?$/)
  await expect(
    page.getByRole('heading', { level: 1, name: 'YouTube' }),
  ).toBeVisible()
  const listId =
    /\/lists\/([a-f0-9]{32})/.exec(new URL(page.url()).pathname)?.[1] ?? ''
  const outputPayload = (await (await outputsResponse).json()) as {
    output: { id: string }
  }
  // Waiting for the add-format control to re-enable is waiting for the whole
  // bind — create, refresh, build, reload — to actually finish.
  await expect(page.locator('#outputs-target')).toBeEnabled()
  return { listId, outputId: outputPayload.output.id }
}

function message(language: 'en' | 'ru', key: string): string {
  const value = dictionaries[language][key]
  if (typeof value !== 'string') throw new Error(`Not a string message: ${key}`)
  return value
}

test.beforeAll(async () => {
  await access(binary)
  await rm(dataRoot, { force: true, recursive: true })
  await rm(singBoxRoot, { force: true, recursive: true })
  await rm(catalogRoot, { force: true, recursive: true })
  await mkdir(singBoxRoot, { recursive: true })
  // The operator's own configuration decides where the rule set goes. This is a
  // real sing-box configuration shape, not a Routevane-specific file.
  await writeFile(
    configPath,
    `${JSON.stringify(
      {
        log: { level: 'warn' },
        route: {
          rule_set: [
            {
              tag: 'routevane',
              type: 'local',
              format: 'source',
              path: 'routevane.json',
            },
          ],
        },
      },
      null,
      2,
    )}\n`,
    'utf8',
  )

  // The browser catalog plus one real product target that has no deployer:
  // MikroTik is copied verbatim from the shipped catalog, so the "manual only"
  // path is exercised against a target the product actually offers.
  await mkdir(join(catalogRoot, 'builtin'), { recursive: true })
  await mkdir(join(catalogRoot, 'targets'), { recursive: true })
  for (const name of ['discord.yaml', 'youtube.yaml']) {
    await copyFile(
      join(sourceCatalog, 'builtin', name),
      join(catalogRoot, 'builtin', name),
    )
  }
  for (const name of ['keenetic.yaml', 'singbox.yaml']) {
    await copyFile(
      join(sourceCatalog, 'targets', name),
      join(catalogRoot, 'targets', name),
    )
  }
  await copyFile(
    join(repositoryRoot, 'catalog', 'targets', 'mikrotik.yaml'),
    join(catalogRoot, 'targets', 'mikrotik.yaml'),
  )

  port = await reserveLoopbackPort()
  origin = `http://127.0.0.1:${port}`
  managedProduct = spawnProduct(
    binary,
    [
      'serve',
      '--port',
      String(port),
      '--catalog-dir',
      catalogRoot,
      '--data-dir',
      dataRoot,
    ],
    { cwd: repositoryRoot, stdio: 'pipe', windowsHide: true },
  )
  const deadline = Date.now() + 20_000
  for (;;) {
    managedProduct.assertAlive()
    try {
      const response = await fetch(`${origin}/health`)
      if (response.ok) break
    } catch {
      // The listener is not up yet.
    }
    if (Date.now() > deadline) {
      throw new Error(`product did not start: ${managedProduct.output()}`)
    }
    await new Promise((done) => setTimeout(done, 100))
  }
})

test.afterAll(async () => {
  let cleanupError: unknown
  try {
    if (managedProduct !== undefined)
      await stopOwnedProduct(managedProduct.process)
    if (port !== 0) await assertPortBindable(port)
  } catch (error) {
    cleanupError = error
  }
  try {
    await rm(dataRoot, { force: true, recursive: true })
    await rm(singBoxRoot, { force: true, recursive: true })
    await rm(catalogRoot, { force: true, recursive: true })
  } catch (error) {
    cleanupError ??= error
  }
  if (cleanupError !== undefined) throw cleanupError
})

// This is the whole point of the slice: the form is exactly what the deployer
// declares — a local destination is asked only for its configuration path —
// and an operator who published an output applies it from the send screen.
test('the send screen builds its form from the deployer and applies the file locally', async ({
  page,
}) => {
  test.setTimeout(120000)
  const { listId, outputId } = await publishList(page, 'singbox')
  const listURL = `${origin}/lists/${listId}`

  // The send action exists on the route page because this format has a
  // deployer, and it leads to that output's own send screen.
  await page
    .locator('main')
    .getByRole('button', { name: 'Actions for route YouTube', exact: true })
    .click()
  await page
    .locator('.rv-menu__panel:visible')
    .getByRole('menuitem', { name: 'Send to sing-box' })
    .click()
  await page.waitForURL(`${listURL}/send/${outputId}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Send to device' }),
  ).toBeVisible()
  await expect(page.getByText('Route: YouTube')).toBeVisible()
  await expect(page.locator('.rv-status__label')).toHaveText('Not applied')

  // A destination on this machine has nowhere to send a credential, so the
  // form the deployer describes is the form the screen renders — no more.
  await expect(page.locator('#send-device')).toBeVisible()
  expect(
    await page
      .locator('label[for="send-device"]')
      .evaluate((element) => element.textContent),
  ).toBe('Path to the sing-box configuration')
  await expect(page.locator('#send-device')).toHaveAttribute(
    'placeholder',
    'file:///C:/sing-box/config.json',
  )
  await expect(page.locator('#send-user')).toHaveCount(0)
  await expect(page.locator('#send-password')).toHaveCount(0)
  await expect(page.locator('#send-interface')).toHaveCount(0)
  const review = page.getByRole('button', { name: 'Check the details' })
  await expect(review).toBeVisible()

  // Installing by hand is the other supported path, present on this screen
  // always, and the catalog is what says where this particular file belongs.
  await expect(
    page.getByRole('heading', { level: 2, name: 'By hand' }),
  ).toBeVisible()
  await expect(
    page.locator('.send__manual').getByRole('link', { name: 'Download JSON' }),
  ).toHaveAttribute('href', /^\/v1\/artifacts\/[a-f0-9]{32}$/)
  await expect(
    page.getByText(
      'Save the file as a local sing-box source rule-set, then reference it from a rule_set entry with type local and format source.',
    ),
  ).toBeVisible()

  // The address form a destination accepts is the destination's own statement:
  // the screen offers this example as the placeholder, so it must accept it.
  await page.locator('#send-device').fill(fileURL(configPath))
  await expect(
    review,
    'the address the deployer itself declares must be accepted',
  ).toBeEnabled()
  await review.click()
  await expect(page.locator('.rv-status__label')).toHaveText('Ready to apply')
  await expect(
    page.getByRole('heading', { name: 'What will happen' }),
  ).toBeVisible()
  await expect(
    page.getByText('A backup is taken before anything changes.'),
  ).toBeVisible()

  // Nothing is written until the second, explicit action.
  await expect(
    readFile(ruleSetPath, 'utf8').then(
      () => 'written',
      () => 'absent',
    ),
  ).resolves.toBe('absent')

  await page.getByRole('button', { name: 'Apply' }).click()
  await expect(page.locator('.rv-status__label')).toHaveText('Applied')
  await expect(page.getByText('The file is applied and verified')).toBeVisible()

  // Every lifecycle step is reported, and the file on disk is the artifact.
  await expect(page.locator('.send__steps li')).toHaveCount(4)
  await expect(page.locator('.send__step-outcome--failed')).toHaveCount(0)
  const deployed = await readFile(ruleSetPath, 'utf8')
  expect(deployed).toContain('youtube.com')
  expect(JSON.parse(deployed).version).toBe(3)

  // The password is used once and never outlives the attempt, and no store may
  // hold a subscription secret. The address is deliberately remembered for the
  // tab, which is why this asserts what must be absent rather than that
  // nothing at all was written.
  const stored = await page.evaluate(() => ({
    local: { ...window.localStorage },
    session: { ...window.sessionStorage },
  }))
  expect(Object.keys(stored.local)).not.toContain('rv.deploy.password')
  expect(Object.keys(stored.session)).not.toContain('rv.deploy.password')
  expect(JSON.stringify(stored)).not.toContain('rv1.')
  expect(await page.locator('#send-device').inputValue()).toBe(
    fileURL(configPath),
  )
})

for (const language of ['en', 'ru'] as const) {
  test.describe(`send recovery ${language}`, () => {
    test.use({ locale: language === 'ru' ? 'ru-RU' : 'en-US' })
    const copy = (key: string): string => message(language, key)
    test('send entry distinguishes unavailable, missing route, missing connection and missing file', async ({
      page,
    }) => {
      test.setTimeout(180000)
      async function auditEntry(): Promise<void> {
        const previous = page.viewportSize()
        for (const width of [320, 768, 1024, 1440]) {
          await page.setViewportSize({ width, height: 900 })
          expect(
            await page.evaluate(
              () =>
                document.documentElement.scrollWidth <=
                document.documentElement.clientWidth,
            ),
          ).toBe(true)
          expect(
            (
              await new AxeBuilder({ page })
                .withTags([
                  'wcag2a',
                  'wcag2aa',
                  'wcag21a',
                  'wcag21aa',
                  'wcag22aa',
                ])
                .analyze()
            ).violations,
          ).toEqual([])
        }
        if (previous !== null) await page.setViewportSize(previous)
      }
      const { listId, outputId } = await publishList(page, 'singbox', language)
      const listURL = `${origin}/lists/${listId}`
      const detailURL = `${origin}/v1/lists/${listId}`
      let unavailable = true
      let pendingRead: (() => void) | undefined
      const mutations: string[] = []
      page.on('request', (request) => {
        // A debounced draft forecast may finish as the route page is left.
        // POST carries its input; this exact endpoint writes no product state.
        const forecast =
          request.method() === 'POST' &&
          request.url() === `${origin}/v1/lists/preview`
        if (request.method() !== 'GET' && !forecast)
          mutations.push(request.url())
      })
      await page.route(detailURL, async (route) => {
        if (unavailable) {
          await route.fulfill({
            status: 500,
            json: { error: 'controlled read failure' },
          })
          return
        }
        await new Promise<void>((resolveRead) => {
          pendingRead = resolveRead
        })
        await route.continue()
      })
      await page
        .locator('main')
        .getByRole('button', {
          name: copy('library.menu').replace('{name}', 'YouTube'),
          exact: true,
        })
        .click()
      await page
        .locator('.rv-menu__panel:visible')
        .getByRole('menuitem', {
          name: copy('library.sendTarget').replace('{target}', 'sing-box'),
        })
        .click()
      await page.waitForURL(`${listURL}/send/${outputId}`)
      await expect(
        page.getByText(copy('send.route.failed'), { exact: true }),
      ).toBeVisible()
      await expect(
        page.getByText(copy('list.missing'), { exact: true }),
      ).toHaveCount(0)
      await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
      await expect(page.getByRole('main')).toHaveCount(1)
      await auditEntry()
      unavailable = false
      await page
        .getByRole('button', { name: copy('action.retry'), exact: true })
        .click()
      await expect(
        page.getByText(copy('list.loading'), { exact: true }),
      ).toBeVisible()
      await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
      await auditEntry()
      await expect.poll(() => pendingRead !== undefined).toBe(true)
      pendingRead!()
      await expect(page.locator('#send-device')).toBeVisible()
      await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
      await page.unroute(detailURL)

      const missingID = '0'.repeat(32)
      await page.goto(`${listURL}/send/${missingID}`)
      await expect(
        page.getByText(copy('send.connection.missing'), { exact: true }),
      ).toBeVisible()
      await expect(page.getByRole('main').getByRole('link')).toHaveAttribute(
        'href',
        `/lists/${listId}`,
      )
      await expect(
        page.getByText(copy('list.missing'), { exact: true }),
      ).toHaveCount(0)

      await auditEntry()
      await page.route(detailURL, async (route) => {
        const response = await route.fetch()
        const payload = (await response.json()) as {
          outputs: { id: string; latest: unknown }[]
        }
        const output = payload.outputs.find(
          (candidate) => candidate.id === outputId,
        )
        expect(output).toBeDefined()
        output!.latest = null
        await route.fulfill({ response, json: payload })
      })
      await page.goto(`${listURL}/send/${outputId}`)
      await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
      await expect(
        page.getByText(copy('library.noArtifact'), { exact: true }),
      ).toBeVisible()
      await expect(page.locator('#send-device')).toHaveCount(0)
      await page.unroute(detailURL)

      await auditEntry()
      const absentResponse = page.waitForResponse(
        `${origin}/v1/lists/${missingID}`,
      )
      await page.goto(`${origin}/lists/${missingID}/send/${outputId}`)
      expect((await absentResponse).status()).toBe(404)
      await expect(
        page.getByText(copy('list.missing'), { exact: true }),
      ).toBeVisible()
      await expect(
        page.getByRole('button', { name: copy('action.retry'), exact: true }),
      ).toHaveCount(0)
      await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1)
      await auditEntry()
      expect(mutations).toEqual([])
      // Prove the observer still catches a real write endpoint while allowing
      // the read-only forecast. Invalid JSON makes these probes non-mutating.
      const updateURL = `${detailURL}/update`
      await page.evaluate(
        async (urls) => {
          for (const url of urls) {
            await fetch(url, {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                'X-Routevane-Request': '1',
              },
              body: '{',
            })
          }
        },
        [`${origin}/v1/lists/preview`, updateURL],
      )
      expect(mutations).toEqual([updateURL])
    })
  })
}

// A format nothing can install automatically still has a send screen: it says
// so in words and offers the manual path instead of an unusable form.

test('a target without a deployer is offered the manual path only', async ({
  page,
}) => {
  test.setTimeout(120000)
  const { listId, outputId } = await publishList(page, 'mikrotik')
  const listURL = `${origin}/lists/${listId}`

  // The route page never offers an automatic send for this format.
  await expect(page.locator('.rv-status__label')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Send to device' })).toHaveCount(
    0,
  )

  await page.goto(`${listURL}/send/${outputId}`)
  await expect(
    page.getByRole('heading', { level: 1, name: 'Send to device' }),
  ).toBeVisible()
  await expect(
    page.getByText('Automatic delivery to MikroTik is unavailable'),
  ).toBeVisible()
  await expect(page.locator('#send-device')).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Check the details' }),
  ).toHaveCount(0)

  // The manual section carries the file and the catalog's own instruction.
  await expect(
    page.getByRole('heading', { level: 2, name: 'By hand' }),
  ).toBeVisible()
  await expect(page.locator('.send__manual').getByRole('link')).toHaveAttribute(
    'href',
    /^\/v1\/artifacts\/[a-f0-9]{32}$/,
  )
  // The instruction is the catalog's own, and the catalog may state it in one
  // language or two. What must reach the screen is the instruction itself.
  await expect(page.locator('.send__manual .send__note')).toContainText(
    '/import',
  )
})

test('a destination the configuration cannot accept is refused before writing', async ({
  page,
}) => {
  test.setTimeout(120000)
  const foreignRoot = join(repositoryRoot, '.cache', 'deploy-browser-foreign')
  await rm(foreignRoot, { force: true, recursive: true })
  await mkdir(foreignRoot, { recursive: true })
  const foreignConfig = join(foreignRoot, 'config.json')
  await writeFile(
    foreignConfig,
    `${JSON.stringify({
      route: {
        rule_set: [
          {
            tag: 'compiled',
            type: 'local',
            format: 'binary',
            path: 'compiled.srs',
          },
        ],
      },
    })}\n`,
    'utf8',
  )

  const { listId, outputId } = await publishList(page, 'singbox')
  await page.goto(`${origin}/lists/${listId}/send/${outputId}`)
  await expect(page.locator('#send-device')).toBeVisible()
  await page.locator('#send-device').fill(fileURL(foreignConfig))
  const review = page.getByRole('button', { name: 'Check the details' })
  await expect(
    review,
    'the address the deployer itself declares must be accepted',
  ).toBeEnabled()
  await review.click()
  await expect(page.locator('.rv-status__label')).toHaveText('Ready to apply')
  await page.getByRole('button', { name: 'Apply' }).click()

  await expect(page.locator('.rv-status__label')).toHaveText('Not applied')
  // The refusal reaches the operator as a sentence about the device, never as
  // the server's own words.
  const visible = (await page.locator('body').innerText()).toLowerCase()
  expect(visible).not.toContain('operation failed')
  // The refusal happens at the probe, so nothing in that directory changed.
  await expect(
    readFile(join(foreignRoot, 'compiled.srs'), 'utf8').then(
      () => 'written',
      () => 'absent',
    ),
  ).resolves.toBe('absent')
  await rm(foreignRoot, { force: true, recursive: true })
})
