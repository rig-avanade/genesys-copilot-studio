# Azure and SharePoint setup

These are deployment steps, not resources already created by this repository.
They incur Azure charges. Choose names, subscription, region, retention, and
resource sizes with your administrator. Use a nonproduction environment first.

## 1. Prepare resources

In the Azure portal, create a resource group and the following resources in
compatible regions:

| Resource | Required configuration |
|---|---|
| Azure AI Search | Basic or a suitable paid tier; system-assigned managed identity; enable Entra/RBAC authentication |
| Storage account | General-purpose v2, TLS 1.2+, anonymous blob access disabled, blob soft delete enabled for 30 days, **blob versioning disabled** |
| Storage containers | Private `approved-knowledge` and `bot-sessions`, created before running commands |
| Container registry | Azure Container Registry for the application image |
| App Service plan/app | Linux, custom container, HTTPS only, Always On, system-assigned managed identity |
| Key Vault | Store the incoming Genesys key, retrieval key, and Copilot Direct Line secret |

Do not enable static website hosting or public containers. The Search data source
must use managed identity, not a storage connection string with an account key.
You can disable storage shared-key access once your operational tools use Entra.

Portal Search settings need to allow the indexer to reach Storage. Managed
identity grants authorization but does not bypass firewalls. If you require
private endpoints, VNet integration and Search private-link support must be
designed for your tier; the basic walkthrough does not provision that networking.

## 2. Grant least-privilege access

Assign roles at the narrowest applicable resource/container scope. Allow time
for role propagation.

| Identity | Roles/access |
|---|---|
| App Service middleware identity | `Storage Blob Data Contributor` on `bot-sessions`; `Search Index Data Reader` on the Search service; `Key Vault Secrets User` for required secrets; `AcrPull` on the registry |
| Search service identity | `Storage Blob Data Reader` on `approved-knowledge` |
| Ingestion identity | `Storage Blob Data Contributor` on both solution containers; `Search Service Contributor` to start the indexer; approved SharePoint read access |
| Setup operator/identity | `Search Service Contributor` for data-source/index/skillset/indexer configuration; resource creation/role-assignment permissions as required |

Use a **dedicated Search service** for this customer-approved corpus if a
service-scoped reader role would otherwise expose internal indexes. Do not give
the public middleware Graph/SharePoint access or ingestion credentials.

Keep the ingestion and setup identities separate from the public web app.
For automated execution in Azure, a managed identity is preferred. The commands
use `DefaultAzureCredential`: Azure managed identity in deployment, or an
authorized `az login`/application credential locally. Do not leave unrelated
`AZURE_CLIENT_SECRET` settings on an app that should use managed identity.

## 3. Approve a SharePoint folder and identity

Create a dedicated folder containing only customer-approved documents. Every
descendant is in scope. Folder membership is the approval boundary; the sync
does not interpret SharePoint approval columns, drafts, or individual ACLs.
Use a dedicated site if available to reduce the application's read scope.

For an unattended ingestion identity:

1. Have the Microsoft tenant administrator provision an app registration or
   managed identity for ingestion.
2. Grant the Microsoft Graph **application** permission `Sites.Selected` to that
   identity, with admin consent/app-role assignment.
3. Separately grant that application `read` access to the selected site.
   `Sites.Selected` alone does not grant access to any site.
4. For an app registration, configure a certificate/federated credential where
   possible. If a client secret is used initially, keep it in Key Vault or a
   secure local secret store; never commit it. The Azure Identity SDK supports
   the corresponding `AZURE_*` settings.

The site grant must be performed by an administrator with permission to manage
site application permissions, not by the restricted ingester itself. A Graph
request body for that administrative grant is:

```http
POST https://graph.microsoft.com/v1.0/sites/{site-id}/permissions
Content-Type: application/json

{
  "roles": ["read"],
  "grantedToIdentities": [
    {
      "application": {
        "id": "INGESTION-APPLICATION-CLIENT-ID",
        "displayName": "Customer knowledge ingester"
      }
    }
  ]
}
```

Find IDs with Microsoft Graph Explorer or an authorized Graph client:

```text
GET /v1.0/sites/{tenant}.sharepoint.com:/sites/{site-name}
GET /v1.0/sites/{site-id}/drives
GET /v1.0/drives/{drive-id}/root:/{approved-folder-path}
```

URL-encode path segments. Save the selected document-library `drive-id` and
folder item's `id`, not a sharing URL. Set `SHAREPOINT_HOST` to the exact tenant
SharePoint hostname (no scheme/path), `SHAREPOINT_DRIVE_ID`, and
`SHAREPOINT_FOLDER_ID`. Downloads follow Graph's preauthenticated redirect only
to that hostname and do not forward Graph credentials.

Manual testing can use an authorized operator identity if its Graph delegated
permissions and SharePoint access permit it. An Azure subscription login alone
does not guarantee Graph consent. Prove folder enumeration and file download
under the **actual ingestion identity** before relying on scheduled execution.

## 4. Configure Azure AI Search

Configure these environment variables from `.env.example`:

```text
SEARCH_ENDPOINT=https://YOURSEARCH.search.windows.net
SEARCH_INDEX=customer-knowledge
STORAGE_RESOURCE_ID=/subscriptions/SUBSCRIPTION/resourceGroups/GROUP/providers/Microsoft.Storage/storageAccounts/ACCOUNT
KNOWLEDGE_CONTAINER=approved-knowledge
```

After authenticating as the setup identity, run:

```sh
go run ./cmd/searchsetup
```

The command creates or updates four resources named from `SEARCH_INDEX`:

- The chunk index with `id`, `parent_id`, `content`, and `title_b64`.
- A managed-identity blob data source restricted to `approved/`, with native
  soft-delete detection.
- A built-in text-splitting skillset (2,000-character pages, 200-character overlap)
  projecting only child chunks into the index.
- An indexer scheduled every 15 minutes, with zero tolerated failed items.

This is lexical retrieval and built-in text splitting. No embedding model,
Azure OpenAI resource, semantic ranker, or paid OCR skill is configured.
`title_b64` preserves Unicode document filenames through ASCII blob metadata;
the middleware decodes it before returning titles to Copilot.

Use a new, dedicated resource name. Running `searchsetup` against an existing
deployment intentionally updates its configuration; incompatible index schema
changes can require a new index. Do not point it at unrelated Search resources.

## 5. Ingest and verify documents

As the ingestion identity, additionally set `STORAGE_ENDPOINT`,
`SESSION_CONTAINER`, and the three SharePoint settings, then run:

```sh
go run ./cmd/ingest
```

The command has a 30-minute overall deadline and supports up to 10,000 enumerated
folder/file items, with 16 MiB per file. It fails rather than silently skipping
unsupported or inaccessible content. Supported extensions are PDF, DOCX, PPTX,
XLSX, TXT, MD, HTML, and HTM. Scanned PDFs, password-protected documents, and
poorly extracted tables require separate remediation or a later OCR design.

Next, open Search **Indexers** and inspect the latest execution. Wait for
successful completion; inspect warnings as well as errors. Confirm that expected
files produced nonempty chunks in **Search explorer**. A sync success alone does
not prove indexing completed or every PDF contains extractable text.

Perform an update, rename, and deletion of a test document, rerun ingestion, wait
for indexing, and verify that stale excerpts disappear. Do this before importing
real customer-facing material.

Run ingestion manually for initial development. For regular refreshes, run the
same command in an approved job runner/container using the ingestion identity.
Choose the cadence with the document owner. The 15-minute Search indexer schedule
only sees blobs already synchronized from SharePoint.

Monitor failed syncs and indexer executions. Keep soft-delete retention longer
than the maximum unobserved indexing outage. Do not purge deleted blobs early.
If retention expires before deletion is indexed, rebuild the index from the
current approved corpus to remove orphaned content.

The `bot-sessions` container also holds the knowledge-source binding/lease in
`knowledge-sync.json`. A lifecycle rule may delete `sessions/` blobs after a
suitable period (for example 7 days); **do not** apply it to the binding file.
Use new containers/indexes rather than reusing a bound deployment for a different
source folder.

## 6. Build and deploy the web app

Build locally with `docker build`, or let your registry build the image:

```sh
az acr build --registry YOURREGISTRY --image genesys-copilot-studio:v1 .
```

Configure the App Service container to use that image. Give its managed identity
`AcrPull`, and enable managed-identity registry authentication in the app's
container settings. Do not enable the registry's admin account for convenience.

The image is non-root and contains no shell. Its default entry point is
`/middleware`; `/ingest` and `/searchsetup` are available for separately
credentialed operator/job containers. The public web app should not run them.

Configure port **8080** as the container target port. For classic App Service
custom-container hosting, set `WEBSITES_PORT=8080`; for sidecar-enabled hosting,
set the main container's target port in its configuration. Keep `PORT=8080`.

Set the server variables from `.env.example`. The web app does **not** need
SharePoint variables, `KNOWLEDGE_CONTAINER`, or `STORAGE_RESOURCE_ID`.

Create two different random API keys (at least 32 characters) using an approved
secret generator. Store these and the Copilot web-channel secret in Key Vault.
Set the corresponding App Service values to Key Vault references, for example:

```text
GENESYS_API_KEY=@Microsoft.KeyVault(SecretUri=https://YOURVAULT.vault.azure.net/secrets/genesys-api-key/)
KNOWLEDGE_API_KEY=@Microsoft.KeyVault(SecretUri=https://YOURVAULT.vault.azure.net/secrets/knowledge-api-key/)
DIRECT_LINE_SECRET=@Microsoft.KeyVault(SecretUri=https://YOURVAULT.vault.azure.net/secrets/direct-line-secret/)
```

Verify the references resolve rather than appearing as literal strings in the
running app. Configure coordinated key rotation with Genesys and the Power
Platform connector. v1 accepts one active key per endpoint.

Enable HTTPS-only, disable unnecessary publishing/basic-auth access, and use
App Service's health-check path `/healthz`. That endpoint is only liveness.
Route JSON container logs to your monitoring destination with appropriate access
and retention. Do not enable request-body/header capture.

Check `/healthz`, then configure Copilot and Genesys using the companion guides.
Do not expose a customer channel until the acceptance checklist passes.
