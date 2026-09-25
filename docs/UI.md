---
status: adopted
---

# Routevane interface contract

Routevane is an operational control surface for one job: make the destinations a
person picked reach the internet through their router's VPN interface, and let
them believe it because they saw what was sent. This file records the system the
product is actually built on. It is for interface contributors, not installation.
[Current requirements](requirements.md) own product scope; this file owns UI behavior.

## The shape of the product

The Electron shell keeps a tray icon while running. Window Close hides the
window without discarding its draft; the tray's Open restores it and Quit ends
owned work. The first hide in a session explains this through a native notification.
Display preferences belong to the permanent application origin, independently
of the private backend port. CLI/browser use retains its own explicit lifetime.

Installed desktop builds place an available application update above the sidebar
collapse control. The action remains reachable with the menu collapsed and explains
the version and restart on hover/focus. One click downloads, installs and restarts;
progress replaces the label, errors offer retry. Idle/offline checks and browser/dev
sessions add no sidebar item. Application updates are independent of source refresh.

The unit of the product is the **profile** (ADR 0013, named by ADR 0028): a
stored, server-owned composition of **lists** and **categories** with no
format of its own. A _list_ is a named set of destinations — domains,
addresses, networks — seeded by the catalog or created by the operator and
editable either way; a _category_ contains lists, ships with the catalog and
is edited by the operator on top of it. A profile publishes in one or more
**formats**, and a format owns its subscription link, its chain of published
files and, when delivery is set up, the connection that carries them. A
**connection** is one registered access — an address, an account, an interface
and, on consent, a password — and it names the access rather than what stands
at the far end, which may be a router, an application or a service (ADR 0041).
The API, the schema and the code keep `output` and `device`; the interface word
and the identifier differ here deliberately, as they already do for three wire
names under ADR 0039. One profile therefore feeds
a router and a phone at once, and editing it changes what both receive. The
interface is a shelf of those objects plus the catalog facts around them —
not a wizard. No object of the product is a «сервис» / "service" on any
surface, and since `0.2.0` none is one in an identifier either: the interface,
the API, the catalog, the schema and the code say list and profile (ADR 0039).
Where the copy does say «сервис» / "service" it means the running local
process — the thing that answered or did not — and the routing plan document,
the semantic hash payload and the plugin protocol still carry `service_id` on
the wire, which [ADR 0039](adr/0039-one-product-vocabulary-across-binary-api-and-storage.md)
records as a separate change rather than a rename.
Sections, addressable by URL:

1. `/` — **Profiles / Профили.** Every stored profile, newest first: its name
   and composition, its formats and content time. The row is a state
   summary, not a toolbar: one overflow menu owns configuration, one-off export,
   delivery, publishing and archive actions. Clicking a noninteractive row cell navigates to the
   profile's own page; the title remains a native link. Nothing on the page explains what a profile is: the rows
   are the explanation. Imported v3 profiles are identified as restored profiles
   and say what to do next rather than leaking migration names. The primary
   action is «Build a profile».
2. `/profiles/new` — **New profile.** The composer keeps the first setup in one
   visible flow: lists or whole categories on the left; the proposed, editable
   name and first format on the right. That choice still
   belongs to the format, not the profile; after the profile is stored, the profile
   page creates and publishes that first format so the one-time subscription
   URL never crosses storage or a URL. **Capacity is part of composing**
   (ADR 0027): the format is one button opening a searchable choice panel. Options are
   grouped into routers and applications (ADR 0028) — two groups inside one
   choice, not two blocks with two selections. Every option carries a live
   forecast of the rules this draft would build (`≈ N of M`), computed from
   the same material a build reads, and the chosen format repeats its forecast
   under the field; a pair that cannot build is refused before it exists,
   with a fitting format named and switchable in one click. The forecast
   guards — an unknown forecast never blocks creation.
   Composition is one dense semantic table, searchable and filterable by
   category. Checking or unchecking a list preserves every row's position and
   focus. A left-hand drag handle stays visible on every row, disabled until selected;
   position is announced in the handle accessible name rather than printed in
   every row. Reordering has arrow-key equivalents. The header checkbox selects
   or clears filtered rows, preserving hidden selections. Only an explicit reorder
   moves rows. Selecting all visible rows is a snapshot of current members.
   The secondary “New lists” menu explicitly controls whether future category
   members are included automatically; this live reference is distinct from
   the table's selection checkbox and never occupies a separate banner.
   The settings rail contains the name, first format choice,
   forecast and create action, without repeating the selected lists. In a
   constrained window it stacks after the table.
   Priority begins grouped by category unless the library has a saved custom
   order. The profile stores that order as its own snapshot. When two selected lists overlap, each affected
   table row shows the number of other selected lists it intersects; activating
   that count reveals every name. Zero means no intersections; a dash means
   unselected or not calculated, with the reason available in the table status.
   Narrow rows show intersecting list names as compact tags. These names
   summarize the complete relation set even when detailed diagnostics are
   capped at 100 records. Category names carry stable colored dots as a
   secondary recognition cue. The interface never turns those records into a manual
   cleanup queue: the higher list owns an equal destination, a covered
   lower-priority rule is omitted, and a broader lower-priority network remains
   when removing it would lose unique addresses. A new member of a live category
   is appended after the profile's saved order. A retained forecast is marked as
   updating. Missing coverage is reported per format and per list: the complete
   lists retain their rules and known overlap relationships, while unavailable
   lists show a dash. Partial overlap counts show confirmed matches as plain
   numbers; an unconfirmed zero remains a dash. A compact incomplete-calculation disclosure
   explains why the named lists were omitted and how to retry. A partial total
   never claims
   that the whole composition fits and never blocks saving or creation. Refresh
   retries source reads; normal automatic recovery reads only missing lists once.
   Publication still requires complete coverage.
3. `/profiles/{profileId}` — **The profile page.** One object with its facets as tabs:
   Contents · Publishing · File · Diagnostics; the active tab and the
   first-setup handoff travel in the URL hash (`#tab=…&setup=…`) because the
   embedded server rejects query strings, and the legacy `/#list={listId}`
   fragment redirects here. A breadcrumb returns to the profile shelf. The Contents
   tab uses the same workspace as profile creation: the dense composition table
   on the left, profile name, existing formats, and save/cancel on the right
   (ADR 0027). The existing formats are a summary; adding or changing one
   remains on the Publishing tab.
   Membership changes keep row positions stable. The table owns selection,
   removal and priority changes by pointer or keyboard; opening a row inspects
   the list without changing membership. Intersection counts and their name disclosures on
   selected rows use the first format, like the row weights, and
   name every other selected list with which that row overlaps. Save
   is enabled only once the draft differs from the stored profile, cancel
   restores it, and a format the draft would overflow is warned about beside
   the save action without blocking it. **The composition table selects and
   writes nothing else** (ADR 0029). Every
   category names its members before it is selected; a mixed checkbox always
   means that only some of those members are active, regardless of how they
   were selected. The table's category filter includes every catalog and
   operator category, then «Без категории» for the lists no category holds
   (ADR 0028). Search filters the available choices without expanding or moving
   unrelated rows.
   Changing a member never adds a provenance label or changes that row's
   height. Selecting the category adds its members together, and each member
   can still be excluded. There is no category menu here, no «Своя
   категория», no «Свой список» and no bin on a row: those change the library,
   and the library is its own section.
   A list's chevron opens its **list card** (ADR 0025, ADR 0026, ADR 0029)
   before or after selection. When the composition workspace has at least
   80rem of available width, the card occupies a nonmodal panel beside the
   table. Placement belongs to the page workspace, shared by the composer,
   profile editor, and library. The panel starts at the page's top inset and ends
   at its bottom inset. Profile settings move above the table while inspecting,
   keeping name, format, and create/save actions available. The same form
   stays mounted, preserving unsaved inputs and validation. The table remains
   interactive; closing the card restores the rail and keyboard focus. At smaller widths
   it becomes a modal right-side sheet up to the shared 64rem working width,
   portalled to the document body, with scroll lock and trapped focus. Switching
   modes preserves the card data and filter. During a non-dismissible write,
   the docked card temporarily makes the underlying workspace inert, preventing
   a list switch from abandoning the operation. It becomes interactive again
   once the result is known. Secondary decisions remain modal.
   The shared workspace centers its content within a maximum width of 88rem
   to keep names, categories and numeric columns close enough to scan. Opening
   a docked card preserves those outer bounds and the table's leading edge;
   the card occupies the right side inside that same container.
   The table's scroll area fills the sheet down to the footer;
   its rows keep their natural height and align at the top, including when a
   filter leaves one row. A long table has no separate empty band between its
   scroll area and the footer. When enlarged controls need more height, the
   card body also scrolls rather than collapsing the table or clipping actions.
   The card opens straight on its
   destination table, with a filter field above it: catalog seeds, operator
   additions and the stored source observations as rows — domains, then IP
   addresses, then networks — each naming only its origin by name
   («catalog», «by hand», «v2fly»), because a value already shows what kind
   it is. **Opened from a profile, the card reads**: the rows carry no control,
   because the profile's own act is the footer and the list itself is edited
   where lists are edited (ADR 0029). The source count («Источники · 2») labels the refresh button, which rereads
   existing sources and invalidates the draft forecast. Source configuration
   stays in the library, behind a separate settings icon.
   The footer is one line: the membership status, the toggle beside
   it, and «applies when the profile is saved» only while the card was opened
   from an unsaved draft. A draft's name is proposed from what is picked, so
   the footer does not repeat it back as if it named the list — it names the
   profile only once the profile is stored. «Открыть в библиотеке» leads to the
   other flow in a new tab, leaving the draft behind it untouched, and the
   composing screen re-reads the catalog when its window comes back.
   Secondary actions — one-off export, send, refresh-and-rebuild, archive —
   live in one overflow menu; format and maintenance choices drill into named
   submenus instead of forming one long flat list.
   Adding another format on the Publishing tab creates it and publishes it in
   one move, because a format with no file is a promise the screen cannot keep.
   The profile refresh rule is configured on this tab beside those formats;
   Settings supplies only the global default. Each format names its next unmet
   delivery condition — choose a connection, turn on automatic delivery, turn
   on profile refresh, or ready — using the persisted format, connection, and
   schedule facts. Automatic delivery is primary when a deployer exists;
   subscription and manual download remain available ways to get the file.
4. `/lists` — **Lists / Списки.** The library: what a list holds and which
   category holds it, for every profile at once (ADR 0029). It uses the same
   dense catalog table, search and category filter as the composer, with list
   management actions in place of membership checkboxes. Category labels use
   the same identity colors. The filter includes «Без категории»; a labelled
   «+» tag beside the filters opens category management with a create action
   at its foot. «Новый список»
   creates a list or adds an existing one to the selected category. A category's
   menu renames the operator's own category and deletes any category — a catalog
   category included, because deletion is a record in the operator's overlay
   and never an edit to the shipped file. Deleting a category asks the one
   question it must: move its lists to «Без категории», or delete them with
   it. A list's row opens its card in the editing mode — rename, entries,
   imports, sources, «Обновить из источников» on the card itself rather than
   inside the sources dialog, and «Удалить список» — and carries a menu whose
   words separate the two acts a bin cannot: «Убрать из категории» and
   «Удалить список». Deleting a list or a category a profile names directly is
   refused with those profiles named; removing a list _from a category_ is
   allowed to change what a profile carries, because that is what naming a
   category means. The library also owns the default list priority. Its complete
   ordered table can be rearranged by its always-visible drag handles or
   keyboard. Priority numbers are visible; save and reset actions appear for a
   changed draft, and a failed save retains the order for retry. This order initializes profiles and forecasts that do not yet
   supply their own priority; saving it never rewrites an existing profile's
   stored order. The list card has no profile-membership footer. The table fills
   the available workspace height and scrolls beneath its header. While the library
   is writing or a list is reading its sources, conflicting menus, switches,
   deletion and dismissal stay unavailable until the result is known.
5. `/profiles/{profileId}/send/{outputId}` — **Send.** An action, not a step:
   automatic applying with a plan, a backup and an audit trail when the format
   has a deployer, and the by-hand path always stated below it.
6. `/connections` — **Connections / Подключения** (ADR 0027; `/devices`
   redirects here). One section answers «куда»: registered connections
   first, as two surfaces of the same level — the registered
   connections on the left, the selected connection or the creation form on
   the right. The first saved connection is selected on arrival. An empty
   registry states across the whole work area that it holds nothing and offers
   «Добавить подключение» beside that statement, rather than putting a form in
   front of someone who has not asked for one; the form that action opens takes
   the work area at a single-column form width, with no list column. With saved
   connections «Добавить подключение» belongs to the collection, so it sits in
   the list's own header beside their number and stays there, disabled, while
   the form is open. The form's «Отменить» discards the draft and reopens
   whatever it replaced — the connection, the first saved one, or the empty
   registry's statement — so the work area is never left empty; choosing a
   connection from the list instead keeps the draft for the next «Добавить
   подключение». Saving selects the server-returned connection ID; forgetting
   the selection opens the next connection or the form. A row states the
   connection's name, its format and address, and what Routevane does with it
   — automatic delivery on, off, or by hand only — and nothing about
   reachability. The selected row is marked by its ground, a hairline accent
   and a mark that is not a colour; the list scrolls inside its own frame
   rather than lengthening the page.
   Saved connections remain selectable beside the creation or
   configuration area; a constrained window stacks these regions in document
   flow. Creation and delivery permission are primary page work, without a modal
   or scroll lock. Switching the work area moves focus to its heading. Password input
   is cleared on every departure from its connection. Saved settings never imply
   verified reachability or successful delivery.
   Router login and route-interface fields are required when the selected
   deployer needs them; the interface help names the Keenetic ID format. A
   saved connection does not send anything.
   A saved connection's editable parameters — name, address, account and route
   interface — are edited in place under «Параметры», in the same two-column
   grid the creation form uses. The format is the connection's identity and is
   never editable: a registered Keenetic does not become an OpenWrt. Save waits
   for a draft that differs from what is stored and has every required field
   filled; «Отменить» and opening another connection both return the stored
   values. Because consent names one exact destination and account, changing
   the address, account or interface turns automatic delivery off and removes
   the stored credential — the screen says so beside Save before it is pressed,
   and afterwards reports the state the server returned. A name-only change
   says nothing of the kind. When the deployment catalog is unavailable the
   parameters are stated rather than edited, because the fields a format asks
   for are that catalog's answer.
   «Автоотправка» is its own section and its own act, never part of saving
   parameters, and it is one control rather than a switch and a button: while
   it is on, the screen says so and offers to turn it off, and it never shows a
   password field or a masked stand-in for a credential it cannot read back.
   «Забыть подключение» is the connection's only secondary action, in the
   action menu of its detail header, and it is confirmed before it runs.
   After registration the screen says
   what remains before unattended delivery, and after opt-in it points to
   choosing that connection in a profile; it never states that a connection is
   unused, because which profiles name it is not a fact this screen reads. The
   reference of supported devices and formats is collapsed beneath them and
   summarized by the catalog's own titles. The words «цель», «вывод» and
   «потребитель» do not appear on any surface, and «подключение» names exactly
   one thing on every surface: the registered access this screen owns
   (ADR 0041). What a profile publishes in is a _format_, and the catalog
   collapsed here is the list of formats this build carries. «Устройство»
   is left for a physical device during a delivery attempt — a deploy step
   or a deploy error — and names no object of the product. If a catalog dependency
   is unavailable, the screen keeps known devices readable and states exactly
   which actions cannot be trusted yet.
7. `/settings` — **Настройки.** Language, theme (system/dark/light), detail
   mode, the default source-refresh schedule, and portable configuration
   transfer. Every section uses one surface and grid: title and explanation
   on the left, related controls and actions on the right. On constrained
   screens the heading precedes its content. Surface contrast, padding and
   whitespace distinguish sections without horizontal separators. Interface
   fields place labels above their controls; export and import stack in the
   same content column. The source-refresh explanation belongs beside its
   schedule. The server's address is the final, secondary section.
   Export and import are separate actions. Preferences live here,
   not in the chrome. Import starts with a product-styled file
   surface backed by the labelled native file chooser, supports dropping a file,
   keeps the chosen file visible while reading or checking it, then shows a
   server-owned preview before enabling the confirmation. Confirmation applies
   only the bytes just previewed; selecting another file invalidates it. Read,
   preview, download, and apply failures remain distinct retryable states. The
   destination must be empty. Imported connections carry no credentials or
   delivery authority, formats carry no publication/subscription state, and
   operator-added HTTP sources carry no portable URL: preview warns that they
   must be recreated. Catalog-source on/off choices and the library's default
   list priority still transfer. The browser
   preserves the exact UTF-8 JSON text through preview and apply, rejects a file
   over the shared 64 MiB boundary before reading it, and never parses then
   rewrites the document sent to the server.

The URL hash carries only page location — the active tab and the first-setup
handoff — never profile contents: a refresh, bookmark, second tab or second
browser resolves the same server-owned object via `GET /v1/profiles` and
`GET /v1/profiles/{profileId}`, never to an empty form. Local storage holds display
preferences only — no product state. Each screen carries exactly one `<h1>` and
one `<main>`. The chrome is a collapsible sidebar of sections; its bottom control switches
between labels and icons and remembers that display preference. Navigation
labels remain single-line throughout the width transition, fading and translating
without switching display or changing button height. Horizontal overflow stays
clipped throughout motion. A detail panel animates as one surface, including its
border; its host paints no separate piece that could appear before the panel. The
server's own address is a Settings fact, not a footer — and no section is
ever locked.

A profile's name and composition are editable; saving them republishes every
format, so a stored profile and the files it stands behind never quietly
disagree. What was published stays immutable: an edit adds a version, it never
rewrites one.

One-off export is not a published format. `POST /v1/profiles/{profileId}/export` renders the
current profile in any available file dialect without adding a consumer, issuing a
subscription or changing publication history. File-dialect labels describe the
bytes (for example `BAT · routes` or `JSON · all rules`), never pretend that
downloading connects the profile to a product.

## Detail modes

The operator chooses how much the surface shows: **Просто / Plain** is the
default; **Подробно / Full** adds identifiers, formats and build snapshots. A
mode changes what is disclosed, never where things are — it is not a second
layout, and no screen exists only in one mode.

## Progressive disclosure is a boundary, not a preference

IP addresses, CIDR masks, TTLs, provenance, reason codes and renderer identities
never appear on a screen's default path. They live behind the File and
Diagnostics tabs, in Full mode, or inside the list card — a surface the
operator deliberately opened to inspect and edit exactly that material. The published file's exact bytes are readable
— that is how a generated file earns trust — but they are one deliberate click
away, because they carry addresses.

Instruction lives where a decision needs it. Optional background — what a
subscription link is, how formats differ — sits behind an informer (`RvInfoTip`).
Keep a concise explanation visible when it is needed to choose safely, understand
an empty state, or recover from failure.
Disclosures are for long secondary content, not for hints. A label or a heading
never gets a sentence under it that restates the label, the placeholder or the
obvious next step; a hint under a field states an input format or a
consequence, or it does not exist. Keep edits on the surface that owns their
scope: composing a profile writes only that profile; the library writes the
library (ADR 0029). Explain shared effects beside an action when they affect
the operator's decision, such as a library edit used by several profiles. A
warning does not justify placing a library edit in the profile composer.
The product never volunteers what it does not do — that it does not scan the
network, that a password is not kept: an
absence cannot be shown to the reader, so the sentence asks for trust instead
of giving evidence. What an act must disclose it discloses beside that act, as
the thing that will happen.

## State and honesty

- Page headers share title geometry and a 44 CSS px primary action. Compact
  actions belong to rows and filters. Changing sections must not change the
  outer content alignment when a scrollbar appears.
- A list is fully read only when every enabled source has a successful current
  read. Empty successful feeds count; a saved format, old catalog revision,
  expired observations or failed source do not establish readiness. A partial
  refresh shows committed rows together with its failure. Forecasts identify
  lists needing refresh and retain the calculation for usable lists.
- Loading reserves space immediately and becomes visible after the shared
  160 ms delay, including reduced motion. Replacing the same list object does
  not clear its filter or restart its card; changing identity does not expose
  the previous list's contents under the new title.
- Every screen state is `loading`, `ready`, `empty`, `degraded` or `error`, and
  every one of them says what is there or missing, why, and what to do next.
  `RvStateNotice` is that shape; a screen does not invent a sixth.
- The first profile read reserves the final page silhouette with a labelled
  loading skeleton; it does not flash a temporary sentence above the profile.
- A status is an icon or a dot plus words — never a filled surface, never a
  colored edge stripe, and never colour alone.
- Membership is a fact, not a status: a list that belongs to the profile says
  so in words beside a check mark in the brand accent, and a list that does not
  says so in words alone. Neither takes the status dot that reports whether a
  file was published.
- The interface reports what it knows. A profile restored from the server says the
  subscription link was shown at creation; a format that left the catalog keeps
  its stored identity instead of disappearing. Nothing is filled in to look
  complete.
- Each format exposes the last persisted publication attempt. A failed rebuild
  never removes the artifact already published, and the screen says the
  previous file still stands. A rebuild of several formats continues after one
  failure and reports the affected formats instead of presenting the group as a
  single success or failure.
- A toggle states an applied fact, never an optimistic click: hiding a format
  changes exactly what its caption says it changes.
- Server codes and server English never reach the operator as themselves.
  Models answer with message keys; the interface owns the sentence.

## The one-time secret

Adding a format creates no bearer credential. The subscription URL is
issued once, after that format's first successful publication, so a failed
initial build leaves no unusable secret behind. It lives in memory for the life
of the tab: never in storage, never in the URL, never in a query, never in a
log. The profile that survives a refresh is the server's; it deliberately
cannot restore the link. A device password is held for one attempt and cleared
when it ends, unless the operator explicitly opts into unattended delivery
backed by the operating system's secret store. The address, account and
interface are not secrets and are remembered for the tab, because asking again
for what was just typed is the redundant entry WCAG 2.2 asks products to stop
doing.

## Desktop layout and window adaptation

Use a dense desktop working surface for the keyboard and pointer scenario in
[the product requirements](requirements.md#product-contract). Keep related data
comparable in tables and adjacent panes when space permits. Adapt the same
interface to a smaller window or enlarged content while keeping its actions
reachable. A narrow viewport is not a requirement for a separate mobile product.

The shell owns one canvas palette and consistent outer insets across every
section. The dark palette uses neutral graphite for the sidebar, canvas and
raised surfaces; green belongs to interactive accents and restrained selection.
Brand accent and semantic success have separate tokens, so changing the theme
does not redefine a successful publication. Searchable and plain single-choice
menus align their names at the leading edge and put selection marks at the end.
Its shared workspace centers the page content with the same width limit
on profiles, composition, library, connections and settings; features do not add
their own page containers. Docked inspection divides the existing workspace
without moving its outer edges. Reading widths belong to form fields and prose within it.
The sidebar keeps its place on desktop and
can collapse to icons with labels on hover or keyboard focus. The profile composer
and library allocate remaining height to their data regions; headings and filters
keep natural height. Short windows and enlarged content may scroll to preserve
access to actions. In a section that fills the window, the data region, or the
composer row that holds it, has a standing height taken from the window itself
— the viewport less that section's own chrome — and its rows scroll inside
that frame rather than down the page. A notice, a bar or a secondary section is therefore an addition to
the page, which lengthens and scrolls with its bottom inset still under the
last row, and never takes height out of the data region.
Invisible accessibility labels must stay within the table's
scrolling context, and list-column widths include controls and their padding.

Category filters are shared by composition and library. «Ещё» opens an anchored
panel with search above a scrollable list of all categories, including custom
ones. Choosing applies one filter and closes the panel; Escape preserves the
previous filter. Both return focus to the trigger, which reflects a selected
category absent from the quick filters. The primary search continues to search
lists. The panel's ground and border must survive portal rendering.

Category filters precede the list search in both workflows. The composition
toolbar shows selection and calculation state; the overlap explanation is
attached to its column heading, and priority semantics stay in documentation.
Refreshing selected lists is available without choosing a format and while a
forecast is calculating. Only an actual source read shows the refresh button's
busy state and prevents a duplicate refresh, apart from conflicting writes.

Library rows open inspection from their non-interactive area and show a chevron
at the trailing edge after the row menu; the named button retains keyboard access. Reorder handles and menus
keep their separate actions. On a list switch, the card clears the previous
list's data but retains its command bar, search and table geometry. Loading
placeholders occupy the table; failures expose retry in the reserved status row.
The refresh button combines its icon with the source count; source configuration
is a separate labelled icon button in the library.
A successful source read is a compact check with a tooltip; pending and failed
reads keep their actionable text in the same command area. The library link is
an icon to the right of the drawer title, before Close and outside its accessible heading. The library
shows priority actions only for an unsaved order, without repeating category
or priority explanations above the table.

Create and edit share the settings form layout. When moved above the table,
its fields align at the top in two equal columns; actions occupy a separate
full-width row below them. Numeric overlap disclosures keep numbers aligned
while providing inset hit areas and a titled list of intersecting names.

This is the shared layout verification matrix; rule and skill files link here:

| Scenario             | Verification                                                                                                                 |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Desktop layout       | Inspect at 1024 and 1440 CSS px wide with representative data, checking density, comparison and keyboard/pointer operation.  |
| Constrained window   | Inspect at 768 CSS px wide and a short desktop window, such as 1280 × 640 CSS px; scrolling must keep actions reachable.     |
| Enlarged text        | Check text enlarged to 200% without losing content or controls.                                                              |
| Accessibility reflow | Check at 320 CSS px wide, equivalent to a 1280 CSS px viewport at 400% browser zoom; preserve information and functionality. |

These are inspection cases, not a minimum window size or device support list.
Use both interface languages and inspect the affected states. See
[Accessibility](#accessibility) for the reflow boundary and manual checks.

## Layout stability

Background loading and status updates must not unexpectedly move the operator's
current control or reading position. Reserve space for predictable inline status
changes; a background load never blanks or re-creates what it is refreshing. An
explicit disclosure, validation message or user-requested layout change may
reflow content while preserving focus and keeping the next action reachable. Long
values wrap or scroll inside their own box; wide tables scroll inside their own
container, never the page. Menus are viewport overlays: they collision-position
above or below their trigger and never extend a table's scroll area. A screen
that unexpectedly jumps during a background update fails this contract.
On the profile page the composition panel holds a standing height, so a notice
appearing above it lengthens the page instead of shortening the table. A docked
card keeps the page inset while the page scrolls under it.
In the desktop composition layout, changing category content must not stretch
adjacent rows or displace the command area; use the reserved detail pane's own
scrolling. At constrained widths, panes may stack as described in the composing
flow above. A modal dialog owns the scroll: the page behind it does not move.

## Tokens

`web/src/assets/styles/tokens.css` holds two levels. Primitives are raw values and
never appear in component code. Roles are what components consume and the only
thing a theme redefines. The initial theme preference is System: follow the
operating system unless the operator explicitly chooses Light or Dark. Both
themes use the same role names; the base CSS palette does not define a user's
theme preference.

The type scale is eight steps: 13px is secondary metadata, 14px is the reading
floor, 15–16px carries normal controls and copy, and 28px is a page title.
Compact controls are at least 36px high, ordinary controls 40px and larger
hit-area choices 44px; check and radio marks are 18px. Choose density for the
task through shared component variants. The `--rv-control-touch` token names
the existing larger size; it does not require every desktop control to use it.
Gaps between controls in a tight group use 8–12px, ordinary component interiors
16–20px, and distinct page sections 32–48px; the space between a field's own
caption and its control is the field component's and is not a group gap.
Anything compared character by character — links, identifiers, addresses, file
contents, interface names — takes the mono role; a text field asks for it with
its `mono` variant, which applies the named `rv-mono` role class. No component
declares a literal colour, size, space, radius or duration.

## Components

`web/src/shared/ui` owns the primitives: `RvButton`, `RvStatus`, `RvStateNotice`,
`RvFacts`, `RvField`, `RvTextInput`, `RvTextarea`, `RvSegmented`, `RvTabs`,
`RvDialog`, `RvSelect`, `RvCombobox`, `RvSearchSelect`, `RvMenu`, `RvInfoTip`, `RvIcon`,
`RvDisclosure`, `RvTable`, `RvFilePicker`, `RvCopyButton`, `RvCodeBlock`. Nuxt UI supplies
the styled mechanics behind `RvButton`, the text fields — `RvTextInput` is
`UInput`, `RvTextarea` is `UTextarea`, `RvField` is `UFormField` — and the
full-height sheet variant of `RvDialog`. The choices `RvSelect`,
`RvSearchSelect` and `RvCombobox` and the remaining overlays use headless Reka
UI primitives. Both libraries are wrapped once here and themed with Routevane
tokens through the `--ui-*` bridge in `nuxt-ui.css`, not through classes: a
feature never imports `U*` or Reka components directly, and no native
`<select>` or `<dialog>` remains. Every control that
sits in a row with a text field shares that row's chosen control height.
Standard fields, choice triggers and buttons use `--rv-control-default`; a page
header's principal action uses `--rv-control-touch`, and a compact row must use
a consistent shared variant for the field and its adjacent controls. In either
case, a field drawn as a bordered wrapper around an input — the combobox — lets
the wrapper own that height rather than the input inside it, or it stands a
border taller than the control beside it. A field — text, select or
combobox — has no hover state: a `<label for>` forwards
`:hover` to its control, so a hovered field would light up from its label.
`RvField` names its control by the caller's id whether or not the control is a
library input. Its hint is the library's help text under the control, and while
the control has an error the error takes the hint's place; the control is
described only by the text actually shown. A disabled text field takes the
library's disabled look, reduced opacity, rather than a colour of its own. A
panel opened from a field is aligned to its leading edge and never narrower
than it; a single choice or name field is `--rv-measure-field` wide; a stack of
dialogs dims the page once.
Composition selection is the shared `ServicePicker` entity used by the create
and edit flows, and its list card is one modal `RvDialog`. There is
one button, one status mark, one fact ledger, one icon set. A refresh command
carries the refresh glyph and turns that glyph in place while its sources are
read, keeping the width it had at rest; while it works it is disabled and
`aria-busy`, and any pending sentence belongs to the status text beside it.
Every other pending command keeps the button's own ring. A disabled primary
keeps the accent at low emphasis, so the principal act stays recognizable while
it is unavailable; the other variants keep the neutral disabled ground.
Icons are hand-set 16-unit strokes, always decorative, always beside or behind an accessible name
— navigation and menus never go icon-only. A feature that needs a different
look asks for a role or a variant, never a parallel class. A component is a
black box: a host styles its own root class, passes props or fills a slot.

The application chrome presents the Routevane wordmark beside the canonical
mark at `/routevane-logo.svg`. The mark is decorative and never replaces the
text name; the shell applies its semantic ink through a CSS mask so it remains
legible in light, dark and high-contrast modes.

## Language

English is the primary language and the fallback when the browser prefers a
language this build does not speak; Russian is a complete, equal dictionary,
and both are equal layout cases. Interface copy lives in
`web/src/shared/i18n/messages.ts` and nowhere else; a literal sentence in a
component is a defect. Catalog data — list titles, format titles,
installation sentences — is data and is never translated in the interface; the
catalog itself may carry per-language variants (`title_en`,
`manual_installation_hint_en`, ADR 0027), and one accessor picks by locale
with the base string as fallback. `document.documentElement.lang` follows the
choice.

## Accessibility

WCAG 2.2 AA is the floor, and it is a gate rather than an aspiration: native
interactive elements, labels tied to controls, an error beside the control that
caused it, a visible focus ring of at least two pixels, targets no smaller than
24px, and every state distinguishable in text. Use the matrix in
[Desktop layout and window adaptation](#desktop-layout-and-window-adaptation).
The 320 CSS px case checks [WCAG Reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html),
including desktop browser enlargement. Content that needs two-dimensional
layout, such as a data table, may scroll in its own container; that exception
does not extend to surrounding forms, text or actions. The
[200% text enlargement check](https://www.w3.org/WAI/WCAG22/Understanding/resize-text.html)
is separate and does not replace reflow verification.

Use axe for the affected states. Serious and critical findings block acceptance;
any stricter existing test assertions remain in force. Automated checks do not
establish WCAG conformance by themselves: also inspect keyboard traversal,
focus, reflow and enlarged content, and review screen-reader behavior for changed
semantics or interactions.

## What the surface does not do

Keep the object-based navigation: profiles, the library, connections and settings
remain directly reachable rather than gated by a setup wizard. A local operation
may show its sequence or prerequisites without locking unrelated sections.
No eyebrow labels, no colored edge stripes, no arrow glyphs welded into copy.
Keep optional tutorials off the default path; retain concise guidance needed for
the current decision, first use or recovery. No dashboard of things
the product does not have. No status the server cannot substantiate — a live
"the router fetched your subscription" indicator waits for the server to record
that fact, and until then the interface does not imply it. Profiles are archived,
not destructively deleted; their published files remain immutable (ADR 0004).
Library lists and categories can be removed through the guarded library flow
(ADR 0029). Removing a library item never deletes published artifact history.

## Shared field and overlay contracts

Text inputs and choice triggers use the same field ground, distinct from their
containing panel. Searchable choices share `RvSearchSelect`: a labelled trigger,
a portalled panel with search at the top, optional groups, a bounded scrolling
option list, and focus returned to the trigger on dismissal. Short policy choices
retain `RvSelect` without a search field. Sidebar tooltips use Reka positioning
and portals, above page content.

A disabled reorder handle cannot initiate a drag. Table fallbacks preserve the
measured cell widths and row height outside their table. Selection never changes
row order. File previews grow with the desktop window while short files keep their
natural height. Loading, stale coverage, failed calculation, and a confirmed zero
remain distinct states; source changes invalidate forecasts for affected drafts.

The application shell owns one viewport. Page content and overflowing navigation
scroll independently; ordinary page scrolling never moves the logo. Sidebar width
changes use the shared motion tokens and honor reduced-motion preferences. The
collapse button's hit area reaches the bottom edge. Breadcrumbs sit above the
profile title in compact metadata type, preserving the title's leading alignment.

Docked inspection transitions capture the page before selection changes and
commit the new Vue layout inside the View Transition API update callback. The
settings form, table area, and detail pane transition together; a docked sheet
has no second slide animation inside its column. Opening and closing use the
same transaction in the profile composer, editor and library. Without API support
or with reduced motion, the layout switches immediately. Overlaid inspection
uses the same snapshot lifecycle; standalone sheets retain their CSS entrance.
The old and new workspace images crossfade while their groups move and resize;
hiding either image early reveals the rearranged content abruptly. Check their
contribution at the start, midpoint and end on both opening and closing, as well
as the actual rendered frames and final layout.

Shared catalog column tokens keep list names comparable in the library and profile
tables. Category columns use the same text-width budget; utility columns remain
reserved for selection, rules and overlaps in profiles, with library actions at
the trailing edge. Additional width does not expand the name beyond its measure.

Loading feedback waits 160ms before becoming visible, including with reduced
motion enabled. The request and guards start immediately. A quick response
removes the pending surface before it is shown; a slow response reveals it
without moving its reserved box. Failure and recovery actions appear immediately.
This behavior belongs to the shared notice/status components and page skeletons.

Component reflow follows the available container width, including facts, file
pickers, profile tables and drawer controls. Named dialog queries also apply to
portalled sheets. Viewport media queries are reserved for the application
scroll-height policy, fullscreen modal boundaries and system preferences.

Category chips and the searchable More panel toggle a shared multiple selection.
Lists from any selected category appear once. With no categories selected, all
lists appear; All categories clears the selection. The table checkbox operates
on this visible union and preserves hidden selections. Library bookmarks retain
all selected categories. Profile overview rows navigate through their noninteractive
cells; their native title links and action menus keep independent behavior.

Inspection transitions capture the populated dialog itself, including its text,
not its empty portal host. Dismissal requests reach the page before the primitive
unmounts content. Docked and overlaid inspections share one snapshot transition;
reduced motion and unsupported browsers switch immediately.

Native data tables share `RvTable`: one bordered scroll surface, rounded corners,
a filled header, cell separators and optional dense rows/sticky headers. Callers
own column geometry and slot content, including selection and menus. Long forms
stay outside comparison rows; FQDN group prefixes are configured in a separate
section below the format table. Format choices and connection names use the
same decorative icons, with a generic device icon for an unknown plugin target.
