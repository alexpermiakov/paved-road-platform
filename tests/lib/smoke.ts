import { expect, type Playwright } from "@playwright/test"

// ArgoCD sync + pod startup can lag behind the ALB becoming reachable.
export const READY_TIMEOUT = 180_000

// Poll a path on the live ALB until it serves 200, so a freshly provisioned
// service that is still syncing doesn't fail the suite spuriously. Each
// service's spec calls this from beforeAll with its own readiness path.
export async function waitForReady(
  playwright: Playwright,
  baseURL: string | undefined,
  path: string,
): Promise<void> {
  const ctx = await playwright.request.newContext({ baseURL })
  await expect
    .poll(
      async () => {
        try {
          return (await ctx.get(path)).status()
        } catch {
          return 0 // ALB/DNS not resolvable yet
        }
      },
      {
        message: `${path} did not return 200 within ${READY_TIMEOUT / 60_000} min`,
        timeout: READY_TIMEOUT,
        intervals: [5_000],
      },
    )
    .toBe(200)
  await ctx.dispose()
}
