# Acceptance before customer exposure

Local tests exercise HTTP contracts and failure behavior with fake upstream
services. They cannot validate tenant licensing, Copilot authoring/publishing,
Graph consent, cloud networking, real extraction quality, or Genesys timeouts.
Record the following results in your normal deployment/change process.

## Knowledge

- Under the ingestion identity, list and download from the approved folder.
  Verify access outside the granted site is denied.
- Ingest a known text document and representative PDF/Office files; wait for
  the indexer to finish. Inspect execution warnings and verify nonempty chunks.
- Ask questions whose answers are near the end of long files, not only the first
  page. Evaluate paraphrases and the actual customer languages.
- Modify, rename, move out, and delete a test document. Rerun sync and indexing;
  verify the removed/old content is no longer returned in fresh sessions.
- Introduce a failed listing/download and confirm no pruning happens.
- Confirm internal-only files, document URLs, and unapproved filenames are absent.
- Verify no-match, retrieval outage, and malicious instructions embedded in a
  document do not cause the agent to invent answers or bypass the grounding rule.

## Conversation

- Start a secured Direct Line conversation with the published agent and confirm
  its final text reply is followed by the actual `genesys.turn.complete` event.
- Through Genesys, ask a question and a context-dependent follow-up; confirm the
  same Copilot conversation is reused and retrieval receives enough context.
- Run two customer conversations and verify isolation.
- Restart the web app between turns; verify continuity through persisted state.
- Verify multi-message answers are returned completely, once, in order.
- Trigger a slow/failed Copilot response; verify a clear Architect failure path,
  no fabricated answer, and no automatic resend of an uncertain turn.
- Measure latency over representative questions and compare with the actual
  Genesys HTTP timeout. Do not launch if the synchronous path cannot fit.
- Confirm customers receive no Microsoft sign-in prompt, cards requiring login,
  or implied human-agent transfer.

## Deployment

- Confirm HTTPS enforcement, private blob containers, appropriate network rules,
  resolved Key Vault references, and least-privilege identities.
- Confirm the deployed middleware cannot read SharePoint directly or write the
  knowledge container.
- Inspect logs and monitoring: no message bodies, document contents, tokens,
  API keys, or preauthenticated download URLs.
- Choose ingestion cadence, monitor sync/indexer failures, and confirm retention
  is longer than potential deletion-processing outages.
- Test key rotation and rollback using a development deployment.
- Record explicitly that phase-2 handoff and future voice are not enabled.
