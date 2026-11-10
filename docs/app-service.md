# Walkthrough 3: Deploy the Go middleware on Azure App Service

Complete the [Search/Storage](azure-setup.md) and
[SharePoint ingestion](sharepoint-ingestion.md) walkthroughs first.

**Where:** Azure portal and a Bash development terminal containing this repository.
No Terraform is used. A container image build is still a terminal command; you
do not need to install Docker locally when using the registry build below.

The app will not be ready until its image, permissions, and settings are all
configured. A resource showing "deployment complete" is not the same as a
working Genesys-to-Copilot conversation.

## 1. Create a container registry

The registry stores the packaged Go application. It is not where customer
documents are stored.

1. In Azure portal, search for **Container registries**, then select **Create**.
2. Select the existing development subscription/resource group.
3. Enter a unique registry name such as `acrgcdev12345` and choose the approved
   region.
4. Select **Basic** for the initial development SKU, after reviewing its price.
5. If **Role assignment permissions mode** is shown, select **RBAC Registry
   Permissions** for this walkthrough.
6. Use your organization's approved development networking settings, review,
   and create the registry.
7. Open the registry. Record its name and **Login server** from Overview.
   Use the displayed login server rather than constructing one; names may
   include a generated suffix depending on the registry configuration.
8. Under **Access keys**, leave the registry **Admin user** disabled.

If an existing registry uses **RBAC Registry + ABAC Repository Permissions**, its
pull role is **Container Registry Repository Reader**, not `AcrPull`. Have the
administrator assign repository-scoped permissions appropriately; do not change
the mode of a shared registry merely to follow this example.

**Check before continuing:** the registry exists and you have its exact login
server. No registry admin password is needed.

## 2. Build the image

1. In your Bash development terminal, make sure ingestion credential variables
   from the previous guide have been cleared.
2. Run `az account show` and verify the intended development subscription.
3. Change to the repository root, where `Dockerfile` and `go.mod` are located.
4. Run the following, replacing the registry name:

```bash
az acr build --registry acrgcdev12345 --image genesys-copilot-studio:v1 .
```

The final `.` means "build the source in this directory." This uploads the build
context to your Azure registry build service; `.dockerignore` excludes local
environment files. Do not add secrets to the Dockerfile or other source files.

5. Wait for a successful build. If the command is denied, your operator needs
   permission to run registry builds and publish their output; ask the registry
   administrator rather than enabling the registry admin user.
6. In the portal, open the registry's **Repositories** page.
7. Open `genesys-copilot-studio` and confirm the tag `v1` exists. Viewing repository
   data may require its own read permission even if the build was authorized.

**Check before continuing:** the registry contains the image. The portal alone
cannot build the Go source by selecting a language runtime.

## 3. Prepare an identity for the initial image pull

The portal's container-creation wizard can use an existing **user-assigned
managed identity** to pull the private image. This avoids needing a registry
password before the web app exists.

This identity is only for image pulling. We will separately enable the web app's
**system-assigned identity** for Search, Storage, and Key Vault.

1. In Azure portal, search for **Managed Identities**, then select **Create**.
2. Select the existing resource group and region.
3. Name the identity `id-gc-image-pull`, then review and create it.
4. Open the registry's **Access Control (IAM)**.
5. Select **Add > Add role assignment**, choose **AcrPull**, and select **Next**.
6. Choose **Managed identity > Select members**.
7. Choose **User-assigned managed identity**, select `id-gc-image-pull`, and
   confirm **Review + assign**.

For an ABAC-mode registry, use the repository reader role described in step 1.
Also check with your registry admin that **ARM audience token authentication**
is allowed for managed-identity pulls. If a pull later fails with token validation
errors, this can be checked with:

```bash
az acr config authentication-as-arm show -r acrgcdev12345
```

Do not change an organization's authentication policy without approval.

**Check before continuing:** the image-pull identity has the correct registry
role. Allow time for role propagation.

## 4. Create the Linux container web app

1. Search for **App Services** in Azure portal.
2. Select **Create > Web App**.
3. On **Basics**, select the existing subscription/resource group.
4. Enter a unique app name, for example `app-gc-dev-12345`.
5. Set **Publish** to **Container**, **Operating System** to **Linux**, and
   select your approved region.
6. Under the Linux App Service plan, create a development plan such as
   `asp-gc-dev`. Select a paid tier with **Always On**, such as **Basic B1**,
   after reviewing its cost. This is not a production capacity recommendation.
7. Open the **Container** tab.
8. Choose **Enhanced configuration with sidecar support on** if the wizard
   offers that choice. We will use one main container, not add a sidecar service.
9. Choose **Azure Container Registry** as the image source and select your registry.
10. Set **Authentication** to **Managed identity** and select `id-gc-image-pull`.
11. Keep the main container name as `main`, select image
    `genesys-copilot-studio`, tag `v1`, and set the target **Port** to **8080**.
12. Leave the startup command empty; the image already starts `/middleware`.
13. Review the configuration and select **Create**.
14. After deployment, open the app and copy its actual **Default domain** from
    Overview. Use that displayed hostname everywhere below; do not guess it
    from the resource name.

If your portal exposes only the older single-container configuration, the
equivalent image settings are under **Deployment Center**, and the port is set
using `WEBSITES_PORT=8080`. Use the configuration path for your hosting model,
not both indiscriminately.

**Check before continuing:** the App Service resource exists and its configured
image/tag is correct. Startup errors about missing settings are expected until
step 8. Do not treat them as a successful deployment.

## 5. Enable the web app's runtime identity and permissions

1. On the App Service resource, open **Settings > Identity**.
2. On **System assigned**, set **Status** to **On**, select **Save**, and confirm.
3. Record the displayed **Object (principal) ID**.
4. Open Storage > Containers > `bot-sessions` > **Access Control (IAM)**.
5. Add **Storage Blob Data Contributor**, choosing **Managed identity** and
   the App Service resource's system-assigned identity.
6. Open Search > **Access Control (IAM)** and assign **Search Index Data Reader**
   to that same App Service identity.

Use the [role-assignment steps](azure-setup.md#7-assign-the-initial-access-roles)
if you need the exact IAM dialog sequence. Do not select the image-pull identity
for these runtime grants, and do not give the web app Graph/SharePoint access.

**Check before continuing:** the App Service system identity can access session
storage and read Search. It cannot write the knowledge container.

## 6. Create Key Vault and store the three application secrets

### 6.1 Create the vault and assign access

1. Search for **Key vaults** in Azure portal, then select **Create**.
2. Use the existing subscription/resource group, a unique vault name such as
   `kv-gc-dev-12345`, and the approved region.
3. Choose the **Standard** pricing tier unless your organization requires another.
4. On **Access configuration**, select **Azure role-based access control**.
5. Keep soft delete enabled and follow organizational recovery/purge-protection
   policy. Use approved networking that allows App Service to reach the vault.
6. Review and create the vault, then open it.
7. In its IAM page, grant your secret-management operator **Key Vault Secrets
   Officer** so they can create/update secrets. Being the resource creator alone
   does not necessarily grant secret-data access.
8. Assign **Key Vault Secrets User** to the App Service's system-assigned identity.
   This lets the app read values without allowing it to manage the vault.

Use a dedicated vault for this integration, or ask your administrator to grant
the runtime access only to these specific secrets in a shared vault.

### 6.2 Prepare and enter the secret values

1. Generate two different random values of at least 32 characters using an
   approved password manager. Name them `genesys-api-key` and `knowledge-api-key`.
   Do not use the example names themselves as passwords.
2. Complete [Copilot Studio steps 1-2](copilot-studio.md#1-prepare-the-customer-facing-agent)
   to obtain the published agent's web-channel secret and correct Direct Line
   endpoint. Return here before configuring its retrieval connector.
3. In Key Vault, open **Objects > Secrets**, then **Generate/Import**.
4. Choose **Manual**, enter name `genesys-api-key`, paste its random value into
   **Secret value**, and select **Create**.
5. Repeat for `knowledge-api-key`.
6. Repeat for `direct-line-secret`, using the actual Copilot web-channel secret.
7. Verify the three secret names appear. Do not copy their values into source
   files or send them in chat.

You will later enter the incoming key into Genesys integration credentials and
the retrieval key into a Power Platform connection. The Direct Line secret
belongs only to the backend-to-Copilot connection.

**Check before continuing:** three secrets exist, and the web app's identity has
permission to read them.

## 7. Reserve the Genesys integration ID

Complete [Genesys step 1](genesys.md#1-create-an-inactive-integration) to create a
dedicated integration, leave it inactive, and record its integration ID. You can
do that before the middleware is healthy.

Return here with that ID. Do not activate a live flow yet.

## 8. Add App Service environment variables

1. Open the App Service resource, then **Settings > Environment variables**.
   Older portal versions put app settings under **Configuration**.
2. On **App settings**, select **Add** for each row below, enter its name/value,
   and confirm the dialog. Use your own saved values, not the example hostnames.

| Name | Value to enter |
|---|---|
| `PORT` | `8080` |
| `GENESYS_INTEGRATION_ID` | The ID from the inactive Genesys integration |
| `GENESYS_BOT_ID` | `qa-assistant` |
| `GENESYS_BOT_VERSION` | `v1` |
| `DIRECT_LINE_ENDPOINT` | The verified regional URL ending in `/v3/directline` |
| `TURN_TIMEOUT` | `20s` initially; confirm the actual Genesys timeout before launch |
| `STORAGE_ENDPOINT` | Your Blob service endpoint |
| `SESSION_CONTAINER` | `bot-sessions` |
| `SEARCH_ENDPOINT` | Your Search service endpoint |
| `SEARCH_INDEX` | `customer-knowledge` |

3. Add the three secret settings as **Key Vault references**, not plaintext.
   Replace the vault hostname in these examples:

```text
GENESYS_API_KEY=@Microsoft.KeyVault(SecretUri=https://YOURVAULT.vault.azure.net/secrets/genesys-api-key/)
KNOWLEDGE_API_KEY=@Microsoft.KeyVault(SecretUri=https://YOURVAULT.vault.azure.net/secrets/knowledge-api-key/)
DIRECT_LINE_SECRET=@Microsoft.KeyVault(SecretUri=https://YOURVAULT.vault.azure.net/secrets/direct-line-secret/)
```

4. For **classic/non-sidecar** container hosting only, also add
   `WEBSITES_PORT=8080`. For the enhanced/sidecar hosting path, verify the main
   container target port is 8080 in Deployment Center.
5. Select **Apply/Save** on the app-settings page and confirm the restart prompt.
6. Inspect the Key Vault reference status in the portal's app-settings diagnostics.
   If a reference is unresolved, check its URI, the App Service system identity's
   role, propagation time, and vault networking before continuing.

Do not put SharePoint IDs, the ingester client secret, or `AZURE_CLIENT_ID` for
the image-pull identity on this app. Runtime credentials should come from its
system-assigned identity. `STORAGE_RESOURCE_ID` and `KNOWLEDGE_CONTAINER` are
operator-command settings, not requirements for the web server.

**Check before continuing:** all required settings are present, Key Vault
references resolve, and the container can start without missing-setting errors.

## 9. Set operating options and confirm liveness

1. In the app's **Configuration > General settings**, enable **Always On** and
   save. If it is unavailable, check the App Service plan tier.
2. Enable **HTTPS Only** on the app's TLS/settings page. Disable unnecessary
   basic-auth publishing/FTP access according to organizational policy.
3. Open **Monitoring > App Service logs** and enable container/application logging
   to the approved destination. Use **Log stream** to inspect startup.
4. Look for the application's **middleware listening** log. Do not enable HTTP
   request-body/header capture or print environment variables for troubleshooting.
5. Open `https://YOUR-ACTUAL-APP-HOSTNAME/healthz` in a browser.
6. Expect `{"status":"ok"}`. The site root `/` is not a chat UI and can return 404.
7. Open **Monitoring > Health check**, enable it with path `/healthz`, and save.

This endpoint proves only that the process is alive. It does not prove that
Copilot, Search, or Genesys connections work.

| Problem | First checks |
|---|---|
| Container image cannot be pulled | Exact login server/image/tag, image-pull identity, registry role mode, ARM token policy, and registry networking |
| Container exits immediately | Log stream for missing settings or unresolved secret references |
| App starts but `/healthz` times out | Main container target port versus classic `WEBSITES_PORT`; app listens on 8080 |
| Secret reference fails | App Service system identity, Key Vault Secrets User, secret name, vault firewall |
| Search request fails | Runtime Search Index Data Reader grant and Search RBAC mode |
| Session storage fails | Runtime Contributor grant specifically on `bot-sessions` and Storage networking |

Continue with [Copilot Studio from step 3](copilot-studio.md#3-create-the-protected-knowledge-connector),
then finish [Genesys from step 2](genesys.md#2-configure-the-endpoint-and-credential).
Run the [acceptance checklist](acceptance.md) before connecting real customers.

Official references: [container app creation](https://learn.microsoft.com/azure/app-service/quickstart-custom-container),
[managed-identity registry access](https://learn.microsoft.com/azure/app-service/configure-custom-container#use-managed-identity-to-pull-an-image-from-azure-container-registry),
and [App Service Key Vault references](https://learn.microsoft.com/azure/app-service/app-service-key-vault-references).
