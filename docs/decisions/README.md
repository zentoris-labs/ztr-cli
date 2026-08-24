# Decision records

Short, dated notes on decisions that shaped `zentoris` and are not obvious from the code
alone - mostly about the public surface (flags, environment variables, endpoints) where the
"why" matters as much as the "what". Newest concerns first; keep each entry to a page.

This directory is public like the rest of the repository - describe the CLI's own behavior
and the reasoning, never Zentoris internals.

| # | Decision |
|---|----------|
| [0003](0003-silent-token-refresh.md) | Silent token refresh with a per-account lock |
| [0002](0002-managed-accounts.md) | Managed accounts: switch, list, and an active default |
| [0001](0001-configuration-surface.md) | Configuration surface: one `--domain` knob, fixed identity constants |
