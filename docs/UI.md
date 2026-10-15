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
sessions add no update item. Application updates are independent of source refresh.

The unit of the product is the **profile** (ADR 0013, named by ADR 0028): a
stored, server-owned composition of **lists** and **categories** with no
format of its own. A _list_ is a named set of destinations — domains,
addresses, networks — seeded by the catalog or created by the operator and
editable either way; a _category_ contains lists, ships with the catalog and
is edited by the operator on top of it. A profile publishes in one or more
**formats**, and a format owns its subscription link, its chain of published
files and, when delivery is set up, the connection that carries them. A
**connection** is one registered access — an address, an account, an interface
and, if the operator saves one, a password — and it names the access rather than what stands
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
   and composition, its formats and content time. A format whose last build
   failed is marked «Build failed» in that cell, with the time of the file still
   published beside it when there is one. The row is a state summary, not a
   toolbar: one overflow menu owns the profile's actions. For a live profile it
   lists open, export to a file (a submenu of file formats), send (one item per
   format, naming the connection it is bound to), add a format (the profile's
   Publishing tab), copy contents (one published format names itself, several
   are a submenu), rebuild files (only while every format has been built, so a
   first link is never produced where it cannot be shown), and, after a
   separator, archive. Clicking a noninteractive row cell navigates to the
   profile's own page; the title remains a native link. Nothing on the page
   explains what a profile is: the rows are the explanation. Imported v3
   profiles are identified as restored profiles and say what to do next rather
   than leaking migration names. The primary action is «Build a profile», and
   the empty shelf does not repeat it.
   **Archive** is a filter of the same table, not a second block: once any
   profile is archived a segmented control above the table reads «Active ·
   Archive N», where N is the server's count of archived profiles and not the
   number of rows loaded. The archive lists its profiles with the same columns
   plus when each was archived, and an informer beside the control carries the
   explanation of what an archived profile is. Archiving runs at once and the
   shelf answers with a notice naming the profile and one «Undo» action.
   An archived row's menu is open, export, restore and, after a separator,
   «Delete permanently…» (trash glyph), which asks first and says what the
   deletion leaves behind: the profile's subscription links stop working,
   published files and the delivery log are kept, and groups and routes already
   sent to routers stay (ADR 0051). When the row goes the count follows, and an
   emptied archive returns the operator to the shelf.
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
   A filter chip, «In profile (N)», shows only the selected rows; a row taken
   out while it is on stays in place, unchecked, until the chip is pressed
   again, so selection still never moves a row.
   The secondary “New lists” menu explicitly controls whether future category
   members are included automatically; this live reference is distinct from
   the table's selection checkbox and never occupies a separate banner.
   The settings rail contains the name, first format choice,
   forecast and create action, without repeating the selected lists. In a
   constrained window it stands above the table, as it already does while a list
   card is docked, with its actions first and its fields in two columns under
   them, so the create action is never pushed below a table that can run to fifty
   rows.
   Create stays pressable while something is missing: pressing it states each
   omission beside the field that lacks it — name, format, or a table with no
   list in it — and moves the keyboard to the first of them. A format the
   forecast cannot weigh in full does not stop creation; beside the create
   action the screen says that its file will not be published until the named
   lists have usable data.
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
   restores it, and a format the draft would overflow is warned about under
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
   The header carries one profile menu: one-off export and send drill into
   named submenus of formats, then «Добавить формат…», «Пересобрать файлы»
   and, after a separator, «В архив»; an archived profile offers only export
   and «Вернуть из архива». Profile facts keep their natural height; only the
   composition workspace fills the window. The header states the files'
   condition only while something needs attention or is under way: a profile
   whose formats are built and current carries no status there, because
   «published» does not say where or when, and the content time already stands
   in the Publishing table. A first build that published nothing reads «Сборка
   не удалась», and its notice carries «Пересобрать файлы».
   The subscription link block stands collapsed above the tabs: its summary
   names the format, and it opens on request. It opens by itself when a link
   has just been issued or reissued, because that is the only moment the link
   can be read (see «The one-time secret»).
   The Publishing tab is one table and what sits around it. Above the table
   stand one field, «Автообновление профиля» (Settings supplies only the
   global default), and the «Добавить формат» action, which opens the
   searchable format choice; adding a format creates it and publishes it in
   one move, because a format with no file is a promise the screen cannot keep.
   Each row is one line: the format with its file extension, its connection,
   the content date, and the date the file last reached that connection
   («Отправлено»: the latest successful send to a saved connection, manual or
   automatic; a dash when there has been none). The connection cell holds only
   the choice, which names connections only, with «Не выбрано» first; a
   connection's delivery state is the option's note, and the connection page
   owns that state. Download and Send are compact icon buttons at the row's
   end, each named for its format and explained by a tooltip. An archived
   profile shows the bound connection as text, offers no changes and no Send;
   Download stays. Each row's
   «⋮» menu holds «Открыть подключение» while a connection is bound,
   «Настройки формата…» (the FQDN group prefix of a domain-based
   format: its rule appears only as a refused save, «Применится при следующей
   сборке» beside Save, and «Пересобрать сейчас»), «Пересобрать», «Выпустить
   новую ссылку…» once the format has published, and «Убрать формат…». Issuing a
   new link and removing a format each confirm with their consequence; the new
   link is shown once (see «The one-time secret»). After a reload no format
   has its link on screen; each format's own menu offers «Выпустить новую
   ссылку…», and the tab carries no notice that speaks for one format of
   several. Automatic delivery is
   primary when a deployer exists; subscription and manual download remain
   available ways to get the file.
   The Diagnostics tab is one table of the rules of the shown file: a list
   filter, a search field, columns for list, value and reason, and a state
   column only when the file excluded something. The table fills the window
   below its heading and scrolls beneath its header; per-list counts follow it.
4. `/lists` — **Lists / Списки.** The library: what a list holds and which
   category holds it, for every profile at once (ADR 0029). It uses the same
   dense catalog table, search and category filter as the composer, with list
   management actions in place of membership checkboxes. Category labels use
   the same identity colors. The filter includes «Без категории»; a labelled
   «+» tag beside the filters creates a category, and «Категории» in the page
   header opens category management. «Новый список»
   creates a list or adds an existing one to the selected category. Creating a
   category and creating a list are each one bounded dialog — a centred box, not
   a working sheet — with the name at the shared field width and the actions
   together at the right of its footer. Category management names itself once,
   in its dialog title; a row is opened by the whole row, so it carries no
   chevron beside its menu, and the actions column of the table is named
   «Действия» for a reader's software, so no two column headers share a name. A category's
   menu renames the operator's own category and deletes any category — a catalog
   category included, because deletion is a record in the operator's overlay
   and never an edit to the shipped file. Deleting a category asks the one
   question it must: move its lists to «Без категории», or delete them with
   it. A list's row opens its card in the editing mode — rename, entries,
   imports, sources, «Обновить из источников» on the card itself rather than
   inside the sources dialog, and «Удалить список» — and carries a menu whose
   words separate the two acts a bin cannot: «Убрать из категории» and
   «Удалить список». A menu panel in which any item has a glyph keeps the glyph
   column for every item in it, so the names start on one line. Deleting a list or a category a profile names directly is
   refused with those profiles named, each as a link to the profile, and an
   archived one is marked as archived and said to be deletable permanently from
   the archive of «Профили», because an archived profile cannot be edited; removing a list _from a category_ is
   allowed to change what a profile carries, because that is what naming a
   category means. The library also owns the default list priority. Its complete
   ordered table can be rearranged by its always-visible drag handles or
   keyboard. Priority numbers are visible; save and reset actions appear for a
   changed draft, and a failed save retains the order for retry. This order initializes profiles and forecasts that do not yet
   supply their own priority; saving it never rewrites an existing profile's
   stored order. The list card has no profile-membership footer. The table fills
   the available workspace height and scrolls beneath its header. While the library
   is writing or a list is reading its sources, conflicting menus, switches,
   deletion and dismissal stay unavailable until the result is known. A first
   read that fails says which of two things happened — nothing answered, or the
   service answered with an error — because they ask for different next steps,
   and the commands that need the lists stay unavailable until they are read;
   the composer and the list card say the same.
5. `/profiles/{profileId}/send/{outputId}` — **Send.** An action, not a step:
   automatic applying with a plan, a backup and an audit trail when the format
   has a deployer, and the by-hand path always stated below it. The trail
   «Профили › {profile}» stands above the title «Отправка · {format}»; a status
   appears only once an attempt exists. The connection choice uses the field
   labels of Connections and does not repeat the chosen connection's facts;
   with no saved connection it reads «Нет сохранённых подключений» with a link
   to add one. A connection whose password is saved shows «Пароль сохранён» and
   «Ввести другой для этой отправки», which reveals the field for this send only;
   otherwise the password field is shown, with «Сохранить пароль в подключении»
   when the system has a credential store, stating that the password will be
   kept in the Windows credential store. The password is saved only after the
   send succeeds: a preview of a router does not contact it and proves nothing
   about the password. A send refused while it used the saved password says
   «Сохранённый пароль не принят» and offers «Ввести другой для этой отправки»;
   «Заменить сохранённый пароль» keeps the new one in its place.
   «Показать изменения» is the primary action until a
   plan exists. The by-hand path is «Скачать файл (.ext)» and a «Как установить
   вручную» disclosure with the numbered catalog steps. An archived profile
   reached by a direct or kept link shows no form: the notice «Профиль в
   архиве, отправка недоступна» says the file is still downloadable from the
   profile page, and «К профилю» leads there.
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
   reachability. The selected row is marked by the shared selected ground and a
   mark that is not a colour, the chevron at its end; it has no frame or edge
   stripe. The format's glyph stands in the name's own line, and the list
   scrolls inside its own frame rather than lengthening the page.
   Saved connections remain selectable beside the creation or
   configuration area; a constrained window stacks these regions in document
   flow. Creation and delivery permission are primary page work, without a modal
   or scroll lock. Switching the work area moves focus to its heading. Password input
   is cleared on every departure from its connection. Saved settings never imply
   verified reachability or successful delivery.
   Router login and route-interface fields are required when the selected
   deployer needs them. What a field takes is explained behind the informer at
   the end of its label row, the shape of a value is its placeholder, and under
   the field stands only its error; the same texts serve the manual fields of
   Send. A saved connection does not send anything.
   A saved connection's editable parameters — name, address, account and route
   interface — are edited in place under «Параметры», in the same two-column
   grid the creation form uses. Adjacent fields share caption and control rows,
   so differing help lengths do not stagger the inputs. The format is the
   connection's identity and is never editable: a registered Keenetic does not
   become an OpenWrt. Both forms validate the same way: Save is available
   whenever the draft differs from what is stored (creation: whenever a format
   is chosen), and pressing it never silently does nothing. A field left empty
   or malformed states its problem beside itself once Save was pressed, and
   from then on the statement follows the value as it is corrected; leaving a
   field states nothing. Every required field is marked for assistive
   technology, and focus moves to the first field that needs attention. The form checks only the
   shape of an address — an explicit scheme, a host, nothing else the field does
   not take. Which networks a device may live on is the service's policy: a
   refusal it names with `address_rejected` and the field `address` (ADR 0050)
   is stated beside the address, in the words of the format's own kind of
   address, until the address changes, and the generic failure notice is not
   drawn beside it. A refusal that names no field is not pinned on one. The same
   holds for the address typed on Send. Because leaving a field states nothing,
   nothing moves the button from under a pointer press; the statements appear
   once the press has landed. After a
   write, `keepFocus` puts focus back where the operator was, or on the first
   field when the control that held it — Save, with nothing left to save — has
   nothing left to offer. «Отменить» and opening another connection both return
   the stored values. Because a password and consent name one exact destination
   and account, changing the address, account or interface turns automatic
   delivery off and removes the saved password — the screen says so beside Save
   before it is pressed, in a notice that lists exactly what will be lost and, for
   a router, that Routevane creates its groups again on the next send while the
   earlier ones stay on the router to be removed by hand (ADR 0040), and
   afterwards reports the state the server returned. A name-only change says
   nothing of the kind. When the deployment catalog is unavailable the
   parameters are stated rather than edited, because the fields a format asks
   for are that catalog's answer.
   «Пароль» and «Автоотправка» are two sections, each its own act, and neither
   is part of saving parameters (ADR 0050). A format that needs a password shows
   «Пароль» under the parameters. Without a saved one it is a field and
   «Сохранить пароль» with the consequence of saving beside them; with one it is
   the statement «Пароль сохранён» with «Заменить» and a trash «Удалить». The
   password is never read back, masked or otherwise, and where the system has no
   secret store the section says saving is unavailable instead of offering a
   field. Removing the password also turns automatic delivery off, and the
   section says so while delivery is on. A format that needs no password shows
   no such section. «Автоотправка» is one switch that states what the server
   holds and changes only when the server confirms. For a format that needs a
   password it is unavailable until one is saved, with the reason in words
   beside it («Сначала сохраните пароль»), and turning it on never carries a
   password of its own. A successful change immediately adopts the returned
   delivery state. If the following registry read fails, that confirmed state
   stays visible while a retry refreshes the list without repeating the
   change.
   «Забыть подключение» is the connection's only secondary action, in the
   action menu of its detail header, and it is confirmed before it runs.
   A confirmed removal leaves the list immediately, even when the following
   registry read fails. Selection moves to a remaining connection or the empty
   registry; the read failure keeps a page-level retry available in either
   case. A refused or unanswered removal keeps the last confirmed card.
   After registration the screen says
   what remains before unattended delivery, and after opt-in it points to
   choosing that connection in a profile; it never states that a connection is
   unused, because which profiles name it is not a fact this screen reads. The
   list of connections is headed «Сохранённые подключения». The screen carries
   no reference of formats: the formats this build carries are listed, and
   chosen for the composer, in Settings, and whether a format can be delivered
   to automatically is the note of its option in the creation form. The words
   «цель», «вывод» and «потребитель» do not appear on any surface, and
   «подключение» names exactly one thing on every surface: the registered
   access this screen owns (ADR 0041). What a profile publishes in is a
   _format_. «Устройство»
   is left for a physical device during a delivery attempt — a deploy step
   or a deploy error — and names no object of the product. If a catalog dependency
   is unavailable, the screen keeps known devices readable and states exactly
   which actions cannot be trusted yet.
7. `/settings` — **Настройки.** Language, theme (system/dark/light), detail
   mode, the formats the profile composer offers, the default source-refresh
   schedule, and portable configuration transfer. The page has the same width
   as Connections. «Форматы» lists every format this build carries as one
   switch each under «Предлагать при добавлении формата»; a switch states what
   this browser holds and is the only place that preference is changed. An
   unreadable catalog says so with a retry instead of an empty list. The
   segmented choices raise the chosen option on a neutral surface with a hairline
   edge, without the accent colour or a check mark, so the choice rests on the
   raised ground and edge rather than on colour. Every section is the shared
   section card (see Components), with the same frame as the cards of
   Connections: title and explanation on the left, related controls and actions
   on the right. On constrained screens the heading precedes its content. No
   horizontal separator runs inside a section. Interface
   fields place labels above their controls; export and import stack in the
   same content column. The source-refresh explanation belongs beside its
   schedule, and the choice is named once, not by a second visible caption. The
   server's address is the final, secondary section, «Локальный сервис». It identifies
   the serving origin, including the local backend address in the desktop app,
   rather than the application window's internal hostname. Below the address
   it states the running service's version after one bounded read when the
   screen opens. A development build reports `dev`; an unavailable version is
   left unstated rather than inferred from the interface package.
   Export and import are separate actions. Preferences live here,
   not in the chrome. Import starts with a product-styled file
   surface backed by the labelled native file chooser, supports dropping a file,
   keeps the chosen file visible while reading or checking it. Choosing a file
   starts its check at once, with no separate step: the server-owned preview,
   or the reason the file was refused, appears in place, and choosing another
   file checks that one instead. The preview's one action is «Импортировать»,
   which opens the confirmation. Confirmation applies
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

The URL hash carries only page location — the active tab, the first-setup
handoff and, on Connections, the connection to open (`#connection=<id>`; an
unknown id opens the default one) — never profile contents: a refresh,
bookmark, second tab or second browser resolves the same server-owned object
via `GET /v1/profile-shelf/{shelf}` and `GET /v1/profiles/{profileId}`, never
to an empty form. Local storage holds display
preferences only — no product state. Each screen carries exactly one `<h1>` and
one `<main>`. The chrome is a collapsible sidebar of sections; its bottom control switches
between labels and icons and remembers that display preference. Navigation
labels remain single-line throughout the width transition, fading and translating
without switching display or changing button height. Horizontal overflow stays
clipped throughout motion. A detail panel animates as one surface, including its
border; its host paints no separate piece that could appear before the panel. The
server's own address and version are Settings facts, not a footer — and no
section is ever locked.

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
obvious next step. A field's example is its placeholder, its explanation sits
behind an informer beside its label, and under a field there is only its error.
No line is kept for that error while there is none, so a form keeps the plain
spacing of its fields; the error appears when a submit finds a problem, wraps
like any text, and pushes what is below down by its own height.
Keep edits on the surface that owns their
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
- A toggle states an applied fact: hiding a format changes exactly what its
  caption says it changes. The one place a switch is drawn ahead of its write is
  a row on a list card (an entry or a source), which is small, independent and
  repeated in quick succession: the switch changes at once, one write per row
  follows the last choice, and a failed write returns the switch to the value
  the server confirmed, says "The change was not applied." on that row and
  offers its retry. A toggle whose effect reaches beyond its own row waits for
  the server.
- Server codes and server English never reach the operator as themselves.
  Models answer with message keys; the interface owns the sentence.

## The one-time secret

Adding a format creates no bearer credential. The subscription URL is
issued once, after that format's first successful publication, so a failed
initial build leaves no unusable secret behind. It lives in memory for the life
of the tab: never in storage, never in the URL, never in a query, never in a
log. The profile that survives a refresh is the server's; it deliberately
cannot restore the link. Reissuing a link follows the same rules: the new URL
is shown once, and the previous link stops working at that moment. A device
password is held for one attempt and cleared when it ends, unless the operator
explicitly saves it for the connection in the operating system's secret store
(ADR 0050); saving is separate from unattended delivery, and the interface never
reads a saved password back. The address, account and
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
A menu, a choice list and an informer panel are raised surfaces: in the dark theme
they stand a step lighter than the canvas and are drawn with a hairline that
reads against it, so an open panel never blends into the page.
Its shared workspace centers the page content with the same width limit
on profiles, composition, library, connections and settings; features do not add
their own page containers. Docked inspection divides the existing workspace
without moving its outer edges. Reading widths belong to form fields and prose within it.
The sidebar keeps its place on desktop and
can collapse to icons with labels on hover or keyboard focus. Below 68.75rem
(1100 CSS px at the default text size) it starts collapsed, so the working
surface keeps its width. The operator's own choice with the collapse control is
kept and wins in any window; only while none is kept does the window decide. The profile composer
and library allocate remaining height to their data regions; headings and filters
keep natural height. Short windows and enlarged content may scroll to preserve
access to actions. Beside the table, the settings rail is pinned at the page inset
and is never taller than the window less that inset twice, and its actions come
first in it, so create and save stay in view wherever the page is scrolled; its
fields scroll inside it when they do not fit, and nothing of them passes under the
actions. In a section that fills the window, the data region, or the
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
toolbar shows calculation state; the number of selected lists is read once, on
the «В профиле (N)» filter chip, and no separate line repeats it. The overlap explanation is
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

Create and edit share the settings form layout, in one order wherever it stands:
the actions first, with a hairline under them, and the fields below. The primary
act — create, or save and rebuild — spans the rail's full width. A line above it
holds what qualifies it only while there is something to say: the edit form's
note that the draft has unsaved changes with its discard action, or a missing
piece that blocks creation; with nothing to say the line takes no room. When moved above the table, the fields align at the top in two equal
columns under the actions. Numeric overlap disclosures keep numbers aligned
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
values wrap or scroll inside their own box, except an option's name in a
choice panel, which the library shortens with an ellipsis; wide tables scroll
inside their own container, never the page. A table's frame never traps the
wheel: when it has nothing more to scroll in that direction, the page scrolls.
Only overlays — menus, choice panels, informers — keep the wheel to themselves. Menus are viewport overlays: they collision-position
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
`RvDisclosure`, `RvTable`, `RvFilePicker`, `RvCopyButton`, `RvCodeBlock`, `RvSection`. Nuxt UI supplies
the styled mechanics behind `RvButton`, the text fields — `RvTextInput` is
`UInput`, `RvTextarea` is `UTextarea`, `RvField` is `UFormField` — and both
variants of `RvDialog`: the full-height sheet is `USlideover` and the bounded
centred panel is `UModal`, which own focus, dismissal and the scroll lock. The
panel owns the inset of its body as it owns its header's and footer's, so a
caller places content in it without padding of its own and that content starts
on the title's line. It also draws the choices: `RvSelect`
is `USelect`, and `RvSearchSelect` — with `RvCombobox` through it — is a
`UCommandPalette` in a `UPopover`. At field size its trigger is drawn as a
field and its panel is as wide as the field; the compact category filter's
panel keeps the panel measure instead of a chip's width. A choice opened inside
a dialog is portalled into that dialog and kept within its bounds. The
remaining overlays use headless Reka UI primitives. Both libraries are wrapped
once here and themed with Routevane
tokens through the `--ui-*` bridge in `nuxt-ui.css`, not through classes: a
feature never imports `U*` or Reka components directly, and no native
`<select>` or `<dialog>` remains. Every control that
sits in a row with a text field shares that row's chosen control height.
Standard fields, choice triggers and buttons use `--rv-control-default`; a page
header's principal action uses `--rv-control-touch`, and a compact row must use
a consistent shared variant for the field and its adjacent controls. In either
case, a field drawn as a bordered wrapper around an input lets the wrapper own
that height rather than the input inside it, or it stands a border taller than
the control beside it. A text field has no hover state: a `<label for>` forwards
`:hover` to its control, so a hovered field would light up from its label. A
choice trigger is a field as well: its ground and border do not change under
the pointer, and it keeps the pointer cursor and the focus ring. A choice
trigger at field size reads
from its leading edge on one line, cut by an ellipsis, and keeps the chosen
value on screen while it is busy; a field's label and the value it holds name
it, and its panel is named by that label rather than by its placeholder.
`RvField` names its control by the caller's id whether or not the control is a
library input. An example of the input's format is its placeholder, drawn in
a muted ink that passes the contrast gate, and an explanation is the field's
`info`: an informer in the label's row, outside the label so the control keeps
the label's words as its name, and named by what it is ("More information"),
never by the label, so a query by label finds the field alone. Where a form holds
more than one informer, `infoLabel` names each by what it explains ("What the
address field takes"), so a reader moving between them can tell them apart. The explanation's
text is also kept in the field, visually hidden, and the control is described by
it together with the error that is actually shown, so a reader hears it without
opening the informer. Under the control there is only the error. The `label-aside`
slot is the label row's place for any other small control. The field draws its
error only while there is one and keeps no empty line for it; forms state errors
once their submit was pressed, never on leaving a field, so an error never
appears in the middle of a press aimed at a button below. A control that shares
a row with the field, such as «Сохранить пароль», sits inside the field beside
its input, so the error runs under both. An
error that arrives while focus is elsewhere is announced by a live region that
stands from the first draw of the field and only receives the text; a region
that appears together with its text is not reliably announced. A file
picker is the one control with a line of its own under the label: which files it
takes is a fact needed before one is chosen, not an explanation. A disabled field
takes the one disabled look stated under the button below, not a colour or an
opacity of its own. A
panel opened from a field is aligned to its leading edge and never narrower
than it; a single choice or name field is `--rv-measure-field` wide; a stack of
dialogs dims the page once.
Composition selection is the shared `ListPicker` entity used by the create
and edit flows, and its list card is one modal `RvDialog`. There is
one button, one status mark, one fact ledger, one icon set. A refresh command
carries the refresh glyph and turns that glyph in place while its sources are
read, keeping the width it had at rest; while it works it is disabled and
`aria-busy`, and any pending sentence belongs to the status text beside it.
Every other pending command keeps the button's own ring. A disabled primary
keeps the accent at low emphasis, so the principal act stays recognizable while
it is unavailable; the other variants keep the neutral disabled ground, and the
destructive variant keeps its own quiet ground the same way.

The final confirmation of an act that cannot be taken back — deleting a profile,
a list or a category for good, forgetting a connection, removing a format,
issuing a new link that ends the old one — is the `danger` variant of
`RvButton`. Its ground is the danger role, which stands with the inverted ink
at 4.5:1 or better in both themes. A reversible act, such as archiving or
restoring, and an additive one, such as applying a reviewed import, keeps the
primary look.
One disabled look serves every control that takes input: a button, a choice
or menu trigger, a text field, an editor, a switch, a segmented choice, a file
picker, a filter chip, a drag handle and a dialog's close control. An
unavailable control is flat and dimmer than an available one: it gives
up its ground, its words and glyphs take the disabled ink — legible at 3:1 or
better in both themes, though a disabled control is exempt from 4.5:1 — its edge
is the faint rule (`--rv-edge-disabled` where the edge is a ring), its cursor says
it is unavailable, and its opacity stays whole: no such control is dimmed by
lowering opacity, which would leave it looking like the enabled control beside
it. The control keeps what states its value without colour: a switch its
position, with the accent at low emphasis while it is on, a segmented choice its
raised ground and a stronger edge, a file picker its dashed edge, a pressed filter chip the
accent at low emphasis. A quiet control — a menu trigger, a handle, a dialog's
close control — has no ground or edge at rest, so it gives up none and takes
the disabled ink alone. Each component owns its own disabled rule and the
library's lowered opacity is overridden there. Only the row a drag leaves
behind is faded, with `--rv-drag-ghost-opacity`; it is a placeholder, not a
control.
Icons are hand-set 16-unit strokes, always decorative, always beside or behind an accessible name
— navigation and menus never go icon-only. A repeated row action in a table,
such as Download or Send on the Publishing tab, may be an icon-only button: its
accessible name says what it acts on and a tooltip shows that name. Every item
of an action menu carries a glyph, as do commands that a sibling command with a
glyph stands beside; form actions such as Save and Cancel carry none. A menu
whose items are mutually exclusive settings, such as «Новые списки», uses its
glyph column for the check mark of the current setting instead. A feature that needs a different
look asks for a role or a variant, never a parallel class. A component is a
black box: a host styles its own root class, passes props or fills a slot.

A page section that stands as a card of its own — every section of Settings,
and on Connections the list of saved connections, the selected connection, the
creation form and the empty registry — is `RvSection`. It is a native
`<section>` named by its own level-two heading, and it draws one look: the
surface ground, a hairline edge, the large radius, the `--rv-space-6` inset and
a heading in `--rv-text-module`, so a section's own parts, headed in
`--rv-text-section`, stay below it. A short description may stand under the
heading, a count beside it without joining its name, and actions at the end of
the header line. Settings puts the heading in a column beside the content
(`layout="beside"`) and stacks the two where the section is too narrow for
both; Connections stacks them. The list of saved connections is `flush`: its
rows run to the frame's edges under a header divided from them by a hairline,
and once the page caps its height they scroll inside the frame. The host sets
the space between the content's own children with `--rv-section-gap`, and moves
the keyboard to the heading through the section when it replaces what the
section shows. Nuxt UI's `UCard` is not used for this: it renders no landmark or
heading, and its insets follow the window's breakpoints rather than the
section's own width.

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
`manual_installation_hint_en`, `manual_installation_steps_en`, ADR 0027), and
one accessor picks by locale with the base string as fallback. `document.documentElement.lang` follows the
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
that fact, and until then the interface does not imply it. A profile is archived
before it can be deleted, and deletion withdraws its links while its published
files remain immutable (ADR 0004, ADR 0051).
Library lists and categories can be removed through the guarded library flow
(ADR 0029). Removing a library item never deletes published artifact history.

## Shared field and overlay contracts

Text inputs and choice triggers use the same field ground, distinct from their
containing panel. Searchable choices share `RvSearchSelect`: a labelled trigger,
a portalled panel with search at the top, optional groups, a bounded scrolling
option list, and focus returned to the trigger on dismissal. Short policy choices
retain `RvSelect` without a search field. Tooltips are `RvTooltip`, drawn by
the library's tooltip: they open on hover and keyboard focus after a quarter
second, describe the control they stand beside, close on Escape, and stand above
page content in a portal. Sidebar tooltips speak only while the sidebar is
collapsed.

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
stay outside comparison rows; a format's FQDN group prefix is configured in
the «Настройки формата…» dialog of its row menu. Format choices and connection names use the
same decorative icons, with a generic device icon for an unknown plugin target.
