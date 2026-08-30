# WolfCON 2026: The Next Generation of ReShare

High-level narrative and background for the presentation. This document is not
kept in sync with individual slides. The authoritative slide source is
`WolfCON-2026-ReShare-first-pass.md`.

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
- `misc/crosslink-component-step-1-entry.png`—focused request-entry path.
- `misc/crosslink-component-step-2-sourcing.png`—focused preparation and sourcing path.
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
