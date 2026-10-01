import { mkdir } from "node:fs/promises";
import { join, relative, resolve } from "node:path";
import { expect, test, type Locator, type Page } from "@playwright/test";
import type {} from "./fixtures/host";
import type { CarePlugin, PluginCatalogEntry } from "../src/types";
import {
  hasPluginProblems, pluginProblems, reconcileCatalog, serializePlugins, toPluginRow,
} from "../src/screens/panel/plugin-model";

const catalog: PluginCatalogEntry[] = [
  {
    plugin: {
      id: "care_onboarding_fe", label: "CARE Onboarding", catalog: true,
      frontend: {
        slug: "care_onboarding_fe",
        url: "https://ohcnetwork.github.io/care_onboarding_fe/assets/remoteEntry.js",
        meta: { config: { auto_onboarding: true } },
      },
    },
    description: "Set up a new clinic's geography, facility, departments, staff, numbering, questionnaires and report templates. Requires internet access.",
    default: true,
  },
  {
    plugin: {
      id: "care_notifications", label: "Booking notifications", catalog: true,
      backend: {
        name: "care_notifications",
        package_name: "git+https://github.com/ohcnetwork/care_booking_notifications_be.git",
        version: "@main",
      },
    },
    description: "Appointment confirmations, reminders, cancellations and reschedule notices by SMS and web push.",
  },
  {
    plugin: {
      id: "care_filly", label: "Filly", catalog: true,
      backend: {
        name: "care_filly", package_name: "git+https://github.com/ohcnetwork/care_filly.git", version: "@main",
        configs: { MEDISPEAK_BASE_URL: "", MEDISPEAK_API_KEY: "" },
      },
      frontend: {
        slug: "care_filly_fe", url: "https://ohcnetwork.github.io/care_filly_fe/assets/remoteEntry.js",
        meta: { config: { MEDISPEAK_API_URL: "" } },
      },
    },
    description: "Voice-driven form filling for clinical notes and questionnaires, powered by Medispeak.",
  },
];
const custom: CarePlugin = {
  id: "custom_example", label: "Custom example",
  backend: { name: "care_example.apps", package_name: "care-example", version: "==1.2" },
  frontend: { slug: "care_example_fe", url: "http://localhost:4178/assets/remoteEntry.js", meta: { config: {} } },
};
const root = (page: Page) => page.locator(".care-plugins");
const saveButton = (page: Page) => root(page).getByRole("button", { name: "Save and apply", exact: true });
const addButton = (page: Page) => root(page).getByRole("combobox", { name: "Add a plugin", exact: true });
const row = (page: Page, name: string) => root(page).getByRole("region", { name, exact: true });
type PluginRead = "ReadPlugins" | "PluginCatalog";

test.beforeEach(async ({ page }) => {
  // Other agents may edit the shared preview while these isolated pages run.
  await page.routeWebSocket(/^ws:\/\/(?:127\.0\.0\.1|localhost):\d+\//, (socket) => socket.close());
  page.on("pageerror", (error) => { throw error; });
});

async function openPlugins(page: Page, options: {
  saved?: CarePlugin[];
  catalog?: PluginCatalogEntry[];
  fail?: PluginRead;
  hold?: PluginRead;
  restorePending?: boolean;
  scenario?: string;
  finishJobs?: boolean;
  ready?: boolean;
} = {}) {
  await page.addInitScript((options) => {
    let preview: Window["careTest"];
    Object.defineProperty(window, "careTest", {
      configurable: true,
      get: () => preview,
      set: (next: Window["careTest"]) => {
        preview = next;
        preview.fixtures.plugins = structuredClone(options.saved);
        preview.fixtures.catalog = structuredClone(options.catalog);
        preview.fixtures.finishJobs = options.finishJobs;
        preview.state.restore_pending = options.restorePending;
        if (options.fail) preview.failNext(options.fail, "EACCES /private/clinic: private config must not appear");
        if (options.hold) preview.hold(options.hold);
      },
    });
  }, {
    saved: options.saved ?? [catalog[0].plugin],
    catalog: options.catalog ?? catalog,
    fail: options.fail,
    hold: options.hold,
    restorePending: options.restorePending ?? false,
    finishJobs: options.finishJobs ?? true,
  });
  await page.goto(`/tests/fixtures/index.html?scenario=${options.scenario ?? "panel-running"}`);
  await page.getByRole("button", { name: "Plugins", exact: true }).click();
  await expect(root(page)).toBeVisible();
  if (options.ready !== false && !options.restorePending) await expect(addButton(page)).toBeEnabled();
}

async function countCalls(page: Page, method: keyof Window["go"]["main"]["App"]) {
  return page.evaluate((method) => window.careTest.calls.filter((call) => call.method === method).length, method);
}

async function countApply(page: Page) {
  return page.evaluate(() => window.careTest.calls.filter((call) =>
    call.method === "ClinicAction" && call.args[0] === "apply-plugins").length);
}

async function addCustom(page: Page): Promise<Locator> {
  await addButton(page).click();
  await page.getByRole("option", { name: "Custom plugin", exact: true }).click();
  const editor = root(page).locator(".care-plugin-row").last();
  await expect(editor.getByLabel("Display name", { exact: true })).toBeVisible();
  return editor;
}

async function fillCustom(editor: Locator) {
  await editor.getByLabel("Display name", { exact: true }).fill("Custom example");
  await editor.getByLabel("Plugin ID", { exact: true }).fill(custom.id);
  await editor.getByLabel("Python module", { exact: true }).fill(custom.backend!.name);
  await editor.getByLabel("pip source", { exact: true }).fill(custom.backend!.package_name);
  await editor.getByLabel("Version", { exact: true }).fill(custom.backend!.version!);
  await editor.getByLabel("Frontend name", { exact: true }).fill(custom.frontend!.slug);
  await editor.getByLabel("remoteEntry.js URL", { exact: true }).fill(custom.frontend!.url);
}

async function capture(page: Page, name: string) {
  if (!process.env.CARE_SCREENSHOTS_DIR) return;
  const folder = relative(process.cwd(), resolve(process.env.CARE_SCREENSHOTS_DIR));
  await mkdir(folder, { recursive: true });
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: join(folder, `${name}.png`), animations: "disabled" });
}

async function assertFits(page: Page) {
  expect(await page.evaluate(() => {
    const table = document.querySelector(".care-plugins")!;
    const dialog = document.querySelector(".care-plugins-dialog");
    const box = dialog?.getBoundingClientRect();
    return {
      documentOverflow: document.documentElement.scrollWidth > innerWidth || document.documentElement.scrollHeight > innerHeight,
      tableOverflow: table.scrollWidth > table.clientWidth + 1,
      dialogOverflow: !!dialog && dialog.scrollWidth > dialog.clientWidth + 1,
      dialogOutside: !!box && (box.left < 0 || box.right > innerWidth || box.top < 0 || box.bottom > innerHeight + 1),
    };
  })).toEqual({ documentOverflow: false, tableOverflow: false, dialogOverflow: false, dialogOutside: false });
}

for (const viewport of [{ width: 1100, height: 700 }, { width: 720, height: 560 }]) {
  test(`reviewed plugin table and custom editor fit ${viewport.width}x${viewport.height}`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await openPlugins(page, { saved: catalog.map((entry) => entry.plugin) });
    await expect(root(page).locator(".care-plugin-row")).toHaveCount(3);
    await assertFits(page);
    await capture(page, `plugins-after-${viewport.width}x${viewport.height}`);
    await row(page, "CARE Onboarding").getByRole("button", { name: "Edit CARE Onboarding" }).click();
    await expect(row(page, "CARE Onboarding").getByLabel("Frontend settings (JSON)")).toBeVisible();
    await assertFits(page);
    await capture(page, `plugins-after-expanded-${viewport.width}x${viewport.height}`);
    const editor = await addCustom(page);
    await fillCustom(editor);
    await editor.getByRole("button", { name: "Add setting", exact: true }).click();
    await editor.getByLabel("Setting 1 name", { exact: true }).fill("AN_EXAMPLE_SETTING_WITH_A_LONG_NAME");
    await editor.getByLabel("Setting 1 value", { exact: true }).fill("A long example configuration value for layout verification.");
    await editor.getByLabel("Setting 1 value", { exact: true }).scrollIntoViewIfNeeded();
    await assertFits(page);
    await capture(page, `plugins-custom-${viewport.width}x${viewport.height}`);
    await root(page).getByRole("button", { name: "Remove CARE Onboarding", exact: true }).click();
    await expect(page.getByRole("alertdialog")).toBeVisible();
    await assertFits(page);
    await capture(page, `plugins-remove-${viewport.width}x${viewport.height}`);
    await page.getByRole("alertdialog").getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(editor.getByLabel("Setting 1 value", { exact: true })).toHaveValue("A long example configuration value for layout verification.");
  });
}

test("loading is not an empty list and requests are single-flight", async ({ page }) => {
  await openPlugins(page, { hold: "ReadPlugins", ready: false });
  await expect(root(page).getByRole("status")).toContainText("Loading plugins");
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  await expect(root(page).getByText("No plugins yet", { exact: true })).toHaveCount(0);
  expect(await countCalls(page, "ReadPlugins")).toBe(1);
  expect(await countCalls(page, "PluginCatalog")).toBe(1);
  await page.evaluate(() => window.careTest.release("ReadPlugins"));
  await expect(saveButton(page)).toBeEnabled();
  await expect(row(page, "CARE Onboarding")).toBeVisible();
});

for (const method of ["ReadPlugins", "PluginCatalog"] as const) {
  test(`${method} failure is honest, retryable, and keeps the other successful read`, async ({ page }) => {
    await openPlugins(page, { fail: method, ready: false });
    await expect(root(page).getByRole("alert")).toContainText(method === "ReadPlugins"
      ? "Couldn't read the saved plugins" : "Couldn't read the plugin catalog");
    await expect(root(page)).not.toContainText("private config");
    await expect(root(page).getByText("No plugins yet", { exact: true })).toHaveCount(0);
    await expect(saveButton(page)).toBeDisabled();
    await expect(addButton(page)).toBeDisabled();
    if (method === "PluginCatalog") await expect(row(page, "CARE Onboarding")).toBeVisible();
    await root(page).getByRole("button", { name: "Retry", exact: true }).click();
    await expect(saveButton(page)).toBeEnabled();
    await expect(row(page, "CARE Onboarding")).toBeVisible();
    expect(await countCalls(page, method)).toBe(2);
    expect(await countCalls(page, method === "ReadPlugins" ? "PluginCatalog" : "ReadPlugins")).toBe(1);
    expect(await countCalls(page, "SavePlugins")).toBe(0);
  });
}

test("empty saved choices stay empty and unsaved removal needs no confirmation", async ({ page }) => {
  await openPlugins(page, { saved: [] });
  await expect(root(page).getByText("No plugins yet", { exact: true })).toBeVisible();
  await addCustom(page);
  await root(page).getByRole("button", { name: "Remove New plugin", exact: true }).click();
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await expect(root(page).getByText("No plugins yet", { exact: true })).toBeVisible();
  await saveButton(page).click();
  await expect(root(page)).toContainText("Plugin task finished.");
  expect(await page.evaluate(() => window.careTest.calls.find((call) => call.method === "SavePlugins")?.args[0])).toEqual([]);
});

test("custom fields mirror native rules and report accessible errors without parser traces", async ({ page }) => {
  await openPlugins(page, { saved: [] });
  const editor = await addCustom(page);
  await expect(saveButton(page)).toBeDisabled();
  await fillCustom(editor);
  await expect(saveButton(page)).toBeEnabled();
  const cases = [
    ["Plugin ID", "bad id", custom.id],
    ["Python module", "care-example", custom.backend!.name],
    ["pip source", "package with spaces", custom.backend!.package_name],
    ["Version", "main", custom.backend!.version!],
    ["Frontend name", "not.a.slug", custom.frontend!.slug],
    ["remoteEntry.js URL", "/assets/remoteEntry.js", custom.frontend!.url],
    ["remoteEntry.js URL", "javascript:alert(1)", custom.frontend!.url],
    ["remoteEntry.js URL", "https://example.com\\remoteEntry.js", custom.frontend!.url],
    ["Frontend settings (JSON)", '{"private_setting":', "{}"],
    ["Frontend settings (JSON)", '["not an object"]', "{}"],
    ["Frontend settings (JSON)", '{"url":"https://example.com/elsewhere.js"}', "{}"],
    ["Frontend settings (JSON)", '{"config":{"number":1e400}}', "{}"],
  ];
  for (const [label, invalid, valid] of cases) {
    const input = editor.getByLabel(label, { exact: true });
    await input.fill(invalid);
    await expect(input).toHaveAttribute("aria-invalid", "true");
    const description = await input.getAttribute("aria-describedby");
    expect(description).toBeTruthy();
    await expect(page.locator(`#${description}`)).toBeVisible();
    await expect(saveButton(page)).toBeDisabled();
    await input.fill(valid);
    await expect(saveButton(page)).toBeEnabled();
  }
  await expect(root(page)).not.toContainText("SyntaxError");
  await editor.getByRole("switch", { name: "Backend", exact: true }).uncheck();
  await editor.getByRole("switch", { name: "Frontend", exact: true }).uncheck();
  await expect(editor).toContainText("Choose Backend, Frontend, or both.");
  await expect(saveButton(page)).toBeDisabled();
  await editor.getByRole("switch", { name: "Backend", exact: true }).check();
  await expect(editor.getByLabel("Python module", { exact: true })).toHaveValue(custom.backend!.name);
  await expect(saveButton(page)).toBeEnabled();
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("duplicate IDs, modules, frontend names, and setting names cannot overwrite one another", async ({ page }) => {
  await openPlugins(page, { saved: [custom] });
  const editor = await addCustom(page);
  await fillCustom(editor);
  await expect(editor).toContainText("This plugin ID is already in use.");
  await expect(editor).toContainText("This backend module is already in use.");
  await expect(editor).toContainText("This frontend name is already in use.");
  await editor.getByLabel("Plugin ID", { exact: true }).fill("custom_other");
  await editor.getByLabel("Python module", { exact: true }).fill("care_other");
  await expect(saveButton(page)).toBeDisabled();
  await editor.getByLabel("Frontend name", { exact: true }).fill("care_other_fe");
  await expect(saveButton(page)).toBeEnabled();
  await editor.getByRole("button", { name: "Add setting", exact: true }).click();
  await editor.getByRole("button", { name: "Add setting", exact: true }).click();
  await editor.getByLabel("Setting 1 name", { exact: true }).fill("EXAMPLE");
  await editor.getByLabel("Setting 2 name", { exact: true }).fill(" EXAMPLE ");
  await expect(editor.getByText("This setting name is already in use.", { exact: true })).toHaveCount(2);
  await expect(saveButton(page)).toBeDisabled();
  await editor.getByRole("button", { name: /Remove setting 2 from/ }).click();
  await expect(saveButton(page)).toBeEnabled();
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("catalog choices respect custom plugin identities as well as catalog IDs", async ({ page }) => {
  await openPlugins(page, { saved: [{
    ...custom, id: "custom_id", backend: { ...custom.backend!, name: "care_notifications" },
    frontend: { ...custom.frontend!, slug: "care_onboarding_fe" },
  }] });
  await addButton(page).click();
  await expect(page.getByRole("option", { name: "CARE Onboarding", exact: true })).toHaveCount(0);
  await expect(page.getByRole("option", { name: "Booking notifications", exact: true })).toHaveCount(0);
  await expect(page.getByRole("option", { name: "Filly", exact: true })).toBeVisible();
  await page.keyboard.press("Escape");
});

test("failed saves keep every edit across tabs and never start an apply", async ({ page }) => {
  await openPlugins(page, { saved: [] });
  const editor = await addCustom(page);
  await fillCustom(editor);
  await editor.getByLabel("Frontend settings (JSON)").fill('{"config":{"example":"keep this draft"}}');
  await page.evaluate(() => {
    window.careTest.hold("SavePlugins");
    window.careTest.failNext("SavePlugins", "write failed: /private/clinic secret-from-native");
  });
  await saveButton(page).evaluate((button: HTMLButtonElement) => { button.click(); button.click(); });
  await expect(root(page).getByRole("button", { name: "Saving…", exact: true })).toBeDisabled();
  await expect(editor.getByLabel("Plugin ID", { exact: true })).toBeDisabled();
  expect(await countCalls(page, "SavePlugins")).toBe(1);
  await page.evaluate(() => window.careTest.release("SavePlugins"));
  await expect(root(page).getByRole("alert")).toContainText("Plugin settings couldn't be saved.");
  await expect(root(page)).not.toContainText("secret-from-native");
  expect(await countApply(page)).toBe(0);
  await page.getByRole("button", { name: "Overview", exact: true }).click();
  await page.getByRole("button", { name: "Plugins", exact: true }).click();
  await expect(editor.getByLabel("Frontend settings (JSON)")).toHaveValue('{"config":{"example":"keep this draft"}}');
  await expect(root(page)).toContainText("Unsaved changes");
  expect(await countCalls(page, "ReadPlugins")).toBe(1);
  expect(await countCalls(page, "PluginCatalog")).toBe(1);
  expect(await page.evaluate(() => window.careTest.logs.join("\n"))).not.toContain("keep this draft");
});

test("saved removal is cancellable with Escape and restores keyboard focus without discarding edits", async ({ page }) => {
  await openPlugins(page);
  const onboarding = row(page, "CARE Onboarding");
  const toggle = onboarding.getByRole("button", { name: "Edit CARE Onboarding", exact: true });
  await toggle.focus();
  await page.keyboard.press("Enter");
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await onboarding.getByLabel("Frontend settings (JSON)").fill('{"config":{"auto_onboarding":false}}');
  const remove = onboarding.getByRole("button", { name: "Remove CARE Onboarding", exact: true });
  await remove.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("alertdialog").getByRole("button", { name: "Cancel", exact: true })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("alertdialog")).toBeHidden();
  await expect(remove).toBeFocused();
  await expect(onboarding.getByLabel("Frontend settings (JSON)")).toHaveValue('{"config":{"auto_onboarding":false}}');
  await page.getByRole("button", { name: "Overview", exact: true }).click();
  await page.getByRole("button", { name: "Plugins", exact: true }).click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await expect(root(page)).toContainText("Unsaved changes");
  await remove.click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Remove plugin", exact: true }).click();
  await expect(onboarding).toHaveCount(0);
  expect(await countCalls(page, "SavePlugins")).toBe(0);
  expect(await countApply(page)).toBe(0);
  await expect(addButton(page)).toBeFocused();
});

test("keyboard catalog selection opens the new editor without stealing focus back", async ({ page }) => {
  await openPlugins(page, { saved: [] });
  await addButton(page).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("option", { name: "CARE Onboarding", exact: true })).toBeFocused();
  await page.keyboard.press("End");
  await expect(page.getByRole("option", { name: "Custom plugin", exact: true })).toBeFocused();
  await page.keyboard.press("Enter");
  const name = root(page).getByLabel("Display name", { exact: true });
  await expect(name).toBeFocused();
  await page.keyboard.type("Keyboard example");
  await page.keyboard.press("Tab");
  await expect(root(page).getByLabel("Plugin ID", { exact: true })).toBeFocused();
  await expect(name).toHaveValue("Keyboard example");
  await expect(saveButton(page)).toBeDisabled();
});

test("tab deactivation closes plugin portals without deleting or resetting the draft", async ({ page }) => {
  await openPlugins(page);
  const onboarding = row(page, "CARE Onboarding");
  await onboarding.getByRole("button", { name: "Edit CARE Onboarding", exact: true }).click();
  const draft = '{"config":{"auto_onboarding":false}}';
  await onboarding.getByLabel("Frontend settings (JSON)").fill(draft);
  await onboarding.getByRole("button", { name: "Remove CARE Onboarding", exact: true }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "Overview", exact: true, includeHidden: true })
    .evaluate((button: HTMLButtonElement) => button.click());
  await expect(root(page)).toBeHidden();
  await expect(page.getByRole("alertdialog")).toBeHidden();
  await page.getByRole("button", { name: "Plugins", exact: true }).click();
  await expect(onboarding.getByLabel("Frontend settings (JSON)")).toHaveValue(draft);
  await expect(page.getByRole("alertdialog")).toBeHidden();
  await addButton(page).click();
  await expect(page.getByRole("listbox")).toBeVisible();
  await page.getByRole("button", { name: "Overview", exact: true, includeHidden: true })
    .evaluate((button: HTMLButtonElement) => button.click());
  await expect(page.getByRole("listbox")).toBeHidden();
  await page.getByRole("button", { name: "Plugins", exact: true }).click();
  await expect(addButton(page)).toHaveAttribute("aria-expanded", "false");
  await expect(onboarding.getByLabel("Frontend settings (JSON)")).toHaveValue(draft);
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("save persists configuration but only the matching completion event finishes apply", async ({ page }) => {
  await openPlugins(page, { finishJobs: false });
  const onboarding = row(page, "CARE Onboarding");
  await onboarding.getByRole("button", { name: "Edit CARE Onboarding", exact: true }).click();
  await onboarding.getByLabel("Frontend settings (JSON)").fill('{"config":{"auto_onboarding":false}}');
  await saveButton(page).click();
  await expect(root(page)).toContainText("Applying plugin changes…");
  await expect(root(page)).not.toContainText("Plugin task finished.");
  await expect(onboarding.getByLabel("Frontend settings (JSON)")).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  await expect(root(page).getByRole("button", { name: "Remove CARE Onboarding" })).toBeDisabled();
  expect(await countCalls(page, "SavePlugins")).toBe(1);
  expect(await countApply(page)).toBe(1);
  await page.evaluate(() => window.careTest.emit("care-done", 0, "backup-now"));
  await expect(root(page)).toContainText("Applying plugin changes…");
  await page.evaluate(() => window.careTest.finishJob("apply-plugins"));
  await expect(root(page)).toContainText("Plugin task finished.");
  await expect(root(page)).toContainText("If CARE is stopped");
  await expect(saveButton(page)).toBeEnabled();
  await expect(root(page)).not.toContainText("Plugins applied");
});

test("fixture persistence saves a copy without treating it as a completed apply", async ({ page }) => {
  await openPlugins(page, { saved: [], finishJobs: false });
  const editor = await addCustom(page);
  await fillCustom(editor);
  await editor.getByLabel("Frontend settings (JSON)").fill('{"config":{"saved":"fixture"}}');
  expect(await page.evaluate(() => window.careTest.fixtures.plugins)).toEqual([]);
  await saveButton(page).click();
  await expect(root(page)).toContainText("Applying plugin changes…");
  const saved = { ...custom, frontend: { ...custom.frontend!, meta: { config: { saved: "fixture" } } } };
  expect(await page.evaluate(() => window.careTest.fixtures.plugins)).toEqual([saved]);
  expect(await page.evaluate(async () => {
    const result = await window.go.main.App.ReadPlugins();
    result[0].label = "Only the returned copy changed";
    return window.careTest.fixtures.plugins[0].label;
  })).toBe(saved.label);
  await expect(root(page)).not.toContainText("Plugin task finished.");
  await page.evaluate(() => window.careTest.finishJob("apply-plugins"));
  await expect(root(page)).toContainText("Plugin task finished.");
});

for (const failure of ["rejected", "async"] as const) {
  test(`${failure} apply keeps saved choices and retry does not save them again`, async ({ page }) => {
    await openPlugins(page, { finishJobs: false });
    if (failure === "rejected") {
      await page.evaluate(() => window.careTest.failNext("ClinicAction", "exited 71: private source and config"));
    }
    await saveButton(page).click();
    if (failure === "async") {
      await expect(root(page)).toContainText("Applying plugin changes…");
      await page.evaluate(() => window.careTest.finishJob("apply-plugins", "exited 71: private source and config"));
    }
    await expect(root(page).getByRole("alert")).toContainText("Settings are saved, but applying didn't finish.");
    await expect(root(page)).not.toContainText("exited 71");
    await expect(row(page, "CARE Onboarding")).toBeVisible();
    await expect(root(page).getByRole("button", { name: "Try applying again", exact: true })).toBeEnabled();
    await root(page).getByRole("button", { name: "Try applying again", exact: true }).click();
    await expect(root(page)).toContainText("Applying plugin changes…");
    expect(await countCalls(page, "SavePlugins")).toBe(1);
    expect(await countApply(page)).toBe(2);
    await page.evaluate(() => window.careTest.finishJob("apply-plugins"));
    await expect(root(page)).toContainText("Plugin task finished.");
  });
}

test("restore-pending state keeps plugin actions disabled without a password bypass", async ({ page }) => {
  await openPlugins(page, { restorePending: true, ready: false });
  await expect(root(page)).toContainText("An earlier restore needs to finish.");
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  expect(await countCalls(page, "SavePlugins")).toBe(0);
  expect(await countCalls(page, "VerifyAdminPassword")).toBe(0);
});

test("another clinic job locks the editor and preserves its dirty data", async ({ page }) => {
  await openPlugins(page, { saved: [], finishJobs: false });
  const editor = await addCustom(page);
  await fillCustom(editor);
  await page.getByRole("button", { name: "Backups", exact: true }).click();
  await page.locator(".care-backups").getByRole("button", { name: "Back up now", exact: true }).click();
  await page.getByRole("button", { name: "Plugins", exact: true }).click();
  await expect(editor.getByLabel("Plugin ID", { exact: true })).toBeDisabled();
  await expect(editor.getByRole("switch", { name: "Backend", exact: true })).toBeDisabled();
  await expect(editor.getByRole("button", { name: "Add setting", exact: true })).toBeDisabled();
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  await page.evaluate(() => window.careTest.finishJob("backup-now"));
  await expect(saveButton(page)).toBeEnabled();
  await expect(editor.getByLabel("Plugin ID", { exact: true })).toHaveValue(custom.id);
  await expect(root(page)).toContainText("Unsaved changes");
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("a running restore locks mounted plugin drafts without losing their settings", async ({ page }) => {
  await openPlugins(page, { finishJobs: false });
  const onboarding = row(page, "CARE Onboarding");
  await onboarding.getByRole("button", { name: "Edit CARE Onboarding", exact: true }).click();
  const settings = root(page).getByLabel("Frontend settings (JSON)");
  const draft = '{"config":{"auto_onboarding":false}}';
  await settings.fill(draft);
  await page.getByRole("button", { name: "Backups", exact: true }).click();
  await page.locator(".care-backups").getByRole("button", { name: "Choose file", exact: true }).click();
  const dialog = page.getByRole("alertdialog");
  await dialog.getByRole("button", { name: "Choose recovery file", exact: true }).click();
  await dialog.getByLabel("CARE Desktop admin password", { exact: true }).fill("ClinicTest123");
  await dialog.getByRole("checkbox", { name: "I understand today's data will be replaced" }).check();
  await dialog.getByRole("button", { name: "Replace current data", exact: true }).click();
  await expect(settings).toBeDisabled();
  await expect(settings).toHaveValue(draft);
  expect(await countCalls(page, "SavePlugins")).toBe(0);
  await page.evaluate(() => window.careTest.finishJob("restore"));
  await expect(settings).toBeEnabled();
  await expect(settings).toHaveValue(draft);
});

test("CARE updates lock all plugin changes until completion", async ({ page }) => {
  await openPlugins(page, { saved: [], scenario: "panel-update", finishJobs: false });
  const editor = await addCustom(page);
  await fillCustom(editor);
  await page.evaluate(() => window.careTest.emit("care-update", {
    backend: "preview-backend-next", frontend: "",
  }));
  await page.locator(".panel-banner").getByRole("button", { name: "Install now", exact: true }).click();
  await expect(editor.getByLabel("Plugin ID", { exact: true })).toBeDisabled();
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  await page.evaluate(() => window.careTest.finishJob("update"));
  await expect(saveButton(page)).toBeEnabled();
  await expect(editor.getByLabel("Plugin ID", { exact: true })).toHaveValue(custom.id);
});

test("Desktop update handoff still blocks plugin writes after care-done", async ({ page }) => {
  await openPlugins(page);
  await page.evaluate(() => {
    window.careTest.progress({ phase: "restarting", done: 50_000_000, total: 50_000_000 });
    window.careTest.finishUpdate();
  });
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("catalog refresh takes authoritative sources while preserving operator settings", () => {
  const stale: CarePlugin = {
    id: "care_filly", label: "Old display name", catalog: true,
    backend: { name: "old_name", package_name: "old-package", configs: { KEEP: "true" } },
    frontend: { slug: "old_slug", url: "https://old.example/entry.js", meta: { config: { custom: true } } },
  };
  const refreshed = reconcileCatalog(toPluginRow(stale, 1), catalog);
  const saved = serializePlugins([refreshed])[0];
  expect(saved.backend).toEqual({ ...catalog[2].plugin.backend, configs: { KEEP: "true" } });
  expect(saved.frontend).toEqual({ ...catalog[2].plugin.frontend, meta: { config: { custom: true } } });
  expect(saved.label).toBe("Filly");
  const removed = reconcileCatalog(toPluginRow({ ...custom, catalog: true }, 2), catalog);
  expect(removed.catalog).toBe(false);
  expect(serializePlugins([removed])[0]).toEqual(custom);
});

test("saving existing settings preserves JSON types and special object keys", () => {
  const configs = JSON.parse('{"__proto__":{"retained":true},"text_boolean":"true","text_number":"123","null":null,"boolean":false,"number":42,"list":[1,2]}');
  const original: CarePlugin = { ...custom, backend: { ...custom.backend!, configs } };
  const draft = toPluginRow(original, 0);
  expect(serializePlugins([draft])[0]).toEqual(original);
  draft.backend!.configs.push({ key: "NEW_BOOLEAN", value: "true" }, { key: "NEW_LIST", value: "[1,2]" });
  expect(serializePlugins([draft])[0].backend!.configs).toEqual({ ...configs, NEW_BOOLEAN: true, NEW_LIST: [1, 2] });
  expect(Object.prototype).not.toHaveProperty("retained");
});

test("native-compatible combinations allow either part, optional versions, and local HTTP URLs", () => {
  for (const plugin of [
    { ...custom, frontend: undefined, backend: { ...custom.backend!, version: "" } },
    { ...custom, backend: undefined, frontend: { ...custom.frontend!, url: "http://localhost:4178/plugin.mjs" } },
    { ...custom, id: "example.ID-with_parts", backend: { ...custom.backend!, version: ">=1.2" } },
  ]) {
    const draft = toPluginRow(plugin, 0);
    expect(hasPluginProblems(pluginProblems(draft, [draft]))).toBe(false);
  }
});
