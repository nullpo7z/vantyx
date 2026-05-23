import type { Locator } from '@playwright/test';

type ForceClickOptions = {
  timeout?: number;
};

/**
 * 安定化のため、Playwright の click() ではなく DOM click() を直接発火する。
 * - オーバーレイ等で click が吸われる/不安定になるケースを回避
 * - 要素が visible になるまで待ってから実行
 */
export async function forceClick(locator: Locator, opts: ForceClickOptions = {}) {
  const timeout = opts.timeout ?? 15000;
  await locator.waitFor({ state: 'visible', timeout });
  await locator.evaluate((el) => (el as HTMLElement).click());
}

