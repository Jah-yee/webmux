import { afterEach, expect, test, vi } from "vitest";
import { z } from "zod";
import { api } from "../src/api";
import { CreateSessionRequestSchema } from "../src/schemas";

vi.stubGlobal("localStorage", { getItem: () => null });
afterEach(() => {
  vi.unstubAllGlobals();
});

test("an invalid request body is rejected before any network request", async () => {
  const fetchMock = vi.fn<typeof fetch>();
  vi.stubGlobal("fetch", fetchMock);
  await expect(
    api(
      "/sessions",
      z.undefined(),
      "POST",
      { configId: "saved", cmd: ["sh"] },
      undefined,
      z.object({ configId: z.string() }).strict(),
    ),
  ).rejects.toThrow();
  expect(fetchMock).not.toHaveBeenCalled();
});

test.each([{ cmd: [] }, { env: {} }])(
  "an empty override does not conflict with configId: %j",
  (override) => {
    const body = { configId: "saved", ...override };
    expect(CreateSessionRequestSchema.parse(body)).toEqual(body);
  },
);

test("non-empty command or environment conflicts with configId", () => {
  for (const body of [
    { configId: "saved", cmd: ["sh"] },
    { configId: "saved", env: { A: "1" } },
  ])
    expect(CreateSessionRequestSchema.safeParse(body).success).toBe(false);
});
