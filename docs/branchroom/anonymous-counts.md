# Anonymous counts

Branchroom collection is disabled by default. The settings and source-access pages
show its state and let a user disable it on their device. The only stored preference
is a boolean; there are no visitor identifiers, cookies or persistent event queues.
Global Privacy Control and Do Not Track also prevent collection.
If the preference cannot be read, collection stays off. Preference-storage failures
are silent and do not affect workspace actions.

An owner can prepare an integration through the existing custom header template:

```html
<meta name="branchroom-telemetry-enabled" content="true">
<meta name="branchroom-telemetry-endpoint" content="https://your-collector.example/v1/events">
```

This example does not configure an endpoint or enable any running instance. A
same-origin relative proxy path ending in `/v1/events` is also supported. Absolute
endpoints require HTTPS. Credentials, query strings and fragments are rejected.

The self-hosted collector must implement the shared version-1 contract. It accepts
only `version`, `app`, `kind`, `name`, `surface` and `route`. Branchroom sends fixed
`settings` or `workspace` route categories, screen counts, successful copy counts,
and fixed `storage_failed` or `validation_failed` categories for clipboard or
time-range failures. It does not send
repository names, scope or grant IDs, usernames, clone URLs, form values, raw errors,
stack traces or precise client timestamps. Security audit logs remain separate.

Requests omit credentials and referrers, reject redirects, time out after two
seconds and never retry. Each page lifetime permits at most 200 events, 20 per
minute and one request in flight. Repeated error categories are suppressed for a
minute. Collection failures do not interrupt workspace actions.

The collector stores daily aggregate counts only, retaining count rows for 30 days
and error rows for 14 days. Its dedicated ingestion path must disable access logs,
strip identifying request metadata at proxies, enforce explicit allowed origins and
provide no public read endpoint. Collector deployment and enabling collection need
separate approval. Nothing in this integration changes Branchroom authorization,
restricted storage, environment materialization or existing audit logging.
