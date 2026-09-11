---
status: adopted
---

# ADR 0041: one interface word per object — format and connection

Replaces the vocabulary clause of
[ADR 0027](0027-capacity-aware-composition-and-one-connections-section.md),
which gave «подключение» to two different objects at once.

## Context

ADR 0027 did two things in one decision. It renamed the Devices page to
«Подключения» / Connections, where the object is a registered access a user
configures — an address, an account, an interface and, on consent, a password.
It also removed «цель», «вывод» and «потребитель» from the interface and
declared that "a profile feeds *connections*, each made of a device or
application and its format", which is the object the API calls an output.

Both sentences were adopted, so the word landed on two objects. Nothing checks
interface copy for that, and the two readings were then written into the UI
contract verbatim: "an **output** — one format, optionally one device" in the
vocabulary paragraph, "a profile feeds _connections_" in §6.

The measurable result, at the time of this decision:

| Surface | Catalog entry (`targets`) | Registered access (`devices`) |
| --- | --- | --- |
| `/connections` | «Устройство или приложение» | **«Подключение»** |
| Profile tab | **«Подключение»** | «Устройство» |

The two screens named the same two objects in transposed order. Three pairs of
strings collided outright: `devices.empty` and `outputs.empty` were the same
sentence, «Подключений пока нет», for different objects; `devices.add`,
`outputs.add` and `outputs.device.add` were the same label, «Добавить
подключение», on three different actions, two of them on one panel; and the
configuration-transfer preview listed «2 устройства» (registered accesses) next
to «3 подключения» (published formats) in one summary.

`docs/requirements.md` additionally claimed that "the interface, the HTTP API,
the catalog, the transfer format and the stored schema" all use the word
*connections*. They do not: the API serves `/v1/devices` and `/v1/outputs/{id}`
and the schema holds `devices` and `outputs`. ADR 0039's table never contained
the word.

**A catalog entry is not a device.** `keenetic` and `keenetic-dns` are one
router entered twice, because their `format_key` values differ
(`keenetic-bat-ipv4-v1` and `keenetic-fqdn-group-v1`). Any word meaning *what
kind of thing this is* — устройство, приложение, система, платформа — cannot
tell that pair apart. The catalog already says what distinguishes them: ADR 0039
renamed this key from `profile_key` to `format_key` for exactly this reason.

**A registered access is not a device either.** What is stored is how to reach
something, not what stands there: a router, an application on a phone, or a
service on a host. «Устройство» over-claims; «подключение» names the access and
claims nothing about the far end.

## Decision

One interface word per object, identical on every screen:

| Object | API and schema | Interface word |
| --- | --- | --- |
| catalog entry, one renderer format | `targets`, `format_key` | **формат** / format |
| registered access | `devices` | **подключение** / connection |
| profile ↔ format binding | `outputs` | the row itself; no separate noun |

The profile's tab stops being «Подключение» and becomes **«Публикация»** /
Publishing: it holds the format, the file, the subscription link, the refresh
rule and the connection that delivers, so it is named for what it does rather
than for one of its columns. Its table reads «Формат» · «Файл» ·
«Подключение» · «Содержимое от» · «Действия»; the column that showed a file
extension under the heading «Формат» is now «Файл», which is what it always
showed.

**The binding gets no noun of its own.** An output is a join, and the row is
already identified by its format. Naming it in the interface would add a third
word for an object nobody creates directly — the user adds a format, and the
join follows.

**«Устройство» is not retired, it is scoped.** It remains where a message is
about a physical device during a delivery attempt: the deploy step names, the
deploy error taxonomy, and the per-target field labels a catalog entry supplies
(`deploy.field.address.keenetic`). It names no object of the product.

**The wire keeps its identifiers.** `outputs`, `devices`, `targetID`,
`deviceID`, the dictionary key families and the CSS tokens do not change. This
is the split ADR 0039 already applies to three wire names whose Go identifiers
follow the current vocabulary while their tags do not; here it runs one layer
up, between the interface word and the API object. Renaming the API would break
`/v1/outputs/{id}`, `/v1/devices`, the stored schema and the transfer format for
a change no operator can observe.

ADR 0027's other vocabulary clauses stand: «цель», «вывод» and «потребитель»
still appear on no surface, and `/connections` keeps its name and its address.

## Consequences

A reader of the release notes sees the profile's tab renamed and one table's
headings change. Nothing moves, nothing is added, and no stored data is touched;
the screens keep their structure.

The interface word and the identifier now differ for two objects. A contributor
reading `outputs.column.device` finds the value «Подключение», and that is
correct: the key names the API field the column writes, the value names the
object the operator sees. The mapping lives in this ADR and in the vocabulary
paragraph of [the UI contract](../UI.md), and nowhere else.

Nothing mechanical enforces one word per object. The collision this ADR removes
was introduced by an adopted decision and survived every gate, so the guard is
review against the table above, not a validator. A future dictionary check that
flags two keys sharing one value would have caught the three colliding pairs;
it is not built here, because it needs a list of deliberate duplicates to be
useful and this tree has none to seed it with.

The retired interface words are not accepted anywhere as input, because none of
them was ever typed: they were labels, not identifiers.
