import { describe, expect, it } from 'vitest'

import { installationSteps } from '@/features/send-artifact/model/installationSteps'

describe('installationSteps', () => {
  it('reads numbered steps written on one line', () => {
    expect(
      installationSteps(
        '1. KeeneticOS 5.0 or newer is required. 2. Choose the interface. 3. Make the router the DNS server.',
      ),
    ).toEqual([
      'KeeneticOS 5.0 or newer is required.',
      'Choose the interface.',
      'Make the router the DNS server.',
    ])
  })

  it.each([
    ['an instruction without numbering', 'Import the file in the app.'],
    ['a lone first number', '1. Import the file in the app.'],
    ['a number that does not start the text', 'Version 1. 2. Next'],
  ])('keeps %s as one step', (_case, hint) => {
    expect(installationSteps(hint)).toEqual([hint])
  })

  it('never splits on a number out of sequence', () => {
    expect(installationSteps('1. Wait 5. Then retry. 2. Done.')).toEqual([
      'Wait 5. Then retry.',
      'Done.',
    ])
  })

  it('states nothing for an empty instruction', () => {
    expect(installationSteps('  ')).toEqual([])
  })
})
