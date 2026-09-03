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

The unit of the product is the **route** (ADR 0013, named by ADR 0028): a
stored, server-owned composition of **lists** and **categories** with no
target of its own. A *list* is a named set of destinations — domains,
addresses, networks — seeded by the catalog or created by the operator and
editable either way; a *category* contains lists, ships with the catalog and
is edited by the operator on top of it. What a route publishes into is an
**output** — one format, optionally one device — and an output owns its
subscription link and its chain of published files. One route therefore feeds
a router and a phone at once, and editing it changes what both receive. The
interface is a shelf of those objects plus the catalog facts around them —
not a wizard. The word «сервис» / "service" appears on no surface; the code
and the API still say `lists` and `services` until the follow-up rename.
Sections, addressable by URL:

1. `/` — **Routes / Маршруты.** Every stored route, newest first: its name
   and composition, its connections and content time. The row is a state
   summary, not a toolbar: one overflow menu owns configuration, one-off export,
   delivery, connection and archive actions. Opening the name navigates to the
   route's own page. Nothing on the page explains what a route is: the rows
   are the explanation. Imported v3 profiles are identified as restored routes
   and say what to do next rather than leaking migration names. The primary
   action is «Build a route».
2. `/lists/new` — **New route.** The composer keeps the first setup in one
   visible flow: the name first — proposed from what is picked below and
   editable at any point — then lists or whole categories, then the first
   device or application. The target still
   belongs to the output, not the route; after the route is stored, the route
   page creates and publishes that first output so the one-time subscription
   URL never crosses storage or a URL. **Capacity is part of composing**
   (ADR 0027): the format is one searchable combobox whose options are
   grouped into routers and applications (ADR 0028) — two groups inside one
   choice, not two blocks with two selections. Every option carries a live
   forecast of the rules this draft would build (`≈ N of M`), computed from
   the same material a build reads, and the chosen format repeats its forecast
   under the field; a pair that cannot build is refused before it exists,
   with a fitting format named and switchable in one click. The forecast
   guards — an unknown forecast never blocks creation.
   A collapsed **List overlaps / Пересечения списков** disclosure explains
   identical typed rules and cross-list containment from that same forecast
   plan. It names the format and the contributing lists; owner links open the
   library in another tab. A retained answer is marked as updating, an
   unavailable answer offers retry, and at most 100 details are shown with
   explicit truncation. These relations are not a claim of device-rule savings.
3. `/lists/{listId}` — **The route page.** One object with its facets as tabs:
   Contents · Connection · File · Diagnostics; the active tab and the
   first-setup handoff travel in the URL hash (`#tab=…&setup=…`) because the
   embedded server rejects query strings, and the legacy `/#list={listId}`
   fragment redirects here. A breadcrumb returns to the route shelf. The Contents
   tab leads with what the route already holds (ADR 0027): its categories and
   lists as rows, each with its forecast weight in rules and a removal
   control drawn as a bin, never as a cross — a cross means "close", and the
   catalog picker that opens beneath the rows behind «Добавить списки» has
   its own header and its own «Скрыть»; choosing is the composer's job and
   reviewing is this page's.
   The overlap disclosure uses the first connection's format, like the row
   weights, and names that connection explicitly. Save
   is enabled only once the draft differs from the stored route, cancel
   restores it, and an output the draft would overflow is warned about beside
   the save action without blocking it. **The picker selects and writes nothing else** (ADR 0029). Every
   category names its members before it is selected; a mixed checkbox always
   means that only some of those members are active, regardless of how they
   were selected. Categories and their lists form a master-detail view: the
   category column — every catalog and operator category, then «Без
   категории» for the lists no category holds (ADR 0028) — keeps its geometry
   while a separate, scrollable pane shows one category at a time. Search
   filters the available choices without expanding or moving unrelated rows.
   Changing a member never adds a provenance label or changes that row's
   height. Selecting the category adds its members together, and each member
   can still be excluded. There is no category menu here, no «Своя
   категория», no «Свой список» and no bin on a row: those change the library,
   and the library is its own section.
   A list's chevron opens its **list card** (ADR 0025, ADR 0026, ADR 0029)
   before or after selection — a full-height right-side sheet, one modal
   `RvDialog` portalled to the document body: it locks the page scroll behind
   it, traps focus, returns it on close, and stays whole over a page scrolled
   to any position. The table's scroll area fills the sheet down to the footer;
   its rows keep their natural height and align at the top, including when a
   filter leaves one row. A long table has no separate empty band between its
   scroll area and the footer. When enlarged controls need more height, the
   card body also scrolls rather than collapsing the table or clipping actions.
   The card opens straight on its
   destination table, with a filter field above it: catalog seeds, operator
   additions and the stored source observations as rows — domains, then IP
   addresses, then networks — each naming only its origin by name
   («catalog», «by hand», «v2fly»), because a value already shows what kind
   it is. **Opened from a route, the card reads**: the rows carry no control,
   because the route's own act is the footer and the list itself is edited
   where lists are edited (ADR 0029). The sources are a fact here
   («Источники · 2»), not a control. The footer is one line: the membership status, the toggle beside
   it, and «applies when the route is saved» only while the card was opened
   from an unsaved draft. A draft's name is proposed from what is picked, so
   the footer does not repeat it back as if it named the list — it names the
   route only once the route is stored. «Открыть в библиотеке» leads to the
   other flow in a new tab, leaving the draft behind it untouched, and the
   composing screen re-reads the catalog when its window comes back.
   Secondary actions — one-off export, send, refresh-and-rebuild, archive —
   live in one overflow menu; format and maintenance choices drill into named
   submenus instead of forming one long flat list.
   Adding another connection on the Connection tab creates the output and
   publishes it in one move, because a format with no file is a promise the
   screen cannot keep. The route refresh rule is configured on this tab beside
   its outputs; Settings supplies only the global default. Each output names
   its next unmet delivery condition — choose a connection, turn on automatic
   delivery, turn on route refresh, or ready — using the persisted output,
   connection, and schedule facts. Automatic delivery is primary when a
   deployer exists; subscription and manual download remain available
   connection methods.
4. `/library` — **Lists / Списки.** The library: what a list holds and which
   category holds it, for every route at once (ADR 0029). The same
   master-detail geometry as the picker with no checkboxes, because nothing
   here is being selected. The category column ends in «Без категории» and
   creates one at its foot; a category's menu adds a list from the whole
   catalog, renames the operator's own, and deletes any of them — a catalog
   category included, because deletion is a record in the operator's overlay
   and never an edit to the shipped file. Deleting a category asks the one
   question it must: move its lists to «Без категории», or delete them with
   it. A list's row opens its card in the editing mode — rename, entries,
   imports, sources, «Обновить из источников» on the card itself rather than
   inside the sources dialog, and «Удалить список» — and carries a menu whose
   words separate the two acts a bin cannot: «Убрать из категории» and
   «Удалить список». Deleting a list or a category a route names directly is
   refused with those routes named; removing a list *from a category* is
   allowed to change what a route carries, because that is what naming a
   category means. This card has no footer: no route is in question.
5. `/lists/{listId}/send/{outputId}` — **Send.** An action, not a step:
   automatic applying with a plan, a backup and an audit trail when the format
   has a deployer, and the by-hand path always stated below it.
6. `/connections` — **Connections / Подключения** (ADR 0027; `/devices`
   redirects here). One section answers «куда»: registered devices and
   applications first, with the catalog-backed «Добавить подключение» form.
   Router login and route-interface fields are required when the selected
   deployer needs them; the interface help names the Keenetic ID format. A
   saved connection does not send anything. After registration the screen says
   what remains before unattended delivery, and after opt-in it points to
   choosing that connection in a route. The reference of supported devices
   and formats is collapsed beneath them. The words «цель», «вывод» and
   «потребитель» do not appear on any surface: a route feeds *connections*,
   each made of a device or application and its format. If a catalog dependency
   is unavailable, the screen keeps known devices readable and states exactly
   which actions cannot be trusted yet.
7. `/settings` — **Настройки.** Language, theme (system/dark/light), detail
   mode, and the server's address. Preferences live here, not in the chrome.

The URL hash carries only page location — the active tab and the first-setup
handoff — never route contents: a refresh, bookmark, second tab or second
browser resolves the same server-owned object via `GET /v1/lists` and
`GET /v1/lists/{listId}`, never to an empty form. Local storage holds display
preferences only — no product state. Each screen carries exactly one `<h1>` and
one `<main>`. The chrome is a sidebar of sections and nothing else — the
server's own address is a Settings fact, not a footer — and no section is
ever locked.

A route's name and composition are editable; saving them republishes every
output, so a stored route and the files it stands behind never quietly
disagree. What was published stays immutable: an edit adds a version, it never
rewrites one.

One-off export is not an output. `POST /v1/lists/{listId}/export` renders the
current route in any available file dialect without adding a consumer, issuing a
subscription or changing publication history. File-dialect labels describe the
bytes (for example `BAT · routes` or `JSON · all rules`), never pretend that
downloading connects the route to a product.

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

Instruction lives where a decision needs it, and only there. Background a
reader may want — what a subscription link is, how formats differ — sits behind
an informer (`RvInfoTip`), never as a paragraph on the default path.
Disclosures are for long secondary content, not for hints. A label or a heading
never gets a sentence under it that restates the label, the placeholder or the
obvious next step; a hint under a field states an input format or a
consequence, or it does not exist. A surface never explains its own reach: if a
screen has to say that an edit applies somewhere else, the edit is on the wrong
screen. Composing a route writes only that route; the library writes the
library (ADR 0029). The product never volunteers what it does
not do — that it does not scan the network, that a password is not kept: an
absence cannot be shown to the reader, so the sentence asks for trust instead
of giving evidence. What an act must disclose it discloses beside that act, as
the thing that will happen.

## State and honesty

- Every screen state is `loading`, `ready`, `empty`, `degraded` or `error`, and
  every one of them says what is there or missing, why, and what to do next.
  `RvStateNotice` is that shape; a screen does not invent a sixth.
- A status is an icon or a dot plus words — never a filled surface, never a
  colored edge stripe, and never colour alone.
- The interface reports what it knows. A route restored from the server says the
  subscription link was shown at creation; a target that left the catalog keeps
  its stored identity instead of disappearing. Nothing is filled in to look
  complete.
- Each output exposes the last persisted publication attempt. A failed rebuild
  never removes the artifact already published, and the screen says the
  previous file still stands. A rebuild of several outputs continues after one
  failure and reports the affected outputs instead of presenting the group as a
  single success or failure.
- A toggle states an applied fact, never an optimistic click: hiding a target
  changes exactly what its caption says it changes.
- Server codes and server English never reach the operator as themselves.
  Models answer with message keys; the interface owns the sentence.

## The one-time secret

Creating an output creates no bearer credential. The subscription URL is
issued once, after that output's first successful publication, so a failed
initial build leaves no unusable secret behind. It lives in memory for the life
of the tab: never in storage, never in the URL, never in a query, never in a
log. The route that survives a refresh is the server's; it deliberately
cannot restore the link. A device password is held for one attempt and cleared
when it ends, unless the operator explicitly opts into unattended delivery
backed by the operating system's secret store. The address, account and
interface are not secrets and are remembered for the tab, because asking again
for what was just typed is the redundant entry WCAG 2.2 asks products to stop
doing.

## Layout stability

Content that appears must not move content that is already there. Message rows,
stage lines and confirmations occupy their space before they have anything to
say; a background load never blanks or re-creates what it is refreshing; long
values wrap or scroll inside their own box; wide tables scroll inside their own
container, never the page. Menus are viewport overlays: they collision-position
above or below their trigger and never extend a table's scroll area. A screen
that jumps when it answers has failed this contract regardless of how it looks
in a screenshot. Variable-length composition data never lives in sibling grid
rows: category selection swaps the contents of a reserved detail pane that
scrolls independently while the surrounding page keeps its command area
reachable. A modal dialog owns the scroll: the page behind it does not move.

## Tokens

`web/src/assets/styles/tokens.css` holds two levels. Primitives are raw values and
never appear in component code. Roles are what components consume and the only
thing a theme redefines. Dark is the default ground because the operating scene
is an evening desk beside a router; light follows the operating system or an
explicit choice, and both come from the same role names.

The type scale is eight steps: 13px is secondary metadata, 14px is the reading
floor, 15–16px carries normal controls and copy, and 28px is a page title.
Compact controls are at least 36px high, ordinary controls 40px and touch
choices 44px; check and radio marks are 18px. Tight groups use 8–12px, ordinary
component interiors 16–20px, and distinct page sections 32–48px. Anything
compared character by character — links, identifiers, addresses, file contents,
interface names — takes the mono role. No component declares a literal colour,
size, space, radius or duration.

## Components

`web/src/shared/ui` owns the primitives: `RvButton`, `RvStatus`, `RvStateNotice`,
`RvFacts`, `RvField`, `RvTextInput`, `RvTextarea`, `RvSegmented`, `RvTabs`,
`RvDialog`, `RvSelect`, `RvCombobox`, `RvMenu`, `RvInfoTip`, `RvIcon`,
`RvDisclosure`, `RvCopyButton`, `RvCodeBlock`. Overlays — dialogs, menus,
popovers, selects and comboboxes — are headless Reka UI primitives wrapped
once here and styled only with the tokens; a feature never imports Reka
directly, and no native `<select>` or `<dialog>` remains. Every control that
sits in a row with a text field shares its height (`--rv-control-touch`), and
a field drawn as a bordered wrapper around an input — the combobox, a search
box — lets the wrapper own that height rather than the input inside it, or it
stands a border taller than the control beside it. A field — text, select or
combobox — has no hover state: a `<label for>` forwards
`:hover` to its control, so a hovered field would light up from its label. A
panel opened from a field is aligned to its leading edge and never narrower
than it; a single choice or name field is `--rv-measure-field` wide; a stack of
dialogs dims the page once.
Composition selection is the shared `ServicePicker` entity used by the create
and edit flows, and its list card is one modal `RvDialog`. There is
one button, one status mark, one fact ledger, one icon set. Icons are hand-set
16-unit strokes, always decorative, always beside or behind an accessible name
— navigation and menus never go icon-only. A feature that needs a different
look asks for a role or a variant, never a parallel class. A component is a
black box: a host styles its own root class, passes props or fills a slot.

## Language

English is the primary language and the fallback when the browser prefers a
language this build does not speak; Russian is a complete, equal dictionary,
and both are equal layout cases. Interface copy lives in
`web/src/shared/i18n/messages.ts` and nowhere else; a literal sentence in a
component is a defect. Catalog data — list titles, target titles,
installation sentences — is data and is never translated in the interface; the
catalog itself may carry per-language variants (`title_en`,
`manual_installation_hint_en`, ADR 0027), and one accessor picks by locale
with the base string as fallback. `document.documentElement.lang` follows the
choice.

## Accessibility

WCAG 2.2 AA is the floor, and it is a gate rather than an aspiration: native
interactive elements, labels tied to controls, an error beside the control that
caused it, a visible focus ring of at least two pixels, targets no smaller than
24px, and every state distinguishable in text. Verified by keyboard and by axe
at 320, 768, 1024 and 1440 pixels. Automation blocks serious findings; it does
not replace the keyboard pass.

## What the surface does not do

No wizard: nothing is a numbered step, and no section is reachable only after
another. No eyebrow labels, no colored edge stripes, no arrow glyphs welded
into copy, no teaching paragraphs on the default path. No dashboard of things
the product does not have. No status the server cannot substantiate — a live
"the router fetched your subscription" indicator waits for the server to record
that fact, and until then the interface does not imply it. Routes are archived,
not destructively deleted; their published files remain immutable (ADR 0004).
Library lists and categories can be removed through the guarded library flow
(ADR 0029). Removing a library item never deletes published artifact history.
