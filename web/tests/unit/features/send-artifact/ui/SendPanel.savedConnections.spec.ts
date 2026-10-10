import { afterEach, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-vue'

import SendPanel from '@/features/send-artifact/ui/SendPanel.vue'
import { useLocale } from '@/shared/i18n/useLocale'

const json = (payload: unknown, status = 200): Response =>
  new Response(JSON.stringify(payload), { status })

const outcome = (
  attemptID: string,
  status: 'succeeded' | 'outcome_unknown',
) => ({
  attempt_id: attemptID,
  artifact_id: 'artifact',
  status,
  result: {
    applied: status === 'succeeded',
    rolled_back: false,
    device: {},
    events: [],
  },
})

const scenario = (
  consent = true,
  status: 'succeeded' | 'outcome_unknown' = 'succeeded',
) => {
  const selected = {
    id: 'd'.repeat(32),
    target_id: 'keenetic',
    target_title: 'Keenetic',
    name: 'Home router',
    address: 'http://192.168.1.1',
    account: 'home-admin',
    interface: 'Wireguard0',
    auto_deliver: consent,
    deployable: true,
  }
  const alternate = {
    ...selected,
    id: 'c'.repeat(32),
    name: 'Office router',
    address: 'http://192.168.1.2',
    account: 'office-admin',
    interface: 'Wireguard1',
    auto_deliver: false,
  }
  const fetchMock = vi.fn(async (input: unknown, init?: RequestInit) => {
    const path = String(input)
    if (path === '/v1/devices') {
      return json({
        devices: [
          alternate,
          selected,
          {
            ...selected,
            id: 'b'.repeat(32),
            name: 'Wrong format',
            target_id: 'singbox',
          },
          {
            ...selected,
            id: 'a'.repeat(32),
            name: 'No adapter',
            deployable: false,
          },
        ],
        secret_store_available: true,
      })
    }
    if (path === '/v1/deployments/targets') {
      return json({
        targets: [
          {
            target_id: 'keenetic',
            title: 'Keenetic',
            deployer_id: 'keenetic-route-bat',
            requirements: {
              address_label: 'Address',
              address_example: 'http://192.168.1.1',
              needs_credential: true,
              needs_interface: true,
              interface_label: 'Interface',
            },
          },
        ],
      })
    }
    if (path.startsWith('/v1/deployment-attempts/'))
      return json(
        outcome(path.slice('/v1/deployment-attempts/'.length), status),
      )
    if (path === '/v1/artifacts/artifact/deploy') {
      const body = JSON.parse(String(init?.body)) as {
        confirm: boolean
        attempt_id?: string
      }
      return body.confirm
        ? json(outcome(body.attempt_id ?? '', status))
        : json({
            plan: {
              artifact_id: 'artifact',
              artifact_hash: 'f'.repeat(64),
              target_id: 'keenetic',
              title: 'Keenetic',
              deployer_id: 'keenetic-route-bat',
              size_bytes: 10,
            },
          })
    }
    throw new Error(`unexpected request ${path}`)
  })
  const props = {
    artifactId: 'artifact',
    profileId: 'profile',
    profileName: 'Profile',
    targetId: 'keenetic',
    target: null,
    preferredDeviceId: selected.id,
  }
  return { selected, alternate, fetchMock, props }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  window.sessionStorage.clear()
})

it.each([
  { language: 'en', consent: true },
  { language: 'ru', consent: true },
  { language: 'en', consent: false },
  { language: 'ru', consent: false },
] as const)(
  'shows the bound access and uses consent=$consent in $language',
  async ({ language, consent }) => {
    const { setLocale, t } = useLocale()
    setLocale(language)
    const credential = crypto.randomUUID()
    const { selected, fetchMock, props } = scenario(consent)
    vi.stubGlobal('fetch', fetchMock)
    const screen = await render(SendPanel, { props })
    const connection = screen.getByRole('combobox', {
      name: t('send.connection.label'),
    })
    await expect.element(connection).toHaveTextContent(selected.name)
    await expect
      .element(screen.getByText(selected.address, { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText(selected.account, { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText(selected.interface, { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByRole('textbox', { name: t('send.field.account') }))
      .not.toBeInTheDocument()
    const password = screen.getByLabelText(t('send.field.password'))
    const review = screen.getByRole('button', {
      name: t('send.review'),
      exact: true,
    })
    await expect.element(password).toHaveValue('')
    await expect
      .element(
        screen.getByText(
          t(consent ? 'send.password.stored' : 'send.password.once'),
        ),
      )
      .toBeVisible()
    expect(review.element()).toHaveProperty('disabled', !consent)
    if (!consent) await password.fill(credential)
    await review.click()
    await screen
      .getByRole('button', { name: t('send.apply'), exact: true })
      .click()
    await expect
      .element(screen.getByText(t('send.applied'), { exact: true }))
      .toBeVisible()
    await expect.element(password).toHaveValue('')
    const requests = fetchMock.mock.calls
      .filter(([input]) => String(input).endsWith('/deploy'))
      .map(
        ([, init]) => JSON.parse(String(init?.body)) as Record<string, unknown>,
      )
    expect(requests).toHaveLength(2)
    expect(requests[0]).toEqual({
      device_id: selected.id,
      expected_connection: {
        device: selected.address,
        username: selected.account,
        interface: selected.interface,
      },
      password: consent ? '' : credential,
      confirm: false,
    })
    expect(requests[1]).toMatchObject({
      ...requests[0],
      confirm: true,
      attempt_id: expect.stringMatching(/^[a-f0-9]{32}$/),
    })
    expect(
      fetchMock.mock.calls.filter(
        ([input]) =>
          String(input).includes('/auto-delivery') ||
          String(input).includes('/device'),
      ),
    ).toHaveLength(1)
    expect(JSON.stringify({ ...window.sessionStorage })).not.toContain(
      credential,
    )
  },
)

it('offers only compatible connections and discards the prior preview and password on selection', async () => {
  const { t } = useLocale()
  const credential = crypto.randomUUID()
  const { alternate, fetchMock, props } = scenario()
  vi.stubGlobal('fetch', fetchMock)
  const screen = await render(SendPanel, { props })
  const password = screen.getByLabelText(t('send.field.password'))
  await password.fill(credential)
  await screen
    .getByRole('button', { name: t('send.review'), exact: true })
    .click()
  await expect
    .element(screen.getByRole('button', { name: t('send.apply'), exact: true }))
    .toBeVisible()
  const connection = screen.getByRole('combobox', {
    name: t('send.connection.label'),
  })
  await connection.click()
  await expect
    .element(screen.getByRole('option', { name: /Wrong format/ }))
    .not.toBeInTheDocument()
  await expect
    .element(screen.getByRole('option', { name: /No adapter/ }))
    .not.toBeInTheDocument()
  await screen.getByRole('option', { name: /^Office router/ }).click()
  await expect
    .element(screen.getByText(alternate.address, { exact: true }))
    .toBeVisible()
  await expect.element(password).toHaveValue('')
  await expect
    .element(screen.getByRole('button', { name: t('send.apply'), exact: true }))
    .not.toBeInTheDocument()
  await expect
    .element(
      screen.getByRole('button', { name: t('send.review'), exact: true }),
    )
    .toBeDisabled()
  await connection.click()
  await screen
    .getByRole('option', { name: t('send.connection.manual'), exact: true })
    .click()
  await expect
    .element(screen.getByRole('textbox', { name: t('send.field.account') }))
    .toBeVisible()
  expect(
    fetchMock.mock.calls.filter(([input]) => String(input).endsWith('/deploy')),
  ).toHaveLength(1)
})

it('keeps manual entry usable when the saved registry cannot be loaded', async () => {
  const { t } = useLocale()
  const credential = crypto.randomUUID()
  const { fetchMock, props } = scenario()
  const healthyFetch = fetchMock.getMockImplementation()!
  fetchMock.mockImplementation(async (input, init) =>
    String(input) === '/v1/devices'
      ? json({ error: 'unavailable' }, 503)
      : healthyFetch(input, init),
  )
  vi.stubGlobal('fetch', fetchMock)
  const screen = await render(SendPanel, { props })
  await expect
    .element(screen.getByText(t('send.connection.failed')))
    .toBeVisible()
  await screen
    .getByRole('textbox', { name: t('deploy.field.address.keenetic') })
    .fill('http://192.168.1.8')
  await screen
    .getByRole('textbox', { name: t('send.field.account') })
    .fill('manual-admin')
  await screen.getByLabelText(t('send.field.password')).fill(credential)
  await screen
    .getByRole('textbox', { name: t('deploy.field.interface.keenetic') })
    .fill('Wireguard8')
  await screen
    .getByRole('button', { name: t('send.review'), exact: true })
    .click()
  await expect
    .element(screen.getByRole('button', { name: t('send.apply'), exact: true }))
    .toBeVisible()
  const request = fetchMock.mock.calls.find(([input]) =>
    String(input).endsWith('/deploy'),
  )
  expect(JSON.parse(String(request?.[1]?.body))).toEqual({
    device: 'http://192.168.1.8',
    username: 'manual-admin',
    password: credential,
    interface: 'Wireguard8',
    confirm: false,
  })
})

it.each(['succeeded', 'outcome_unknown'] as const)(
  'names the actual recipient of a %s delivery after the selection changes and the page reloads',
  async (status) => {
    const { setLocale, t } = useLocale()
    setLocale('en')
    const credential = crypto.randomUUID()
    const { selected, alternate, fetchMock, props } = scenario(true, status)
    vi.stubGlobal('fetch', fetchMock)
    const screen = await render(SendPanel, { props })
    const connection = screen.getByRole('combobox', {
      name: t('send.connection.label'),
    })
    await connection.click()
    await screen.getByRole('option', { name: /^Office router/ }).click()
    await screen.getByLabelText(t('send.field.password')).fill(credential)
    await screen
      .getByRole('button', { name: t('send.review'), exact: true })
      .click()
    await screen
      .getByRole('button', { name: t('send.apply'), exact: true })
      .click()
    const delivery = screen.getByRole('region', {
      name: t('send.attempt.title'),
    })
    await expect
      .element(delivery.getByText(alternate.address, { exact: true }))
      .toBeVisible()

    await connection.click()
    await screen.getByRole('option', { name: /^Home router/ }).click()
    await expect
      .element(
        screen
          .getByRole('region', { name: t('send.connection.selected') })
          .getByText(selected.address, { exact: true }),
      )
      .toBeVisible()
    await expect
      .element(delivery.getByText(alternate.address, { exact: true }))
      .toBeVisible()
    await expect
      .element(delivery.getByText(selected.address, { exact: true }))
      .not.toBeInTheDocument()
    expect(
      screen
        .getByRole('button', { name: t('send.review'), exact: true })
        .element(),
    ).toHaveProperty('disabled', status === 'outcome_unknown')
    if (status === 'outcome_unknown') {
      await delivery
        .getByRole('button', {
          name: t('send.outcomeUnknown.check'),
          exact: true,
        })
        .click()
    }
    await expect
      .element(
        delivery.getByText(
          t(
            status === 'outcome_unknown'
              ? 'send.outcomeUnknown.title'
              : 'send.applied',
          ),
          { exact: true },
        ),
      )
      .toBeVisible()
    await screen.unmount()

    const reloaded = await render(SendPanel, { props })
    await expect
      .element(
        reloaded.getByRole('combobox', { name: t('send.connection.label') }),
      )
      .toHaveTextContent(selected.name)
    const recovered = reloaded.getByRole('region', {
      name: t('send.attempt.title'),
    })
    await expect
      .element(recovered.getByText(alternate.address, { exact: true }))
      .toBeVisible()
    await expect
      .element(recovered.getByText(alternate.account, { exact: true }))
      .toBeVisible()
    await expect
      .element(recovered.getByText(alternate.interface, { exact: true }))
      .toBeVisible()
    await expect
      .element(recovered.getByText(selected.address, { exact: true }))
      .not.toBeInTheDocument()
    await expect
      .element(
        recovered.getByText(
          t(
            status === 'succeeded'
              ? 'send.applied'
              : 'send.outcomeUnknown.title',
          ),
          { exact: true },
        ),
      )
      .toBeVisible()
    await expect
      .element(reloaded.getByLabelText(t('send.field.password')))
      .toHaveValue('')
    expect(JSON.stringify({ ...window.sessionStorage })).not.toContain(
      credential,
    )
    const writes = fetchMock.mock.calls.filter(
      ([input, init]) =>
        String(input).endsWith('/deploy') &&
        (JSON.parse(String(init?.body)) as { confirm: boolean }).confirm,
    )
    expect(writes).toHaveLength(1)
    const applied = JSON.parse(String(writes[0]?.[1]?.body)) as Record<
      string,
      unknown
    >
    expect(applied.password).toBe(credential)
    expect(
      JSON.parse(
        window.sessionStorage.getItem('rv.deploy.recipient.artifact') ?? 'null',
      ),
    ).toEqual({
      attemptID: applied.attempt_id,
      device: alternate.address,
      username: alternate.account,
      interfaceName: alternate.interface,
    })
  },
)

it.each(['en', 'ru'] as const)(
  'labels a legacy result without attributing it to the selected connection in %s',
  async (language) => {
    const { setLocale, t } = useLocale()
    setLocale(language)
    const { selected, fetchMock, props } = scenario()
    window.sessionStorage.setItem('rv.deploy.attempt.artifact', 'e'.repeat(32))
    vi.stubGlobal('fetch', fetchMock)
    const screen = await render(SendPanel, { props })
    const delivery = screen.getByRole('region', {
      name: t('send.attempt.title'),
    })
    await expect
      .element(delivery.getByText(t('send.applied'), { exact: true }))
      .toBeVisible()
    await expect
      .element(delivery.getByText(t('send.attempt.recipientUnknown')))
      .toBeVisible()
    await expect
      .element(delivery.getByText(selected.address, { exact: true }))
      .not.toBeInTheDocument()
    await expect
      .element(
        screen
          .getByRole('region', { name: t('send.connection.selected') })
          .getByText(selected.address, { exact: true }),
      )
      .toBeVisible()
    expect(
      fetchMock.mock.calls.some(([input]) => String(input).endsWith('/deploy')),
    ).toBe(false)
  },
)
