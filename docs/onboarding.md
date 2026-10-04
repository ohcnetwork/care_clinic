# Facility setup with CARE Onboarding

[Documentation index](README.md)

Facility setup lives in the [CARE Onboarding frontend plugin](https://github.com/ohcnetwork/care_onboarding_fe), not in a separate CARE Clinic page. Its source, curated datasets and master-sheet converter are maintained in the plugin repository, with the datasets in `data_source/`. CARE Clinic only registers the hosted plugin; it does not bundle its assets or datasets.

For the desktop's computer checks, recovery exports, installation and control
panel, use [Using CARE Clinic](desktop-workflows.md). Reaching the desktop
Overview means the local installation completed; it does not mean a facility,
staff or clinical catalog has already been created in CARE.

## Default installation

CARE Onboarding is a frontend-only [catalog entry](plugins.md). **New clinic installations** enable it automatically with:

| Setting | Value |
| --- | --- |
| Display name | CARE Onboarding |
| Plugin ID / frontend name | `care_onboarding_fe` |
| Remote URL | `https://ohcnetwork.github.io/care_onboarding_fe/assets/remoteEntry.js` |
| Frontend settings | `{"config":{"redirect_after_login":true}}` |

`Clinic.Setup` initializes the catalog defaults before building images. The resulting list is persisted in `plugins.json`; normal startup registers its frontend entries in CARE's `PlugConfig` table. Initialization preserves any existing list, including an explicitly empty one, so retries do not re-enable a removed plugin.

Existing installations are **not** automatically opted in. Add **CARE Onboarding** from CARE Clinic's **Plugins** tab, then choose **Save and apply**. Enable the frontend build setting below and rebuild the CARE frontend before reloading CARE. If a custom entry already uses its plugin ID or frontend name, remove that entry before adding the catalog version.

The whole plugin bundle is served by GitHub Pages. No local preview server or extra container is needed, but staff browsers need internet access to download it. Removing the plugin and saving disables it without deleting data already imported into CARE.

## CARE compatibility and startup

Automatic onboarding runs **after normal CARE login**, using CARE's existing plugin component-override feature. No CARE frontend or backend source changes, custom login screen, or new backend endpoint are required.

The bundled `deployments/frontend.env` enables the dashboard override:

```dotenv
REACT_MFE_REGISTERED_COMPONENTS=UserDashboard
```

This is a **build-time setting**. New installations include it when building CARE. Existing installations preserve their installed `frontend.env`; add `UserDashboard` to that file's existing registration list and rebuild the CARE frontend. Keep other registered components; `*` already includes the dashboard. Saving plugin settings alone does not rebuild the frontend.

The selected CARE frontend must support manifest `overrides`, registration of `UserDashboard`, and passing the original component as `__base`. Without registration, the plugin remains available manually through **Facility Setup** but cannot redirect the dashboard automatically.

Use CARE Clinic's **Open** button or visit `https://<clinic>.local/`, then sign in with the initial superuser. When the home dashboard opens, the plugin confirms the account is a superuser and checks the authenticated `/api/v1/facility/?limit=1&offset=0` endpoint. This checks the superuser's instance-wide facility list, including private clinics, rather than their assigned memberships.

No facilities means redirect to `/admin/onboarding`. An existing facility or an ordinary staff account keeps the original CARE dashboard. Failed or malformed checks show an error with **Try again**, never an automatic redirect. Normal CARE login and MFA remain unchanged.

The check runs on dashboard entry, not continuously during setup. After a facility has been created, resume unfinished setup through **Facility Setup** in the same browser. `/onboarding` remains an authenticated alias for bookmarks.

Set `config.redirect_after_login` to `false` for manual-only setup. Replace saved experimental `auto_onboarding` metadata with the new setting; the plugin no longer uses the experimental pre-login integration.

## Data loading

The plugin calls CARE's REST APIs using the CARE session; it does not write directly to the database or store a separate login token. It is restricted to superusers.

The setup flow covers geography, the clinic, departments and locations, staff memberships, invoice and patient numbering, clinical questionnaires, and report templates. Questionnaires and templates have separate steps. Existing records and sharing links are reconciled so retries do not duplicate successful imports.

Progress is saved per CARE instance in the browser, without passwords or contact details. Continue incomplete setup in the same browser; existing-facility checks and a single-tab lock help prevent accidental duplicate setup.

Clinical catalog import remains disabled until the required definitions and facility mappings are complete. Maintain fixtures and the master sheet in the plugin repository, and use its build, conversion and test commands. The [plugin README](https://github.com/ohcnetwork/care_onboarding_fe#readme) is the source of truth for the current flow and API contracts.
