import { devices, expect, test, type Page } from "@playwright/test";

test.use({ ...devices["iPhone 13"] });

async function expectReadableFocusList(
  page: Page,
  itemSelector: string,
): Promise<void> {
  await expect(page.locator(itemSelector).first()).toBeVisible();

  const metrics = await page.evaluate((selector) => {
    const fontSize = (node: Element | null): number => node
      ? Number.parseFloat(getComputedStyle(node).fontSize)
      : 0;
    const rect = (node: Element | null): DOMRect | null => node?.getBoundingClientRect() ?? null;
    const compactRect = (node: Element | null) => {
      const r = rect(node);
      return r ? { left: r.left, right: r.right, height: r.height } : null;
    };
    const item = document.querySelector(selector);
    const title = item?.querySelector(".title") ?? null;
    const meta = item?.querySelector(".meta-left") ?? null;
    const search = document.querySelector(".focus-list .search-input");
    const stateButton = document.querySelector(".focus-list .state-btn");
    const focusList = document.querySelector(".focus-list");
    const tokenValue = (node: Element | null, name: string): string => node
      ? getComputedStyle(node).getPropertyValue(name).trim()
      : "";

    return {
      viewportWidth: window.innerWidth,
      documentWidth: document.documentElement.scrollWidth,
      focusTypeToken: tokenValue(focusList, "--focus-mobile-type-body"),
      focusHitTarget: tokenValue(focusList, "--focus-mobile-hit-target"),
      searchFontSize: fontSize(search),
      stateButtonFontSize: fontSize(stateButton),
      stateButtonRect: compactRect(stateButton),
      itemFontSize: fontSize(item),
      itemRect: compactRect(item),
      titleFontSize: fontSize(title),
      metaFontSize: fontSize(meta),
      itemBounds: [...document.querySelectorAll(selector)].slice(0, 6).map(compactRect),
    };
  }, itemSelector);

  expect(metrics.focusTypeToken).toMatch(/rem$/);
  expect(metrics.focusHitTarget).toMatch(/rem$/);
  expect(metrics.documentWidth).toBeLessThanOrEqual(metrics.viewportWidth);
  expect(metrics.searchFontSize).toBeGreaterThanOrEqual(17);
  expect(metrics.stateButtonFontSize).toBeGreaterThanOrEqual(16);
  expect(metrics.stateButtonRect?.height ?? 0).toBeGreaterThanOrEqual(44);
  expect(metrics.itemFontSize).toBeGreaterThanOrEqual(17);
  expect(metrics.itemRect?.height ?? 0).toBeGreaterThanOrEqual(72);
  expect(metrics.titleFontSize).toBeGreaterThanOrEqual(19);
  expect(metrics.metaFontSize).toBeGreaterThanOrEqual(16);
  for (const bounds of metrics.itemBounds) {
    expect(bounds?.left ?? 0).toBeGreaterThanOrEqual(0);
    expect(bounds?.right ?? 0).toBeLessThanOrEqual(metrics.viewportWidth);
  }
}

async function expectReadableDetail(page: Page): Promise<void> {
  await expect(
    page.locator(".focus-layout .pull-detail .detail-title, .focus-layout .issue-detail .detail-title"),
  ).toBeVisible();

  const metrics = await page.evaluate(() => {
    const detail = document.querySelector(".pull-detail, .issue-detail");
    const fontSize = (selector: string): number => {
      const node = document.querySelector(selector);
      return node ? Number.parseFloat(getComputedStyle(node).fontSize) : 0;
    };
    const rect = (selector: string) => {
      const r = document.querySelector(selector)?.getBoundingClientRect();
      return r ? { left: r.left, right: r.right, height: r.height } : null;
    };
    const tokenValue = (name: string): string => detail
      ? getComputedStyle(detail).getPropertyValue(name).trim()
      : "";
    const overflowingVisible = [...document.querySelectorAll(".focus-layout *")]
      .filter((el) => {
        const r = el.getBoundingClientRect();
        return r.width > 0 && r.height > 0 && r.left < window.innerWidth && r.right > window.innerWidth + 0.5;
      })
      .map((el) => el.className?.toString() || el.tagName.toLowerCase());

    return {
      viewportWidth: window.innerWidth,
      documentWidth: document.documentElement.scrollWidth,
      detailTypeToken: tokenValue("--detail-mobile-type-body"),
      detailHitTarget: tokenValue("--detail-mobile-hit-target"),
      titleFontSize: fontSize(".detail-title"),
      metaFontSize: fontSize(".meta-item"),
      bodyFontSize: fontSize(".inset-box, .markdown-body, .add-description-btn, .loading-placeholder, .comment-editor-input"),
      chipFontSize: fontSize(".chip, .state-chip, .status-chip"),
      copyNumberFontSize: fontSize(".copy-number-btn"),
      copyNumberRect: rect(".copy-number-btn"),
      overflowingVisible,
    };
  });

  expect(metrics.detailTypeToken).toMatch(/rem$/);
  expect(metrics.detailHitTarget).toMatch(/rem$/);
  expect(metrics.documentWidth).toBeLessThanOrEqual(metrics.viewportWidth);
  expect(metrics.titleFontSize).toBeGreaterThanOrEqual(23);
  expect(metrics.metaFontSize).toBeGreaterThanOrEqual(16);
  expect(metrics.bodyFontSize).toBeGreaterThanOrEqual(17);
  expect(metrics.chipFontSize).toBeGreaterThanOrEqual(16);
  expect(metrics.copyNumberFontSize).toBeGreaterThanOrEqual(16);
  expect(metrics.copyNumberRect?.height ?? 0).toBeGreaterThanOrEqual(44);
  expect(metrics.overflowingVisible).toEqual([]);
}

test.describe("phone routes", () => {
  test("phone viewport visiting desktop root redirects to the mobile activity route", async ({ page }) => {
    await page.goto("/");

    await expect(page).toHaveURL(/\/m(?:\?|$)/);
    await expect(page.locator(".mobile-shell")).toBeVisible();
    await expect(page.locator(".mobile-tab--active")).toHaveText("Activity");
    await expect(page.locator(".app-header")).toHaveCount(0);
    await expect(page.locator("footer")).toHaveCount(0);

    const metrics = await page.evaluate(() => {
      const search = document.querySelector(".search-input");
      const rect = search?.getBoundingClientRect();
      return {
        viewportWidth: window.innerWidth,
        documentWidth: document.documentElement.scrollWidth,
        searchLeft: rect?.left ?? 0,
        searchRight: rect?.right ?? 0,
      };
    });

    expect(metrics.documentWidth).toBeLessThanOrEqual(metrics.viewportWidth);
    expect(metrics.searchLeft).toBeGreaterThanOrEqual(0);
    expect(metrics.searchRight).toBeLessThanOrEqual(metrics.viewportWidth);
  });

  test("mobile activity uses a phone-first inbox rather than the desktop threaded list", async ({ page }) => {
    await page.goto("/m?range=30d&view=threaded");

    await expect(page.locator(".mobile-shell")).toBeVisible();
    await expect(page.locator(".mobile-activity-inbox")).toBeVisible();
    await expect(page.getByRole("heading", { name: "What needs attention?" })).toBeVisible();
    await expect(page.locator(".threaded-view")).toHaveCount(0);

    const metrics = await page.evaluate(() => {
      const firstCard = document.querySelector(".mobile-activity-card");
      const firstButton = document.querySelector(".mobile-activity-card button");
      const title = document.querySelector(".mobile-activity-card__title");
      const meta = document.querySelector(".mobile-activity-card__meta");
      const eventLabel = document.querySelector(".mobile-activity-event__body strong");
      const eventAuthor = document.querySelector(".mobile-activity-event__body span");
      const eventTime = document.querySelector(".mobile-activity-event time");
      const mobileTitle = document.querySelector(".mobile-title");
      const mobileTab = document.querySelector(".mobile-tabs a");
      const desktopButton = document.querySelector(".mobile-desktop-link");
      const search = document.querySelector(".search-input");
      const cardRect = firstCard?.getBoundingClientRect();
      const buttonRect = firstButton?.getBoundingClientRect();
      const searchRect = search?.getBoundingClientRect();
      const fontSize = (node: Element | null): number => node
        ? Number.parseFloat(getComputedStyle(node).fontSize)
        : 0;
      return {
        viewportWidth: window.innerWidth,
        documentWidth: document.documentElement.scrollWidth,
        cardHeight: cardRect?.height ?? 0,
        touchTargetHeight: buttonRect?.height ?? 0,
        titleFontSize: fontSize(title),
        metaFontSize: fontSize(meta),
        eventLabelFontSize: fontSize(eventLabel),
        eventAuthorFontSize: fontSize(eventAuthor),
        eventTimeFontSize: fontSize(eventTime),
        mobileTitleFontSize: fontSize(mobileTitle),
        mobileTabFontSize: fontSize(mobileTab),
        desktopButtonFontSize: fontSize(desktopButton),
        searchLeft: searchRect?.left ?? 0,
        searchRight: searchRect?.right ?? 0,
      };
    });

    expect(metrics.documentWidth).toBeLessThanOrEqual(metrics.viewportWidth);
    expect(metrics.searchLeft).toBeGreaterThanOrEqual(0);
    expect(metrics.searchRight).toBeLessThanOrEqual(metrics.viewportWidth);
    expect(metrics.cardHeight).toBeGreaterThanOrEqual(120);
    expect(metrics.touchTargetHeight).toBeGreaterThanOrEqual(52);
    expect(metrics.titleFontSize).toBeGreaterThanOrEqual(25);
    expect(metrics.metaFontSize).toBeGreaterThanOrEqual(19);
    expect(metrics.eventLabelFontSize).toBeGreaterThanOrEqual(19);
    expect(metrics.eventAuthorFontSize).toBeGreaterThanOrEqual(18);
    expect(metrics.eventTimeFontSize).toBeGreaterThanOrEqual(18);
    expect(metrics.mobileTitleFontSize).toBeGreaterThanOrEqual(20);
    expect(metrics.mobileTabFontSize).toBeGreaterThanOrEqual(20);
    expect(metrics.desktopButtonFontSize).toBeGreaterThanOrEqual(17);
  });

  test("mobile activity Open thread routes to a focused thread detail", async ({ page }) => {
    await page.goto("/m?range=30d&view=threaded");
    await expect(page.locator(".mobile-activity-open").first()).toBeVisible();

    await page.locator(".mobile-activity-open").first().click();

    await expect(page).toHaveURL(/\/focus\/(?:host\/[^/]+\/)?(?:pulls|issues)\//);
    await expect(page.locator(".focus-layout")).toBeVisible();
    await expect(
      page.locator(
        ".focus-layout .pull-detail .detail-title, .focus-layout .issue-detail .detail-title",
      ),
    ).toBeVisible();
    await expectReadableDetail(page);
    await expect(page.locator(".mobile-shell")).toHaveCount(0);
  });

  test("phone users can opt out of automatic mobile redirect", async ({ page }) => {
    await page.goto("/?desktop=1");

    await expect(page).toHaveURL(/\/?desktop=1$/);
    await expect(page.locator(".app-header")).toBeVisible();
    await expect(page.locator(".mobile-shell")).toHaveCount(0);
  });

  test("mobile PR and issue tabs use dedicated phone routes", async ({ page }) => {
    await page.goto("/m/pulls");
    await expect(page.locator(".mobile-shell")).toBeVisible();
    await expect(page.locator(".mobile-tab--active")).toHaveText("PRs");
    await expect(page.locator(".focus-list")).toBeVisible();
    await expectReadableFocusList(page, ".pull-item");

    await page.getByRole("link", { name: "Issues" }).click();
    await expect(page).toHaveURL(/\/m\/issues(?:\?|$)/);
    await expect(page.locator(".mobile-tab--active")).toHaveText("Issues");
    await expect(page.locator(".focus-list")).toBeVisible();
    await expectReadableFocusList(page, ".issue-item");
  });
});

test.describe("high-density phone routes", () => {
  const pixel7 = devices["Pixel 7"];
  test.use({
    viewport: pixel7.viewport,
    deviceScaleFactor: pixel7.deviceScaleFactor,
    isMobile: pixel7.isMobile,
    hasTouch: pixel7.hasTouch,
    userAgent: pixel7.userAgent,
  });

  test("mobile activity sizing stays rem-based and readable on high-density Android displays", async ({ page }) => {
    await page.goto("/m?range=30d&view=threaded");

    await expect(page.locator(".mobile-shell")).toBeVisible();
    await expect(page.locator(".mobile-activity-inbox")).toBeVisible();

    const metrics = await page.evaluate(() => {
      const fontSize = (selector: string): number => {
        const node = document.querySelector(selector);
        return node ? Number.parseFloat(getComputedStyle(node).fontSize) : 0;
      };
      const shell = document.querySelector(".mobile-shell");
      const inbox = document.querySelector(".mobile-activity-inbox");
      const tokenValue = (node: Element | null, name: string): string => node
        ? getComputedStyle(node).getPropertyValue(name).trim()
        : "";
      const filterButtons = [...document.querySelectorAll(".mobile-activity-filters button")]
        .map((button) => button.getBoundingClientRect())
        .map((rect) => ({ left: rect.left, right: rect.right }));
      const search = document.querySelector(".search-input")?.getBoundingClientRect();
      return {
        dpr: window.devicePixelRatio,
        viewportWidth: window.innerWidth,
        documentWidth: document.documentElement.scrollWidth,
        chromeTypeToken: tokenValue(shell, "--mobile-chrome-type-sm"),
        activityTypeToken: tokenValue(inbox, "--mobile-type-body"),
        densityScale: tokenValue(inbox, "--mobile-device-density-scale"),
        bodyFontSize: fontSize(".mobile-activity-lede"),
        filterFontSize: fontSize(".mobile-activity-filters button"),
        tabFontSize: fontSize(".mobile-tabs a"),
        searchHeight: search?.height ?? 0,
        searchLeft: search?.left ?? 0,
        searchRight: search?.right ?? 0,
        filterButtons,
      };
    });

    expect(metrics.dpr).toBeGreaterThanOrEqual(2.5);
    expect(metrics.chromeTypeToken).toMatch(/rem$/);
    expect(metrics.activityTypeToken).toMatch(/rem$/);
    expect(metrics.densityScale).toBe("");
    expect(metrics.documentWidth).toBeLessThanOrEqual(metrics.viewportWidth);
    expect(metrics.bodyFontSize).toBeGreaterThanOrEqual(20);
    expect(metrics.filterFontSize).toBeGreaterThanOrEqual(19);
    expect(metrics.tabFontSize).toBeGreaterThanOrEqual(20);
    expect(metrics.searchHeight).toBeGreaterThanOrEqual(44);
    expect(metrics.searchLeft).toBeGreaterThanOrEqual(0);
    expect(metrics.searchRight).toBeLessThanOrEqual(metrics.viewportWidth);
    for (const button of metrics.filterButtons) {
      expect(button.left).toBeGreaterThanOrEqual(0);
      expect(button.right).toBeLessThanOrEqual(metrics.viewportWidth);
    }
  });
});
