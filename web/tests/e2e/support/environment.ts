/** Keep workstation process settings while disabling personal plugin discovery. */
export const isolatedProductEnvironment = (
  parent: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv => ({ ...parent, ROUTEVANE_PLUGINS_DIR: '' })

/**
 * A local sing-box configuration address the running product can accept.
 *
 * The deployer resolves the URL against the operating system it runs on and
 * refuses a path that is not absolute there, so a Windows drive letter is not
 * an address a Linux build can be given. The example the form shows is the
 * product's own constant and stays as it is; this is what a suite saves.
 */
export const localConfigAddress = (file = 'config.json'): string =>
  process.platform === 'win32'
    ? `file:///C:/sing-box/${file}`
    : `file:///opt/sing-box/${file}`
