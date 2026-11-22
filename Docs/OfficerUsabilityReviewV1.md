# Guild Officer Usability Review — V1

**Status:** Review only. The recommendations below are not approved and nothing
in the application was changed by this review.
**Date:** 2026-11-22
**Reviewed revision:** `ab0714d` (working tree)
**Perspective:** A guild officer who uses the system every day to answer “do we
have it?”, receive stock, give stock to members, move stock, keep availability
dates correct, import the bank export, and investigate changes.
**Constraint:** This review does not propose a new phase. Every recommendation
improves an existing V1 route, screen, label, or workflow.

---

## 1. Executive verdict

V1 is operationally credible. The security model, audit trail, optimistic
concurrency, soft deletion, import review, and all-or-nothing bulk removal are
strong foundations. From a daily-use perspective, however, the product is still
organized around **records and configuration** rather than around the **tasks an
officer performs**.

The six largest usability problems are:

1. **The application does not land an officer on operational work.** A successful
   login redirects to Settings, and `/admin` also redirects to Settings. The most
   configuration-heavy screen is the default destination.
2. **Inventory is presented as individual rows, not as an item.** The new
   one-row-per-line quantity model makes duplicate stacks normal. A daily “how
   many do we have?” question requires paging through rows and summing manually.
3. **Availability is largely invisible where decisions are made.** The admin list
   can sort by open/bazaar dates but does not display the availability state or
   either date, and it cannot filter by them.
4. **Every drill-down loses the officer’s place.** Detail, edit, remove, and bulk
   removal return to an unfiltered list. The officer re-enters the same search,
   status, location, sort, and page several times per task.
5. **There is no task action for the most common inventory movement.** Giving,
   receiving, moving, or partially adjusting stock is expressed as a raw edit of
   quantity, recipient, location, and dates. The meaning of `GivenTo` is not
   explained, and setting it does not alter public availability.
6. **Import review is powerful but its mental model is inconsistent.** The upload
   page says every accepted row starts selected; the review page says container
   rows are not selected. Location checkboxes both hide rows and destroy their
   selection. “Accepted”, “staged”, “reviewable”, and “selected” are not
   distinguished in the history table.

Two smaller themes recur: technical labels appear in officer-facing copy, and
there is no persistent indication of the current section, pending imports, or
the context from which a record was opened.

**Overall V1 judgment:** suitable for careful, low-volume administration by an
operator who knows the model; not yet efficient for repeated daily use by a
guild officer who thinks in items, members, and stock movements.

---

## 2. Scope, method, and terms

### 2.1 Method

This review is based on a route, template, handler, service, and static-asset
walkthrough of the current working tree. Click counts are derived from the
actual links, redirects, and forms. No moderated usability session, clickstream
measurement, screen-reader audit, or visual browser pass was performed. Mobile
observations are inferred from the CSS and templates and should be confirmed in
a browser before being treated as visual defects.

### 2.2 Terms

- **Officer** means an authenticated `Admin` account. V1 defines no other access
  level (`internal/domain/identity.go`), so officer and administrator are the
  same person in this review.
- **Record** means one `GuildBankItems` row. Under the quantity model, one record
  represents one imported or manually entered item line, not one physical unit.
- **Item** means the thing an officer thinks about, normally identified by EQUID
  and name, across all of its records.
- **Public inventory** means the unauthenticated `/` and `/inventory/:id` views.
- **Admin inventory** means `/admin/inventory` and its detail/edit/remove pages.

### 2.3 Deliberate non-findings

- Public inventory omits recipient and exact source location. This is treated as
  an intentional privacy boundary, not a defect. Recommendations preserve it.
- Destructive actions use a confirmation page rather than a browser dialog. This
  is appropriate for the no-JavaScript design.
- The import review is JavaScript-dependent after upload. That is documented and
  was not treated as a V1 usability defect in this review.
- The owner-run consolidation command is intentionally outside the web UI. The
  review recommends **visibility**, not an automatic merge action.

---

## 3. Current daily journeys

The journeys below are the model used for the ranking. “Pages” counts navigations
or full form submissions, not individual keystrokes.

| Journey | Current path | What costs the officer time |
|---|---|---|
| Start the day | Login → Settings | The officer must then choose Admin inventory, Imports, or Audit from the header. Pending imports are not surfaced. |
| Answer “how many do we have?” | Inventory → search by name → page through matching records → add quantities mentally | One item can occupy many rows. Totals, stack count, and location summary are absent. |
| Give stock to a member | Admin inventory → search → open record → Edit → change quantity/recipient/date → Save → detail → Back → re-apply filters | No “give” task, no partial-quantity model, and the result page loses list context. |
| Receive new stock | Admin inventory → Add item → enter EQUID, fallback name, quantity, location, recipient, dates → Add → detail → Back → re-apply filters | No existing-stock context for the EQUID, no “save and add another”, and no member or location suggestions. |
| Move stock between bank locations | Edit each record separately | Bulk editing is limited to removal. Moving twenty stacks costs twenty edit/save cycles. |
| Remove stock | Row Remove link → confirmation → Remove → detail → Back → re-apply filters | Bulk removal exists, but it returns to an unfiltered list and has no select-all control. |
| Import the bank export | Login → Imports → upload → review detail | The upload is efficient, but default-selection copy, scope behavior, and count vocabulary create avoidable doubt. |
| Investigate a change | Audit or record history | Audit rows show raw event codes and JSON, and do not link to the inventory record. |

---

## 4. Findings

### 4.1 Navigation and orientation

**F-01 — Login and `/admin` land on Settings.**
`redirectAfterLogin` sends an HTML login to `/admin/settings`
(`internal/api/cookies.go`), and the `/admin` root redirects to the same page
(`internal/api/ui.go`). Settings is the least frequent daily destination and
includes database and session configuration.

**F-02 — The header has no current-section state and prioritizes Settings.**
The visible admin order is Settings, Admin inventory, Imports, Audit
(`internal/web/templates/partials.html`). No link carries `aria-current`. On
narrow screens the header becomes a column but the nav remains a single
non-wrapping flex row (`internal/web/static/app.css`), so six items can overflow.

**F-03 — There is no operational home.**
No screen answers “what needs attention today?”: pending import batches,
records opening this week, recently modified stock, missing metadata, or items
with multiple stacks. The officer must inspect each area independently.

**F-04 — Public detail has no officer shortcut.**
`/inventory/:id` shows only public fields and a static “Back to guild inventory”
link. The list has an Edit link for admins, but the detail page does not link to
the admin record or history. An officer who opens a public record to inspect it
must navigate back to the list to act.

**F-05 — Audit does not link back to the record.**
Audit filters are exact-match text fields for table and record ID, and rows render
raw JSON with no item link. The record detail has “Open the full history”, but the
reverse path is missing.

### 4.2 Inventory comprehension

**F-06 — The list is record-centric while the question is item-centric.**
The public list shows Name, Type, Class, Race, Qty, Availability, and Link
(`internal/web/templates/inventory_list.html`). Duplicate names appear as
separate rows with no item total. The admin list is also one row per record.
Under QTY-07/08/09 this is expected, but the UI does not provide the aggregate
view the model now requires.

**F-07 — Duplicate stacks are not identifiable as a group.**
There is no “same EQUID” view, stack count, or duplicate-stack indicator. An
officer cannot see that four records represent the same item without searching
and comparing manually. Consolidation is correctly owner-run, but its input is
invisible in the application.

**F-08 — The admin list hides availability and lifecycle dates.**
The admin table shows Qty, Location, Group, Recipient, Status, and Modified, but
not the availability state, `DateOpen`, or `DateBazaar`
(`internal/web/templates/admin_inventory.html`). The filters can sort by those
dates but cannot filter by availability or date window. An officer deciding what
to move to personas or bazaar must open records one at a time.

**F-09 — “Removed rows stay visible” contradicts the default.**
The admin page says removed rows stay visible, but the default status filter is
Active (`internal/service/admin_inventory.go`). Removed rows only appear after
the officer changes Status to Removed or All.

**F-10 — Availability labels have no legend.**
`Mains`, `Persona`, and `Bazaar` are displayed as badges on the public list and
detail, but no page explains the transitions or shows the dates together with the
label. The detail page shows both, but a member scanning the list has no key.

### 4.3 Workflow clarity

**F-11 — The import upload copy contradicts the review rules.**
The upload page says “Every accepted row starts selected”
(`internal/web/templates/imports.html`). The review page says container rows in
General, Bank, and SharedBank are not selected and Hoard is always selected
(`internal/web/templates/import_detail.html`). Both cannot be true.

**F-12 — “Accepted”, “reviewable”, “staged”, and “selected” are not
distinguished in import history.**
Recent batches show Source rows and Accepted rows only. A batch can report 197
accepted rows but expose 166 reviewable rows because `Unspecified` rows are
excluded from review. The officer has no on-screen explanation for the
difference, and the detail page’s “Accepted rows” figure is adjacent to
“Ignored rows” without a “Reviewable rows” figure.

**F-13 — Location checkboxes perform two unrelated actions.**
Checking a group includes its rows in scope **and** selects them; unchecking
hides the rows **and** clears their selection
(`internal/web/static/imports.js`). The help text documents this, but the control
looks like a display filter. An officer who unchecks a location to reduce visual
noise can silently discard a carefully built selection.

**F-14 — Import summary language is implementation-shaped.**
The summary presents eight cards including “Item rows / units”, “Spell rows /
qty”, “new / merged”, “Records to create”, and “Records to import”. The
distinction between the last two is not obvious, and a once-per-session import
does not benefit from that much internal vocabulary.

**F-15 — The `GivenTo` field has no workflow definition.**
The edit form exposes Recipient as free text, but no screen explains whether it
means allocated, reserved, already handed out, or a note. Setting it does not
change availability, and the public projection deliberately omits it. Partial
issuance is not representable: a row with quantity 10 and recipient “Marc” cannot
express “one of these ten went to Marc” without splitting the stack or changing
the quantity. This is the largest gap between the data model and a daily guild
workflow.

### 4.4 Efficiency and click cost

**F-16 — List context is lost on every drill-down.**
The admin detail “Back to inventory”, edit Cancel, and public detail “Back” links
are static paths. Bulk removal redirects to `/admin/inventory` with no filters.
Only the per-record detail pages retain the record ID itself.

**F-17 — Daily stock movements require the generic edit form.**
There is no Give, Receive, Adjust, or Move action. The officer must edit fields
that are normally immutable context (location, dates) and remember the current
quantity before changing it. “Add item” starts a fresh record every time, even
when adding to existing stock of the same EQUID.

**F-18 — Bulk actions are remove-only.**
The only bulk operation is page-local removal. There is no select-all checkbox,
no visible selected count, and no bulk location, recipient, or date update.
Selection is limited to the current page and disappears when the officer pages
or changes filters.

**F-19 — Common filters are mixed with rarely used exact identifiers.**
The admin filter grid always shows Search, ID, EQUID, Location group, Recipient,
Metadata, Item type, Status, Sort, and Page size. ID and EQUID are occasional
forensic filters; recipient is exact-match free text. The frequent filters (name,
availability, location, status) are not visually separated from the rare ones.

**F-20 — Recipient filtering requires exact spelling and case.**
`givenTo` is an exact SQL predicate under the column collation. A misspelled or
differently cased name silently returns no rows, and there is no known-recipient
selector or autocomplete.

**F-21 — Sorting requires a twelve-option dropdown and table headers are inert.**
The list offers twelve sort values in a select. Clicking a column header does not
sort, and the officer cannot see which sort is active without reading the
control. Sorting is preserved in pager URLs, which is good, but the interaction
is slower than a header click.

**F-22 — Import history is a fixed recent-50 table.**
`/admin/imports` always requests 50 batches with no search, filter, pagination,
or “resume review” affordance. File names are the only identifier besides date
and status. This is tolerable now but degrades as daily imports accumulate.

### 4.5 Forms and feedback

**F-23 — Add/edit has no stock context or assistance.**
The form asks for a raw EQUID, fallback name, source location, and recipient, but
does not show existing stock for the EQUID, suggest a known location, or suggest
a known recipient. Quantity is a text input with `inputmode=numeric`
(`internal/web/templates/admin_inventory_form.html`), with no increment/decrement
control and no visible current-stock reference.

**F-24 — Help text describes implementation, not consequence.**
Examples include “one record stores this quantity”, “items accept 1-100000;
spells merge by EQUID”, and the multi-sentence “Save and remove” explanation with
transaction and audit details. These are accurate but not written for an officer
deciding what will happen to the stock.

**F-25 — Success feedback is easy to miss or loses context.**
Notices appear on the record detail after create, update, remove, and restore.
Bulk removal returns to the list with a count, but the officer is no longer in
the filtered context where the action started. There is no persistent “last
action” indicator on the inventory list.

### 4.6 Import and audit readability

**F-26 — Import warnings are raw codes.**
Warning badges show codes such as warning-code strings joined by commas, with
details only in a `title` tooltip. There is no on-page legend or plain-language
summary, so an officer must know the import vocabulary or inspect a tooltip.

**F-27 — Audit values are raw JSON.**
The audit list and record history render Old and New as `<code>` JSON. This is
excellent evidence and should remain, but there is no human-readable summary of
which fields changed, no direct link to the affected item, and no “show raw JSON”
progressive disclosure.

---

## 5. Ranked recommendations

The ranking reflects **daily frequency × cost of the current workaround**. It is
not an estimate of implementation effort.

### 5.1 High impact

#### H-01 — Make `/admin` an officer work queue and fix the landing/navigation order

**Problem:** Login and `/admin` go to Settings; operational work has no home;
pending imports are invisible; the nav has no current state.

**Recommendation:** Land successful admin logins and `/admin` on Admin inventory
or, preferably, a compact officer home built from existing data: active records,
pending or failed import batches, records opening in the next seven days, recently
modified records, and metadata-missing records. Put each tile behind a link.
Order the header as Inventory, Admin inventory, Imports, Audit, Settings; mark the
current section; move Settings last and label it “Admin settings”. Allow the nav
to wrap on narrow screens. Show a pending-import badge when an uploaded batch has
not been committed.

**Evidence:** F-01, F-02, F-03, F-22.

**Payoff:** Removes two navigation steps from every session, prevents forgotten
review batches, and tells the officer what needs attention before they start
searching.

**Success signal:** After login, the officer can reach current stock in one step;
the header identifies the current section; a pending batch is visible without
opening Imports; the nav does not overflow at phone width.

#### H-02 — Add an item-centric stock view with totals and stack drill-down

**Problem:** The only inventory views are record lists. The one-row-per-line model
makes totals and stack relationships invisible, so “how many do we have?” requires
manual summation.

**Recommendation:** Make the primary inventory result one row per item (EQUID and
name), showing total quantity, active stack count, availability mix, and location
groups. Expand the row to the individual records with quantity, location,
recipient, and status. Keep a record-level view for editing and audit. In the
admin view, show a “multiple stacks” indicator and a same-EQUID filter; do not
merge records automatically. Preserve the public privacy boundary by showing
location groups but not exact source location or recipient.

**Evidence:** F-06, F-07, F-08; QTY-07/08/09 quantity model.

**Payoff:** Replaces unbounded row scanning with one answer. It also makes
duplicate stacks and distribution across bank tabs visible without a CLI.

**Success signal:** Searching a duplicated item returns one summary row with a
correct total; expanding it shows every contributing record; the officer can sort
or filter by total quantity and stack count.

#### H-03 — Put availability state and lifecycle dates into the admin list and filters

**Problem:** The admin list is where stock is managed, but availability badges and
dates are only on detail pages. Sort-by-date is available without the dates being
visible.

**Recommendation:** Add Availability, Opens, and Bazaar columns to the admin table,
and add availability/date-window filters alongside Status. Use the same
`Mains`/`Persona`/`Bazaar` vocabulary as the public view. Add quick filters such as
“Opening this week” and “Bazaar-ready now”. Keep the existing date sorts.

**Evidence:** F-08, F-10; admin table and filter template.

**Payoff:** The officer can triage what is becoming available without opening
records one by one, reducing the most common read-amplification loop.

**Success signal:** The officer can list every Persona-available record, or every
record opening in the next seven days, without exporting or opening details.

#### H-04 — Preserve list context through every action

**Problem:** Detail, edit, remove, and bulk removal lose filters, sort, and page.
The officer repeatedly re-enters the same search context.

**Recommendation:** Carry the current list URL (server-validated local path)
through detail, edit, remove, and restore. Label the return action “Back to
results” and restore the exact filter/page state. After bulk removal, return to
the same filtered page, adjusted for the removed rows. Apply the same rule to the
public detail page so a member returns to the search they came from.

**Evidence:** F-16; static back links in `inventory_detail.html`,
`admin_inventory_detail.html`, `admin_inventory_form.html`; bulk redirect in
`admin_pages_handlers.go`.

**Payoff:** Eliminates a filter re-entry cycle after almost every write. This is
the highest click-count reduction available without changing the inventory model.

**Success signal:** Edit a record opened from page 3 of a filtered list; saving and
returning restores page 3, the same filters, and the same sort.

#### H-05 — Define and expose the issue/receive/move workflow

**Problem:** The most frequent stock movements are expressed as a raw edit.
`GivenTo` has no stated meaning, does not change public availability, and cannot
represent a partial issue from a multi-unit stack.

**Recommendation:** First make an owner decision on `GivenTo`: is it an allocation,
a completed handout, or a note? Then expose task actions on the item and record
views: Give to member, Add stock, Adjust quantity, and Move location. Each action
should show current quantity and the resulting quantity before saving, use a
known-member/location suggestion, and default dates consistently. If partial
issuance must be supported, treat it as a deliberate inventory-model decision
rather than hiding it inside the edit form. At minimum, label the field as
“Allocated/Given to” and show “On hand” separately so an officer cannot assume it
removes stock from the public view.

**Evidence:** F-15, F-17, F-20, F-23, F-24; `GivenTo` is optional text in
`Design.md`, and the update form exposes it without workflow guidance.

**Payoff:** Reduces errors and page transitions, and removes the ambiguity that
currently makes the public availability of allocated stock questionable.

**Success signal:** Giving stock to a member is a named action with a clear
before/after quantity and an explicit effect on public availability. The officer
does not need to reason about transaction or audit implementation.

#### H-06 — Reconcile the import review mental model and count vocabulary

**Problem:** Upload copy contradicts default selection, location checkboxes
conflate scope with selection, and accepted/staged/reviewable/selected counts are
not distinguished.

**Recommendation:** State one rule on both import screens: “Container rows in
General, Bank and SharedBank start unselected; Hoard rows start selected;
non-bank-location rows are excluded from review.” Replace “Every accepted row
starts selected.” Split the location control into an explicit **Show/Hide** scope
control and a separate **Select/Clear** selection control, or make unchecking
scope-only and provide a distinct clear action. Add a count strip that always
shows Source, Accepted, Ignored, Excluded from review, Reviewable, Selected, and
Records to create. Label the recent-batch columns consistently with that strip.

**Evidence:** F-11, F-12, F-13, F-14; `imports.html`, `import_detail.html`,
`imports.js`, `ImportAdminImprovements.md` IMP-04/IMP-06.

**Payoff:** Prevents accidental selection loss and removes the need to reconcile
three different count stories before committing.

**Success signal:** The upload page and review page state the same default rule;
unchecking a location never changes the persisted selection; every count visible
on the page has a definition in one line.

### 5.2 Medium impact

#### M-01 — Add useful bulk operations and a visible selection summary

**Problem:** Bulk removal is the only bulk action; there is no select-all or
cross-page selection, and no bulk way to move location or change recipient/dates.

**Recommendation:** Keep removal all-or-nothing and page-local, but add a
select-all-on-page control, a persistent selected count, and bulk Update location,
Update recipient, and Adjust dates actions with a preview. If cross-page selection
is not offered, say so prominently and clear the selection when filters change.

**Evidence:** F-18; admin inventory bulk form.

**Payoff:** Moving a bank tab or reassigning a group of stacks becomes one
operation instead of one edit per record.

**Success signal:** The officer can move every selected stack from “Bank 2” to
“Bank 3” in one confirmed operation with an audit trail per record.

#### M-02 — Make add/edit context-aware and task-oriented

**Problem:** The form has no existing-stock context, no autocomplete, no
save-and-add-another, and generic quantity/date controls.

**Recommendation:** After an EQUID is entered, show existing active stock for that
EQUID: total quantity, stack count, locations, and availability. Offer known
recipients and location groups as suggestions while still allowing free text.
Add Save and add another for receiving several stacks. Use a numeric input with
increment/decrement controls and show the resulting quantity for an adjustment.
Move implementation details into expandable help.

**Evidence:** F-23, F-24; `admin_inventory_form.html`.

**Payoff:** Prevents accidental duplicate stacks and reduces repeated add cycles.

**Success signal:** The officer sees current stock before saving a new stack and
can receive five stacks without returning to the list between each one.

#### M-03 — Rationalize the admin filter and table interactions

**Problem:** Ten permanent filter controls, exact-match recipient input, a
12-option sort select, and no active-filter summary.

**Recommendation:** Separate common filters (search, availability, location,
status) from advanced identifiers (ID, EQUID, metadata, recipient). Show active
filters as removable chips. Replace exact recipient text with a known-recipient
selector plus an “any text” fallback. Make table headers sortable and show the
active sort. Correct the “removed rows stay visible” copy to match the default
Active filter.

**Evidence:** F-09, F-19, F-20, F-21.

**Payoff:** Fewer controls to scan, fewer empty-result typos, and faster sorting.

**Success signal:** The officer can clear one filter without resetting all; a
misspelled member name is impossible to submit as an exact filter.

#### M-04 — Improve public browsing without weakening privacy

**Problem:** The public list has no availability filter or label legend, and the
detail page loses the member’s search context and offers no officer shortcut.

**Recommendation:** Add an Availability filter and a one-line legend explaining
Mains → Persona → Bazaar. Preserve the return URL on detail. For an authenticated
admin, add a contextual “Open admin record” link on public detail. Keep recipient
and exact source location private.

**Evidence:** F-04, F-10, F-16.

**Payoff:** Members can find what they are eligible for, and officers can move
from a public view to action without detouring through the list.

**Success signal:** Filtering by Persona returns only currently available stock;
Back returns to the filtered page; an admin sees one clear path to the admin
record.

#### M-05 — Simplify the import summary and give the batch a clear lifecycle

**Problem:** Eight summary cards use implementation vocabulary, warnings are raw
codes, and pending batches have no persistent status outside the Imports page.

**Recommendation:** Reduce the summary to the decisions the officer makes:
records to create, item units, spells new/merged, rows with warnings, and rows
excluded. Put a short “What will happen” sentence above the commit button. Show
warning codes with a plain-language label and a details disclosure. Add a visible
batch state timeline (Uploaded → Reviewed → Committed/Failed) and a pending
badge in navigation (also part of H-01).

**Evidence:** F-14, F-26; `imports.js` summary card list.

**Payoff:** Makes a high-consequence commit understandable without knowing the
import pipeline.

**Success signal:** A first-time officer can explain what the commit will create
after reading one sentence; warning rows carry a readable reason.

#### M-06 — Make audit readable without discarding the raw evidence

**Problem:** Audit is raw codes, raw JSON, exact-match filters, and no links back
to the affected record.

**Recommendation:** Add a human-readable change summary beside each JSON value
(e.g. “Qty 4 → 3; Recipient — → Marc”), link `GuildBankItems` record IDs to the
admin record, and offer event-type and table selectors. Keep the raw JSON behind
a “Raw values” disclosure so the evidence remains available.

**Evidence:** F-05, F-27; audit templates and handlers.

**Payoff:** An officer can answer “who changed this and what changed?” without
reading JSON or copying IDs.

**Success signal:** Every audit row for an inventory change offers a direct path
to that record and a readable field-level summary.

### 5.3 Nice to have

#### N-01 — Give import history search, pagination, and resume affordances

Add filters for batch status, uploader, and date; paginate beyond the recent 50;
and label an uploaded-but-uncommitted row with a “Continue review” action. This is
most valuable once imports become a weekly or daily routine.

**Evidence:** F-22; fixed limit of 50 in the imports page handler.

#### N-02 — Remember views and support filtered export

Allow the officer to save a small set of named views (for example “Bazaar-ready”,
“Needs metadata”, “Bank 2”), restore the last filters, and export the current
filtered result. This reduces repeated filter setup and supports reconciliation
outside the application without adding new inventory semantics.

**Evidence:** F-19, F-21.

#### N-03 — Improve the upload affordance

Add drag-and-drop, display the selected file name and size before upload, and
reject obvious wrong extensions with a plain-language message. The current
file-picker is functional; this is polish rather than a blocker.

**Evidence:** `imports.html` file input and upload script.

#### N-04 — Finish keyboard and assistive-technology polish

Add a skip link, move focus to the notice after a post-redirect action, ensure
select-all controls announce their effect, and give the mobile nav a wrapping or
collapsible treatment. The existing table-scroll regions, captions, and
`aria-live` summaries are good foundations.

**Evidence:** header nav CSS and shared templates.

#### N-05 — Add a “current stock” reference to removal

On the remove confirmation page, show the item’s total active quantity and other
stacks of the same EQUID, not just the selected record. This helps the officer
avoid removing the last stack when another location was intended. This is a
read-only, low-risk addition to the existing confirmation page.

**Evidence:** `admin_remove.html` shows only the selected record.

---

## 6. What V1 already does well

These should be preserved while acting on the recommendations:

- **Destructive actions are deliberate.** Removal uses a confirmation page,
  version tokens, and an all-or-nothing bulk transaction.
- **Concurrent edits are protected.** `expectedModAt` prevents silent last-write
  wins, and conflict messages tell the officer to reload.
- **Import selection is auto-saved with rollback.** A failed save restores the
  last saved selection and reports the error rather than leaving a half-applied
  selection.
- **Empty and beyond-last states are explicit.** Both lists explain no-match and
  out-of-range pages and offer a recovery link.
- **Tables remain readable on narrow screens.** The table-scroll regions preserve
  columns instead of hiding them.
- **Audit evidence is complete and append-only.** Raw before/after JSON and
  correlation IDs are the right evidence layer; the recommendation is to add a
  readable layer, not to remove it.
- **The public API and HTML share contracts.** Filters and availability states do
  not diverge between the public JSON and rendered pages.

---

## 7. Decisions the owner should make before acting

These are product decisions inside V1, not new phases:

1. **What does `GivenTo` mean?** Allocation, completed handout, or annotation? The
   answer determines whether setting it should affect public availability and
   whether partial issuance needs to be represented.
2. **Should the primary inventory view be item-centric or record-centric?** An
   item-centric default answers the daily question; a record-centric default is
   better for forensic work. The recommendation is item summary first, records on
   expansion.
3. **Should merging duplicate stacks ever be available in the UI?** This review
   recommends visibility and drill-down only; merging should remain an explicit
   owner action unless the owner decides otherwise.
4. **How much import detail should be visible by default?** The recommendation is
   one plain-language “what will happen” statement with advanced counts and
   warnings behind disclosure.

---

## 8. Closing statement

No application source, template, stylesheet, route, migration, or runtime
behavior was changed for this review. The document is a usability assessment and
a ranked recommendation set only. None of the recommendations requires reopening
an earlier phase or defining a new one; each is an improvement to an existing V1
screen or workflow.