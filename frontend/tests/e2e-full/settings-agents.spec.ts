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

test("settings preserves quoted empty workspace agent arguments", async ({
  page,
}) => {
  await page.goto(`${isolatedServer!.info.base_url}/settings`);
  await page.locator(".settings-page")
    .waitFor({ state: "visible", timeout: 10_000 });

  const argsInput = page.getByLabel("Codex arguments");
  const saveButton = page.getByRole("button", { name: "Save agents" });

  await argsInput.fill("\"\"");
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
      throw new Error("settings agents API context not initialized");
    }
    const response = await api.get("/api/v1/settings");
    const settings = await response.json() as {
      agents: Array<{ key: string; command: string[] }>;
    };
    return settings.agents.find((agent) => agent.key === "codex")?.command;
  }).toEqual(["codex", ""]);

  await page.reload();
  await page.locator(".settings-page")
    .waitFor({ state: "visible", timeout: 10_000 });
  await expect(page.getByLabel("Codex arguments")).toHaveValue("\"\"");
});
