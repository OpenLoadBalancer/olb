import { test, expect } from '@playwright/test'

test.describe('Smoke tests', () => {
  test('loads the dashboard page', async ({ page }) => {
    await page.goto('/')

    // Should show the app shell with navigation
    await expect(page.locator('nav')).toBeVisible()

    // Dashboard should have a heading
    await expect(page.getByRole('heading', { name: /dashboard/i })).toBeVisible()
  })

  test('navigates between pages', async ({ page }) => {
    await page.goto('/')

    // Navigate to Pools page
    await page.getByRole('link', { name: /pools/i }).click()
    await expect(page).toHaveURL(/.*pools/)
    await expect(page.getByRole('heading', { name: /pool/i })).toBeVisible()

    // Navigate to Cluster page
    await page.getByRole('link', { name: /cluster/i }).click()
    await expect(page).toHaveURL(/.*cluster/)
    await expect(page.getByRole('heading', { name: /cluster/i })).toBeVisible()

    // Navigate to Settings page
    await page.getByRole('link', { name: /settings/i }).click()
    await expect(page).toHaveURL(/.*settings/)
    await expect(page.getByRole('heading', { name: /settings/i })).toBeVisible()
  })

  test('skip-to-content link is present', async ({ page }) => {
    await page.goto('/')

    // Tab to focus the skip link
    await page.keyboard.press('Tab')

    // The skip-to-content link should become visible on focus
    const skipLink = page.getByText('Skip to content')
    await expect(skipLink).toBeVisible()
  })

  test('responsive sidebar collapses on small screens', async ({ page }) => {
    await page.setViewportSize({ width: 640, height: 480 })
    await page.goto('/')

    // On mobile, sidebar should be visually hidden via -translate-x-full class
    const nav = page.locator('[aria-label="Main navigation"]')
    await expect(nav).toHaveClass(/-translate-x-full/)
  })
})
