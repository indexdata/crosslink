# WolfCON 2026: The Next Generation of ReShare

Working content document. This is the narrative source for the eventual slide deck, not the deck itself.

## Working abstract

A walkthrough of the next generation of ReShare. We will look at the ILL platform's improved architecture and how it helps practitioners model ILL workflows while allowing service providers to deploy with simplicity and scale. We will review interoperability with third-party ILL systems and, if all goes well, demonstrate a working system. Finally, we will discuss the roadmap and migration plan for existing ReShare implementations.

## Core thesis

The next generation of ReShare keeps the community's workflow knowledge and commitment to interoperability, but moves them onto a smaller, clearer platform. Open protocols define the edges, a broker coordinates the work, and an explicit state model defines the workflow. The result is easier to operate, easier to understand, and easier to adapt without creating a separate implementation for every consortium.

Short version:

> Keep the service model. Make the workflow explicit. Simplify everything around it.

## Audience and intended outcome

The audience will include ILL practitioners, consortium leaders, implementers, service providers, and developers. The talk should give each group a useful answer:

- Practitioners: Can the system represent the way we actually work, including exceptions?
- Consortium leaders: Can policies and service models evolve without creating a permanent fork?
- Implementers: Are the integration points standard, observable, and testable?
- Service providers: Can this be deployed, upgraded, supported, and scaled economically?
- Existing ReShare users: Is there a credible path from today's system to the new one?

By the end, the audience should understand that this is not merely a backend rewrite. It is an architectural change that makes workflow a visible, testable part of the product.

## Tone and storytelling rules

- Begin with the problem practitioners and operators experience, not with Go or PostgreSQL.
- Treat legacy ReShare respectfully. It proved the service and accumulated valuable community knowledge; its success also exposed where the original technical assumptions no longer fit.
- Explain architecture through the journey of one request.
- Introduce implementation choices only after explaining the requirement each choice satisfies.
- Use concrete exceptions—invalid patron, conditional supply, cancellation, unfilled supplier, local supply—to prove flexibility.
- Avoid presenting configurability as unlimited scripting. The model is deliberately constrained by supported, validated capabilities.
- Be precise about what exists now, what is planned for the conference demo, and what belongs to the roadmap.

## Proposed narrative arc

The talk has five acts:

1. **Why change?** ReShare proved the value of community-owned ILL, but the legacy backend became disproportionately difficult to operate and evolve.
2. **What did the replacement need to do?** Preserve ReShare's service knowledge and integrations while reducing operational weight and making workflows explicit.
3. **How does the new system work?** Walk a request through the component architecture, from APIs and protocols to workflow, events, and persistence.
4. **Why is the state model the important change?** Show how states, actions, events, outcomes, and applicability rules describe real borrowing and lending workflows.
5. **How do we get there?** Demonstrate interoperability, show a working transaction, and give existing implementations a credible migration path.

## Slide-by-slide working outline

The outline assumes a 40–45 minute session plus questions. Timing and slide count should be revised when the conference slot is confirmed.

### 1. The next generation of ReShare

**Purpose:** Establish that this is a walkthrough of a working successor architecture, not a speculative redesign.

**On slide:**

- The next generation of ReShare
- Architecture, adaptable workflows, interoperability, and migration
- Presenter name, affiliation, WolfCON 2026

**Speaker direction:** Open with the user-facing promise: community-owned resource sharing should be able to evolve without the platform becoming harder to run or the workflow becoming harder to change.

### 2. From last year's broker to this year's platform

**Purpose:** Connect directly to the WolfCON 2025 presentation.

**On slide:** A simple “then → now” progression.

- 2025: a standards-based broker connecting ILL systems
- 2026: the broker also runs ReShare's own borrowing and lending workflows
- New since last year: Patron Requests API, NCIP integration, explicit state model, user/staff events, scheduling and batch actions

**Reuse:** The 2025 deck's broker and interoperability material becomes evidence for the platform story rather than the opening premise.

### 3. ReShare proved community-owned resource sharing works

**Purpose:** Begin from success and continuity.

**On slide:**

- Community-owned, ILS-neutral resource sharing
- Production workflows shaped by practitioners
- Integrations across diverse library systems
- Years of knowledge about the happy path—and all the exceptions

**Key line:** “We are replacing an implementation, not starting the product over.”

### 4. Why the legacy backend needed a successor

**Purpose:** Describe the accumulated mismatch between the platform and the job.

**Draft content—validate with the ReShare team before converting to slides:**

- The original backend combined a Grails/Groovy/GORM application stack, Kafka-based asynchronous processing, and a modular deployment model.
- Those choices supported rapid development and integration, but operating the complete environment meant many services and infrastructure dependencies.
- Core workflow behavior was spread across domain logic, status handlers, protocol handling, and asynchronous events.
- Changing a workflow often meant tracing and changing application code, then testing the effects across integrations.
- Maintaining the application framework, messaging infrastructure, and module lifecycle increased the cost of deployment, upgrades, troubleshooting, and local development.
- Vendor-specific protocol behavior accumulated alongside the core workflow and made boundaries harder to see.

**Evidence to visualize:** The legacy development tooling describes a full Okapi/FOLIO/ReShare environment of roughly 30 containers. The `mod-rs` documentation describes a Grails/GORM domain application whose application events are distributed over Kafka. These facts do not by themselves prove failure; they illustrate the operational shape we wanted to simplify.

**Avoid:** “The old system was bad.” Better: “The original choices got ReShare into production; operating experience gave us better requirements for the next generation.”

### 5. Requirements before technology

**Purpose:** Make the architecture feel inevitable rather than fashionable.

**On slide:** Two columns: “Preserve” and “Improve.”

**Preserve:**

- ILS/LSP neutrality
- ISO 18626 and NCIP interoperability
- Borrowing and lending workflows
- Consortial policies and supplier selection
- Multi-tenant operation
- Practitioner control over service behavior

**Improve:**

- A small deployment footprint with few moving parts
- Independent horizontal scaling and fast startup
- Explicit, inspectable workflow definitions
- Strong validation and automated testing
- Clear adapter boundaries for catalogs, directories, ILSs, and ILL peers
- Complete transaction and event visibility
- A staged migration path with continuity for existing users

**Key line:** “The technology stack was selected against these requirements.”

### 6. System at a glance

**Purpose:** Give the audience a mental model and reveal the full design once before walking through it.

**On slide:**

> A lightweight broker with open edges and a declarative workflow core.

**Primary visual:** `misc/crosslink-component-diagram.jpg`.

**Speaker direction:** Do not explain every box. Establish the layers by color:

- Blue: external systems
- Green: APIs and internal services
- Yellow: core workflow
- Purple: extensions and adapters
- Pink: background processing
- Orange: events
- Teal: persistence

**Key line:** “The diagram looks detailed, but there is one center of gravity: a request changes through a state model, and every meaningful change becomes an event.”

Internal requesters and suppliers use JSON/SSE APIs, external ILL systems use ISO 18626, ILS integration uses NCIP, and Directory and catalog services remain replaceable dependencies.

### 7. Step 1: A request enters through the appropriate edge

**Purpose:** Start the architecture walkthrough using a single request.

**Highlight on component diagram:** Staff UI → Patron Requests API → State Model; external ILL peer → ISO 18626 Handler → State Model.

**On slide:**

- ReShare UI and other internal clients use an OpenAPI-described JSON API.
- Existing ILL systems continue to exchange ISO 18626 messages.
- Both paths create a unified, observable transaction inside the broker.
- Server-Sent Events provide live updates to browser clients.

**Message:** Native ReShare requests and brokered third-party requests share infrastructure without pretending their interfaces are identical.

### 8. One request, two complementary views

**Purpose:** Explain how the platform serves practitioners without hiding the protocol and routing detail needed by implementers and support teams.

**On slide:** Two linked columns.

**Patron Request:**

- Practitioner-facing borrowing or lending lifecycle
- Current state and available actions
- Items, notifications, and attention flags
- Links to events and the underlying ILL transaction

**ILL Transaction:**

- ISO 18626 message exchange
- Supplier location and rota decisions
- Adapter calls and normalized protocol data
- Complete technical event history

**Key line:** “One service journey, linked at two levels of detail.”

Every native Patron Request is backed by an ILL transaction. The API keeps these views connected rather than collapsing practitioner workflow and protocol diagnostics into one overloaded object.

### 9. Step 2: The broker prepares and sources the request

**Purpose:** Explain service coordination around the workflow.

**Highlight:** Directory Client, Supplier Locator, Availability/Holdings Discovery, CQO layer, Catalog.

**On slide:**

- Resolve institutions and their policies through the Directory.
- Locate candidate suppliers using pluggable discovery.
- Rank/filter the rota according to consortium policy.
- Check real-time availability when the supplier supports it.
- Fail open on adapter errors rather than treating a temporary catalog failure as confirmed unavailability.

**Message:** Discovery is a capability behind an interface, not a permanent dependency on one catalog product.

### 10. Step 3: Contracts at the boundaries

**Purpose:** Show both the open standards and the implementation techniques that keep those boundaries precise and testable.

**Highlight:** ISO 18626 Handler and Client, Vendor Compatibility Layer, LMS Adapter, schema-driven protocol models.

**On slide:** Two columns: “Open interfaces” and “Keep them honest.”

**Open interfaces:**

- ISO 18626 for peer ILL messages
- NCIP for patron and circulation operations
- SRU/Z39.50 for discovery and availability
- OpenAPI JSON for internal clients and staff UI

**Keep them honest:**

- Schema-generated protocol models
- Generated API types and routing contracts
- Typed SQL access through `sqlc`
- Vendor compatibility shims
- `illmock` and integration suites exercising real boundaries

**Message:** Generated and tested contracts let the handwritten code concentrate on workflow decisions.

**Reuse/adapt:** 2025 slides 5, 6, and 8.

### 11. Step 4: A transition becomes durable work

**Purpose:** Explain exactly how event-driven behavior remains durable, observable, and safe across multiple service instances.

**Highlight:** State Model ↔ Event Bus ↔ PostgreSQL, plus Scheduler, Batch Actions, and Notifications.

**On slide:**

```text
User action or ISO message
          ↓
Durable event in PostgreSQL
          ↓  LISTEN / NOTIFY wakes workers
One worker claims and processes the event
          ↓
Result, next state, and history are recorded
```

- The durable row—not the notification—is the work.
- Work survives restarts and failed work remains visible.
- Multiple instances can safely compete for work.
- Retries, scheduled tasks, and batch actions use the same core patterns.

**Message:** The request is not a mutable black box. We can explain how it arrived at its current state.

**Implementation detail:** Event processing claims durable rows before work begins. Scheduled tasks use database locking to avoid duplicate execution and include recovery for work left running after interruption.

### 12. A small core supports a complete platform

**Purpose:** Translate the compact runtime into both operator benefits and visible practitioner functionality.

**On slide:** Two columns.

**Core runtime:**

- One Go service around shared durable state
- PostgreSQL as the primary operational dependency
- No Kafka cluster required for core broker messaging
- Database migrations are explicit and can run separately from the service
- Helm deployment and health/operations endpoints

**Built on the same core:**

- Scheduler and batch actions
- Request aging and retry workflows
- Notifications, templates, and email delivery
- Pull-slip generation and document delivery
- Live UI updates through Server-Sent Events
- Multi-tenant request ownership and API resolution

**Reuse/adapt:** 2025 slide 7.

**Proof points to refresh before the talk:** current image size, idle memory, startup time, number of containers in a minimal and production deployment, and a horizontal scaling demonstration or measured result.

### 13. Interoperability is part of the architecture

**Purpose:** Make clear that ReShare can participate in a mixed ecosystem.

**On slide:**

- Transparent broker mode when peers can track the selected supplier
- Opaque broker mode when the peer expects a conventional point-to-point partner
- Supplier changes, local supply, cancellation continuation, and condition negotiation mapped onto standard messages
- Directory data can be added to messages so every peer does not need a synchronized local directory
- Compatibility shims for real-world ISO 18626 differences

**Examples:** ReShare, Alma/Rapido, and ILLiad. Confirm the exact systems and production claims to name in 2026.

**Reuse/adapt:** 2025 slides 3, 5, and 6.

### 14. The central change: workflow is now a model

**Purpose:** Pivot from system architecture to practitioner control.

**On slide:**

> The workflow is no longer only an emergent property of application code. It has an explicit definition.

Show a small excerpt from `misc/state-models.yaml`, preferably one state with one manual action, one automatic action, one incoming event, and their transitions.

**Current implementation snapshot (August 2026):**

- One unified default model covers `Loan`, `Copy`, and `CopyOrLoan` requests.
- It defines separate requester and supplier views of the lifecycle.
- The model contains 42 state entries: 26 requester-side and 16 supplier-side.
- Applicability rules allow states and actions to differ by service type without duplicating the whole workflow.

The exact counts are useful as speaker evidence but may be too implementation-specific for the slide.

### 15. Anatomy of the state model

**Purpose:** Teach the vocabulary with a compact visualization.

**On slide:**

```text
current state
    + user/system action -- outcome --> next state
    + incoming message event ---------> next state
```

Then define:

- **State:** what is true now; also carries UI hints such as display name, editability, “needs attention,” and terminal status.
- **Action:** what staff or automation can do now.
- **Event:** what an external message tells us happened.
- **Outcome:** success, review, or failure can lead to different states.
- **Transition:** the permitted next state.
- **Applicability:** whether a state/action/event applies to Loan, Copy, or CopyOrLoan.

**Message:** The same model constrains backend behavior and tells the UI what is possible now.

### 16. A borrowing request, step by step

**Purpose:** Make the model concrete using the happy path plus meaningful branches.

**Suggested visual:** A simplified state path, not the complete 42-state graph.

```text
NEW
  -> VALIDATED
  -> METADATA_UPDATED
  -> READY_TO_SEND
  -> SENT
  -> SUPPLIER_LOCATED
  -> WILL_SUPPLY
  -> SHIPPED
  -> RECEIVED
  -> CHECKED_OUT
  -> CHECKED_IN
  -> SHIPPED_RETURNED
  -> COMPLETED
```

Add three side branches:

- `NEW -> INVALID_PATRON` when validation requires staff review
- `SUPPLIER_LOCATED -> CONDITION_PENDING` for negotiation
- supplier `UNFILLED` causes the broker to continue through the rota before the request becomes globally unfilled

For copy requests, branch from `SHIPPED` to a digital supply action and omit the physical return states. Verify final labels immediately before deck production.

### 17. From model to API to UI

**Purpose:** Prove that the state model is executable product behavior rather than documentation beside the code.

**On slide:**

```text
YAML model
    ↓ validated for Loan / Copy / CopyOrLoan
Action mapping for the request's current state
    ↓                         ↓
GET available actions        POST selected action
    ↓                         ↓
UI renders valid choices     Backend enforces the same rules
```

**Concrete example:** In `INVALID_PATRON`, the API can offer revalidate, skip validation, or close. It will not offer—or accept—an unrelated action such as check out.

The available-actions response includes action names, parameters, availability, and which action is primary. Automatic actions normally do not appear as manual choices; after a failure, the model can expose the action for staff intervention.

**Message:** The UI and backend do not maintain separate workflow rules.

### 18. Customizable with guardrails

**Purpose:** Explain what can change while answering “what stops a bad model from breaking live requests?”

**On slide:** Two columns.

**Shape the workflow:**

- Choose supported actions in each state
- Make actions automatic or manual
- Map success, review, and failure outcomes
- Map incoming protocol messages to transitions
- Apply states and actions by service type
- Mark primary, closing, editable, terminal, and attention behavior

**Enforce the contract:**

- Publish the broker's supported capabilities
- Reject unknown actions and message events
- Prevent requester/supplier crossovers
- Validate initial states, transitions, and closing paths
- Check service-type applicability and consistency
- Exercise action and message mappings in tests

**Message:** The model is configuration with a contract, not an unbounded rules engine.

**Concrete examples:** Require or skip patron validation, insert a metadata-review stop, make duplicate detection automatic, use different fulfillment actions for physical loans and digital copies, or attach notifications to significant workflow actions.

Model definitions are semantically versioned, generated, validated, and embedded. Administration and promotion of consortium-specific models can evolve without changing the execution engine itself.

### 19. Demo: follow one request across every boundary

**Purpose:** Prove that the diagram and model describe a running system.

**Preferred demo script:**

1. Create a borrowing request through the Patron Requests API or staff UI.
2. Watch automatic validation, metadata update, duplicate check, and send actions.
3. Show supplier location and availability decisions.
4. Show the outbound ISO 18626 request to an external or mocked peer.
5. Respond `Unfilled` from the first supplier and show the broker continue to another supplier.
6. Respond `WillSupply`/`Loaned` and show the requester state update live through SSE.
7. Perform a staff action such as receive or check out and show the NCIP exchange.
8. Open the event history to explain exactly why the request reached its current state.

**Fallback assets:** Pre-record the complete demo and retain a known-good request ID plus captured event log. A live demo can still be attempted first.

**Optional state-model proof:** Change one safe model behavior in a development build—for example, turn an automatic step into a manual review step—and replay the opening of the flow. Only include this if the rebuild/restart can be made crisp and honest.

### 20. Migration: continuity before cutover

**Purpose:** Give existing implementations a credible shape of migration without inventing commitments.

**Draft staged approach—requires product and implementation confirmation:**

1. **Inventory:** Map each consortium's institutions, symbols, policies, service levels, templates, integrations, and local workflow variations.
2. **Translate configuration:** Move directory, catalog, ILS/NCIP, peer, and policy settings to their new equivalents; identify gaps explicitly.
3. **Validate integrations:** Use `illmock` and vendor-specific test suites to prove ISO 18626, NCIP, discovery, and availability behavior.
4. **Parallel deployment:** Run the new service beside legacy ReShare, initially with non-production or selected traffic.
5. **Migrate workflow entry points:** Move staff UI/API traffic and external peer endpoints in a controlled cohort.
6. **Handle in-flight requests:** Choose and document whether existing requests drain in legacy, are migrated, or are handled through a bounded dual-system period.
7. **Preserve history:** Define the searchable/archive experience for completed legacy requests and audit data.
8. **Cut over and retire:** Complete reconciliation, switch remaining integrations, monitor, and retire legacy components only after acceptance.

**Key line:** “The migration unit should be a controlled cohort with observable acceptance criteria, not an all-at-once database replacement.”

### 21. Roadmap to production adoption

**Purpose:** Separate demonstrated capability from remaining productization.

Use a three-column “Now / Next / Later” slide once milestones are confirmed.

**Candidate topics to place, not yet commitments:**

- Staff UI integration and production workflow coverage
- Runtime distribution and governance of consortium-specific state models
- State-model versioning for in-flight requests
- Migration/export/import tooling and historical access
- Broader third-party conformance testing
- Deployment automation, metrics, dashboards, and operational runbooks
- Pilot consortium, phased production rollout, and legacy support window

Every roadmap item should have an owner or status before it reaches the deck.

### 22. Closing: a platform that can evolve with the service

**Purpose:** Return to practitioner and provider value.

**On slide:**

- Open at the edges
- Explicit at the center
- Small enough to operate
- Flexible enough to evolve
- A migration path that protects continuity

**Closing line:** “The next generation of ReShare is not just a smaller backend. It makes the community's ILL workflow a first-class, visible, evolvable part of the platform.”

## How to use the component diagram throughout the deck

Do not display the complete diagram on five consecutive slides without changing it. Build a progressive walkthrough from the same source image so the audience keeps its spatial orientation:

1. Show the full diagram and color legend.
2. Fade everything except request entry points.
3. Highlight directory, supplier location, and catalog/availability.
4. Highlight ISO 18626, NCIP, and compatibility adapters.
5. Highlight state model, event bus, scheduler, and PostgreSQL.
6. Return to the full diagram after the demo and trace the path the audience just saw.

For the eventual deck, redraw the diagram as editable vector shapes or create high-resolution crops/overlays. The current JPEG is adequate for planning but text may become difficult to read when projected.

## State-model visual candidates

We will probably need three different visuals rather than one complete state diagram:

1. **Grammar visual:** state + action/event + outcome = transition.
2. **Practitioner flow:** the simplified borrowing happy path with three exception branches.
3. **Configuration proof:** a small YAML excerpt next to the corresponding rendered state transition.

A complete 42-state graph is useful as an appendix or zoomable artifact, but it is unlikely to teach well on a single presentation slide.

## Claims and proof points to confirm

Before converting this document into slides, confirm or measure:

- Conference session length and desired depth for architecture versus migration.
- Exact title, presenter list, and whether “CrossLink” should appear in the title or primarily as the implementation name.
- The agreed product naming relationship: ReShare, next-generation ReShare, CrossLink Broker, and any UI name.
- Which legacy pain points the ReShare community is comfortable stating publicly.
- Current and legacy production topology: container/service count, CPU/memory profile, startup time, and deployment effort.
- Whether runtime-loaded or tenant-specific state models will exist by the conference date.
- How state-model versions will behave for in-flight requests.
- Which third-party systems have current production or test evidence that may be named.
- What can be shown in the live demo and what requires mocks.
- Pilot status, migration tooling status, earliest adopter cohort, and target dates.
- Treatment of in-flight requests and historical data during migration.
- Whether the roadmap includes non-returnables, digital delivery, DCB, or other service models beyond the default Loan/Copy workflow.

## Material reusable from WolfCON 2025

Source: `wolfcon/ILL-wolfcon-2025.pptx`.

- Slide 3: broker benefits—standards, Directory, pluggable discovery, external configuration.
- Slides 5–6: ISO 18626 in a broker, opaque/transparent modes, supplier identity, local supply, cancellation, and condition negotiation.
- Slide 7: Go, small deployment, schema-driven models, PostgreSQL event bus, and `sqlc`.
- Slide 8: integration testing, `illmock`, and vendor compatibility shims.
- Slide 9: the Patron Requests API and NCIP as the then-future bridge to the full platform.

Most of this should be shortened and reorganized around the request walkthrough. The 2026 talk should not repeat the 2025 deck in order.

## Source notes

Local implementation sources:

- `misc/crosslink-component-diagram.jpg`—primary architecture visual.
- `misc/crosslink-arch.png`—simplified system context visual.
- `broker/README.md`—broker responsibilities, APIs, interoperability modes, adapter behavior, and deployment configuration.
- `misc/state-models.yaml`—human-maintained state model, batch action defaults, and notification template defaults.
- `misc/state-model.json` and `broker/oapi/open-api.yaml`—state-model and API contracts.
- `broker/patron_request/service/statemodel.go`—model loading and validation.
- `broker/patron_request/service/statemodel_capabilities.go`—supported states, actions, parameters, and message events.
- `broker/patron_request/service/action.go` and `message-handler.go`—action and message execution.

External background sources used only to ground the legacy section:

- [Legacy `mod-rs` repository documentation](https://github.com/openlibraryenvironment/mod-rs)—describes the Grails/GORM domain model, Okapi environment, and Kafka event handling.
- [Legacy ReShare deployment tooling](https://github.com/openlibraryenvironment/reshare-tools)—describes a full Okapi/FOLIO/ReShare development deployment of roughly 30 containers.
- [The history of ReShare](https://projectreshare.org/the-history-of-reshare/)—production history, integrations, and previous architectural evolution.

## Next content pass

The next pass should resolve the claims-and-proof-points questions, then deepen four sections:

1. The legacy pain points with agreed examples and measured operational evidence.
2. A precise requirements-to-design mapping.
3. One state-model example chosen with an ILL practitioner.
4. A migration plan with named phases, responsibilities, acceptance criteria, and dates.
