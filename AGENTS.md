# Curu contributor guidance

- Follow the selected task and the accepted scope in `roadmap.md`. Preserve every roadmap ID during reconciliation. Do not expand into later tasks or use the old Curu prototype as an implementation source.
- `roadmap.md` is the initial product baseline. No full design exists yet: CURU-001 creates the product and architecture baseline documents, including `doc/implementation-design.md`. Do not treat that path as an existing input or pre-complete the task during setup.
- Product documents describe source and product contracts and the intended state after integration. Keep operational progress, review verdicts, pending integration and completion announcements in external records. Keep planning and readiness in `roadmap.md`; use delivery IDs in `capabilities.md` for provenance.
- For CURU-001, verify documentation and consistency against the accepted roadmap and dependencies. Establish exact executable build, test, start, migrate and bootstrap commands when implementation exists; do not invent or run Go or database checks against an absent application.
- Chris owns material product choices. Make routine documentation organization decisions without approval questions; leave unresolved JWT, authorization and template field policy for their scoped review tasks.
