# Genesys Bot Connector setup

This guide covers the bot integration, not WhatsApp/channel provisioning.
Use an existing test message flow and a Genesys administrator with the required
integration, text-bot, and Architect permissions.

## Configure the integration

1. Open the existing **Genesys Bot Connector** integration under Admin >
   Integrations, or create a dedicated development integration.
2. Set **Bot Connector Handle Utterance URI** to:
   `https://YOURAPP.azurewebsites.net/genesys/utterance`.
3. In integration credentials, add:
   `Authorization` = `Bearer <GENESYS_API_KEY>`.
   Use the dedicated incoming key, **not** the Copilot Direct Line secret or
   retrieval API key.
4. Record the integration ID in `GENESYS_INTEGRATION_ID` on the App Service.
5. Activate the integration.

## Register the logical bot

Use the Genesys API Explorer or an authenticated API client for your Genesys
region:

```text
PUT https://{your-genesys-api-host}/api/v2/integrations/botconnector/{integration-id}/bots
```

The request body is [`examples/genesys-bots.json`](../examples/genesys-bots.json).
It registers `qa-assistant`, version `v1`, matching the default environment
example. These are logical connector identifiers; the configured Direct Line
secret selects the actual Copilot agent.

The PUT operation replaces the bot list for that integration. Use a dedicated
integration or preserve any existing entries; do not overwrite unrelated bots.
The example advertises English only. Change `supportedLanguages` only after
verifying your Copilot agent and retrieval corpus support the requested languages.

No handoff/completion intents are defined for v1. Successful Q&A turns return
`MoreData` to keep the conversation open.

## Configure Architect

In the existing test Inbound Message flow, use **Call Bot Connector** and select
the configured integration, logical bot, and version. Make sure a subsequent
message in the same bot exchange retains the Genesys `botSessionId` and
`conversationId`.

Configure the action's failure/timeout paths to send a neutral availability
message and end or restart the bot exchange as appropriate. Do not route to a
human queue in v1. Avoid an immediate retry loop, because the external request
may already have been accepted by Copilot.

Confirm the actual HTTP request/response timeout allowed for your integration.
Set `TURN_TIMEOUT` below that limit with margin for lease cleanup and network
transport. The default 20 seconds plus cleanup is only a starting configuration,
not a claim about your organization's Genesys timeout. Measure the complete
Copilot + retrieval + generation round trip.

## Wire contract

[`examples/genesys-request.json`](../examples/genesys-request.json) shows a text
request. Extra Genesys fields are accepted but not blindly forwarded to Copilot.
The authenticated body must match the configured integration, bot, and version.

A successful response resembles:

```json
{
  "botId": "qa-assistant",
  "botVersion": "v1",
  "botSessionId": "example-bot-session",
  "botState": "MoreData",
  "languageCode": "en-US",
  "replyMessages": [{"type": "Text", "text": "A grounded answer."}]
}
```

A valid request with a processing error gets `botState: Failed`, empty
`replyMessages`, and `errorInfo` with a diagnostic reference. Invalid
authentication/input is rejected with HTTP 401/400. No internal error body,
customer prompt, or credential is echoed.

Complete the acceptance checklist before attaching this integration to a live
customer channel.
