import { describe, expect, it } from "vitest";
import { resolveInventoryApiBase } from "./config";

describe("resolveInventoryApiBase", () => {
  it("builds the production API base from the runtime API origin", () => {
    expect(resolveInventoryApiBase({ apiOrigin: "http://localhost:8000" }, true)).toBe(
      "http://localhost:8000/api/inventory-storage",
    );
  });

  it("normalizes a trailing slash on the runtime API origin", () => {
    expect(resolveInventoryApiBase({ apiOrigin: "https://warehouse.example/" }, true)).toBe(
      "https://warehouse.example/api/inventory-storage",
    );
  });

  it("fails loudly when production runtime configuration has no API origin", () => {
    expect(() => resolveInventoryApiBase({}, true)).toThrow(
      "window.__WAREHOUSE_CONFIG__.apiOrigin is required in production",
    );
  });

  it("retains the existing standalone API origin in Vite development", () => {
    expect(resolveInventoryApiBase({}, false)).toBe("http://localhost:8082");
  });
});
