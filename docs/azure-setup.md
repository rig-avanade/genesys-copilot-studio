# Walkthrough 1: Azure AI Search and document storage

Start here. This walkthrough creates the search service and storage that the
integration needs. You do not need to deploy the middleware yet.

**Where:** your browser, at [Azure portal](https://portal.azure.com), followed by
a short command in your development terminal. We are using the Azure portal,
not Terraform. These instructions do not create resources until you perform them.

**What you will have at the end:** a Search service with identity-based access,
two private storage containers, and an empty index ready for SharePoint documents.

## Before you start

You need an Azure subscription where you may create resources. To assign roles,
you also need a role such as **Role Based Access Control Administrator**, **User
Access Administrator**, or **Owner** at the appropriate scope. **Contributor**
alone generally cannot assign roles. If a button is disabled, ask your Azure
administrator to perform that step rather than granting yourself broader access.

Use a development resource group. Search, Storage, App Service, and Container
Registry can incur charges even when no customer is chatting. Review the price
shown by Azure before creating each paid resource.

The identity walkthrough assumes the Azure subscription and SharePoint site
belong to the same Microsoft Entra tenant. If they do not, resolve the cross-tenant
identity design with your administrators before following the application-grant
steps; an app registration in one tenant is not automatically usable in another.

This walkthrough uses public service endpoints protected by authentication.
That is not anonymous access. If your organization requires private endpoints,
stop and have its Azure administrator design the networking first; do not
disable an existing firewall just to make the walkthrough work.

Portal menu groupings can change. If a named page is not visible in the left
menu, use the resource menu's search box to find it by name.

## 1. Choose names and a region

Keep a private setup worksheet with the following **non-secret** values. The
examples are not reserved; change the suffix if Azure says a name is taken.

| Value | Example | Why you need it |
|---|---|---|
| Subscription | Your development subscription | Determines billing and resource ownership |
| Resource group | `rg-genesys-copilot-dev` | Groups this integration's Azure resources |
| Region | An organization-approved region | Keep Search, Storage, and App Service together where supported |
| Search service name | `srch-gc-dev-12345` | Becomes part of the Search endpoint |
| Storage account name | `stgcdev12345` | Lowercase letters/numbers only; globally unique |
| Knowledge container | `approved-knowledge` | Private copies of approved SharePoint files |
| Session container | `bot-sessions` | Conversation mappings and ingestion coordination |
| Search index name | `customer-knowledge` | The searchable collection inside the Search service |

An Azure **Search service** and a **search index** are different. First you
create the service in the portal; later our setup command creates the index
and its processing pipeline inside it.

## 2. Create the resource group

1. Sign in to [Azure portal](https://portal.azure.com).
2. Check the directory and subscription you are using. If necessary, use the
   directory/subscription controls in the portal header to select the correct
   organization.
3. In the top search bar, type **Resource groups** and open the service.
4. Select **Create**.
5. Select your subscription, enter `rg-genesys-copilot-dev` or your chosen name,
   and select your approved region.
6. Select **Review + create**, then **Create** after validation passes.
7. Open the new resource group.

**Check before continuing:** the resource group's Overview shows the expected
subscription and name. An empty resource list is normal at this point.

## 3. Create Azure AI Search

1. In the portal's top search bar, type **Azure AI Search** and open the service.
   If you reach Marketplace instead, select the Microsoft **Azure AI Search**
   offering.
2. Select **Create**.
3. On **Basics**, select the subscription and resource group from step 2.
4. Enter your unique **Service name**, for example `srch-gc-dev-12345`.
5. Select your approved **Location/Region**. If it is unavailable or at capacity,
   choose another organization-approved region that supports the required
   resources; do not select an arbitrary country to bypass residency requirements.
6. Under **Pricing tier**, select **Change pricing tier** or the tier selector.
7. Select **Basic**, review the displayed estimate, and select **Select/Apply**.
   Basic is the starting development recommendation, not a production sizing
   guarantee. Do not select Free for this walkthrough, which assumes a paid
   service with managed-identity indexing.
8. Leave optional semantic ranking, extra capacity, and unrelated AI features at
   their defaults unless your administrator requires them. This v1 does not
   require an Azure OpenAI model, embeddings, or a Foundry project.
9. If the wizard includes **Networking**, use the organization's approved public
   endpoint configuration for this development walkthrough. Private networking
   requires additional setup beyond these steps.
10. Select **Review + create**, review the subscription, region, tier, and cost,
    then select **Create**.
11. Wait for deployment to complete and select **Go to resource**.
12. On **Overview**, copy the service **URL/Endpoint**, for example:
    `https://srch-gc-dev-12345.search.windows.net`.
    Save it in your worksheet as `SEARCH_ENDPOINT`.

**Check before continuing:** the Search resource opens successfully, its pricing
tier is Basic, and you have copied the HTTPS endpoint. There are no documents
yet, and that is expected.

## 4. Give Search its own managed identity

A managed identity is an Azure-managed identity for a resource. Here, Search
will use it to read private document blobs without a storage password.
Enabling the identity does **not** automatically grant access to Storage.

1. Stay on the Search resource, not the resource group.
2. Find and open **Identity** in the left resource menu.
3. Select the **System assigned** tab.
4. Change **Status** to **On**.
5. Select **Save**, and confirm **Yes** if Azure asks to enable the identity.
6. Wait for the save to finish. Copy the displayed **Object (principal) ID**
   into your worksheet as the Search identity's principal ID.

Do not confuse this principal ID with a subscription ID, application/client ID,
Search endpoint, or resource ID. No secret needs to be generated for this identity.

**Check before continuing:** Identity shows **Status: On** and a principal ID.
In step 7, you will grant this identity access to one specific storage container.

## 5. Enable identity-based access to Search

This controls how our setup command and middleware access the Search service.
It is a different connection from Search reading Storage.

1. On the Search resource, open **Keys**. It is commonly grouped under
   **Security + networking**; use the menu search if necessary.
2. Find **API access control**.
3. Select **Role-based access control** for this new, dedicated service.
   This allows Microsoft Entra tokens rather than Search API keys.
4. Save/apply the change and confirm any prompt.
5. Reopen or refresh the page and verify the selected mode.

You may also see **API keys** and **Both**. **API keys** alone will not work with
our identity-based client. **Both** permits Entra access too, but keeps key access
enabled; it is useful only when deliberately retaining another key-based client.
Do not change authentication on a shared existing service without checking its
other consumers. This walkthrough assumes a new service dedicated to this bot.

**Check before continuing:** Entra/RBAC access is enabled. You do not need to
copy either Search admin key into `.env`, App Service, or Copilot Studio.

## 6. Create Storage and its private containers

### 6.1 Create the storage account

1. In the portal's top search bar, open **Storage accounts**, then select **Create**.
2. Select the same subscription and resource group.
3. Enter a unique account name such as `stgcdev12345`.
4. Select the same region as Search where possible.
5. If asked for **Primary service**, choose **Azure Blob Storage or Azure Data
   Lake Storage**. Select **Standard** performance and **Locally-redundant
   storage (LRS)** for this development setup.
6. On **Advanced**, keep secure transfer required and minimum TLS at **1.2** or
   higher. Leave **hierarchical namespace** disabled for this Blob Storage setup.
7. Disable **Allow enabling anonymous access on individual containers** or
   **Allow Blob anonymous access**, depending on the label shown.
8. On **Data protection**, enable **soft delete for blobs** and set retention to
   **30 days**. Leave **versioning for blobs** disabled.
9. Keep other recovery settings according to organizational policy. Do not
   enable an option that automatically turns blob versioning back on.
10. On **Networking**, choose the approved development public-endpoint setup.
    Search still needs both network reachability and permission to read blobs.
11. Select **Review + create**, then **Create**. After deployment, open the account.

**Why these deletion settings matter:** our indexer uses native blob soft-delete
detection. It must see deleted blobs before retention expires, and this policy
requires blob versioning to be disabled. Configure this before the first indexer
run, not after loading documents.

### 6.2 Check the settings and create containers

1. Open **Data management > Data protection** and confirm blob soft delete is
   enabled for 30 days and blob versioning is disabled.
2. Open **Security + networking > Configuration** and confirm anonymous blob
   access is disabled and secure transfer is required.
3. Open **Data storage > Containers**, then select **+ Container**.
4. Enter `approved-knowledge` and leave **Anonymous access level** set to
   **Private (no anonymous access)**. Select **Create**.
5. Repeat to create `bot-sessions`, also private.
6. Open **Endpoints** and copy the **Blob service** endpoint into your worksheet
   as `STORAGE_ENDPOINT`. It resembles `https://stgcdev12345.blob.core.windows.net`.
   Do not copy the File, Queue, Table, or static-website endpoint.
7. Open the account's **Properties**, or **Overview > JSON View**, and copy its
   full resource ID into your worksheet as `STORAGE_RESOURCE_ID`.

The resource ID is not a URL. Its shape is:

```text
/subscriptions/SUBSCRIPTION-ID/resourceGroups/RESOURCE-GROUP/providers/Microsoft.Storage/storageAccounts/ACCOUNT-NAME
```

**Check before continuing:** both containers exist and are private. You have
saved the Blob endpoint and account resource ID. Do not upload documents manually
yet; the ingester adds metadata that the Search pipeline needs.

## 7. Assign the initial access roles

Azure role assignment means choosing **where** access applies (scope), **what**
is permitted (role), and **who** receives it (member).

### 7.1 Let Search read the knowledge container

1. Open your Storage account, then **Containers > approved-knowledge**.
2. Open this container's **Access Control (IAM)** page. Check the breadcrumb:
   you should be inside the container, not at subscription scope.
3. Select **Add > Add role assignment**.
4. On **Role**, search for and select **Storage Blob Data Reader**, then **Next**.
5. On **Members**, choose **Managed identity**, then **Select members**.
6. Select your subscription, choose the managed-identity resource type for
   **Search service**, and select your Search service.
7. Verify the selected identity matches the Search principal ID recorded earlier.
   Select **Select**, then **Review + assign** and confirm the assignment.
8. Open **Role assignments** and confirm the Search identity has the Reader role
   on this container.

If Search is not offered in the resource-type picker, ask your administrator to
assign that recorded principal ID at the container scope. Do not select your
personal account instead; the indexer runs as Search, not as you.

### 7.2 Let your setup account create the index and inspect results

1. Open the **Search service > Access Control (IAM)**.
2. Select **Add > Add role assignment**.
3. Select **Search Service Contributor**, then **Next**.
4. Choose **User, group, or service principal**, select your setup user's account,
   then **Review + assign**.
5. Repeat the process to assign **Search Index Data Reader** to that same user.
   This second role permits reading documents in Search explorer.

An Azure **Owner** or **Contributor** management role does not automatically mean
you can query Search documents. These data/service roles are deliberate.
Allow several minutes for assignments to propagate before investigating a 403.

The ingestion app and public middleware receive their own narrower grants in
the next walkthroughs. Do not give the middleware SharePoint access.

### 7.3 Let your operator inspect the uploaded test files

1. Open Storage > Containers > `approved-knowledge` > **Access Control (IAM)**.
2. Select **Add > Add role assignment** and choose **Storage Blob Data Reader**.
3. Choose **User, group, or service principal**, select the operator who will
   inspect the test files, and confirm **Review + assign**.
4. When opening the container's browser, select **Microsoft Entra user account**
   authentication if the portal offers an authentication-method switch.

This is for the later visual ingestion check. An account's ability to manage the
Storage resource is not the same as permission to read blob contents. The
operator does not need access to the private session container for this check.

**Check before continuing:** Search can read `approved-knowledge`, and your setup
user has both Search roles and can inspect the approved document blobs. No
storage account key or Search API key was shared.

## 8. Prepare your development terminal

The portal creates the Azure resources. Our small Go command creates the
project-specific index, data source, skillset, and indexer with consistent fields.
You do not need to reproduce those JSON definitions by hand in the portal.

1. Open a terminal in your checkout of this repository, on the branch containing
   the implementation. You should see `go.mod`, `.env.example`, and `cmd/`.
   A GitHub Codespace or a local editor terminal is suitable.
2. Use **Bash** for the commands below (Linux/macOS, WSL, or a Bash-capable
   development environment). These are not PowerShell commands.
3. Run `go version`. It must be Go 1.26 or newer. If missing, follow the
   [Go installation instructions](https://go.dev/doc/install).
4. Run `az version`. If Azure CLI is missing, follow
   [Install Azure CLI](https://learn.microsoft.com/cli/azure/install-azure-cli).
5. Run the following, replacing the subscription ID with your worksheet value:

```bash
az login
az account set --subscription "YOUR-SUBSCRIPTION-ID"
az account show --query "{subscription:name,subscriptionId:id,tenantId:tenantId}" --output table
```

If a remote terminal cannot open a browser, use `az login --use-device-code`
instead and follow its instructions on Microsoft's sign-in page. Do not share
the device code. If your organization blocks that login method, ask its admin
for an approved developer login.

**Check before continuing:** the displayed account is in the intended
subscription and tenant. In this terminal, do not set ingestion-app
`AZURE_CLIENT_*` credentials yet; `searchsetup` should use your setup identity.

## 9. Create the Search index and processing pipeline

1. In the repository root, copy `.env.example` to `.env` if `.env` does not
   already exist. Do not overwrite an existing file containing configuration.
2. Open `.env` in your editor. Fill the following values from your worksheet:

| Setting | Enter |
|---|---|
| `SEARCH_ENDPOINT` | Your Search HTTPS endpoint |
| `SEARCH_INDEX` | `customer-knowledge` |
| `STORAGE_RESOURCE_ID` | The full Storage account resource ID |
| `KNOWLEDGE_CONTAINER` | `approved-knowledge` |
| `STORAGE_ENDPOINT` | Your Blob service endpoint |
| `SESSION_CONTAINER` | `bot-sessions` |

3. Save `.env`. Leave the Genesys/Copilot secrets empty for now; `searchsetup`
   does not need them.
4. Export the file's settings into your Bash terminal and run the command:

```bash
set -a
. ./.env
set +a
go run ./cmd/searchsetup
```

Only source an environment file you created and trust. `.env` is ignored by Git
and Docker; that is not permission to put its contents into chat or screenshots.

The first run may download Go dependencies. A successful command prints a
message containing **search resources configured**. It creates these resources:

| Portal section | Name | Purpose |
|---|---|---|
| Indexes | `customer-knowledge` | Stores searchable text chunks |
| Data sources | `customer-knowledge-source` | Reads private blobs under `approved/` |
| Skillsets | `customer-knowledge-split` | Splits text into 2,000-character chunks with 200-character overlap |
| Indexers | `customer-knowledge-indexer` | Extracts files and updates/deletes chunks |

5. Return to the Search resource in the portal. Open each of those sections and
   verify its named resource exists.
6. Open **Indexes > customer-knowledge** and inspect the fields. Expect `id`,
   `parent_id`, `content`, and `title_b64`.
7. Open **Indexers > customer-knowledge-indexer** and inspect its execution
   history. With no documents uploaded, zero indexed documents is normal.

`title_b64` is an encoded filename used to safely preserve Unicode metadata.
The middleware converts it back to a normal title. The indexer runs every
15 minutes, but that schedule only reads Storage; it does not fetch SharePoint.

**Check before continuing:** all four resources exist. The index can be empty,
but authorization or data-source errors must be resolved.

## 10. Common problems

| What you see | Check this first |
|---|---|
| Create or Add role assignment is disabled | Confirm directory/subscription and ask an administrator for the specific operation |
| `search returned HTTP 403` | Confirm RBAC is enabled, your current login has Search Service Contributor, and role propagation has finished |
| Search explorer cannot query | Assign Search Index Data Reader to the signed-in user and choose Entra/role-based authentication if prompted |
| Indexer cannot read Storage | Check Search identity is on, its Reader role is on the correct container, and Storage networking permits access |
| A command says an environment variable is required | Save `.env`, source it again in the same terminal, and check the exact setting name |
| Credential acquisition fails | Run `az account show`; verify the intended login and remove stale app-credential environment settings from this terminal |
| No documents appear yet | That is expected before the SharePoint ingestion walkthrough |

Do not solve a 403 by making containers public or by pasting account keys into
the app. Fix the identity, scope, or networking that caused it.

## Next

Continue with [Walkthrough 2: SharePoint ingestion](sharepoint-ingestion.md).
After that, [Walkthrough 3: App Service deployment](app-service.md) creates and
configures the web app. No Copilot, Genesys, or customer channel has been changed
by the steps above.

Official references: [create Search](https://learn.microsoft.com/azure/search/search-create-service-portal),
[Search RBAC](https://learn.microsoft.com/azure/search/search-security-enable-roles),
[Search managed identities](https://learn.microsoft.com/azure/search/search-how-to-managed-identities),
and [blob soft delete](https://learn.microsoft.com/azure/storage/blobs/soft-delete-blob-enable).
