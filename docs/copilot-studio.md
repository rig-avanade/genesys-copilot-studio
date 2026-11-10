# Walkthrough 4: Connect and configure the Copilot Studio agent

This configures your **existing** agent; it does not create another chatbot in Go.
Use a development copy/version first so that you do not change a live agent's
authentication, knowledge, or topics accidentally.

**If the App Service guide sent you here:** complete steps 1-2, save the endpoint
and secret securely, and return there. Steps 3 onward require a healthy deployed
middleware and indexed test documents.

Portal labels can vary by tenant and authoring experience. If your environment
does not expose a described capability, confirm availability with its Power
Platform administrator rather than substituting an unrelated connection.

## 1. Prepare the customer-facing agent

1. Open [Copilot Studio](https://copilotstudio.microsoft.com).
2. Select the intended Power Platform **environment**, then open your agent.
3. Confirm this is the development agent/version you intend to change.
4. Open **Settings > Security > Authentication**.
5. Select **No authentication** and save. This applies to customers; it does not
   make the middleware's APIs or Azure resources anonymous.
6. Open the agent's **Knowledge** page. Identify the existing SharePoint source.
7. Remove/disable that native source from the development agent's customer-facing
   route. Do not retain it as a hidden fallback: native SharePoint retrieval may
   prompt for a user's Microsoft identity.
8. Have your administrator confirm that the environment's DLP policies and
   licensing allow a custom connector/agent flow to the middleware hostname.

The replacement path is approved SharePoint files -> Azure AI Search -> the
backend retrieval tool. Customers never need a SharePoint connection.

**Check before continuing:** the development agent uses No authentication and
does not rely on native SharePoint retrieval for this flow.

## 2. Obtain the secured Direct Line connection details

1. Open the agent's **Publish** page and publish the intended development version
   if it has not been published yet.
2. Open **Settings > Security > Web channel security**. Depending on the UI,
   web/custom-app connection details may also be reached from **Channels**.
3. Enable **Require secured access** for the channel and save, if it is not
   already enabled.
4. Copy one of the channel's secret values to an approved secret manager.
   This is the value for Key Vault secret `direct-line-secret`.
5. Find the agent/environment's Direct Line endpoint in its custom/web-app
   connection guidance. Save the **Direct Line v3 base URL** separately as
   `DIRECT_LINE_ENDPOINT`.

The global base URL is `https://directline.botframework.com/v3/directline`.
Use the correct regional endpoint when required for your environment. Do not
infer the region from the country in your email address or from a Genesys region.
A Power Platform **token endpoint**, a demo website URL, and a Direct Line
**base URL** are different things; the middleware requires the latter.

This middleware uses a web-channel secret server-to-server. It does not use a
Microsoft customer access token, delegated OAuth, or an Agents SDK connection.
If the secured channel/secret is unavailable in your tenant, stop and resolve
that connection model before attempting deployment.

**Check before continuing:** you have the secret securely stored and the correct
HTTPS base URL ending in `/v3/directline`. Never paste the secret into chat,
source files, browser JavaScript, or the Genesys incoming credential.

If following the App Service walkthrough, return to its
[Key Vault step](app-service.md#62-prepare-and-enter-the-secret-values) now.

## 3. Create the protected knowledge connector

The connector gives Power Platform a description of our `/knowledge/search`
API. Its connection stores the API key separately from the topic definition.

1. Confirm `https://YOUR-APP-HOSTNAME/healthz` returns `{"status":"ok"}` and the
   Search index contains your test document.
2. Open the Power Platform maker portal, such as
   [Power Apps](https://make.powerapps.com), in the **same environment** as the agent.
3. Open **Custom connectors**. If hidden from the main menu, use
   **More > Discover all** to find it.
4. Select **New custom connector > Import an OpenAPI file**.
5. Name it `CustomerApprovedKnowledge` and select this repository's
   [`api/knowledge.openapi.json`](../api/knowledge.openapi.json).
6. In **General**, replace `YOURAPP.azurewebsites.net` with the actual App Service
   hostname copied from Azure. Keep scheme **HTTPS** and base URL `/`.
   The Host field contains the hostname, not `https://` or `/knowledge/search`.
7. In **Security**, confirm authentication type **API Key**, parameter name
   `X-API-Key`, and parameter location **Header**.
8. In **Definition**, confirm operation `SearchApprovedKnowledge` is a POST to
   `/knowledge/search`, accepts a `query` string, and returns an array with
   `Content` and `Title` fields.
9. Select **Create connector** or **Update connector**, as appropriate.
10. Open the connector's **Test** page and select **New connection**.
11. Enter the raw value of `knowledge-api-key` from Key Vault. Do not prefix it
    with `Bearer`, and do not enter the Genesys or Direct Line secret.
12. Return to the test page, select that connection, enter a known question such
    as `When does the example support desk open?`, and run the operation.

Expect HTTP 200 and a result resembling:

```json
[
  {
    "Content": "The example support desk opens at 09:00 on weekdays.",
    "Title": "support-hours.pdf"
  }
]
```

An empty array means no matching indexed content. HTTP 401 means the connector
credential is wrong; HTTP 502 means the backend retrieval failed. These are not
interchangeable results.

**Check before continuing:** the connector retrieves the test excerpt using its
stored owner connection. No customer has supplied credentials.

## 4. Expose retrieval to the agent using an owner connection

Use the following agent-flow path when you need an explicit owner-authenticated
connection for an unauthenticated customer-facing agent. If you already have a
working direct connector tool with maker-provided authentication and a typed
table output, you may use that instead and skip JSON parsing in step 5.

1. In Copilot Studio, open **Tools** and choose to add a new **agent flow**.
   In environments exposing flow creation through Power Automate, create it in
   the same environment with trigger **When an agent calls the flow**.
2. On the trigger, add a **Text** input named `query`.
3. Add an action, find `CustomerApprovedKnowledge`, and select
   **SearchApprovedKnowledge**.
4. Choose the connection created in step 3. Its credential belongs to the owner,
   not the customer.
5. Set the action's `query` field to the trigger's `query` input.
6. Rename this action to `SearchApprovedKnowledge` so the expression below
   refers to the correct action name.
7. Add **Respond to the agent**, with a **Text** output named `resultsJson`.
8. Set that output using the expression:

```text
string(body('SearchApprovedKnowledge'))
```

9. Save/publish the flow and add it as a tool to the agent if it is not already
   attached. Give it a clear description: "Retrieve excerpts from customer-approved
   documents; use a self-contained question including relevant conversation context."
10. Test it with the same known question. Verify `resultsJson` contains a JSON
    array, not the entire HTTP response envelope or an extra layer of quoted JSON.

If the tenant requires end-user connections and will not permit this
owner-connection model, stop and ask the Power Platform administrator. Do not
work around DLP or switch the customer-facing agent to a shared employee login.

Let failed connector actions fail the flow. Do not configure an error branch
that returns `"[]"`, because that would disguise an outage as "no matching document."

**Check before continuing:** an agent flow/tool can return the excerpt without
a customer sign-in prompt.

## 5. Wire retrieval into the Q&A topic

Adding a tool does not automatically make every topic use it. Configure the
development agent's Q&A/Conversational boosting topic explicitly.

1. Open **Topics** and the topic that handles knowledge questions, commonly
   **Conversational boosting** under system topics. Confirm it actually handles
   your test question in the agent's test trace.
2. Add a tool/action node before answer generation and select the retrieval flow.
3. For the first explicit-question test, map its `query` input to
   `System.Activity.Text`. Save the returned text to `Topic.KnowledgeResultsJson`.
4. Add **Variable management > Parse value**. Parse
   `Topic.KnowledgeResultsJson` and save the result to `Topic.KnowledgeResults`.
5. Use the sample array from step 3 to infer a table with string fields
   `Content` and `Title`. If using a direct connector tool's typed table,
   save that table directly to `Topic.KnowledgeResults` instead.
6. Add a **Condition** that checks `IsEmpty(Topic.KnowledgeResults)`.
7. On the empty branch, add a message such as:
   "I couldn't find that information in the approved documents."
8. On the nonempty branch, add **Create generative answers**.
9. Set its question input to the user's question and open its data-source
   configuration. Choose **Custom data** and bind its search-results table to
   `Topic.KnowledgeResults`.
10. Disable other knowledge sources and general-model/web-search fallback for
    this route. Keep the existing native SharePoint source out of it.
11. Send the generated answer once. If the generative-answers node already sends
    it, do not add a second message node that repeats it. If generation returns
    no answer, send a clear no-answer message instead of completing an empty turn.
12. Add the completion event in step 6 after the final message in both branches.

The custom-data binding in topic code has this shape:

```yaml
customDataSource:
  searchResults: =Topic.KnowledgeResults
```

Do not replace an entire topic with that fragment. Use the node's UI/code editor
to set the binding inside the existing generative-answers action. If Custom data
is unavailable in the authoring experience, confirm the supported topic/tool
configuration for your tenant before publishing.

In the agent/topic instructions, require answers to use retrieved excerpts only,
admit insufficient evidence, and treat document instructions as untrusted data.
Source titles are provided; private SharePoint/blob URL metadata is not.

**Follow-up questions need an additional check:** `System.Activity.Text` is enough
for an explicit question, but "What about the second option?" is a poor standalone
search query. Before launch, configure the agent's query formulation/tool input
to include the relevant conversation context and verify it in the test trace.
Conversation persistence alone does not solve context-dependent retrieval.
Do not claim follow-up support merely because the same session ID is reused.

## 6. Send a completion event and handle failures

The middleware must know when the entire answer is finished. It waits for an
activity of type `event`, named `genesys.turn.complete`, not a pause in output.

1. In the Q&A topic, identify the final message in each successful/no-match path.
2. Add a send-activity **event** immediately after it. Use the authoring UI's
   event activity option where available, or its code editor to add this action
   at the same indentation as the other actions:

```yaml
- kind: SendActivity
  id: completeGenesysTurn
  activity:
    kind: EventActivityTemplate
    name: genesys.turn.complete
```

3. Use a unique action `id` for each additional copy in the topic, such as
   `completeGenesysNoMatch`.
4. Validate/save the topic. Do not send `genesys.turn.complete` as ordinary
   message text; it must be an event.
5. In the retrieval failure/system error path, emit an event named
   `genesys.turn.failed`, or let the turn fail explicitly. Do not send a normal
   completion event followed by a general-knowledge answer.
6. Keep the conversation open after each successful answer. Do not add an
   end-conversation action after every Q&A response.
7. Disable unsolicited Conversation Start greetings for this integration's
   development route, along with handoff/authentication/card-only paths that
   do not fit the v1 text Q&A contract.

**Check before continuing:** all Q&A paths have a final text reply and the proper
event; failure paths do not pretend to be a grounded answer. The current tenant's
authoring schema must accept the event action before you publish.

## 7. Publish and verify the actual channel

1. Test the known question in Copilot Studio's test pane. Inspect the trace:
   retrieval should execute, return approved text, and feed answer generation.
2. Test an unanswerable question. Confirm the agent admits that the approved
   documents do not provide the answer.
3. Test a retrieval failure and verify it is not treated as an empty result.
4. Publish the updated development agent.
5. Finish the [Genesys walkthrough](genesys.md#2-configure-the-endpoint-and-credential)
   and repeat the tests through the secured, published channel.
6. Verify that real Direct Line traffic contains the text reply followed by
   `type: event`, `name: genesys.turn.complete`. If the middleware times out,
   inspect the published topic/event contract as well as network latency.

The authoring test pane is not proof that the published Direct Line channel
works. If the event cannot be emitted in your tenant, stop and resolve a supported
completion contract; do not replace it with an arbitrary sleep.

No human-agent handoff or voice is enabled by these steps.
