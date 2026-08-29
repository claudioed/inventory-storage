// Standalone Playwright smoke check for inventory-mfe (dev-only script,
// not part of the build). Loads http://localhost:5182/ directly, confirms
// the page renders (Inventory title visible), and that the SKU lookup +
// demandRef search round-trip to the real backend on :8082 without
// console errors. Delete or keep local -- not wired into CI.
import { chromium } from "playwright";

async function main() {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  const consoleErrors = [];
  const pageErrors = [];

  page.on("console", (msg) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });
  page.on("pageerror", (err) => pageErrors.push(String(err)));

  await page.goto("http://localhost:5182/", { waitUntil: "networkidle" });

  const title = await page.textContent("h1");
  console.log("h1 text:", title);
  if (title?.trim() !== "Inventory") {
    throw new Error(`Expected h1 "Inventory", got "${title}"`);
  }

  // SKU lookup against the real backend.
  await page.fill('input[placeholder="SKU"]', "TEST-SKU");
  await page.click('button:has-text("Look up usable")');
  await page.waitForSelector("text=Usable quantity:", { timeout: 5000 });
  const usableText = await page.textContent("body");
  console.log("Usable-quantity section rendered:", usableText.includes("Usable quantity:"));

  // demandRef search against the real backend (expect empty-state, since
  // no reservation exists yet for this ref -- still a full round trip).
  await page.fill('input[placeholder="Demand ref"]', "smoke-test-demand-ref");
  await page.click('button:has-text("Search")');
  await page.waitForSelector("text=No reservations found for this demand ref.", { timeout: 5000 });
  console.log("Empty-state for unknown demandRef rendered correctly.");

  console.log("console errors:", consoleErrors);
  console.log("page errors:", pageErrors);

  await browser.close();

  if (consoleErrors.length > 0 || pageErrors.length > 0) {
    process.exit(1);
  }
  console.log("PASS: inventory-mfe standalone smoke check succeeded.");
}

main().catch((err) => {
  console.error("FAIL:", err);
  process.exit(1);
});
