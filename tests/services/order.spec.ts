import { test, expect } from "@playwright/test"
import { waitForReady } from "../lib/smoke"

const ORDERS = "/orders"
const ORDER_STATUSES = [
  "pending",
  "confirmed",
  "processing",
  "shipped",
  "delivered",
]

test.beforeAll(async ({ playwright, baseURL }) => {
  await waitForReady(playwright, baseURL, ORDERS)
})

test("GET /orders returns a well-formed order", async ({ request }) => {
  const res = await request.get(ORDERS)
  expect(res.status()).toBe(200)
  expect(res.headers()["content-type"]).toContain("application/json")

  const { order } = await res.json()
  expect(order, 'response body has an "order" object').toBeTruthy()
  expect(order.order_id).toMatch(/^ORD-\d{6}$/)
  expect(ORDER_STATUSES).toContain(order.status)
  expect(order.items).toBeGreaterThanOrEqual(1)
  expect(order.items).toBeLessThanOrEqual(10)
})

test("GET /orders is stable across repeated requests", async ({ request }) => {
  for (let i = 0; i < 5; i++) {
    const res = await request.get(ORDERS)
    expect(res.status(), `request ${i + 1}`).toBe(200)
    const { order } = await res.json()
    expect(order.order_id, `request ${i + 1}`).toMatch(/^ORD-\d{6}$/)
  }
})

