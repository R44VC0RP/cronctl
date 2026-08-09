# Interface Discovery

Two blind product-discovery rounds were run with General and Fable sub-agents.
Neither agent saw the implementation or README in round one. In round two, each
reviewed the current interface and the other agent's strongest expectations.

## Shared Expectations

- Root verbs should be guessable without docs: `add`, `list`, `show`, `rm`,
  `pause`, `resume`, `run`, `next`, `runs`, `logs`, and `why`.
- `--every` and `--cron` are easier to infer than an overloaded schedule flag.
- A successful mutation must report the next fire and scheduler health. Saving
  an unrunnable job silently is the scheduler's cardinal failure.
- Missed-run behavior after sleep or downtime must be explicit and observable.
- Direct argv execution, stable JSON, durable history, and idempotency are trust
  anchors for agents.
- Portable export/apply is more useful than workflows, plugins, or remote
  contexts at this stage.

## Decisions

- Keep root verbs. `daemon` and `service` remain the only noun groups.
- Preserve every original command and flag as a compatibility alias.
- Never install login persistence implicitly. `add` reports scheduler readiness
  and tells the caller how to fix it.
- Keep manual runs in the CLI process through the same executor and locks used
  by the daemon. IPC adds a protocol and security boundary without current value.
- Implement JSON export/apply without prune. Deletion ownership must be designed
  before a manifest is allowed to remove jobs.
- Implement `catchup=skip|once` before one-shot jobs because the old in-memory
  cursor made sleep and restart behave differently.

## Deferred Boundaries

One-shot schedules, retries, secrets, script bundling, queues, workflows,
remote contexts, event triggers, and distributed scheduling remain separate
design problems. They should not destabilize the cross-platform scheduler core.
