Routevane — start here

This is a ready-to-run application, not a source checkout.
No Go, Node.js, Python, or developer setup is required.

START
Windows: double-click start-routevane.cmd.
Linux/macOS: open a terminal here and run: sh ./start-routevane.sh
The launcher runs routevane and opens the page after the local UI is ready.
If no browser opens, visit http://127.0.0.1:8765 on this computer.
The console stays open. Press Ctrl+C to stop.

For a different port:
  Windows: start-routevane.cmd 9000
  Linux/macOS: sh ./start-routevane.sh 9000
For a headless run, set ROUTEVANE_NO_BROWSER=1 before launching.

FIRST USE
Open Lists to inspect the supplied library without changing anything.
Open Profiles and choose Build a profile to select lists and a connection format.
Copy a subscription link when first shown: its secret cannot be recovered.
The server is local-only; a router cannot fetch this computer's loopback URL.
Device delivery changes a device only after your explicit setup/consent.

FILES
start-routevane.*       The launcher.
routevane[.exe]         The application it runs; "version" reports its version.
catalog/                Supplied lists, categories, and formats.
data/                   Created on first start: your database and published files.
LICENSE                 Routevane license.
THIRD_PARTY_NOTICES.txt Dependency license notices.
SBOM.spdx.json          Machine-readable dependency inventory.
Keep all supplied files together. Do not treat data/ as a cache.

UPDATE AND BACKUP
Stop Routevane before copying data. Back up data/ and any catalog changes.
Verify and extract the new archive into a different writable folder.
Copy data/ to the new folder, and review/merge catalog changes.
Keep the old folder and stopped-state backup until the new version works.
Rollback uses the old binary AND its pre-update backup, not a migrated database.

REMOVE
Disable opted-in device credentials in Connections first, then stop Routevane.
Save data/ elsewhere if wanted; removing the extracted folder removes its data.
Uninstalling Routevane does not undo rules already applied to your devices.

TROUBLESHOOTING
Missing files: extract the whole archive again, not just its launcher.
Port in use: stop your known instance or choose another port.
Page absent: read the console and open the printed address manually.
Verify identity: run routevane version (./routevane on Linux/macOS).
Health: http://127.0.0.1:8765/health should return status "ok".
Never post database files, device credentials, or subscription tokens publicly.

Documentation: https://github.com/Muratovnik/routevane
Private security reports: el.muratovnik@gmail.com
