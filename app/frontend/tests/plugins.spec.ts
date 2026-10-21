import { mkdir } from "node:fs/promises";
import { join, relative, resolve } from "node:path";
import { expect, test, type Locator, type Page } from "@playwright/test";
import type {} from "./fixtures/host";
import type { CarePlugin, PluginCatalogEntry } from "../src/types";
import {
  describeChanges, hasPluginProblems, pluginChanges, pluginProblems, reconcileCatalog, requiredConfig,
  serializePlugins, settingValue, toPluginRow, withSetting,
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
];
const filly: PluginCatalogEntry = {
  plugin: {
    id: "care_filly", label: "Filly", catalog: true,
    backend: {
      name: "care_filly",
      package_name: "git+https://github.com/ohcnetwork/care_filly.git",
      version: "@develop",
      configs: { MEDISPEAK_API_KEY: "", MEDISPEAK_BASE_URL: "" },
    },
  },
  description: "AI scribe: Medispeak dictation that fills CARE forms.",
  default: false,
  required_configs: ["MEDISPEAK_API_KEY", "MEDISPEAK_BASE_URL"],
  settings: [
    { part: "backend", key: "MEDISPEAK_API_KEY", label: "Medispeak API key", type: "secret" },
    { part: "backend", key: "MEDISPEAK_BASE_URL", label: "Medispeak address", type: "url" },
  ],
  setup_steps: [{ text: "After applying, turn on Filly's screens from CARE's plugin marketplace.", action: "care" }],
};
const custom: CarePlugin = {
  id: "custom_example", label: "Custom example",
  backend: { name: "care_example.apps", package_name: "care-example", version: "==1.2" },
  frontend: { slug: "care_example_fe", url: "http://localhost:4178/assets/remoteEntry.js", meta: { config: {} } },
};
const root = (page: Page) => page.locator(".care-plugins");
const saveButton = (page: Page) => root(page).getByRole("button", { name: /^(Save and apply|Try applying again)$/ });
const leaveDialog = (page: Page) => page.getByRole("alertdialog", { name: "Your plugin changes aren't saved" });
const unsaved = /\d+ unsaved changes?/;
const optionNames = (page: Page) => page.getByRole("option").locator(".care-plugins-picker-name");
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

async function expectSaveBlocked(page: Page) {
  const before = await countCalls(page, "SavePlugins");
  await saveButton(page).click();
  await expect(root(page).locator(".care-plugins-savebar")).toContainText("before saving");
  expect(await countCalls(page, "SavePlugins")).toBe(before);
}

async function leaveTo(page: Page, tab: string, choice: "Keep editing" | "Discard changes") {
  await page.getByRole("button", { name: tab, exact: true }).click();
  await expect(leaveDialog(page)).toBeVisible();
  await leaveDialog(page).getByRole("button", { name: choice, exact: true }).click();
  await expect(leaveDialog(page)).toBeHidden();
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
    await openPlugins(page);
    await expect(root(page).locator(".care-plugin-row")).toHaveCount(1);
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
    await expect(row(page, "CARE Onboarding").getByText("Will be removed", { exact: true })).toBeVisible();
    await assertFits(page);
    await capture(page, `plugins-remove-${viewport.width}x${viewport.height}`);
    await page.getByRole("button", { name: "Overview", exact: true }).click();
    await expect(leaveDialog(page)).toBeVisible();
    await assertFits(page);
    await capture(page, `plugins-leave-${viewport.width}x${viewport.height}`);
    await leaveDialog(page).getByRole("button", { name: "Keep editing", exact: true }).click();
    await root(page).getByRole("button", { name: "Undo removing CARE Onboarding", exact: true }).click();
    await expect(editor.getByLabel("Setting 1 value", { exact: true })).toHaveValue("A long example configuration value for layout verification.");
  });
}

test("loading is not an empty list and requests are single-flight", async ({ page }) => {
  await openPlugins(page, { hold: "ReadPlugins", ready: false });
  await expect(root(page).locator(".care-plugins-loading")).toContainText("Loading plugins");
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  await expect(root(page).getByText(/^No plugins yet/)).toHaveCount(0);
  expect(await countCalls(page, "ReadPlugins")).toBe(1);
  expect(await countCalls(page, "PluginCatalog")).toBe(1);
  await page.evaluate(() => window.careTest.release("ReadPlugins"));
  await expect(addButton(page)).toBeEnabled();
  await expect(row(page, "CARE Onboarding")).toBeVisible();
  await expect(saveButton(page)).toBeDisabled();
  await expect(root(page).locator(".care-plugins-savebar")).toContainText("No unsaved changes");
});

for (const method of ["ReadPlugins", "PluginCatalog"] as const) {
  test(`${method} failure is honest, retryable, and keeps the other successful read`, async ({ page }) => {
    await openPlugins(page, { fail: method, ready: false });
    await expect(root(page).getByRole("alert")).toContainText(method === "ReadPlugins"
      ? "Couldn't read the saved plugins" : "Couldn't read the plugin catalog");
    await expect(root(page)).not.toContainText("private config");
    await expect(root(page).getByText(/^No plugins yet/)).toHaveCount(0);
    await expect(saveButton(page)).toBeDisabled();
    await expect(addButton(page)).toBeDisabled();
    if (method === "PluginCatalog") await expect(row(page, "CARE Onboarding")).toBeVisible();
    await root(page).getByRole("button", { name: "Retry", exact: true }).click();
    await expect(addButton(page)).toBeEnabled();
    await expect(row(page, "CARE Onboarding")).toBeVisible();
    expect(await countCalls(page, method)).toBe(2);
    expect(await countCalls(page, method === "ReadPlugins" ? "PluginCatalog" : "ReadPlugins")).toBe(1);
    expect(await countCalls(page, "SavePlugins")).toBe(0);
  });
}

test("empty saved choices stay empty and unsaved removal needs no confirmation", async ({ page }) => {
  await openPlugins(page, { saved: [] });
  await expect(root(page).getByText(/^No plugins yet/)).toBeVisible();
  await addCustom(page);
  await root(page).getByRole("button", { name: "Remove New plugin", exact: true }).click();
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await expect(root(page).getByText(/^No plugins yet/)).toBeVisible();
  await expect(saveButton(page)).toBeDisabled();
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("custom fields mirror native rules and report accessible errors without parser traces", async ({ page }) => {
  await openPlugins(page, { saved: [] });
  const editor = await addCustom(page);
  await expect(editor.getByLabel("Plugin ID", { exact: true })).not.toHaveAttribute("aria-invalid", "true");
  await expectSaveBlocked(page);
  await expect(editor.getByLabel("Plugin ID", { exact: true })).toHaveAttribute("aria-invalid", "true");
  await fillCustom(editor);
  await expect(root(page).locator(".care-plugins-savebar")).not.toContainText("before saving");
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
    await input.blur();
    await expect(input).toHaveAttribute("aria-invalid", "true");
    const description = await input.getAttribute("aria-describedby");
    expect(description).toBeTruthy();
    await expect(page.locator(`#${description}`)).toBeVisible();
    await expect(root(page).locator(".care-plugins-savebar")).toContainText("before saving");
    await input.fill(valid);
    await expect(input).not.toHaveAttribute("aria-invalid", "true");
    await expect(root(page).locator(".care-plugins-savebar")).not.toContainText("before saving");
  }
  await expect(root(page)).not.toContainText("SyntaxError");
  await editor.getByRole("switch", { name: "Backend", exact: true }).uncheck();
  await editor.getByRole("switch", { name: "Frontend", exact: true }).uncheck();
  await expect(editor).toContainText("Choose Backend, Frontend, or both.");
  await expectSaveBlocked(page);
  await editor.getByRole("switch", { name: "Backend", exact: true }).check();
  await expect(editor.getByLabel("Python module", { exact: true })).toHaveValue(custom.backend!.name);
  await expect(root(page).locator(".care-plugins-savebar")).not.toContainText("before saving");
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("duplicate IDs, modules, frontend names, and setting names cannot overwrite one another", async ({ page }) => {
  await openPlugins(page, { saved: [custom] });
  const editor = await addCustom(page);
  await fillCustom(editor);
  await expectSaveBlocked(page);
  await expect(editor).toContainText("This plugin ID is already in use.");
  await expect(editor).toContainText("This backend module is already in use.");
  await expect(editor).toContainText("This frontend name is already in use.");
  await editor.getByLabel("Plugin ID", { exact: true }).fill("custom_other");
  await editor.getByLabel("Python module", { exact: true }).fill("care_other");
  await expect(editor).toContainText("This frontend name is already in use.");
  await editor.getByLabel("Frontend name", { exact: true }).fill("care_other_fe");
  await expect(root(page).locator(".care-plugins-savebar")).not.toContainText("before saving");
  await editor.getByRole("button", { name: "Add setting", exact: true }).click();
  await editor.getByRole("button", { name: "Add setting", exact: true }).click();
  await editor.getByLabel("Setting 1 name", { exact: true }).fill("EXAMPLE");
  await editor.getByLabel("Setting 2 name", { exact: true }).fill(" EXAMPLE ");
  await expect(editor.getByText("This setting name is already in use.", { exact: true })).toHaveCount(2);
  await expect(root(page).locator(".care-plugins-savebar")).toContainText("before saving");
  await editor.getByRole("button", { name: /Remove setting 2 from/ }).click();
  await expect(root(page).locator(".care-plugins-savebar")).not.toContainText("before saving");
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("catalog choices respect custom plugin identities as well as catalog IDs", async ({ page }) => {
  await openPlugins(page, { saved: [{
    ...custom, id: "custom_id",
    frontend: { ...custom.frontend!, slug: "care_onboarding_fe" },
  }] });
  await addButton(page).click();
  await expect(page.getByRole("option", { name: "CARE Onboarding", exact: true })).toHaveCount(0);
  await expect(optionNames(page)).toHaveText(["Custom plugin"]);
  await page.keyboard.press("Escape");
});

test("an expanded catalog plugin shows its whole description and its setup steps", async ({ page }) => {
  await openPlugins(page, { saved: [{ ...filly.plugin, backend: { ...filly.plugin.backend!, configs: {
    MEDISPEAK_API_KEY: "msk_live_kept", MEDISPEAK_BASE_URL: "https://medispeak.example/api/v2",
  } } }], catalog: [...catalog, filly] });
  const editor = row(page, "Filly");
  await editor.getByRole("button", { name: "Edit Filly", exact: true }).click();
  await expect(editor.getByText(filly.description!, { exact: true })).not.toHaveClass(/truncate/);
  const steps = editor.getByRole("note", { name: "Also needed for Filly" });
  await expect(steps).toContainText(filly.setup_steps![0].text);
  await steps.getByRole("button", { name: "Open CARE" }).click();
  expect(await countCalls(page, "OpenURL")).toBe(1);
});

test("the catalog offers only onboarding alongside custom plugins", async ({ page }) => {
  await openPlugins(page, { saved: [] });
  await addButton(page).click();
  await expect(optionNames(page)).toHaveText(["CARE Onboarding", "Custom plugin"]);
});

test("failed saves keep every edit and leaving asks before discarding them", async ({ page }) => {
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
  await leaveTo(page, "Overview", "Keep editing");
  await expect(root(page)).toBeVisible();
  await expect(editor.getByLabel("Frontend settings (JSON)")).toHaveValue('{"config":{"example":"keep this draft"}}');
  await expect(root(page)).toContainText(unsaved);
  await expect(page.locator(".care-panel-rail-unsaved")).toBeVisible();
  await leaveTo(page, "Overview", "Discard changes");
  await expect(root(page)).toBeHidden();
  await expect(page.locator(".care-panel-rail-unsaved")).toHaveCount(0);
  await page.getByRole("button", { name: "Plugins", exact: true }).click();
  await expect(root(page).getByText(/^No plugins yet/)).toBeVisible();
  expect(await countCalls(page, "ReadPlugins")).toBe(1);
  expect(await countCalls(page, "PluginCatalog")).toBe(1);
  expect(await page.evaluate(() => window.careTest.logs.join("\n"))).not.toContain("keep this draft");
});

test("saved removal stays visible, can be undone and keeps earlier edits", async ({ page }) => {
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
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  const undo = onboarding.getByRole("button", { name: "Undo removing CARE Onboarding", exact: true });
  await expect(undo).toBeFocused();
  await expect(onboarding.getByText("Will be removed", { exact: true })).toBeVisible();
  await expect(root(page).locator(".care-plugins-savebar")).toContainText("Remove CARE Onboarding.");
  await page.keyboard.press("Enter");
  await expect(onboarding.getByText("Will be removed", { exact: true })).toHaveCount(0);
  await toggle.click();
  await expect(onboarding.getByLabel("Frontend settings (JSON)")).toHaveValue('{"config":{"auto_onboarding":false}}');
  await expect(root(page).locator(".care-plugins-savebar")).toContainText("Change CARE Onboarding.");
  await remove.click();
  await saveButton(page).click();
  await expect(root(page)).toContainText("Plugin changes are applied.");
  await expect(onboarding).toHaveCount(0);
  expect(await page.evaluate(() => window.careTest.calls.find((call) => call.method === "SavePlugins")?.args[0])).toEqual([]);
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
  await expectSaveBlocked(page);
  await expect(root(page).getByLabel("Plugin ID", { exact: true })).toBeFocused();
});

test("tab deactivation closes plugin portals and a clean list leaves without asking", async ({ page }) => {
  await openPlugins(page);
  const onboarding = row(page, "CARE Onboarding");
  await onboarding.getByRole("button", { name: "Edit CARE Onboarding", exact: true }).click();
  const draft = JSON.stringify(catalog[0].plugin.frontend!.meta, null, 2);
  await expect(onboarding.getByLabel("Frontend settings (JSON)")).toHaveValue(draft);
  await addButton(page).click();
  await expect(page.getByRole("listbox")).toBeVisible();
  await page.getByRole("button", { name: "Overview", exact: true, includeHidden: true })
    .evaluate((button: HTMLButtonElement) => button.click());
  await expect(page.getByRole("listbox")).toBeHidden();
  await page.getByRole("button", { name: "Plugins", exact: true }).click();
  await expect(addButton(page)).toHaveAttribute("aria-expanded", "false");
  await expect(leaveDialog(page)).toHaveCount(0);
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
  await expect(root(page)).not.toContainText("Plugin changes are applied.");
  await expect(onboarding.getByLabel("Frontend settings (JSON)")).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  await expect(root(page).getByRole("button", { name: "Remove CARE Onboarding" })).toBeDisabled();
  expect(await countCalls(page, "SavePlugins")).toBe(1);
  expect(await countApply(page)).toBe(1);
  await page.evaluate(() => window.careTest.emit("care-done", 0, "backup-now"));
  await expect(root(page)).toContainText("Applying plugin changes…");
  await page.evaluate(() => window.careTest.finishJob("apply-plugins"));
  await expect(root(page)).toContainText("Plugin changes are applied.");
  await expect(root(page)).toContainText("CARE is online");
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeEnabled();
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
  expect(await page.evaluate(() => window.careTest.fixtures.plugins)).toEqual([]);
  await page.evaluate(() => window.careTest.finishJob("apply-plugins"));
  await expect(root(page)).toContainText("Plugin changes are applied.");
  expect(await page.evaluate(() => window.careTest.fixtures.plugins)).toEqual([saved]);
  expect(await page.evaluate(async () => {
    const result = await window.go.main.App.ReadPlugins();
    result[0].label = "Only the returned copy changed";
    return window.careTest.fixtures.plugins[0].label;
  })).toBe(saved.label);
});

for (const failure of ["rejected", "async"] as const) {
  test(`${failure} apply keeps the draft and retry stages it again`, async ({ page }) => {
    await openPlugins(page, { finishJobs: false });
    await root(page).getByRole("button", { name: "Edit CARE Onboarding", exact: true }).click();
    await root(page).getByLabel("Frontend settings (JSON)").fill('{"config":{"auto_onboarding":false}}');
    if (failure === "rejected") {
      await page.evaluate(() => window.careTest.failNext("ClinicAction", "exited 71: private source and config"));
    }
    await saveButton(page).click();
    if (failure === "async") {
      await expect(root(page)).toContainText("Applying plugin changes…");
      await page.evaluate(() => window.careTest.finishJob("apply-plugins", "exited 71: private source and config"));
    }
    await expect(root(page).getByRole("alert")).toContainText("The plugin changes couldn't be applied.");
    await expect(root(page)).not.toContainText("exited 71");
    await expect(row(page, "CARE Onboarding")).toBeVisible();
    await expect(saveButton(page)).toBeEnabled();
    await saveButton(page).click();
    await expect(root(page)).toContainText("Applying plugin changes…");
    expect(await countCalls(page, "SavePlugins")).toBe(2);
    expect(await countApply(page)).toBe(2);
    await page.evaluate(() => window.careTest.finishJob("apply-plugins"));
    await expect(root(page)).toContainText("Plugin changes are applied.");
  });
}

for (const failure of ["rejected", "async"] as const) {
  test(`${failure} three-plugin batch is discarded on tab change without removing applied plugins`, async ({ page }) => {
    await openPlugins(page, { finishJobs: false });
    for (const suffix of ["one", "two", "three"]) {
      const editor = await addCustom(page);
      await fillCustom(editor);
      await editor.getByLabel("Display name", { exact: true }).fill(`Custom ${suffix}`);
      await editor.getByLabel("Plugin ID", { exact: true }).fill(`custom_${suffix}`);
      await editor.getByLabel("Python module", { exact: true }).fill(`care_${suffix}`);
      await editor.getByLabel("Frontend name", { exact: true }).fill(`care_${suffix}_fe`);
    }
    await expect(root(page).getByText("New · not applied", { exact: true })).toHaveCount(3);
    if (failure === "rejected") {
      await page.evaluate(() => window.careTest.failNext("ClinicAction", "something else is still running"));
    }
    await saveButton(page).click();
    if (failure === "async") {
      await expect(root(page)).toContainText("Applying plugin changes");
      await page.evaluate(() => window.careTest.finishJob("apply-plugins", "plugin loading failed; previous settings were restored and CARE is back online: failed batch"));
    }
    await expect(root(page).getByRole("alert")).toContainText("These changes were not applied");
    await expect(root(page).locator(".care-plugin-row")).toHaveCount(4);
    await leaveTo(page, "Overview", "Discard changes");
    await page.getByRole("button", { name: "Plugins", exact: true }).click();
    await expect(root(page).locator(".care-plugin-row")).toHaveCount(1);
    await expect(row(page, "CARE Onboarding")).toBeVisible();
    await expect(root(page).getByText(/not applied/)).toHaveCount(0);
    await expect(root(page)).not.toContainText(unsaved);
    expect(await countCalls(page, "SavePlugins")).toBe(1);
    expect(await page.evaluate(() => window.careTest.fixtures.plugins.map((p) => p.id))).toEqual(["care_onboarding_fe"]);
    await addButton(page).click();
    await expect(optionNames(page)).toHaveText(["Custom plugin"]);
  });
}

for (const success of [false, true]) {
  test(`apply finishing on another tab ${success ? "keeps successful plugins" : "discards failed plugins"}`, async ({ page }) => {
    await openPlugins(page, { saved: [], finishJobs: false });
    await addButton(page).click();
    await page.getByRole("option", { name: "CARE Onboarding", exact: true }).click();
    await saveButton(page).click();
    await expect(root(page)).toContainText("Applying plugin changes");
    await page.getByRole("button", { name: "Overview", exact: true }).click();
    await page.evaluate((success) => window.careTest.finishJob("apply-plugins", success ? undefined : "plugin load failed"), success);
    await page.getByRole("button", { name: "Plugins", exact: true }).click();
    await expect(root(page).locator(".care-plugin-row")).toHaveCount(success ? 1 : 0);
    await expect(root(page).getByText(/not applied/)).toHaveCount(0);
    await expect(root(page)).not.toContainText(unsaved);
  });
}

test("discarding a failed batch restores edits and removals to the latest successful list", async ({ page }) => {
  await openPlugins(page, { finishJobs: false });
  await fillCustom(await addCustom(page));
  await saveButton(page).click();
  await expect(root(page)).toContainText("Applying plugin changes");
  await page.evaluate(() => window.careTest.finishJob("apply-plugins"));
  await expect(root(page)).toContainText("Plugin changes are applied");
  await expect(row(page, "Custom example").getByText("Active", { exact: true })).toBeVisible();

  const onboarding = row(page, "CARE Onboarding");
  await onboarding.getByRole("button", { name: "Edit CARE Onboarding", exact: true }).click();
  await onboarding.getByLabel("Frontend settings (JSON)").fill('{"config":{"auto_onboarding":false}}');
  await root(page).getByRole("button", { name: "Remove Custom example", exact: true }).click();
  await saveButton(page).click();
  await expect(root(page)).toContainText("Applying plugin changes");
  await page.evaluate(() => window.careTest.finishJob("apply-plugins", "plugin load failed"));
  await expect(root(page).getByRole("alert")).toContainText("These changes were not applied");
  await expect(row(page, "Custom example").getByText("Will be removed", { exact: true })).toBeVisible();
  await root(page).locator(".care-plugins-savebar").getByRole("button", { name: "Discard changes", exact: true }).click();
  await expect(root(page).locator(".care-plugin-row")).toHaveCount(2);
  await expect(row(page, "Custom example")).toBeVisible();
  await onboarding.getByRole("button", { name: "Edit CARE Onboarding", exact: true }).click();
  await expect(onboarding.getByLabel("Frontend settings (JSON)")).toHaveValue(/"auto_onboarding": true/);
  await expect(root(page)).not.toContainText(unsaved);
  await expect(root(page).getByRole("alert")).toHaveCount(0);
  expect(await countCalls(page, "SavePlugins")).toBe(2);
});

test("restore-pending state keeps plugin actions disabled without a password bypass", async ({ page }) => {
  await openPlugins(page, { restorePending: true, ready: false });
  await expect(root(page)).toContainText("An earlier restore needs to finish.");
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  expect(await countCalls(page, "SavePlugins")).toBe(0);
  expect(await countCalls(page, "VerifyAdminPassword")).toBe(0);
});

for (const recovered of [true, false]) {
  test(`plugin failure reports ${recovered ? "successful rollback" : "unfinished recovery"} without leaking native details`, async ({ page }) => {
    await openPlugins(page, { finishJobs: false });
    await fillCustom(await addCustom(page));
    await saveButton(page).click();
    await expect(root(page)).toContainText("Applying plugin changes");
    await page.evaluate((recovered) => {
      window.careTest.state.plugin_recovery_pending = !recovered;
      window.careTest.finishJob("apply-plugins", recovered
        ? "plugin loading failed; previous settings were restored and CARE is back online: private-token"
        : "plugin loading failed (private-token). Plugin rollback is unfinished; CARE could not be recovered. Start CARE to retry recovery: private-path");
    }, recovered);
    await expect(root(page).getByRole("alert")).toContainText(recovered ? "CARE is back online" : "CARE has not been confirmed online");
    await expect(root(page)).not.toContainText("private-token");
    await expect(root(page)).toContainText(unsaved);
    await expect(row(page, "Custom example")).toBeVisible();
    expect(await page.evaluate(() => window.careTest.fixtures.plugins.map((p) => p.id))).toEqual(["care_onboarding_fe"]);
    if (!recovered) {
      await expect(saveButton(page)).toBeDisabled();
      await page.getByRole("button", { name: "Overview", exact: true }).click();
      await expect(page.getByRole("button", { name: "Recover clinic", exact: true })).toBeEnabled();
      await page.getByRole("button", { name: "Recover clinic", exact: true }).click();
      expect(await page.evaluate(() => window.careTest.calls.filter((call) => call.method === "ClinicAction").slice(-1)[0]?.args[0])).toBe("start");
    }
  });
}

test("an outside lock disables the editor and preserves dirty data", async ({ page }) => {
  await openPlugins(page, { saved: [], finishJobs: false });
  const editor = await addCustom(page);
  await fillCustom(editor);
  await leaveTo(page, "Backups", "Keep editing");
  await page.evaluate(() => window.careTest.progress({ phase: "verifying", done: 50_000_000, total: 50_000_000 }));
  await expect(editor.getByLabel("Plugin ID", { exact: true })).toBeDisabled();
  await expect(editor.getByRole("switch", { name: "Backend", exact: true })).toBeDisabled();
  await expect(editor.getByRole("button", { name: "Add setting", exact: true })).toBeDisabled();
  await expect(saveButton(page)).toBeDisabled();
  await expect(addButton(page)).toBeDisabled();
  await expect(page.locator(".care-panel-rail-unsaved")).toBeVisible();
  await page.evaluate(() => window.careTest.finishUpdate("preview verification failure"));
  await expect(saveButton(page)).toBeEnabled();
  await expect(editor.getByLabel("Plugin ID", { exact: true })).toHaveValue(custom.id);
  await expect(root(page)).toContainText(unsaved);
  expect(await countCalls(page, "SavePlugins")).toBe(0);
});

test("a running restore locks the plugin editor until it finishes", async ({ page }) => {
  await openPlugins(page, { finishJobs: false });
  const onboarding = row(page, "CARE Onboarding");
  await onboarding.getByRole("button", { name: "Edit CARE Onboarding", exact: true }).click();
  const settings = root(page).getByLabel("Frontend settings (JSON)");
  const draft = JSON.stringify(catalog[0].plugin.frontend!.meta, null, 2);
  await page.getByRole("button", { name: "Backups", exact: true }).click();
  await page.locator(".care-backups").getByRole("button", { name: "Choose file", exact: true }).click();
  const dialog = page.getByRole("alertdialog");
  await dialog.getByRole("button", { name: "Choose recovery file", exact: true }).click();
  await dialog.getByLabel("CARE Clinic admin password", { exact: true }).fill("ClinicTest123");
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
    id: "care_onboarding_fe", label: "Old display name", catalog: true,
    backend: { name: "old_name", package_name: "old-package", configs: { KEEP: "true" } },
    frontend: { slug: "old_slug", url: "https://old.example/entry.js", meta: { config: { custom: true } } },
  };
  const refreshed = reconcileCatalog(toPluginRow(stale, 1), catalog);
  const saved = serializePlugins([refreshed])[0];
  expect(saved.backend).toBeUndefined();
  expect(saved.frontend).toEqual({ ...catalog[0].plugin.frontend, meta: { config: { custom: true } } });
  expect(saved.label).toBe("CARE Onboarding");
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

test("Filly cannot be saved until its required settings are entered", async ({ page }) => {
  await openPlugins(page, { saved: [], catalog: [...catalog, filly] });
  await addButton(page).click();
  await page.getByRole("option", { name: "Filly", exact: true }).click();
  const editor = row(page, "Filly");
  const key = editor.getByLabel("Medispeak API key (required)", { exact: true });
  const base = editor.getByLabel("Medispeak address (required)", { exact: true });
  await expect(editor.getByText("Needs setup", { exact: true })).toBeVisible();
  await expect(key).toHaveAttribute("type", "password");
  await expect(base).not.toHaveAttribute("type", "password");
  await editor.getByRole("button", { name: "Show Medispeak API key", exact: true }).click();
  await expect(key).toHaveAttribute("type", "text");
  await expect(key).not.toHaveAttribute("aria-invalid", "true");
  await expect(editor.getByLabel("Setting 1 name", { exact: true })).toHaveCount(0);
  await expectSaveBlocked(page);
  await expect(key).toHaveAttribute("aria-invalid", "true");
  await expect(key).toBeFocused();
  await expect(editor.getByText("Enter a value; Filly needs this setting.")).toHaveCount(2);

  await key.fill("msk_live_example");
  await base.fill("   ");
  await expect(editor.getByText("Enter a value; Filly needs this setting.")).toHaveCount(1);
  await base.fill("https://medispeak.example/api/v2");
  await expect(editor.getByText("Enter a value; Filly needs this setting.")).toHaveCount(0);
  await expect(editor.getByText("New · not applied", { exact: true })).toBeVisible();
  await editor.getByRole("button", { name: "Technical details", exact: true }).click();
  await editor.getByRole("button", { name: "Add setting", exact: true }).click();
  await editor.getByLabel("Setting 3 name", { exact: true }).fill("EXTRA");
  await editor.getByLabel("Setting 3 value", { exact: true }).fill("kept");
  await expect(editor.getByRole("button", { name: "Remove setting 3 from Filly", exact: true })).toBeVisible();
  await saveButton(page).click();
  await expect(root(page)).toContainText("Plugin changes are applied.");
  expect(await page.evaluate(() => window.careTest.calls.find((call) => call.method === "SavePlugins")?.args[0])).toEqual([{
    ...filly.plugin,
    backend: {
      ...filly.plugin.backend!,
      configs: { MEDISPEAK_API_KEY: "msk_live_example", MEDISPEAK_BASE_URL: "https://medispeak.example/api/v2", EXTRA: "kept" },
    },
  }]);
});

test("described settings edit typed values in place and keep text as text", () => {
  const faster: PluginCatalogEntry = {
    plugin: { id: "faster", label: "Faster", catalog: true, frontend: {
      slug: "faster", url: "https://example.com/remoteEntry.js",
      meta: { config: { auto: true, methods: ["cash"] } },
    } },
    settings: [
      { part: "frontend", key: "config.auto", label: "Auto", type: "boolean" },
      { part: "frontend", key: "config.methods", label: "Methods", type: "choices",
        options: [{ value: "cash", label: "Cash" }, { value: "ddpo", label: "Direct deposit" }] },
    ],
  };
  let draft = toPluginRow(faster.plugin, 0);
  const [auto, methods] = faster.settings!;
  expect(settingValue(draft, auto)).toBe(true);
  draft = withSetting(withSetting(draft, auto, false), methods, ["cash", "ddpo"]);
  expect(serializePlugins([draft])[0].frontend!.meta).toEqual({ config: { auto: false, methods: ["cash", "ddpo"] } });

  let fillyDraft = toPluginRow(filly.plugin, 1);
  fillyDraft = withSetting(fillyDraft, filly.settings![0], "123");
  expect(serializePlugins([fillyDraft])[0].backend!.configs!.MEDISPEAK_API_KEY).toBe("123");
});

test("saved removals and edits are summarised and excluded from the saved list", () => {
  const applied = [toPluginRow(catalog[0].plugin, 0), toPluginRow(custom, 1)];
  const rows = [{ ...applied[0], removed: true }, { ...applied[1], label: "Renamed" }, toPluginRow(filly.plugin, 2)];
  const changes = pluginChanges(rows, applied);
  expect(describeChanges(changes)).toBe("Add Filly, remove CARE Onboarding and change Renamed.");
  expect(serializePlugins(rows).map((plugin) => plugin.id)).toEqual(["custom_example", "care_filly"]);
  expect(hasPluginProblems(pluginProblems(rows[0], rows))).toBe(false);
});

test("required catalog settings survive catalog refresh and come back if missing", () => {
  const saved: CarePlugin = {
    ...filly.plugin, label: "Old name",
    backend: { ...filly.plugin.backend!, version: "@main", configs: { MEDISPEAK_API_KEY: "msk_live_kept", OTHER: 1 } },
  };
  const refreshed = reconcileCatalog(toPluginRow(saved, 1), [filly]);
  expect(refreshed.backend!.version).toBe("@develop");
  expect(refreshed.backend!.configs.map(({ key, value }) => [key, value])).toEqual([
    ["MEDISPEAK_API_KEY", "msk_live_kept"], ["OTHER", "1"], ["MEDISPEAK_BASE_URL", ""],
  ]);
  const problems = pluginProblems(refreshed, [refreshed], [filly]);
  expect(problems.configValues).toEqual([undefined, undefined, "Enter a value; Filly needs this setting."]);
  expect(hasPluginProblems(problems)).toBe(true);
  expect(hasPluginProblems(pluginProblems(refreshed, [refreshed]))).toBe(false);
  expect(serializePlugins([refreshed])[0].backend!.configs).toEqual({ MEDISPEAK_API_KEY: "msk_live_kept", OTHER: 1, MEDISPEAK_BASE_URL: "" });

  const duplicate = { ...refreshed, backend: { ...refreshed.backend!, configs: [
    ...refreshed.backend!.configs, { key: "MEDISPEAK_API_KEY", value: "" },
  ] } };
  expect([0, 1, 2, 3].map((index) => requiredConfig(duplicate, [filly], index))).toEqual([true, false, true, false]);
  const asCustom = { ...refreshed, catalog: false };
  expect(requiredConfig(asCustom, [filly], 0)).toBe(false);
});
