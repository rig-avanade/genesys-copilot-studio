# Copilot Studio configuration

Use a development copy/version of the existing published agent first. Its
language support, licensing, environment policies, and external API permissions
remain tenant prerequisites. None of these settings are changed by compiling Go.

## 1. Keep customer sign-in separate from backend credentials

For this customer-approved corpus, the agent can use **No authentication** for
customers. Remove/disable its native SharePoint knowledge source for this flow:
that source would depend on the caller's Microsoft identity and SharePoint access.

The retrieval tool must use a **maker/owner-provided backend connection**, not an
end-user OAuth connection. Customers must not be prompted to connect to Azure or
SharePoint. Have the Power Platform admin confirm that this authentication model
and the middleware endpoint are allowed by the environment's DLP policies.

## 2. Establish the Direct Line connection

In the published agent's web/custom-app connection and web-channel security
settings, enable the secured Direct Line channel and obtain its web-channel
secret. Store that value as `DIRECT_LINE_SECRET` in Key Vault.

Set `DIRECT_LINE_ENDPOINT` to the correct regional Direct Line v3 base URL
ending in `/v3/directline`, as provided for the agent/environment. The public
global endpoint is `https://directline.botframework.com/v3/directline`; use the
appropriate regional endpoint where required. Do not substitute a Power
Platform token-issuance URL for the Direct Line base URL.

This implementation uses the web-channel secret server-to-server. It does not
implement delegated Microsoft user authentication, interactive OAuth cards,
an anonymous public token broker, or an Agents SDK connection. If your tenant
does not expose/allow this secured channel for the agent, stop and resolve the
connection model before deployment.

Validate that the secret/base URL pair can start a conversation with the correct
published agent. Publishing and web-channel access are distinct from a
successful conversation in Copilot Studio's authoring test pane.

## 3. Add the approved-knowledge retrieval tool

1. Import [`api/knowledge.openapi.json`](../api/knowledge.openapi.json) as a Power
   Platform custom connector in the agent's environment.
2. Replace `YOURAPP.azurewebsites.net` with the deployed middleware hostname.
3. In the connector's API-key security configuration, keep header `X-API-Key`.
   Create an owner connection containing `KNOWLEDGE_API_KEY`. Do not put that
   value into a topic, OpenAPI file, prompt, source-controlled solution, or user
   message.
4. Add `SearchApprovedKnowledge` as a tool/action that uses that owner connection.
   If your tenant's tool configuration cannot share a maker connection for an
   unauthenticated agent, use an agent flow with its owner's connector connection:
   accept a `query` text input, invoke the connector, and return the response as
   `resultsJson` text. Do not switch to end-user credentials as a workaround.
5. Test the connector: `{"query":"a known document question"}` should return an
   array such as `[{"Content":"approved excerpt","Title":"guide.pdf"}]`.
   Unauthorized credentials should produce 401; an unavailable Search backend
   should produce 502, not an empty array.

When using the agent-flow variant, return the connector array serialized as
JSON and parse it in the topic using **Parse value** into a table with text
fields `Content` and `Title`. Save the table as `Topic.KnowledgeResults`.
For a direct connector action, save its typed response array to that variable.
The tenant-specific action/flow/connection IDs are deliberately not hardcoded in
this repository.

## 4. Ground the answer, rather than just adding a tool

Configure the Q&A/Conversational boosting topic to:

1. Produce a self-contained retrieval question using the current question and
   relevant conversation context. Passing only "What about the second option?"
   will not make keyword search conversational.
2. Invoke the retrieval action with that question.
3. If the result table is empty, send a clear "I couldn't find that information
   in the approved documents" response.
4. Otherwise, add **Create generative answers**. Set its question input to the
   user's question and its custom data/search results to
   `Topic.KnowledgeResults`. Disable other knowledge sources and general-model/
   web-search fallback for this customer-facing flow.
5. Send the grounded answer exactly once. If using the node's automatic message
   output, do not add another message node that duplicates it.
   If generation produces no answer (including moderation or insufficient
   evidence), send a clear no-answer message rather than completing an empty turn.
6. Emit the completion event described below after the final message in **both**
   the answered and no-match branches.

In topic code, the supported custom-data binding is:

```yaml
customDataSource:
  searchResults: =Topic.KnowledgeResults
```

Use the authoring UI to configure the complete generative-answer node so that
its selected-knowledge settings match your tenant's current schema. A data
retrieval tool alone does not guarantee that the agent grounds answers in its
output; inspect the published behavior.

In the agent/topic instructions, require it to use retrieved excerpts only,
admit insufficient evidence, and treat document instructions as untrusted
content. Titles are returned for source attribution. Private SharePoint/blob
links are intentionally omitted because customers cannot open them.

On retrieval/tool failure, do not route into the empty-result or general-answer
branch. Configure the topic's error path to emit `genesys.turn.failed`, or let
the turn fail explicitly. The Genesys failure path can notify the customer.

## 5. Emit an explicit completion event

The middleware waits for an activity of type `event` with name
`genesys.turn.complete`. Add a send-activity event after the final reply in every
Q&A branch. The event template is:

```yaml
- kind: SendActivity
  id: completeGenesysTurn
  activity:
    kind: EventActivityTemplate
    name: genesys.turn.complete
```

For an explicit error event, use the same event template with
`name: genesys.turn.failed`. Do not send the event name as a chat message.
Validate the node in your current Copilot authoring schema and inspect the actual
Direct Line activities: `type` must be `event`, not `message`.

Do not end the entire conversation after each answer. The next Genesys turn must
reuse it. Disable unsolicited Conversation Start greetings for this integration,
and remove handoff, authentication, adaptive-card-only, and interactive question
paths from the v1 Q&A route. Every completed Q&A turn must send at least one text
reply and its completion event within the connector's time budget.

Publish the updated agent. Verify the event and response in a real secured
Direct Line conversation, not just the authoring test pane. If your tenant cannot
emit the required event, do not bypass the check with a sleep/quiet timeout;
resolve and verify a supported completion contract before going live.
