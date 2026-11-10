# Architecture and operating contract

## Agreed scope

The existing Copilot Studio agent remains the answer generator. The Go service
adapts Genesys Bot Connector messages to Direct Line v3. It does not host another
LLM or implement business topics. Customer authentication is disabled because
all indexed material is explicitly approved for every customer.

Automatic backend authentication is separate from customer sign-in. Managed
identity is preferred for Azure resources. SharePoint ingestion uses an
authorized backend identity or an authorized operator during manual setup.
No shared employee password or customer impersonation is used.

## A question and answer

1. Genesys posts its `IncomingMessageRequest` to `/genesys/utterance`.
2. The service authenticates Genesys, validates the configured integration/bot/
   version, and takes a distributed lease for that bot session.
3. It restores or creates the corresponding Direct Line conversation and
   persists a pending-turn marker **before** sending the user's message.
4. Copilot calls the protected retrieval API through an owner-authenticated tool.
5. Go queries Azure AI Search. Up to five 2,000-character chunks are returned as
   `Content` and `Title` records, without private SharePoint/blob URLs.
6. Copilot grounds its answer in those excerpts, sends the answer, then emits
   `genesys.turn.complete`.
7. Go polls using Direct Line's opaque watermark, collects all answer messages
   across pages, saves the new cursor, and returns Genesys `MoreData`.

The completion event is mandatory. Quiet-period heuristics and the activity ID
are not used as substitutes for completion or a polling watermark. Automatic
`DynamicPlanFinished` events are not sufficient for this repository's explicit
topic contract. Configure every customer-facing Q&A path accordingly.

## Session state

Session blobs contain a Direct Line conversation ID, watermark, pending marker,
and timestamp, not customer message text, Direct Line tokens, or cached answers.
Blob names and Direct Line user IDs are hashes of integration, bot, version,
Genesys conversation, and bot-session identifiers. Azure encrypts storage at rest.
Direct Line and Copilot retain conversation data under their own tenant policies.

Blob leases serialize turns across App Service instances and are renewed while
work runs. Concurrent turns for the same session fail explicitly instead of
interleaving. Unrelated sessions can proceed concurrently. After 24 hours of
inactivity, the next turn starts a new Copilot conversation.

The Genesys request contract has no guaranteed unique per-message delivery ID.
Exactly-once delivery therefore is **not** claimed. The middleware does not
deduplicate by text because repeating a question can be intentional. If sending
or receiving a turn has an uncertain outcome, the pending marker is retained,
and the bot session must be restarted rather than automatically reposting.
A lost HTTP response after a completed save can still lead an external retry to
send a duplicate question. Avoid automatic HTTP retries for utterance POSTs.

## Knowledge lifecycle

The ingestion command fully enumerates the configured folder recursively,
including Graph pagination, and copies files into the private `approved/` blob
prefix. Stable file IDs determine blob names; eTags avoid unchanged downloads.
Linked/remote items, unsupported formats, oversized files, malformed listings,
and download/upload failures stop synchronization explicitly.

Deletion runs only after a complete source listing and successful uploads.
Changing the configured source or destination is blocked by a stored binding;
use new containers and a new index for a different knowledge boundary. An empty
approved folder intentionally removes all previously synchronized documents.
Only one sync can run at a time for the configured control container.

Azure AI Search extracts text, splits it into overlapping chunks, and projects
them into the search index. Its native blob soft-delete policy processes removed
documents. This requires soft delete enabled **before the first indexing run**
and blob versioning disabled. Keep retention longer than the worst expected
indexer outage and monitor both sync failures and indexer failures/warnings.

Updates and removals are eventually consistent: SharePoint sync must run, then
Search indexing must finish. A successful `ingest` exit means synchronization
completed and Search accepted the run request, **not** that indexing finished.
Existing Copilot conversation context can retain previously retrieved material.
This is not suitable for immediate per-user access revocation.

## Failure handling and security boundaries

- Invalid HTTP input/authentication gets an explicit 4xx response.
- A valid Genesys request whose processing fails receives `botState: Failed`
  with `errorInfo` and no fabricated answer. Configure the Architect failure
  path to notify the customer; human transfer is not implemented.
- Retrieval failures return HTTP 502, not an empty success. The agent must not
  fall back to general model knowledge on retrieval failure.
- A no-match result is a valid empty array; the agent should say it cannot find
  the answer in approved documents.
- Logs include correlation IDs and sanitized errors, never prompts, document
  contents, credentials, full upstream URLs, or raw upstream response bodies.
- Transport credentials are not customer authentication. Keep the two endpoint
  keys separate and restrict public access as your networks permit.
- Only an approved folder belongs in the corpus. SharePoint ACLs are not
  propagated or enforced per customer. Document filenames are also
  customer-visible, so approve those too.
- Treat retrieved document text as data, not instructions. Grounding and
  prompt-injection behavior must be evaluated in the published agent.

## Capacity and future phases

The synchronous connector request must fit the actual Genesys HTTP timeout.
`TURN_TIMEOUT` defaults to 20 seconds (allowed range 1-45 seconds); cleanup can
take up to another 5 seconds. `botSessionTimeout` is **not** the HTTP timeout.
Measure end-to-end latency with real documents and your published agent. If the
Genesys limit is too short, a separately designed supported asynchronous
integration is needed; do not simply keep increasing timeouts.

v1 uses lexical BM25 retrieval, not semantic/vector retrieval. It does not promise
cross-language retrieval or paraphrase recall. Evaluate actual questions before
choosing a language analyzer, embeddings, or semantic ranking.

Phase 2 can map Copilot handoff events to a separately agreed Genesys routing
contract. v1 deliberately fails such events rather than pretending a transfer
succeeded. Future voice can add a speech adapter (potentially Azure Speech)
around the text conversation boundary. No audio endpoints, voice licensing,
telephony configuration, or STT/TTS are implemented now.

## Contract references

The old Genesys Power Virtual Agents blueprint is useful for the **Genesys**
configuration only; it explicitly says its Microsoft adapter is for classic PVA.
This implementation does not reuse that adapter.

- [Genesys Bot Connector blueprint](https://github.com/GenesysCloudBlueprints/bot-connector-for-ms-power-virtual-agent/blob/main/blueprint/index.md)
- [Genesys incoming contract](https://github.com/MyPureCloud/platform-client-sdk-dotnet/blob/master/build/docs/IncomingMessageRequest.md)
- [Genesys outgoing contract](https://github.com/MyPureCloud/platform-client-sdk-dotnet/blob/master/build/docs/OutgoingMessageRequest.md)
- [Microsoft Copilot Direct Line sample](https://github.com/microsoft/semantic-kernel/blob/main/python/samples/demos/copilot_studio_agent/src/direct_line_agent.py)
- [Microsoft custom grounding topic example](https://github.com/microsoft/contoso-real-estate-power-platform/blob/main/src/portal/solution/ContosoRealEstatePortal/src/botcomponents/contoso_3c237b1a-7213-4759-b062-c6294730ec77.topic.Search/data)
- [Azure Search projection examples](https://github.com/Azure-Samples/azure-search-rest-samples/tree/main/projections)

Provider documentation and authoring UI can change. Live acceptance in your
tenant remains required, including the completion event, indexing/deletion,
connection ownership, and Genesys timeout behavior.
