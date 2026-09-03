---
title: "The Next Generation of ReShare"
subtitle: "Architecture, adaptable workflows, interoperability, and migration"
author: "Jakub Skoczen · Head of Engineering, Index Data · WOLFcon 2026"
---

# From CrossLink broker to CrossLink platform

| WOLFcon 2025 | WOLFcon 2026 |
|---|---|
| Standards-based ILL broker | Complete ILL workflow platform |
| Connect external peers | Native borrowing and lending |
| Route and observe transactions | Model and operate the lifecycle |

New: Patron Requests API · NCIP · explicit state model · live UI events · scheduling

::: notes
Last year we presented CrossLink as a standards-based broker. This year the broker has become the foundation for ReShare's own borrowing and lending workflows. The key addition is not simply another API; workflow itself is now explicit.
:::

# From a proven service to the next generation

:::::::::::::: {.columns style="height: 4.35in;"}
::: {.column width="47%"}
## What ReShare proved

- Community-owned resource sharing works
- ILS-neutral workflows can operate in production
- Practitioners can shape the service
- Open standards connect diverse systems
:::
::: {.column width="47%"}
## What needed to change

- Substantial application and persistence framework overhead
- Tenant topology multiplied operational work
- Kafka added infrastructure and failure windows
- Workflow behavior was spread across the application
- Changes required tracing, releases, and broad testing
:::
::::::::::::::

> We are replacing an implementation, not the product.

::: notes
Begin with success and continuity. ReShare established the community-owned service model and accumulated deep knowledge of both the happy path and its exceptions. The next generation preserves that product while replacing an implementation that had become costly to operate and difficult to evolve. The original architecture accelerated early delivery; production experience gave us clearer requirements for its successor.
:::

# Requirements before technology

:::::::::::::: {.columns}
::: {.column width="47%"}
## Preserve

- ILS/LSP neutrality
- First-class ISO 18626 and NCIP
- Borrowing and lending workflows
- Consortial routing and supplier selection
- Multi-tenant operation
:::
::: {.column width="47%"}
## Improve

- Smaller, more reliable operational footprint
- Explicit and adaptable workflows
- Observable and auditable requests
- One shared deployment for virtual tenants
- Support for native direct borrowing across the consortium
- A richer, OpenAPI-based Directory for networks, tiers, policies, and integrations (NCIP, Z39.50)
:::
::::::::::::::

::: notes
The requirements came before the technology choices. Preserve the service contract while making the platform easier to operate and evolve. The richer Directory carries network and tier membership, ILL policy, LMS and NCIP settings, catalog connections, and holdings policy through a regular OpenAPI service. Shared tenancy and centralized discovery create the foundation for native direct borrowing across the consortium.
:::

# A platform built around the transaction

![CrossLink component architecture](../misc/crosslink-component-diagram.jpg){width=58%}

::: {.api-observability-banner style="margin: 0.04in 0 0; padding: 0.05in 0.14in; border: 0.035in solid #b31782; border-radius: 0.14in; background: #f7edf4; text-align: center;"}
<p style="margin: 0; font-size: 14pt; line-height: 1.05; white-space: nowrap;"><strong>Open protocols at the edges · explicit workflow at the center · durable work underneath</strong></p>
:::

::: notes
Do not explain every box. Follow one request through the platform. Staff clients use OpenAPI JSON and receive live updates through Server-Sent Events. External ILL peers use ISO 18626; ILS integration uses NCIP; discovery uses SRU or Z39.50 behind replaceable adapters. At the center, requests progress through an explicit state model and meaningful changes become durable PostgreSQL events. Go, typed sqlc access, and a lightweight internal event bus keep the implementation small without giving up reliability.
:::

# One request, two complementary views

:::::::::::::: {.columns style="height: 4.35in;"}
::: {.column width="47%"}
## Patron Request

- The lifecycle practitioners operate
- Borrowing or lending perspective
- Current state and available actions
- Items, notifications, and attention flags
- Search, filtering, and batch work
:::
::: {.column width="47%"}
## ILL Transaction

- Supplier and rota decisions
- ISO 18626 message exchange
- Adapter and protocol activity
- Complete durable event history
:::
::::::::::::::

::: {.api-observability-banner}
**A hyperlinked API connects the request, transaction, protocol activity, and event history—from intent to outcome.**
:::

::: notes
Every native Patron Request is backed by an ILL transaction. Staff work with the lifecycle-oriented resource, while implementers and support teams can follow routing, protocol, and integration detail. The API links these complementary views rather than collapsing them into one overloaded object. This is complete observability without exposing technical complexity in the everyday staff workflow.
:::

# Workflow is now a YAML model

![State model grammar](wolfcon-2026-state-model-anatomy.svg){width=70%}

```{.yaml .compact-state-yaml}
- name: NEW
  side: REQUESTER
  initial: true
  primaryAction: validate-patron
  actions:
    - name: validate-patron
      trigger: auto
      transitions: {success: VALIDATED, review: INVALID_PATRON}
```

::: notes
A state says what is true now. An action says what a person or automation may do. An incoming protocol event says what happened elsewhere, and outcomes map to permitted next states. The YAML example makes that grammar concrete: a new requester-side request automatically validates the patron, then follows an explicit success or review path. One definition connects backend behavior, automation, and UI affordances.
:::

# A borrowing request, step by step

![Simplified borrowing workflow](wolfcon-2026-borrowing-flow.svg){width=80%}

> The happy path and every exception are explicit, inspectable, and testable.

::: notes
A state says what is true now. An action says what a person or automation may do. An incoming event says what happened elsewhere, and an outcome selects a permitted next state. The default model covers Loan, Copy, and CopyOrLoan from both borrower and supplier perspectives, with 42 workflow states and 32 distinct actions. The diagram shows that the common path remains understandable while invalid patrons, metadata review, conditional supply, and transaction routing are represented explicitly.
:::

# One workflow model drives the API and UI

![State model to API and UI](wolfcon-2026-model-api-ui.svg){width=96%}

::: notes
The API publishes the actions allowed in the current state, their parameters, availability, and which action is primary. The UI renders those choices, and the backend checks the same model when an action is submitted. A consortium can choose supported actions, make them manual or automatic, map outcomes and incoming messages, and vary behavior by service type. Generated validation rejects unknown actions, impossible transitions, requester/supplier crossovers, and invalid closing paths. Customization changes the workflow without creating a separate set of UI rules.
:::

# One deployment, many virtual tenants

![Shared multi-tenant runtime](wolfcon-2026-shared-runtime.svg){width=96%}

::: {.peer-network-banner}
**A shared deployment is an option, not a constraint**

Independent local brokers can still form a peer-to-peer ILL network
:::

::: notes
Tenant identity and ownership are part of each request, not the deployment topology. Institutions share the broker fleet, tables, migrations, and PostgreSQL deployment while authorization and owner-scoped operations preserve isolation. This removes the need to multiply application deployments as the consortium grows. The same architecture enables native direct borrowing: a shared union catalog, live availability, internal fulfillment, and local ILS integration can operate across the consortium. Where local control is preferred, independently operated brokers can connect through the same standards-based peer network.
:::

# Lightweight is measurable

| Measure | New broker | mod-rs 2.13 | Difference |
|---|---:|---:|---:|
| Container image | 35.6 MB | 246.0 MB | **6.9× smaller** |
| Slowest cold start—3 runs | 0.667 s | 37.006 s | **55× faster** |
| App working set (RSS) | 19.7 MiB | 1,509.2 MiB | **77× lower** |
| Core runtime pieces | 2 | 5 | **3 fewer** |
| Test-to-production LOC ratio | 1.71:1 | 0.16:1 | **10.9× higher** |

::: {.measurement-note}
Native ARM and a 2 GiB app limit. Legacy app tier includes Kafka + ZooKeeper; PostgreSQL and Okapi excluded.
:::

> A smaller operational footprint means simpler deployment, recovery, scaling, and upgrades.

::: notes
These are local comparative measurements, not production capacity figures. The cold-start row uses the slowest of three successful runs for both applications. The official mod-rs image is x86-only, so its unchanged platform-neutral JAR was run on native ARM Java 11. The app-working-set comparison includes the broker alone versus mod-rs, Kafka, and ZooKeeper; PostgreSQL and Okapi are excluded. The test-to-production ratio compares 34,695 test lines with 20,239 production lines in the broker, and 4,868 test lines with 31,000 production lines in mod-rs. Use the numbers as evidence for the operational design, not as a general language benchmark. Transition to the live demonstration after this slide: create a request, watch automatic preparation and sourcing, receive protocol updates, perform an NCIP action, and inspect the event history.
:::

# Adoption with a migration plan

![Controlled migration cutover](wolfcon-2026-migration-cutover.svg){width=92%}

> Rehearse with real data, prove readiness, and choose the cutover path that fits each consortium.

::: notes
Return from the demo to adoption. First rehearse with representative data in an environment isolated from live effects. Validate three things: data reconciliation, staff work such as actions, email and pull slips, and integrations such as NCIP, ISO 18626, and discovery. Then choose between two controlled paths. A consortium can transfer active requests during a bounded maintenance window, or leave existing requests in legacy ReShare while routing all new work to CrossLink. In either case, each request remains owned by one system and the change is made only after the evidence passes. Close on continuity: the next generation keeps the community's service model while making it easier to operate, understand, and evolve.
:::
