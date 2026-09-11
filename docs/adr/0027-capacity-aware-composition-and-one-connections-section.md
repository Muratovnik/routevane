---
status: adopted
---

# ADR 0027: capacity joins composition, and «куда» is one section

> Current scope: the vocabulary clause below gave «подключение» to both the
> registered access and the profile-to-format binding.
> [ADR 0041](0041-one-interface-word-per-object.md) replaces that clause; the
> section, its address and the retirement of «цель», «вывод» and «потребитель»
> stand.

## Context

A full product walk found the default path broken: the shipped
«Видео» collection on the flagship Keenetic target failed its very first build
— 1742 projected rules against a 1024-rule format — although the product knew
both numbers before the list existed, and although its own catalog carries the
answer (the domain-based Keenetic format). Around that central miss, the walk
confirmed four structural ones: three competing notions of «куда» (цели,
устройства, выводы/потребители) split across two surfaces; a list page whose
Contents tab replays the whole composer instead of showing what the list holds
and what it weighs; a service card that edits global state from inside one
list with the scope stated only in a footnote; and a catalog that speaks only
Russian under an English-first product.

## Decision

- **Capacity is a composition input, not a build error.**
  `POST /v1/lists/preview` forecasts, read-only and from exactly the material
  a build would read, the projected rule count per target for a draft
  composition. The composer shows the forecast on every target card, refuses
  to create a pair that cannot build, and names a fitting alternative with a
  one-click switch. The forecast guards, it does not gate: an unknown forecast
  never blocks creation, and editing an existing list warns per output without
  blocking a save. A build can still fail later — data grows — and the failed
  path keeps its existing reporting. A draft whose services were never
  observed has no honest number yet, so composing observes them once — the
  same operator-initiated read the service card performs on open (ADR 0026) —
  and asks again; each service is read at most once per page session.
- **One section answers «куда»: Подключения / Connections.** The former
  Devices page becomes `/connections` (the old address redirects); registered
  devices lead, and the format catalog is a collapsed reference beneath them.
  The words «цель», «вывод» and «потребитель» leave the interface: what a list
  feeds is a *connection*, made of a device or application and its format.
  *(The last sentence is replaced by ADR 0041: what a profile publishes in is
  a format, and «подключение» names only the registered access.)*
- **The Contents tab shows the list first.** The list's own collections and
  services render as rows with their forecast weights and removal controls;
  the full catalog picker appears only behind «Добавить сервисы». The composer
  keeps its always-visible picker — choosing is its whole job.
- **The service card states its scopes.** A persistent caption under the title
  says edits apply to every list, and the membership control in the footer
  names the specific list it toggles.
- **The target catalog is bilingual.** Targets carry optional `title_en` and
  `manual_installation_hint_en`; the surface picks by locale and falls back to
  the base strings, so an English surface stops quoting Russian instructions.

## Consequences

The forecast endpoint runs the planning pipeline per target on demand; that is
accepted local cost for a truthful number, and it persists nothing. The e2e
that proved «a failed first build stays retryable» now provisions its broken
pair through the API, because the composer deliberately refuses to create it.
Localized titles reach every surface through one accessor, so a future locale
is a dictionary-and-catalog change, not a screen change.
