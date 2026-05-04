import {
  expect,
  request as playwrightRequest,
  test,
  type APIRequestContext,
} from "@playwright/test";
import {
  startIsolatedE2EServer,
  type IsolatedE2EServer,
} from "./support/e2eServer";

let isolatedServer: IsolatedE2EServer | undefined;
let api: APIRequestContext | undefined;

test.beforeAll(async () => {
  isolatedServer = await startIsolatedE2EServer();
  api = await playwrightRequest.newContext({
    baseURL: isolatedServer.info.base_url,
  });
});

test.afterAll(async () => {
  await api?.dispose();
  await isolatedServer?.stop();
});

test("settings saves and reloads workspace terminal options", async ({
  page,
}) => {
  await page.goto(`${isolatedServer!.info.base_url}/settings`);
  await page.locator(".settings-page")
    .waitFor({ state: "visible", timeout: 10_000 });

  const input = page.getByLabel("Monospace font family");
  const renderer = page.getByLabel("Terminal renderer");
  const saveButton = page.getByRole("button", { name: "Save", exact: true });
  await expect(input).toHaveValue("");
  await expect(renderer).toHaveValue("xterm");

  await renderer.selectOption("ghostty-web");
  await input.click();
  await input.pressSequentially(
    "\"Iosevka Term\", monospace",
  );
  await expect(saveButton).toBeEnabled();
  const saveResponsePromise = page.waitForResponse((response) =>
    response.url().endsWith("/api/v1/settings") &&
    response.request().method() === "PUT"
  );
  await saveButton.click();
  const saveResponse = await saveResponsePromise;
  const saveBody = await saveResponse.text();
  expect(
    saveResponse.status(),
    `PUT /api/v1/settings failed: ${saveBody}`,
  ).toBe(200);

  await expect.poll(async () => {
    if (!api) {
      throw new Error("settings terminal API context not initialized");
    }
    const response = await api.get("/api/v1/settings");
    const settings = await response.json() as {
      terminal: { font_family: string; renderer: string };
    };
    return settings.terminal;
  }).toEqual({
    font_family: "\"Iosevka Term\", monospace",
    renderer: "ghostty-web",
  });

  await page.reload();
  await page.locator(".settings-page")
    .waitFor({ state: "visible", timeout: 10_000 });
  await expect(page.getByLabel("Monospace font family"))
    .toHaveValue("\"Iosevka Term\", monospace");
  await expect(page.getByLabel("Terminal renderer"))
    .toHaveValue("ghostty-web");
});
