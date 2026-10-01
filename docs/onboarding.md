# Facility setup with CARE Onboarding

[Documentation index](README.md)

Facility setup lives in the [CARE Onboarding frontend plugin](https://github.com/ohcnetwork/care_onboarding_fe), not in a separate Desktop page. Its source, curated datasets and master-sheet converter are maintained in the plugin repository, with the datasets in `data_source/`. Desktop only registers the hosted plugin; it does not bundle its assets or datasets.

## Default installation

CARE Onboarding is a frontend-only [catalog entry](plugins.md). **New clinic installations** enable it automatically with:

| Setting | Value |
| --- | --- |
| Display name | CARE Onboarding |
| Plugin ID / frontend name | `care_onboarding_fe` |
| Remote URL | `https://ohcnetwork.github.io/care_onboarding_fe/assets/remoteEntry.js` |
| Frontend settings | `{"config":{"auto_onboarding":true}}` |

`Clinic.Setup` initializes the catalog defaults before building images. The resulting list is persisted in `plugins.json`; normal startup registers its frontend entries in CARE's `PlugConfig` table. Initialization preserves any existing list, including an explicitly empty one, so retries do not re-enable a removed plugin.

Existing installations are **not** automatically opted in. Add **CARE Onboarding** from Desktop's **Plugins** tab, then choose **Save and apply** and reload CARE. If a custom entry already uses its plugin ID or frontend name, remove that entry before adding the catalog version.

The whole plugin bundle is served by GitHub Pages. No local preview server or extra container is needed, but staff browsers need internet access to download it. Removing the plugin and saving disables it without deleting data already imported into CARE.

## CARE compatibility and startup

Automatic pre-login onboarding requires both CARE integration changes:

- The backend's public `GET /api/v1/plug_config/setup_status/` endpoint returns `{"required":true}` when there are no facilities, including private facilities in the existence check.
- The CARE frontend supports the manifest's `onboarding.path` extension and `meta.config.auto_onboarding`. It redirects an unconfigured instance to `/onboarding`, displays the username/password setup login, and lets a superuser load the plugin.

**Registering the plugin does not install those host changes.** The locally tested CARE frontend/backend changes must also be published and included in the source refs selected in `deployments/.env` before distributing a fresh installer with automatic onboarding. Until then, do not treat the Desktop catalog entry alone as a complete pre-login integration.

With compatible CARE builds, use Desktop's **Open** button or visit `https://<clinic>.local/`. The plugin also provides **Facility Setup** in CARE's admin navigation at `/admin/onboarding`. An already configured clinic is not automatically redirected away from its normal login.

## Data loading

The plugin calls CARE's REST APIs using the CARE session; it does not write directly to the database or store a separate login token. It is restricted to superusers.

The setup flow covers geography, the clinic, departments and locations, staff memberships, invoice and patient numbering, clinical questionnaires, and report templates. Questionnaires and templates have separate steps. Existing records and sharing links are reconciled so retries do not duplicate successful imports.

Progress is saved per CARE instance in the browser, without passwords or contact details. Continue incomplete setup in the same browser; existing-facility checks and a single-tab lock help prevent accidental duplicate setup.

Clinical catalog import remains disabled until the required definitions and facility mappings are complete. Maintain fixtures and the master sheet in the plugin repository, and use its build, conversion and test commands. The [plugin README](https://github.com/ohcnetwork/care_onboarding_fe#readme) is the source of truth for the current flow and API contracts.
