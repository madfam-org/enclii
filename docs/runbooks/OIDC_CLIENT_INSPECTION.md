# Read-only OAuth client evidence

`enclii --profile admin secrets provision oidc inspect --platform crea-sim --json`
uses the existing managed Enclii admin session to GET the Janua client inventory
and the selected internal UUID's detail. The output contains only public client
metadata. It does not register, reconcile, resolve or rotate credentials, write
Vault intake, switch organization, create membership, or send business mail.
The internal-key registration path is deliberately unavailable to inspection.

A missing/ambiguous client, secret-bearing provider response, malformed JSON,
provider denial or transport failure returns a redacted error and no metadata.
An inactive client or drift is reported as evidence, never repaired automatically.
Only an embedded registry platform is selectable; mutation/registry override
flags are not accepted by the inspect child command.

The UI `crea-sim` profile alone declares `madfam:silent_auth`. Janua requires
this explicit first-party trust opt-in for this client name to reuse its own
browser session with `prompt=none` and preconsent. The `crea-sim-mcp` profile,
client names, audiences, organizations, callbacks, grants and consumer roles do
not change. The flag does not grant a human membership or application authority.

Declaring the registry does not reconcile a live client. Before any separately
reviewed change, capture fresh metadata and require an exact fingerprint match
for the actual ID, name, audience, organization, callback, scopes, grants,
confidentiality and active status. Any approved provider request must PATCH
only `allowed_scopes`, adding `madfam:silent_auth` to that same UI client. Re-read
the metadata afterwards: every other allowlisted field must remain equal. Do
not run general provisioning merely to apply this trust change: that flow can
also resolve/rotate credentials and submit intake.

Deploy the qualified SIM bootstrap with its activation flag off first, then
activate only after the live trust opt-in is verified. On withdrawal, disable
the SIM flag first and verify the same client before removing only this scope.
A real invited browser walkthrough remains separate from local synthetic tests.
