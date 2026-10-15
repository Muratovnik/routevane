/**
 * The text field family as the built product draws it. The unit suite renders
 * the facades over doubles of the library's components; this suite is where
 * the library itself is shown to keep the contract the facades promise: a
 * label that names its control, the one error under it, the standard field height, no hover, a visible focus ring, the mono
 * role only where a value is compared character by character, and the same
 * behaviour for a library choice inside the same field.
 */

import { expect, type Locator, type Page } from '@playwright/test'

import { audit } from './support/audits'
import { englishCopy } from './support/copy'
import { CONTROL_DEFAULT } from './support/geometry'
import { deviceField } from './support/queries'
import { test } from './support/served-product'

test.use({ productData: 'field-family' })

// What the browser paints for one element, and the texts its description
// points at, read the way an operator's software resolves them.
const paint = (field: Locator) =>
  field.evaluate((element) => {
    const style = getComputedStyle(element)
    return {
      background: style.backgroundColor,
      boxShadow: style.boxShadow,
      fontFamily: style.fontFamily,
      height: element.getBoundingClientRect().height,
      outlineColor: style.outlineColor,
      outlineStyle: style.outlineStyle,
      outlineWidth: Number.parseFloat(style.outlineWidth),
    }
  })

const described = (field: Locator) =>
  field.evaluate((element) =>
    (element.getAttribute('aria-describedby') ?? '')
      .split(' ')
      .filter((id) => id !== '')
      .map((id) => document.getElementById(id)?.textContent?.trim() ?? null),
  )

const resolved = (page: Page, property: string, token: string) =>
  page.evaluate(
    ([name, value]) => {
      const probe = document.createElement('span')
      probe.style.setProperty(name!, `var(${value})`)
      document.body.append(probe)
      const painted = getComputedStyle(probe).getPropertyValue(name!)
      probe.remove()
      return painted
    },
    [property, token],
  )

for (const colorScheme of ['dark', 'light'] as const) {
  test(`text fields keep the field contract in the ${colorScheme} theme`, async ({
    page,
    origin,
    assertProductAlive,
  }) => {
    await page.emulateMedia({ colorScheme })
    await page.goto(`${origin}/connections`)
    await page
      .getByRole('button', { name: 'Add a connection', exact: true })
      .click()

    // A choice sits in the same field as a text input, and its label names it
    // just the same. A button is named by its label alone, so the trigger also
    // names what it holds: the caption, then the choice or its placeholder.
    const target = deviceField(page, 'devices.field.target')
    await expect(target).toHaveAttribute('id', 'device-target')
    await expect(target).toHaveAccessibleName(
      `${englishCopy('devices.field.target')} ${englishCopy('devices.field.target.pick')}`,
    )
    expect((await paint(target)).height).toBe(CONTROL_DEFAULT)
    await target.click()
    await page.getByRole('option', { name: 'Keenetic' }).click()

    const name = deviceField(page, 'devices.field.name')
    const address = deviceField(page, 'deploy.field.address.keenetic')
    const account = deviceField(page, 'deploy.field.account.keenetic')
    const routes = deviceField(page, 'deploy.field.interface.keenetic')
    for (const field of [name, address, account, routes]) {
      await expect(field).toBeVisible()
      // The standard field height, which the button in its row shares
      // (docs/UI.md, Components).
      expect((await paint(field)).height).toBe(CONTROL_DEFAULT)
    }
    const save = page.getByRole('button', { name: 'Save', exact: true })
    expect((await paint(save)).height).toBe(CONTROL_DEFAULT)

    // The field ground is its own role, distinct from the panel around it.
    expect((await paint(address)).background).toBe(
      await resolved(page, 'background-color', '--rv-color-field'),
    )

    // An address and a login are compared character by character; the name
    // of the connection is read as prose.
    const mono = await resolved(page, 'font-family', '--rv-font-mono')
    for (const field of [address, account, routes])
      expect((await paint(field)).fontFamily).toBe(mono)
    expect((await paint(name)).fontFamily).not.toBe(mono)

    // A field has no hover state, and a label forwards its hover to the
    // control it names, so neither the field nor its caption may light it.
    const rest = await paint(address)
    await address.hover()
    expect(await paint(address)).toEqual(rest)
    await page
      .getByText(englishCopy('deploy.field.address.keenetic'), { exact: true })
      .hover()
    expect(await paint(address)).toEqual(rest)

    // Nothing stands under the field while nothing is wrong: the explanation is
    // the informer's, the example is the placeholder. The explanation is still
    // the field's description, kept out of sight, so a reader hears it.
    const explanation = englishCopy('deploy.field.interface.keenetic.hint')
    expect(await described(routes)).toEqual([explanation])
    // Drawn nowhere: every box holding the words is at most a hairline.
    const boxes = await page
      .getByText(explanation, { exact: true })
      .evaluateAll((nodes) =>
        nodes.map((node) => node.getBoundingClientRect().height),
      )
    expect(boxes.length).toBeGreaterThan(0)
    for (const height of boxes) expect(height).toBeLessThanOrEqual(1)
    await expect(routes).not.toHaveAttribute('aria-invalid')
    await expect(routes).toHaveAttribute(
      'placeholder',
      englishCopy('deploy.field.interface.keenetic.placeholder'),
    )

    // Keyboard traversal reaches the field and draws a ring of at least two
    // pixels (docs/UI.md, Accessibility).
    // The informer stands in the label row before the control, so the
    // keyboard meets it first, in the order the row is read.
    await account.focus()
    await page.keyboard.press('Tab')
    await expect(
      page.getByRole('button', { name: 'What the interface field takes' }),
    ).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(routes).toBeFocused()
    const focused = await paint(routes)
    expect(focused.outlineStyle).toBe('solid')
    expect(focused.outlineWidth).toBeGreaterThanOrEqual(2)
    // The library eases the ring's colour in; it settles on the focus role.
    const focusColor = await resolved(page, 'color', '--rv-color-focus')
    await expect
      .poll(async () => (await paint(routes)).outlineColor)
      .toBe(focusColor)

    // Leaving the field empty states nothing; the omission is stated when a
    // submission asks for it, under the control, and the description is then the
    // explanation and the error that is actually shown.
    await page.keyboard.press('Shift+Tab')
    await expect(routes).not.toHaveAttribute('aria-invalid')
    expect(await described(routes)).toEqual([explanation])
    await save.click()
    const required = englishCopy('devices.validation.required')
    await expect(routes).toHaveAttribute('aria-invalid', 'true')
    expect(await described(routes)).toEqual([explanation, required])

    await routes.fill('Wireguard0')
    await expect(routes).not.toHaveAttribute('aria-invalid')
    expect(await described(routes)).toEqual([explanation])

    expect(await audit(page, `field-family-${colorScheme}`)).toEqual([])
    assertProductAlive()
  })
}
