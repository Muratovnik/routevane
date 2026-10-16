# Changelog

All notable user-facing changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use
[Semantic Versioning](https://semver.org/). While Routevane is `0.y.z`, a
breaking change raises the minor version and everything else raises the patch.
The [release policy](docs/releasing.md) states which surfaces a version protects
and how entries are generated and validated.

This file starts at the first published version, 0.1.0.

## [Unreleased]

## [0.4.2](https://github.com/Muratovnik/routevane/compare/v0.4.1...v0.4.2) (2026-10-16)

### Features

- **planner:** ([e719557](https://github.com/Muratovnik/routevane/commit/e71955722b437b7cc19b58639351cfae4fcec09b)) Formats that take both domains and addresses (Keenetic domain-based, MikroTik, sing-box and AmneziaVPN) offer «Keep addresses beside domains» in Format settings…, off by default. With it on, a list's addresses that its domains already cover are kept beside them for clients that reach the service without a DNS lookup the router sees; they still pass every value check. Diagnostics says such an address is covered by domain rules and that the format settings can keep it, and reports an address a domain-only format cannot carry as unsupported. The database moves to schema 21: 0.4.1 refuses a database that 0.4.2 has opened, so keep the backup taken before updating if you may go back.

### Bug Fixes

- **planner:** ([d37e76e](https://github.com/Muratovnik/routevane/commit/d37e76e2e8b57d52bab700004898bb85f844b06b)) A prefix from a community-declared feed wider than /16 for IPv4 or /32 for IPv6 is no longer published. It appears in Diagnostics as too wide, so a list such as DeepSeek no longer routes whole regional blocks like 60.0.0.0/8.
- **web:** ([e719557](https://github.com/Muratovnik/routevane/commit/e71955722b437b7cc19b58639351cfae4fcec09b)) Reopening Format settings right after a save shows the saved FQDN group prefix instead of the previous one.
- **web:** ([08433d9](https://github.com/Muratovnik/routevane/commit/08433d9634c3da57bba2c19380b9ff353b653217)) The weight forecast on the Composition tab counts the addresses of a format that keeps them beside domains, so a rule-limit warning is no longer missed.

## [0.4.1](https://github.com/Muratovnik/routevane/compare/v0.4.0...v0.4.1) (2026-10-15)

### Features

- **delivery:** ([f53f13e](https://github.com/Muratovnik/routevane/commit/f53f13e67b2b3dd5402f7faf2ebd5de2e96ffef5)) The Publishing tab of a profile shows, for each format, when its file last reached the connection the format is bound to. Only a successful send to that connection counts; a send to a typed address or to another connection does not. Scheduled automatic delivery is now recorded in the same delivery journal. The database moves to schema version 20, so a 0.4.0 binary refuses it after the upgrade, and sends made before the upgrade are not shown.
- **web:** ([846051e](https://github.com/Muratovnik/routevane/commit/846051e19d201cd52c8e0237d8c0feadd64c42f4)) Fields show an error only when there is one: it appears after the first Save and then follows the value, and no empty line is kept under each field. The profile header shows a status only for a problem or work under way, the subscription link is collapsed until it is issued, and Save and rebuild spans the action panel.
- **web:** ([846051e](https://github.com/Muratovnik/routevane/commit/846051e19d201cd52c8e0237d8c0feadd64c42f4)) The Publishing table is lighter: the connection cell holds the choice alone, «Open the connection» moved into the row menu, Download and Send are icon buttons with tooltips, and an archived profile offers no Send. Connections no longer repeat the supported-format reference that Settings owns.
- **web:** ([846051e](https://github.com/Muratovnik/routevane/commit/846051e19d201cd52c8e0237d8c0feadd64c42f4)) Segmented controls drop the check mark and mark the selected option with a stronger edge, menu items and row actions carry glyphs, and the mouse wheel over a table scrolls the page once the table reaches its end. The library numbers its lists by priority.
- **web:** ([4c49ec9](https://github.com/Muratovnik/routevane/commit/4c49ec943f856196e867ff3074ed741ce8abc84f)) The send page of an archived profile, opened from a kept link, states that sending is unavailable and leads back to the profile. Choosing a configuration file in Settings checks it at once, leaving a single Import step.
- **web:** ([e99307b](https://github.com/Muratovnik/routevane/commit/e99307b16260add94d7177693e814fbaf63536ab)) Settings and Connections draw their sections as one card with the same edge, inset and heading.
- **web:** ([36fb530](https://github.com/Muratovnik/routevane/commit/36fb53068e727db556b3291b3d86c6064df49f7f)) The Diagnostics tab can narrow its table to the excluded rules, and orders addresses by value: 10.0.0.2 before 10.0.0.10, IPv4 before IPv6, and a network before the single addresses inside it.

### Bug Fixes

- **web:** ([2839336](https://github.com/Muratovnik/routevane/commit/2839336a705ffd9d2102221a67a3b4d8a491f66e)) The library filter row stays on one line beside an open list card when the system font is wider, so the list table no longer runs past the card.

## [0.4.0](https://github.com/Muratovnik/routevane/compare/v0.3.3...v0.4.0) (2026-10-15)

### Highlights

Routevane 0.4.0 lets a connection keep its password on its own, separate from
automatic delivery, and lets you delete archived profiles, remove a format from
a profile and issue a new subscription link. A build refused for incomplete
coverage now names the lists it could not cover. Keenetic FQDN groups are named
after their list instead of two hashes; the next build and send of an output
replaces its old groups and leaves other outputs' and manual groups alone.
Editing a target's title or installation steps no longer strands existing
outputs after an upgrade.

The screens follow one look for unavailable controls, state the service's
refusals at the fields they concern, collapse the sidebar in a narrow window,
and confirm irreversible actions with a destructive button.

### BREAKING CHANGES

`POST /v1/artifacts/{id}/deploy` now answers a refused device address with
status 422 and the code `address_rejected`, the same status the device routes
use, instead of 400. The response names the refused field and reports
`attempt_started: false`. Other connection faults on this route keep
`connection_invalid` with 400. Clients that branched on the 400 status for a
refused address must read 422 or the code.

`GET /v1/profile-pages/{cursor}` is removed. Page through profiles with
`GET /v1/profile-shelf/{shelf}/{cursor}`, which the screens already use.

### Features

- **devices:** ([820429d](https://github.com/Muratovnik/routevane/commit/820429d3f280e3f2e9fdcf8a6fb1585df3f99b2c), [c5a1036](https://github.com/Muratovnik/routevane/commit/c5a1036675194347710cbd1bfc833394b1258796)) Save a connection's password in the operating system's credential store on its own; manual sends use it, and automatic delivery is a separate switch that no longer deletes the password when turned off.
- **profiles:** ([97be074](https://github.com/Muratovnik/routevane/commit/97be074a8b85248bab1cc7b075ea4eef71ed344d), [4008c30](https://github.com/Muratovnik/routevane/commit/4008c30275610a9a4340fc64e36e820903ef709a)) Delete an archived profile, remove a format from a profile, and issue a new subscription link for a format; the previous link is revoked and answers 410. The profiles shelf filters active and archived profiles, and archiving can be undone from the status line.
- **web:** ([05614c2](https://github.com/Muratovnik/routevane/commit/05614c2b4825a14dbf79d22e799ae190dd452ff0), [0fded05](https://github.com/Muratovnik/routevane/commit/0fded05e2b768437039b22e5c9d50f006a79fb63)) The Publishing tab is arranged around its format table with a menu per format; the send page uses a saved password, can save a typed one on consent, and previews changes first.
- **api:** ([1b22973](https://github.com/Muratovnik/routevane/commit/1b229730f3853889ae9cf34f166c96669466a45f), [7f17ce5](https://github.com/Muratovnik/routevane/commit/7f17ce5bfa32c8392e269b3552bd538d97d00912), [f77985f](https://github.com/Muratovnik/routevane/commit/f77985f15ed83afaacf1a3cd349a50394b248906), [038f6a6](https://github.com/Muratovnik/routevane/commit/038f6a6fdbd6101cfe58e324363392fbf1ab5478)) A build refused for incomplete coverage lists the uncovered lists, also after a reload. Device and deploy routes name a refused address, an invalid password and an unavailable credential store, and the screens state these refusals at their fields.
- **catalog:** ([18c6456](https://github.com/Muratovnik/routevane/commit/18c64562a81adc6e7b974fb9a87573054ba38a3f), [635b0c8](https://github.com/Muratovnik/routevane/commit/635b0c8044aef5e9331cce53d3ade85e5f832b44)) A target lists its manual installation steps as steps; the Send screen and the supported-formats informer show them in order.
- **web:** ([d787522](https://github.com/Muratovnik/routevane/commit/d787522abec7d1966518e31bb9871d9d82bcb17d), [1e7a97c](https://github.com/Muratovnik/routevane/commit/1e7a97ce089bd4915d01f7a1ae8b6d187e59df69)) The sidebar starts collapsed below 1100 px (at the default text size) unless you chose otherwise, and the settings rail and its actions stay inside the window.
- **web:** ([eb45539](https://github.com/Muratovnik/routevane/commit/eb455390f86f439b9246dad92519741704492934), [a052f48](https://github.com/Muratovnik/routevane/commit/a052f48da484d0c0d96ad2fdadff3f1565ba28d1), [7a35d6b](https://github.com/Muratovnik/routevane/commit/7a35d6bd465480a06c68ba12d9f64fde7cd4883b), [bb680de](https://github.com/Muratovnik/routevane/commit/bb680de59045be0fff6c74235dc9de156ffe3522), [47b19e1](https://github.com/Muratovnik/routevane/commit/47b19e19303a043c3da385bc888217a39e7754b6)) Irreversible actions use a destructive button, and unavailable buttons, fields, switches, choices and chips share one flat look.

### Bug Fixes

- **keenetic:** ([0d54d2a](https://github.com/Muratovnik/routevane/commit/0d54d2ab4e577fc088b411de19d488cb886ec75f)) Name FQDN groups `<prefix>-<list>-<output code>-s<shard>`, for example `routevane-chatgpt-db8428-s1`, instead of two hashes.
- **catalog:** ([c25a460](https://github.com/Muratovnik/routevane/commit/c25a4605e7bad42fdfdff00beb65e596bdb0262c)) Compare an output with its own target's artifact revision, so editing any target's text no longer refuses every existing output after an upgrade. Outputs whose format or renderer changed are still refused, as before.
- **web:** ([f1634c5](https://github.com/Muratovnik/routevane/commit/f1634c5b50ec02f9fe7341707a25fe34a4aa8844)) Profile tabs are history entries, so Back returns to the previous tab.
- **web:** ([74b497e](https://github.com/Muratovnik/routevane/commit/74b497e66a8175bd11077bd205fca951c5231429), [8f4bf61](https://github.com/Muratovnik/routevane/commit/8f4bf6150473f4fb6f861d760b2b8d15626a2e01)) A refused send no longer claims the format changed, and a failed list write says whether the service answered.
- **web:** ([cdcf667](https://github.com/Muratovnik/routevane/commit/cdcf6676fd6008c519534331694a34aaae6be5a5), [1b01d66](https://github.com/Muratovnik/routevane/commit/1b01d669ad92c5e91d678510d246bbda4f35357c), [73fcbe4](https://github.com/Muratovnik/routevane/commit/73fcbe420ac5aa34eec66f3d339e79538cf40b82), [d08e61c](https://github.com/Muratovnik/routevane/commit/d08e61c9404c0b6bd96b425e00167c913adf6a89)) A format row asks for a connection only once delivery is set up; the connection form keeps focus during writes, and a cancelled password replacement clears its stale refusal.
- **web:** ([4f4510b](https://github.com/Muratovnik/routevane/commit/4f4510b0b2cc40fa2fabc1e2aa3c1fcf9b3194fa), [d71d8ed](https://github.com/Muratovnik/routevane/commit/d71d8edc3ffd57bfab373c74e47490006185b0f1), [c94163c](https://github.com/Muratovnik/routevane/commit/c94163ca134343e841dab678404685d9d0541c64), [804b878](https://github.com/Muratovnik/routevane/commit/804b878ed9b73e53828d796c8476f5ae47f6fc0f), [0f85b19](https://github.com/Muratovnik/routevane/commit/0f85b193b86b21b30bfa8a37a674b7935c0c8675), [e6cf10f](https://github.com/Muratovnik/routevane/commit/e6cf10f6196592797734aff5235f8013407fdea4)) Composer actions sit above its fields and stay in reach, choice triggers stay on one line, and table and refusal text is no longer cut.
- **web:** ([28ddeee](https://github.com/Muratovnik/routevane/commit/28ddeeeb6331504f9fdf4e4f4db5e3b8c1f81c55), [0815358](https://github.com/Muratovnik/routevane/commit/081535894575131c188a23f49ba9a08123fd1420), [7899044](https://github.com/Muratovnik/routevane/commit/789904406a98472f2bffdbf0f6ebd83b6e6db28f)) Menus, choice lists and the strong edge stay distinct from the canvas in the dark theme.
- **web:** ([5c81a80](https://github.com/Muratovnik/routevane/commit/5c81a80358e61b83656e02cd0083ad0df424b7b1)) A field's error keeps its one line: a text too long for it is cut with an ellipsis and read whole from the field's description and tooltip, so it no longer moves the controls below.

## [0.3.3](https://github.com/Muratovnik/routevane/compare/v0.3.2...v0.3.3) (2026-10-11)

### Bug Fixes

- **desktop:** ([80bbc7a](https://github.com/Muratovnik/routevane/commit/80bbc7a87d0a8ef91334b45ae1299da4bdb4e542)) Installing or updating the desktop application re-points existing Start menu and desktop shortcuts at the installed application. Previously an update kept a shortcut that opened another `Routevane.exe`, such as a development build, which then reported its version as `dev`. A shortcut that was deleted stays deleted.
- **desktop:** ([565ba9a](https://github.com/Muratovnik/routevane/commit/565ba9a5b006de9e51efa2657788871047450c77)) Building a desktop installer verifies that the bundled core reports the installer's own version and refuses the build otherwise.
- **web:** ([c21a994](https://github.com/Muratovnik/routevane/commit/c21a994be4aec965208ef26c7c6755af83492f23)) The running version is shown in Settings, in the Service section below the service address, instead of at the bottom of the navigation menu.
- **planner:** ([b4935ff](https://github.com/Muratovnik/routevane/commit/b4935ff46e39ea4cd91bec6ab46c04b76eb6b94b)) Excluding a value in a list's contents keeps it out of builds and forecasts even when a feed or DNS observation supplied it; the plan records it as rejected with `operator_excluded`. Previously only catalog values were removed.
- **planner:** ([446cea4](https://github.com/Muratovnik/routevane/commit/446cea4de979012651a187cd951f52ca81251f7a)) While a feed or DNS source is failing inside its grace window, builds keep only what its last successful read reported. Previously they could bring back a value that read had already withdrawn, such as a range removed from a feed or an address rotated out of a DNS answer.
- **planner:** ([b4935ff](https://github.com/Muratovnik/routevane/commit/b4935ff46e39ea4cd91bec6ab46c04b76eb6b94b)) Plans are deterministic when two source classes produce the same address or network, and `lossless_collapsed` is recorded only on networks that actually merged. Affected plans get a new hash on their next build.
- **api:** ([b4935ff](https://github.com/Muratovnik/routevane/commit/b4935ff46e39ea4cd91bec6ab46c04b76eb6b94b)) Refreshing a profile with many lists no longer fails at the 20-second screen budget; the route has its own five-minute budget.
- **web:** ([c21a994](https://github.com/Muratovnik/routevane/commit/c21a994be4aec965208ef26c7c6755af83492f23)) The profile page keeps unsaved edits and the open tab when it reloads after a rebuild, save, archive or device change; a failed reload keeps the page and offers a retry.
- **web:** ([c21a994](https://github.com/Muratovnik/routevane/commit/c21a994be4aec965208ef26c7c6755af83492f23)) Importing a configuration refreshes the list library in the open session, a refused archive or restore on the profile shelf is reported, and creating a profile can no longer submit twice before the page changes.

## [0.3.2](https://github.com/Muratovnik/routevane/compare/v0.3.1...v0.3.2) (2026-10-11)

### Bug Fixes

- **desktop:** ([e423840](https://github.com/Muratovnik/routevane/commit/e42384012028540b529ac220cd9ea246044b2cb0), [509fb3f](https://github.com/Muratovnik/routevane/commit/509fb3fcf31660c4126886b5e6cf3d158e8d1678)) Offer to choose a new local port when Windows has reserved the saved one, instead of failing to start. The question warns that previously copied subscription addresses need the new port; quitting keeps the saved address, and data and subscription tokens are preserved.
- **desktop:** ([e0e0efd](https://github.com/Muratovnik/routevane/commit/e0e0efde247d15b2bf56167aa66b56ab3f93556f)) Choose new desktop ports from 20000–29999, below the ranges where Windows reserves ports and opens outgoing connections. Existing installations keep their saved port.
- **desktop:** ([509fb3f](https://github.com/Muratovnik/routevane/commit/509fb3fcf31660c4126886b5e6cf3d158e8d1678)) Limit how long quitting waits for the background service once it has stopped.

## [0.3.1](https://github.com/Muratovnik/routevane/compare/v0.3.0...v0.3.1) (2026-10-11)

### Bug Fixes

- **keenetic:** ([ae74cbd](https://github.com/Muratovnik/routevane/commit/ae74cbd5719456f6af4ef53af89d5e6ab3105728)) Add readable list and category descriptions to FQDN groups while preserving their stable ownership. Build and send a new artifact to receive the descriptions; already published artifacts stay unchanged.
- **delivery:** ([ae74cbd](https://github.com/Muratovnik/routevane/commit/ae74cbd5719456f6af4ef53af89d5e6ab3105728)) Avoid restoring an old device backup after a confirmed failure before writing or after a verified no-op. Validate device commands and complete payloads before applying them, while keeping conservative recovery for partial or ambiguous writes.
- **connections:** ([540975d](https://github.com/Muratovnik/routevane/commit/540975ddc5a59f2c0caffa4cca58d8da08523182), [ae74cbd](https://github.com/Muratovnik/routevane/commit/ae74cbd5719456f6af4ef53af89d5e6ab3105728)) Clear password input when switching connections and preserve confirmed deletion and automatic-delivery changes when a refresh fails. Align connection fields in both languages and narrow windows.
- **desktop:** ([ae74cbd](https://github.com/Muratovnik/routevane/commit/ae74cbd5719456f6af4ef53af89d5e6ab3105728), [540975d](https://github.com/Muratovnik/routevane/commit/540975ddc5a59f2c0caffa4cca58d8da08523182)) Repair configuration transfer, show the running version and actual service address, and keep dismissed submenus closed until a new interaction.
- **storage:** ([540975d](https://github.com/Muratovnik/routevane/commit/540975ddc5a59f2c0caffa4cca58d8da08523182), [ae74cbd](https://github.com/Muratovnik/routevane/commit/ae74cbd5719456f6af4ef53af89d5e6ab3105728)) Preserve profile references during concurrent library edits, keep replacement database connections configured safely after cancellation, and drain service work before closing storage.
- **inputs:** ([ae74cbd](https://github.com/Muratovnik/routevane/commit/ae74cbd5719456f6af4ef53af89d5e6ab3105728), [66ffda6](https://github.com/Muratovnik/routevane/commit/66ffda611ae58aa4aead2bf2360cb79c53211b00)) Reject unsafe imported files and network destinations, including deprecated ORCHID addresses in `2001:10::/28`. Current ORCHIDv2 and DET destinations remain accepted.

## [0.3.0](https://github.com/Muratovnik/routevane/compare/v0.2.3...v0.3.0) (2026-10-10)

### Highlights

Routevane 0.3.0 makes publication and manual device delivery recoverable after
an interrupted response. It adds saved-connection selection for manual delivery,
steadier profile and list state, and consistent text fields and choice controls.

Linux installations using external plugins must permit tracing their own child
processes, reading `/proc`, and setting child resource limits. Plugin startup is
refused if containment cannot be installed. Running without external plugins
remains available.

### BREAKING CHANGES

Confirmed `POST /v1/artifacts/{id}/deploy` requests now require a client-generated
`attempt_id`: 128 random bits encoded as 32 lowercase hexadecimal characters.
Older HTTP clients must add this field; a request without it is rejected before
deployment with `attempt_id_required`. Read-only planning and the CLI deployment
command keep their existing contract.

Keep the same attempt ID when recovering the same request. Use
`GET /v1/deployment-attempts/{id}` to recover its result after a lost response;
reusing the ID for the same destination returns the recorded outcome without
replaying device effects. A different request cannot reuse that ID. An
`outcome_unknown` result requires an explicit status check and must not trigger
another apply automatically. See [ADR 0042](https://github.com/Muratovnik/routevane/blob/v0.3.0/docs/adr/0042-identify-manual-deployment-before-device-effects.md)
for the complete request and recovery contract.

### Features

- **delivery:** ([8b02949](https://github.com/Muratovnik/routevane/commit/8b02949559f63d75d0f43a37bf0a03208d867bc3)) Choose a saved compatible connection for manual delivery, confirm its exact destination, and recover a completed attempt after a dropped response or restart without sending it again.

### Bug Fixes

- **publication:** ([8b02949](https://github.com/Muratovnik/routevane/commit/8b02949559f63d75d0f43a37bf0a03208d867bc3), [db68e8d](https://github.com/Muratovnik/routevane/commit/db68e8d90d759ad4f8e1b12a8deef3bf253bb1cd)) Keep the previous valid artifact available when a candidate fails, and keep publication, archived profiles, list changes and delivery records consistent during recovery.
- **runtime:** ([db68e8d](https://github.com/Muratovnik/routevane/commit/db68e8d90d759ad4f8e1b12a8deef3bf253bb1cd)) Stop owned listeners, child processes and browser proxy work when cancelled; enforce one CLI database writer and reject unsafe output paths before replacing configuration.
- **discovery:** ([f019362](https://github.com/Muratovnik/routevane/commit/f019362411ac0864cce3966238d3ce55ddfef721), [db68e8d](https://github.com/Muratovnik/routevane/commit/db68e8d90d759ad4f8e1b12a8deef3bf253bb1cd)) Launch the browser executable that was checked and keep browser cleanup bounded when a discovery session is cancelled.
- **ui:** ([a596649](https://github.com/Muratovnik/routevane/commit/a596649c97c942d7815996ff96b00912f64b675e), [1075049](https://github.com/Muratovnik/routevane/commit/1075049e502c1ae39b062a35146e9962b2c0e2c4), [63c2fc4](https://github.com/Muratovnik/routevane/commit/63c2fc453fb7ff6f6e35e24822b70e77c0ba2993)) Restore list membership without stale selection and use consistent keyboard, focus and busy states for text fields and choice controls.
- **dependencies:** ([e48cc03](https://github.com/Muratovnik/routevane/commit/e48cc03fddf9deac8c691d2b6e7fac86d21713d0), [db68e8d](https://github.com/Muratovnik/routevane/commit/db68e8d90d759ad4f8e1b12a8deef3bf253bb1cd)) Update bundled runtimes and dependencies, and use explicitly maintained derivatives for braces depth handling and node-forge RSA input validation while preserving vulnerability checks.

### Performance Improvements

- **storage:** ([db68e8d](https://github.com/Muratovnik/routevane/commit/db68e8d90d759ad4f8e1b12a8deef3bf253bb1cd)) Initialize an empty database without rebuilding its schema repeatedly and load profile details in batches instead of one query per profile.

## [0.2.3](https://github.com/Muratovnik/routevane/compare/v0.2.0...v0.2.3) (2026-09-12)

### Bug Fixes

- **storage:** ([273923f](https://github.com/Muratovnik/routevane/commit/273923fc377ab0f0aa208d5909e4ccb016e2fb17), [e26bdde](https://github.com/Muratovnik/routevane/commit/e26bdde598afc6487e59317e8c64ab6d794d8d74), [d8d0b33](https://github.com/Muratovnik/routevane/commit/d8d0b330a09e94a6c7859498961f00c0eb8694bf)) Open a database on a slow or busy machine instead of reporting it unavailable or incompatible. Building the current schema, and importing a pre-release database into it, are startup work whose size follows the data; both are now measured by what that work costs rather than by the budget a single query gets, and a check that cannot read the database says so instead of declaring the schema wrong.

## [0.2.0](https://github.com/Muratovnik/routevane/compare/v0.1.7...v0.2.0) (2026-09-11)

### Highlights

Routevane 0.2.0 completes the one-product vocabulary across the executable,
CLI, HTTP API, catalog, storage, configuration transfer, and web application.
It also adds clearer connection management and safer delivery, recovery, and
browser workflows.

### BREAKING CHANGES

This release completes the vocabulary migration. The executable, database, and
lock are now named `routevane`; a composition is a **profile**, its sources are
**lists**, and a renderer's profile is a **format**. CLI flags such as
`--service` and `--service-id`, the corresponding API and catalog names, and
the old browser addresses must be updated to their new names. Configuration
transfer is now `config-transfer-v1.4`. Retired catalog keys and transfer
versions remain readable for one minor version; an existing database is
migrated in place and cannot be opened by an older binary. See
[ADR 0039](https://github.com/Muratovnik/routevane/blob/main/docs/adr/0039-one-product-vocabulary-across-binary-api-and-storage.md)
for the complete boundary.

### Features

- **connections:** ([23458d5](https://github.com/Muratovnik/routevane/commit/23458d51cee5b9cfa2493085b12e3cf069f30e78)) Edit a registered connection's name, address, account, and route interface in place. Changing an identity-bearing field turns automatic delivery off and removes the stored credential before the change is saved.
- **connections:** ([23458d5](https://github.com/Muratovnik/routevane/commit/23458d51cee5b9cfa2493085b12e3cf069f30e78), [cff2008](https://github.com/Muratovnik/routevane/commit/cff2008a7a69471688142ada4e4babefcd5cb310)) Manage connections in a two-column workspace and get an explicit empty state with an action to add the first connection.
- **ui:** ([63e2014](https://github.com/Muratovnik/routevane/commit/63e20149d21958476990872a3ae436ab24a465e0), [c16b76c](https://github.com/Muratovnik/routevane/commit/c16b76ca6b41adcc430c5d54756d2bc0e4b0b6d2)) Show each profile publication with its format, file, connection, and source content clearly named, including the publisher's own mark.
- **keenetic:** ([073c3d3](https://github.com/Muratovnik/routevane/commit/073c3d390e6859062acf97c223916ea419832f49)) Use output-scoped, collision-checked group names with an optional per-output prefix, changing only groups with persisted exact ownership.

### Bug Fixes

- **keenetic:** ([073c3d3](https://github.com/Muratovnik/routevane/commit/073c3d390e6859062acf97c223916ea419832f49)) Treat save errors inside HTTP 200 responses as failures; recovery now saves and reads back the restored configuration, with recovery failure taking priority over the original verification error.
- **keenetic:** ([073c3d3](https://github.com/Muratovnik/routevane/commit/073c3d390e6859062acf97c223916ea419832f49)) Detect and repair `auto` policy drift on owned groups.
- **discovery:** ([073c3d3](https://github.com/Muratovnik/routevane/commit/073c3d390e6859062acf97c223916ea419832f49)) Block direct WebRTC UDP and enforce the host limit while connections are still pending.
- **scheduler:** ([073c3d3](https://github.com/Muratovnik/routevane/commit/073c3d390e6859062acf97c223916ea419832f49)) Preserve newer schedule choices, include profiles beyond the first 200, and check build capacity after overlapping rules are pruned.
- **ui:** ([073c3d3](https://github.com/Muratovnik/routevane/commit/073c3d390e6859062acf97c223916ea419832f49)) Restore send-menu navigation, visible retry states, and catalog refresh after busy operations; preserve keyboard access and drafts in tabs, and close repeated single-selection pickers even when they become busy.
- **plugins:** ([073c3d3](https://github.com/Muratovnik/routevane/commit/073c3d390e6859062acf97c223916ea419832f49)) Keep shutdown bounded when a plugin stops reading its input, and tolerate transient Windows executable locks after process exit.

## [0.1.7](https://github.com/Muratovnik/routevane/compare/v0.1.5...v0.1.7) (2026-09-06)

### Highlights

This release adds a Windows desktop application. Routevane opens in its own
window, keeps scheduled work running from the tray, and an installed build
offers a newer version that one explicit click downloads, installs and
restarts. The operational interface is rebuilt around ordered tables for routes
and lists, with category filtering, steadier selection and clearer loading and
refresh feedback. The CLI package stays independently distributable and does
not require the desktop application.

### Features

- **desktop:** Open Routevane in its own application window, with a tray lifetime that keeps owned background work running after the window is closed. ([59f0a82](https://github.com/Muratovnik/routevane/commit/59f0a8285c6762f605df23ce55cad8398fd24063))
- **desktop:** Install Windows builds from a per-user installer, and update an installed build from GitHub releases on one explicit click; a failed download leaves the current version usable. ([49a4778](https://github.com/Muratovnik/routevane/commit/49a4778740c56946cc21dd1ae52ab2d847d9b9a9))
- **ui:** Rebuild route composition and the library as ordered tables with category filtering and shared desktop workspaces. ([1689947](https://github.com/Muratovnik/routevane/commit/168994745ad75eee052e6e1591de7e1b3320c2cf), [744555f](https://github.com/Muratovnik/routevane/commit/744555ff6b8511805a33ad50648d76c263e343c4), [01e5027](https://github.com/Muratovnik/routevane/commit/01e5027c80b6e1a329c357c1dc7d4a911ddfb9c9))
- **priority:** Give a new route its library default list order and summarize cross-list overlaps before building. ([4dfb142](https://github.com/Muratovnik/routevane/commit/4dfb142e39996321bf669bfee5433cebbc1be76c))

### Bug Fixes

- **ui:** Keep route and list selection stable in one table while inspecting, filtering and reordering. ([aff5ddf](https://github.com/Muratovnik/routevane/commit/aff5ddfec9a2f147f40c659f9eadb64bcd9f31e7), [723dc2b](https://github.com/Muratovnik/routevane/commit/723dc2bc7f94c422952409aca067bb66cf7743ea), [370164e](https://github.com/Muratovnik/routevane/commit/370164e82ca95e01555b507a1ac4e05a09105273), [86522e2](https://github.com/Muratovnik/routevane/commit/86522e261bb34ad1f41d894e0a6467cebdc051e7))
- **ui:** Give each composition workspace one scroll owner, and keep docked panels from shifting the page around them. ([f960fee](https://github.com/Muratovnik/routevane/commit/f960fee1d531cfa5382646b6513e454ed012c0b3), [aab825a](https://github.com/Muratovnik/routevane/commit/aab825a1e74368d11a23df5cb8c9827ca775a62a), [5c98618](https://github.com/Muratovnik/routevane/commit/5c986188b5256708056cb573c3040d6003c68a41))
- **ui:** Report loading, refreshing and inspection consistently across pages instead of flashing transient states, and keep partial forecasts visible. ([f402ce1](https://github.com/Muratovnik/routevane/commit/f402ce17c9b23b6a56aac215d96c2d3bc69e6a83), [26e0435](https://github.com/Muratovnik/routevane/commit/26e04351a47f3d5f827a6bd9fe438362e0cde9c5), [e1756c4](https://github.com/Muratovnik/routevane/commit/e1756c4d9d1e969279a37c06b2ff0603c21fbaf5), [61fc855](https://github.com/Muratovnik/routevane/commit/61fc855acccf7b90d8981a9a88198f8cec2a6c5e))
- **priority:** Remove the global cap on the size of a route's list order. ([92eff42](https://github.com/Muratovnik/routevane/commit/92eff4221d3a1f0d6b5982307c2b2f765a2688ea))
- **web:** Debounce list card toggles so rapid changes are not lost. ([c955e65](https://github.com/Muratovnik/routevane/commit/c955e6505a5e1671a05d0c3b1cbb1fab19cc3ed6))
- **release:** Publish the Windows installer, its blockmap and the updater feed under the release's single checksum manifest. ([e11f9ac](https://github.com/Muratovnik/routevane/commit/e11f9ac0208d0680939604314f2196532d10890a))

## [0.1.5](https://github.com/Muratovnik/routevane/compare/v0.1.3...v0.1.5) (2026-09-04)

### Highlights

This release contains the Routevane 0.1.3 product changes and corrects the
release validation deadline exposed by Linux race instrumentation.

### Bug Fixes

- **release:** Allow instrumented server startup to reach its listener without weakening the readiness assertion, and report an early process exit directly. ([a7a63c4](https://github.com/Muratovnik/routevane/commit/a7a63c402324c383858597b35459b4e5ff48db33))
- **release:** Await the source-refresh response instead of racing drawer focus during browser validation. ([64a06f6](https://github.com/Muratovnik/routevane/commit/64a06f6d0d32aefb14e03f3122c8c0f52a028aeb))

## [0.1.3](https://github.com/Muratovnik/routevane/compare/v0.1.2...v0.1.3) (2026-09-04)

### Highlights

This release turns list overlaps into an automatic, route-local priority rule,
adds a reviewed configuration transfer, and makes the operational interface much
clearer while a list is loading, refreshing, failing, or being edited. It also
adds Russian user documentation and clearer Keenetic route ownership.

### Features

- **routes:** Resolve shared destinations by list priority, with drag-and-drop and keyboard reordering, while preserving unique address coverage. ([0d0b410](https://github.com/Muratovnik/routevane/commit/0d0b4101151f0143569e598a8954a80bbca8bf28))
- **config:** Export, preview, validate, and import portable Routevane configuration into a fresh installation. ([a7e542c](https://github.com/Muratovnik/routevane/commit/a7e542c4d5559af44abe089e6379739bb76167aa), [620f86b](https://github.com/Muratovnik/routevane/commit/620f86bf4abc372d9f0f7236e224f8a44c9bcbbc))
- **keenetic:** Label managed static routes with their category and list, and retain enough ownership data to update them safely. ([0c50dff](https://github.com/Muratovnik/routevane/commit/0c50dffcbb2f1249ca4160e0a1e9b5217fee54dd), [a7bbd4a](https://github.com/Muratovnik/routevane/commit/a7bbd4a6bc77fde8136ecc5cfb0a17256380144a))
- **ui:** Add the Routevane mark, adopt maintained Nuxt UI interaction mechanics, and add Russian user guides. ([7288827](https://github.com/Muratovnik/routevane/commit/72888276094a2c5f143476c0438e4daea40dc6a6), [a9e9282](https://github.com/Muratovnik/routevane/commit/a9e92827c4b567453c937b5a3a4d3a3337ffadb0), [d7f8448](https://github.com/Muratovnik/routevane/commit/d7f844830110263695943fc3912e36ab50e1c130))

### Bug Fixes

- **ui:** Keep loading, refresh, error, disabled, drawer, form, and dense-list states unambiguous and operable. ([eaa1c88](https://github.com/Muratovnik/routevane/commit/eaa1c88ba00c4bc8ad4a4e2f99cb09ba43cb70ab), [0d44270](https://github.com/Muratovnik/routevane/commit/0d44270af3614a476c7c5e9b7be55415645758ce))
- **config:** Preserve exact transfer bytes and reject malformed or inconsistent configuration before changing the destination. ([1451360](https://github.com/Muratovnik/routevane/commit/1451360e5f1f1a3a9ded22fe74c5008a03f0f57b), [647c68f](https://github.com/Muratovnik/routevane/commit/647c68f2fd94d1331caf647208803cf7fcb51063), [ca58a69](https://github.com/Muratovnik/routevane/commit/ca58a69469fb64bcf3719fad2d61b596e2549f8f))
- **delivery:** Serialize manual and scheduled writes to a device, and stop owned development servers when their supervisor exits. ([1d8360d](https://github.com/Muratovnik/routevane/commit/1d8360d803d519723b85b693a92926afe5901f65), [52c3a8a](https://github.com/Muratovnik/routevane/commit/52c3a8aaea01cd924c495816f79e757aaa3408da))

## [0.1.2](https://github.com/Muratovnik/routevane/compare/v0.1.1...v0.1.2) (2026-09-03)

### Highlights

This release makes list loading and error recovery clearer, explains rules shared
by multiple lists, and expands the built-in catalog. Maintainers also get a
hot-reload development workflow and the managed release process with relkit 0.12.1.

### Features

- **release:** Use a managed release workflow with reviewed publication plans,
  resumable receipts, and verification of the published artifacts.
  ([916ee32](https://github.com/Muratovnik/routevane/commit/916ee32de88ded45d73d86c01024bb44b29a6e11))
- **catalog:** Add a bounded Cursor domain list.
  ([2f557ea](https://github.com/Muratovnik/routevane/commit/2f557eabd4f5a6b9c70088c58c11c8093752ddf8))
- **catalog:** Add GitHub Copilot, Twitch, and Kinopub domain lists.
  ([74d2d37](https://github.com/Muratovnik/routevane/commit/74d2d37d744025e6a6bce1dbafb90e12a50cea6d))
- **forecast:** Explain cross-list rule overlaps before building a route.
  ([6922895](https://github.com/Muratovnik/routevane/commit/692289523dc493fe825e9d7cc2572beac817dacd))
- **dev:** Add hot reload for the frontend and backend during local development.
  ([d0a5ddb](https://github.com/Muratovnik/routevane/commit/d0a5ddb25c14e96f75d7a9e2d74b0fbc43b83f4c))

### Bug Fixes

- **docs:** Keep internal work records out of the public source tree.
  ([00afeeb](https://github.com/Muratovnik/routevane/commit/00afeeb41d8e519594c44d035ccf74d735e14df0))
- **tooling:** Apply repository restrictions at their intended publication boundaries.
  ([6cd88dd](https://github.com/Muratovnik/routevane/commit/6cd88dd3782fabb62cd9f0a1d160abaecdfadef3))
- **tooling:** Parse workflow action references as YAML when checking pinned actions.
  ([894844c](https://github.com/Muratovnik/routevane/commit/894844c827a5efededeeef844ca72e6db0a1f29e))
- **keenetic:** Reject ambiguous DNS route snapshots instead of accepting uncertain state.
  ([bab1010](https://github.com/Muratovnik/routevane/commit/bab10100ebfb787e87cd700ee93f2cc73b3e0ae4))
- **ui:** Keep partial list contents readable during loading, expose recoverable errors,
  and prevent unavailable actions from appearing ready to use.
  ([32803af](https://github.com/Muratovnik/routevane/commit/32803af9178e061c2f0cb84ae730b6951e7152bc))

## [0.1.1](https://github.com/Muratovnik/routevane/compare/v0.1.0...v0.1.1) (2026-09-01)

### Highlights

This maintenance release improves the installation documentation and release
process. Application behavior, the API, CLI, and archive layout are unchanged.

- Simplify the quick start, explain the included launcher, and separate download
  verification, updates, backups, and troubleshooting into a user maintenance guide.
  ([3190d5a](https://github.com/Muratovnik/routevane/commit/3190d5a46760808474e2fc1f1a125b6879fd221b))
- Validate the curated Vue-like changelog entry before both building and
  publishing a release, preserving the reviewed text and commit links.
  ([f1a8871](https://github.com/Muratovnik/routevane/commit/f1a887161509d5ec3477bb761910bcb3420e261f))

## [0.1.0](https://github.com/Muratovnik/routevane/releases/tag/v0.1.0) (2026-09-01)

### Highlights

Routevane's first public release turns a local route library into verified,
immutable files for routers and client applications. It ships as one binary
with an embedded browser UI, clear archive installation paths, explicit device
consent, and a release pipeline that verifies the same tagged source on every
supported native runner before publishing it.

### Breaking Changes

- Building Routevane from source requires Go 1.27. Release archives remain
  self-contained and do not require Go or Node.js on the user's machine.

### Features

- Compose reusable service and category libraries, observe maintained DNS and
  HTTPS sources, inspect forecasts, and publish immutable subscriptions.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Export six end-user formats for Keenetic, OpenWrt, MikroTik, Amnezia,
  sing-box, and Keenetic FQDN groups, plus Raw JSON diagnostics.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Extend refresh and rendering through contained external plugins with a
  versioned SDK and bounded permissions.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Bind an output to one registered device and opt into scheduled delivery
  through the same probe, backup, deploy, verify, and rollback lifecycle used
  by manual delivery.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Download verified release archives for Windows amd64/arm64, Linux
  amd64/arm64, and macOS arm64 with license, notices, SBOM, checksums, and
  launchers included.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed),
  [83dd181](https://github.com/Muratovnik/routevane/commit/83dd181e1dfa9f9b59c386d2a3aaf2e12858b4e1))

### Bug Fixes

- Separate user installation from contributor setup; include offline lifecycle
  instructions and open the browser only after the embedded UI is ready.
  ([83dd181](https://github.com/Muratovnik/routevane/commit/83dd181e1dfa9f9b59c386d2a3aaf2e12858b4e1))
- Preserve executable permissions in Unix archives produced on Windows and
  verify the actual shipped launchers with fresh runtime data.
  ([83dd181](https://github.com/Muratovnik/routevane/commit/83dd181e1dfa9f9b59c386d2a3aaf2e12858b4e1))
- Replace stale plugin templates with runnable, tested example files.
  ([83dd181](https://github.com/Muratovnik/routevane/commit/83dd181e1dfa9f9b59c386d2a3aaf2e12858b4e1))
- Build from source paths containing spaces without bypassing type checking;
  keep child-command diagnostics visible when a developer command fails.
  ([f99000d](https://github.com/Muratovnik/routevane/commit/f99000d56c0f6f7c225682e33bbd5c913c88b4d9))
- Keep Go 1.27 analysis and sandboxed Chromium acceptance working on Linux;
  verify browser-opening behavior in both embedded-UI and API-only builds.
  ([f2f7adc](https://github.com/Muratovnik/routevane/commit/f2f7adc5f253dc724899caff1dd765a170b01d4a),
  [4adbd60](https://github.com/Muratovnik/routevane/commit/4adbd607a81e57e80b9ebe8d0c56068dc524043b))
- Keep output builds and scheduled delivery attempts independent so one
  timeout or invalid output cannot starve its siblings or replace a previous
  valid artifact.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Revalidate output binding, connection metadata, consent, and credentials as
  one authorization decision before unattended device changes.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Refuse secret-bearing or non-local device destinations before they can be
  persisted, and revoke stored consent when connection metadata changes.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Contain verified plugin snapshots and remove stale combobox highlights from
  the browser control surface.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
