# Walkthrough 5: Genesys Bot Connector

This connects an existing Genesys test messaging flow to the middleware.
It does not set up WhatsApp, phone numbers, or other customer channels.

Use a dedicated development integration and flow. You need permissions to manage
integrations, the bot catalog, and Architect flows. If you can view an item but
cannot edit it, ask your Genesys administrator for the specific operation.

**If the App Service guide sent you here:** complete step 1 only, record the
integration ID, and return. Do not activate the integration or change a live flow.

## 1. Create an inactive integration

1. Sign in to your organization's Genesys Cloud region.
2. Open **Admin**.
3. Under **Integrations**, open **Integrations**.
4. Select **Add integration** or the catalog/install action.
5. Find **Genesys Bot Connector** and select **Install**.
6. Give it a clear name, such as `Copilot Q&A - Development`, and save.
7. Leave its **Status** as **Inactive**.
8. Record the integration ID shown in its details or URL. It is the integration's
   GUID, not the organization ID, Genesys conversation ID, or Copilot agent ID.

If you already have a dedicated unused development integration, you can reuse it.
Do not repurpose an existing integration that another flow depends on.

**Check before continuing:** the dedicated integration exists and is inactive.
Save its ID as `GENESYS_INTEGRATION_ID`.

If following the App Service guide, return to
[its app-settings step](app-service.md#8-add-app-service-environment-variables).

## 2. Configure the endpoint and credential

Before this step, confirm the App Service `/healthz` endpoint is healthy and the
Copilot retrieval/completion-event topic has been configured and published.

1. Reopen the dedicated integration under **Admin > Integrations**.
2. Open **Configuration > Properties**.
3. Find **Bot Connector Handle Utterance URI**.
4. Enter the actual middleware URL:

```text
https://YOUR-ACTUAL-APP-HOSTNAME/genesys/utterance
```

5. Open **Credentials**, then **Configure**.
6. Add a credential field with name **Authorization**.
7. Set its value to `Bearer ` followed by the value of Key Vault secret
   `genesys-api-key`. Include the space after `Bearer`, but not quotation marks.
8. Save the credentials and integration configuration.
9. In Azure App Service settings, verify `GENESYS_INTEGRATION_ID` matches this
   integration. Keep `GENESYS_BOT_ID=qa-assistant` and `GENESYS_BOT_VERSION=v1`
   for the example below.

Do not enter the Copilot Direct Line secret or `knowledge-api-key` here.
Those credentials protect different connections.

**Check before continuing:** the endpoint ends in `/genesys/utterance`, the
authorization field is saved, and the IDs match the web app.

## 3. Register the logical bot

Genesys needs a bot catalog entry so Architect can display the bot and version.
This does not create or publish a new Copilot Studio agent.

1. Open the [Genesys Developer Center](https://developer.genesys.cloud) and its
   **API Explorer**.
2. Select/authenticate to your organization's region. Do not use a region from
   an example if your organization is hosted elsewhere.
3. Find the operation for reading the integration's Bot Connector bots:

```text
GET /api/v2/integrations/botconnector/{integration-id}/bots
```

4. Supply the integration ID and execute the GET. If entries already exist,
   review them before making changes.
5. Find the corresponding update operation:

```text
PUT /api/v2/integrations/botconnector/{integration-id}/bots
```

6. Open [`examples/genesys-bots.json`](../examples/genesys-bots.json) from this
   repository and paste its JSON into the request body.
7. Confirm it registers bot `qa-assistant`, version `v1`, matching App Service.
8. Execute the PUT. A successful response is normally HTTP **204 No Content**.
9. Repeat the GET and verify the catalog now contains that bot and version.

**Important:** PUT replaces the catalog for this integration. On a shared
integration, preserve unrelated existing entries instead of overwriting them.
Using a dedicated integration avoids that risk.

The example advertises English (`en-us`) only. Add other languages only after
checking both the Copilot agent and retrieval behavior. Empty intents are
intentional for this continuing Q&A exchange; v1 does not define handoff intents.

If API Explorer returns 403, ask the Genesys administrator to confirm the
integration/text-bot permissions. Do not copy access tokens into repository files
as a workaround.

**Check before continuing:** the GET response shows the expected logical bot and
version. The configured Direct Line secret, not this logical name, selects the
actual Copilot Studio agent.

## 4. Connect an Architect development flow

1. Activate the dedicated integration after its endpoint and catalog are ready.
2. Open **Admin > Architect**.
3. Open an existing development **Inbound Message** flow, or create a separate
   test flow. Do not edit the production flow for the first test.
4. Check out/edit the flow if Architect requires it.
5. In the toolbox, open **Bot** and add a **Call Bot Connector** action at the
   point where the customer's text should be handled.
6. Select the development **Bot Integration**, then `qa-assistant` and `v1`
   in the available bot/version selectors.
7. If the bot is missing, verify integration status and the catalog GET from
   step 3 before repeatedly recreating the action.
8. Configure the action's failure/timeout paths. For v1, send a neutral response
   such as "Sorry, I couldn't complete that request. Please try again later,"
   then end the bot exchange as appropriate for the test flow.
9. Do not add a human-queue transfer or an immediate retry loop. Handoff is
   phase 2; an uncertain Copilot request may already have been accepted.
10. Validate the flow, resolve errors, save, and publish the development version.
11. Have the channel owner attach it only to an existing **test** messaging
    route. Channel provisioning itself is outside this walkthrough.

The bot returns `MoreData` after a successful answer, so the exchange can
continue. Do not exit and restart the bot action after every answer: subsequent
messages in the same exchange need the same `botSessionId` and `conversationId`.

**Check before continuing:** the action uses the correct integration/version,
and its failure path does not silently drop the interaction or transfer to a human.

## 5. Send the first test message

1. Open the existing test messaging channel connected to that development flow.
2. Ask the known question from the approved test document.
3. Confirm the answer reflects the document and no Microsoft sign-in prompt
   appears.
4. Ask a second explicit question, then a context-dependent follow-up.
5. Inspect the Genesys conversation/flow diagnostics and App Service logs if a
   turn fails. Use the middleware's diagnostic reference to correlate the failure;
   do not enable logging of API keys or full customer message bodies.
6. Test an unknown question and confirm a grounded no-answer response.
7. Simulate an unavailable backend in a controlled development test and confirm
   the Architect failure path is visible to the customer.

[`examples/genesys-request.json`](../examples/genesys-request.json) is a reference
for the incoming JSON shape, not a file you upload to Architect. A successful
middleware response contains `botState: MoreData` and text `replyMessages`.
A processing failure contains `botState: Failed`, empty messages, and `errorInfo`.
Bad credentials/input produce HTTP 401/400.

## 6. Check timing before going live

1. Ask your Genesys administrator to confirm the actual HTTP request/response
   timeout for this Bot Connector integration.
2. Compare it with measured elapsed time across Copilot generation and Search
   retrieval. Test representative documents and questions, not just greetings.
3. Set App Service's `TURN_TIMEOUT` below that Genesys limit, with margin for
   transport and up to 5 seconds of lease cleanup.
4. Repeat the test after changing the setting.

The default `20s` is an initial application setting, not a claim about Genesys'
allowed timeout. `botSessionTimeout` is a session-lifetime setting, not the HTTP
request timeout. If the synchronous path cannot fit, stop and revisit the
integration design; simply increasing the application timeout will not fix it.

| Symptom | First checks |
|---|---|
| Bot/version missing in Architect | Correct region, active integration, registered catalog, and user permissions |
| HTTP 401 | `Authorization` credential uses `Bearer ` plus the incoming Genesys key |
| HTTP 400 | Integration/bot/version IDs, text input, conversation/session identifiers, language |
| `TURN_FAILED` or no reply | Published Copilot connection, actual completion event, backend roles, and timing |
| First turn works but later turns fail | Reused session IDs, expired/uncertain turns, and whether the topic emits completion on every path |
| Follow-up retrieves unrelated content | Agent's standalone query formulation; session persistence is not query rewriting |

Finish the [acceptance checklist](acceptance.md) before attaching real customer
traffic. Voice and human-agent handoff remain disabled.
