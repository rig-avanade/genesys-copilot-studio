# Walkthrough 2: SharePoint folder to Azure AI Search

Complete [Walkthrough 1](azure-setup.md) first. You should already have Storage
containers, the Search pipeline, and a working local `.env` file.

**Where:** SharePoint in your browser, Microsoft Entra/Azure portal, Microsoft
Graph Explorer, then your development terminal.

**What you will have at the end:** approved documents searchable in Azure AI
Search. Customers still do not need Microsoft sign-in.

## 1. Create a small approved test folder

1. Open the SharePoint site you intend to use.
2. Open its document library, commonly called **Documents**.
3. Select **New > Folder**, name it `CustomerApproved`, and create it.
4. Put one small, text-based PDF or DOCX into the folder. Use a harmless test
   document with an easily recognized sentence, for example:
   "The example support desk opens at 09:00 on weekdays."
5. Check that the document opens and its text is selectable/copyable. A scanned
   image of a page is not equivalent to extractable text; OCR is not included.
6. Record the site's hostname and path, for example
   `contoso.sharepoint.com` and `/sites/CustomerSupport`.

Everything inside this folder, including subfolders and filenames, must be
approved for all customers. The ingester does not interpret approval columns,
draft status, or user-specific SharePoint ACLs. Avoid links/shortcuts to other
sites or drives; those are not followed.

**Check before continuing:** the folder contains only approved test material,
not a copy of an entire internal document library.

## 2. Create a dedicated ingestion application

This application reads SharePoint and writes the private document copies.
It is separate from the public middleware. Creating it does not create another
Copilot agent and does not sign customers into Microsoft.

Ask your Microsoft tenant administrator to help with this section if you cannot
register applications or grant application permissions.

1. In Azure portal, open **Microsoft Entra ID**.
2. Select **App registrations > New registration**.
3. Enter a name such as `genesys-copilot-knowledge-ingester`.
4. Choose **Accounts in this organizational directory only (Single tenant)**.
5. Leave **Redirect URI** empty; this is a backend process, not an interactive
   customer sign-in app.
6. Select **Register**.
7. On the new app's **Overview**, record **Application (client) ID** and
   **Directory (tenant) ID**. These IDs are not secrets.
8. Open **API permissions > Add a permission > Microsoft Graph**.
9. Select **Application permissions**, search for `Sites.Selected`, select it,
   then select **Add permissions**.
10. Have an authorized tenant administrator select **Grant admin consent for
    your organization** and approve it.

Do not select delegated permissions for this unattended app. Do not add
`Sites.Read.All` or `Sites.FullControl.All` to the ingester just to bypass the next
step: `Sites.Selected` deliberately requires a separate selected-site grant.

**Check before continuing:** the app has Microsoft Graph application permission
`Sites.Selected`, and the consent status is granted. It still cannot read any
site until step 4.

## 3. Find the site, document-library, and folder IDs

Use [Microsoft Graph Explorer](https://developer.microsoft.com/graph/graph-explorer)
with an authorized administrator/operator account. This is a separate tool from
the Azure portal. Sign in to the correct Microsoft tenant.

Graph Explorer uses its own delegated permissions. Its default sign-in may not
have permission to perform these reads; if a request is denied, use its **Modify
permissions** panel and your organization's approval process. Do not confuse
these operator permissions with the ingester's application permissions.

1. Set the request method to **GET** and API version to **v1.0**.
2. Enter the following URL, replacing the hostname and site path, and select
   **Run query**:

```text
https://graph.microsoft.com/v1.0/sites/contoso.sharepoint.com:/sites/CustomerSupport
```

3. In the JSON response, copy the complete `id` value as `SITE_ID`. It can contain
   a hostname and comma-separated GUIDs; keep the whole value.
4. Run this request using that ID:

```text
https://graph.microsoft.com/v1.0/sites/SITE_ID/drives
```

5. In `value`, find the document library containing `CustomerApproved`. Copy that
   library's `id` as `SHAREPOINT_DRIVE_ID`. Do not choose a different library just
   because its display name is similar.
6. Run:

```text
https://graph.microsoft.com/v1.0/drives/DRIVE_ID/root:/CustomerApproved
```

7. Copy the returned folder's `id` as `SHAREPOINT_FOLDER_ID`. Confirm the response
   has a `folder` property.
8. Save `SHAREPOINT_HOST` as only the hostname, such as
   `contoso.sharepoint.com`, without `https://` or a path.

For a nested folder, use its path relative to the selected library's root.
URL-encode spaces as `%20`. A sharing link or browser address is not a folder ID.

**Check before continuing:** you have three distinct IDs: site, drive, and
folder. The runtime needs the drive/folder IDs; the site ID is used for the grant.

## 4. Grant the application read access to that site

An administrator with permission to manage site application permissions must
perform this grant. In Graph Explorer, this typically requires the administrator's
approved delegated `Sites.FullControl.All` permission. That elevated permission
belongs to the administrative operation, **not** to the ingester.

1. In Graph Explorer, change the method to **POST**.
2. Use:

```text
https://graph.microsoft.com/v1.0/sites/SITE_ID/permissions
```

3. In **Request body**, enter the following, replacing the application ID with
   the **Application (client) ID** from step 2:

```json
{
  "roles": ["read"],
  "grantedToIdentities": [
    {
      "application": {
        "id": "INGESTER-APPLICATION-CLIENT-ID",
        "displayName": "genesys-copilot-knowledge-ingester"
      }
    }
  ]
}
```

4. Run the request. Expect a successful creation response, normally HTTP 201.
5. Use **GET** on the same `/permissions` URL and confirm that this application
   has the `read` role.

This grants read access at the selected **site** boundary. Our configuration
restricts synchronization to the selected **folder**, but that folder restriction
is not a separate Graph permission. A dedicated approved-content site gives a
stronger permission boundary than a folder inside a broad internal site.

**Check before continuing:** the app has both admin consent for `Sites.Selected`
and the selected site's `read` grant. They are two separate requirements.

## 5. Give the ingester its Azure roles

Use the [IAM assignment pattern in Walkthrough 1](azure-setup.md#7-assign-the-initial-access-roles).
For this application, choose **User, group, or service principal** as the member
type and select the ingestion application. Confirm its application identity,
not a similarly named user.

Make these three assignments:

| Open this resource's IAM page | Role | Member |
|---|---|---|
| Storage > Containers > `approved-knowledge` | Storage Blob Data Contributor | Ingestion application |
| Storage > Containers > `bot-sessions` | Storage Blob Data Contributor | Ingestion application |
| Search service | Search Service Contributor | Ingestion application |

The session-container permission lets the ingester hold a synchronization lock
and record its source binding. The Search role lets it request an indexer run.
It does not need Search Index Data Contributor because the indexer writes the
documents.

**Check before continuing:** all three assignments appear at their intended
scopes. Allow several minutes for propagation.

## 6. Configure credentials for the first development run

For production automation, prefer an Azure-hosted managed identity or a
certificate/federated application credential. That job runner is not created by
this walkthrough. For a first manual development run, a short-lived client secret
is a straightforward option if organizational policy permits it.

1. Open the ingestion app registration.
2. Go to **Certificates & secrets > Client secrets > New client secret**.
3. Enter a description such as `local-ingestion-development` and choose the
   shortest practical expiry allowed by policy.
4. Select **Add**, then copy the secret's **Value** immediately to an approved
   secret manager. The **Secret ID** is not the credential.
5. Do not put this value in Git, a document, an issue, a screenshot, or chat.
   Track the expiry and remove the development secret when it is no longer needed.

If client secrets are prohibited, stop here and have the administrator configure
an approved Azure Identity SDK credential. Do not weaken the tenant policy.

## 7. Run synchronization

1. Open the repository's `.env` file.
2. Fill `SHAREPOINT_HOST`, `SHAREPOINT_DRIVE_ID`, and `SHAREPOINT_FOLDER_ID` using
   step 3. Keep the Azure settings from Walkthrough 1 unchanged.
3. In your Bash development terminal, reload the file:

```bash
set -a
. ./.env
set +a
```

4. Set the app's non-secret IDs and enter the secret at a hidden prompt:

```bash
export AZURE_TENANT_ID="YOUR-DIRECTORY-TENANT-ID"
export AZURE_CLIENT_ID="YOUR-INGESTER-APPLICATION-CLIENT-ID"
read -r -s -p "Ingestion client secret: " AZURE_CLIENT_SECRET
printf '\n'
export AZURE_CLIENT_SECRET
go run ./cmd/ingest
```

The secret is not echoed and is not placed literally into shell history.
These settings make the Go command use the ingestion app rather than your
earlier `az login` identity. Do not set them on the public App Service.

5. On success, expect a log containing **folder synchronized; indexer accepted,
   completion still pending**, with upload/unchanged/deletion counts.
6. After the run, clear these temporary credentials from the terminal:

```bash
unset AZURE_CLIENT_SECRET AZURE_CLIENT_ID AZURE_TENANT_ID
```

**Check before continuing:** Storage > `approved-knowledge` now contains files
under `approved/`. Hashed blob names are normal. A successful command does not
yet prove that Search has finished indexing.

## 8. Verify the Search results

1. Open the Search service in Azure portal.
2. Open **Indexers > customer-knowledge-indexer** and refresh the execution
   history. Wait for the latest run to finish.
3. Open its result/details. Inspect both errors and warnings; do not ignore
   empty extraction or unreadable-document warnings.
4. Open **Search explorer** and select `customer-knowledge`. If asked for an
   authentication method, use your Microsoft Entra/role-based identity.
5. If the explorer offers a JSON query view, enter the following and run it.
   Otherwise enter the query text `example support desk` in the search box:

```json
{
  "search": "example support desk",
  "select": "content,title_b64",
  "top": 5
}
```

6. Confirm `content` contains the distinctive sentence from your test file.
   `title_b64` is expected to look encoded; the middleware decodes it later.
7. Change the test file's opening time, rerun ingestion with the ingestion
   credentials, wait for indexing, and confirm the new content is returned.
8. Rename the file, repeat, then remove the test file from the approved folder
   and repeat. Confirm old content disappears after indexing.

Do not test deletion with your only copy of a real document. An empty approved
folder deliberately removes all previously synchronized documents.

**Check before continuing:** creation, update, rename, and removal all work.
Then put your approved test document back if you want to use it for the bot test.

## 9. Limits, refreshes, and troubleshooting

Supported files: PDF, DOCX, PPTX, XLSX, TXT, MD, HTML, and HTM, up to 16 MiB per
file. The run has a 30-minute deadline and a 10,000-item enumeration limit.
Linked items, unsupported files, unreadable content, and authentication failures
must be resolved; the command does not silently skip them.

| Symptom | Likely place to check |
|---|---|
| Graph HTTP 403 | Application permission type, admin consent, and selected-site read grant |
| Graph HTTP 404 | Correct drive/folder IDs and whether the identity can see that site |
| Storage HTTP 403 | Both container grants for the ingestion app; firewall rules |
| Search HTTP 403 | Search Service Contributor on the ingestion app and RBAC mode |
| Search HTTP 409 | An indexer run may already be active; inspect its status and retry after it finishes |
| Redirect hostname error | Use the actual tenant SharePoint hostname, not a sharing-link host |
| Uploads exist but results are empty | Inspect indexer history/warnings, extraction, and document text |
| Source/destination changed error | Use new containers/index for a new approved source; do not delete the binding to bypass the safeguard |

Rerun ingestion whenever you need to bring SharePoint changes into Storage.
The Search indexer's 15-minute schedule does not perform that first step.
Choose an approved scheduler and refresh cadence later, with failure monitoring.

Keep soft-delete retention longer than the worst expected indexing outage.
Updates/deletions are eventually consistent, and existing Copilot conversations
can retain previously retrieved content. If urgent removal is required, pause
customer exposure until removal and fresh-session behavior are verified.

The session container's `knowledge-sync.json` is the permanent source binding.
If you later add a storage lifecycle rule, target old `sessions/` blobs only,
not that binding file.

Continue with [Walkthrough 3: App Service deployment](app-service.md).
