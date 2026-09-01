# Routevane

Routevane builds routing lists for your router or VPN client. Choose services
and categories, combine them into a route, and export it in the format your
device uses. Refresh lists manually or on a schedule.

It runs on your computer with a browser interface in English and Russian.
No account or cloud service is required. Routevane prepares routing rules;
it does not provide a VPN connection.

## Download and start

The release is ready to run: **you do not need Go, Node.js, Python, or build tools**.
You only need a browser to use the interface.

### Windows

1. Open the [latest release](https://github.com/Muratovnik/routevane/releases/latest).
   Under **Assets**, download the ZIP ending in `windows-amd64.zip` for most PCs,
   or `windows-arm64.zip` for Windows on ARM. Do not choose **Source code**.
2. Extract the whole ZIP into a folder you can write to.
3. Double-click **`start-routevane.cmd`** in the extracted folder.

`start-routevane.cmd` is the included Windows launcher, not an installer.
It starts the application and opens the interface in your browser. Keep it
together with `routing-agent.exe` and `catalog/`; the EXE alone is a command-line
program and does not open the interface when double-clicked.

### Linux and macOS

Download the ZIP for your computer from the same release:

| Computer | Archive ending |
| --- | --- |
| Linux x64 | `linux-amd64.zip` |
| Linux ARM64 | `linux-arm64.zip` |
| Mac with Apple silicon, macOS 13+ | `darwin-arm64.zip` |

Extract it, open a terminal in the extracted application folder, and run:

```sh
sh ./start-routevane.sh
```

On all platforms, the interface is at **http://127.0.0.1:8765**. If it does not
open automatically, open that address yourself. Keep the console window open
while using Routevane; press **Ctrl+C** in it to stop.

[Download verification, updates, and troubleshooting](docs/installation.md)
are covered separately.

## Build your first route

1. Open **Lists** to see the supplied services and categories.
2. Open **Routes → Build a route**, select the lists you want, and choose an
   output format.
3. After a successful build, save the subscription link when it is shown, or
   open **Connection** to download the file. The link's secret cannot be
   displayed again. Subscriptions work only on the computer running Routevane.

You can add more formats to the same route. If an update fails, the last valid
file stays available. Sending rules to a device is a separate, explicit action;
automatic delivery is off until you enable it.

## Supported outputs

Export for **Keenetic**, **sing-box**, **OpenWrt**, **MikroTik**, and **AmneziaVPN**.
Keenetic and local sing-box also have delivery integrations.

Formats are tested, but physical-router operation and third-party client
acceptance are not yet verified. See the [format guide and limitations](docs/usage.md#supported-outputs)
before applying rules to a device.

## Help and development

- [Update, back up, or remove Routevane](docs/installation.md#updates-and-backups)
- [CLI commands, imports, and browser-assisted discovery](docs/usage.md)
- [Build from source and contribute](CONTRIBUTING.md)
- [Report a bug](https://github.com/Muratovnik/routevane/issues) · [Report a security issue privately](SECURITY.md)
- [All documentation](docs/README.md) · [Changelog](CHANGELOG.md) · [MIT license](LICENSE)
