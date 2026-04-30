# Handoff Guide

## Purpose

This directory exists to transfer work between isolated Codex threads or agents without depending on full chat history. A handoff should carry only the minimum context needed to continue the task safely:

- current goal
- exact files to read
- current result status
- open risks or blockers
- single recommended next action

Avoid copying full conversations, full diffs, or long command logs into handoff files.

## Source Of Truth

For collaboration in this repository:

1. `AGENTS.md` defines the repository engineering baseline and non-negotiable architecture rules.
2. `.codex/state/current_task.md` is the canonical task snapshot.
3. `.codex/state/queue.md` tracks ownership, priority, and dependencies.
4. `.codex/state/results/` stores concise outputs from coder, tester, and reviewer threads.
5. `.codex/handoffs/` stores role-to-role transfer notes when a new thread needs explicit context.

If these files conflict with an old chat transcript, prefer the repository files and refresh them.

## Who Reads What

### Main / Orchestrator

Read:

- `AGENTS.md`
- `.codex/state/current_task.md`
- `.codex/state/queue.md`
- latest relevant files under `.codex/state/results/`
- any active handoff file needed for assignment

Write:

- `.codex/state/current_task.md`
- `.codex/state/queue.md`
- new handoff files when delegating

### Coder

Read:

- `AGENTS.md`
- `.codex/state/current_task.md`
- assigned handoff file
- only the code files listed as relevant

Write:

- code changes within assigned scope
- `.codex/state/results/<task-id>--coder.md`
- a new handoff file only if ownership is being transferred

### Tester

Read:

- `.codex/state/current_task.md`
- assigned handoff file
- `.codex/state/results/<task-id>--coder.md`
- only the files required for verification

Write:

- `.codex/state/results/<task-id>--tester.md`
- a new handoff file only if verification outcome must be transferred

### Reviewer

Read:

- `.codex/state/current_task.md`
- assigned handoff file
- `.codex/state/results/<task-id>--coder.md`
- `.codex/state/results/<task-id>--tester.md` if it exists
- only the files required for review

Write:

- `.codex/state/results/<task-id>--reviewer.md`
- a new handoff file only if follow-up ownership is being transferred

## Recommended Naming

- Handoff file: `.codex/handoffs/<task-id>--<from>-to-<to>.md`
- Result file: `.codex/state/results/<task-id>--<role>.md`

If the same role runs multiple rounds, append a short suffix such as `-v2` or a timestamp.

## Recommended Handoff Format

```md
# Handoff

- Task ID: `TASK-XXXX`
- From: `main`
- To: `coder`
- Date: `YYYY-MM-DD HH:MM TZ`
- Status: `ready | blocked | needs_followup`

## Summary

<2-5 lines describing the current state.>

## Read First

- `AGENTS.md`
- `.codex/state/current_task.md`
- `<relevant file>`
- `.codex/state/results/<task-id>--<role>.md`

## Constraints

- <scope boundary>
- <what not to change>

## Verification Status

- Completed: <item>
- Not run: <item>
- Failed: <item>

## Open Risks

- <risk or blocker>

## Next Action

<single next step for the receiver>
```

## Recommended Result Format

Use result files for durable outputs that other threads can consume without reopening old chat history.

~~~md
# Result

- Task ID: `TASK-XXXX`
- Role: `coder | tester | reviewer`
- Status: `done | partial | blocked | failed`

## Summary

<short outcome summary>

## Changed Files / Checked Files

- `<path>`

## Details

- <what changed, what was verified, or what was reviewed>

## Tests

- Ran: `<command>` -> `pass | fail | not_run`

## Minimal Logs

```text
<only essential tail output or error excerpt>
```

## Risks / Follow-ups

- <remaining risk or recommendation>
~~~

## Context Budget Rules

- Prefer summary plus exact file references over raw history.
- Quote only the minimal log tail needed to explain a failure.
- Do not paste large code blocks when a file path and line reference are enough.
- If a new thread needs more context, update `current_task.md` or add a handoff file instead of replaying the whole conversation.
