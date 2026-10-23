import { expect, it } from "vitest";
import { accessEntryHint } from "../src/accessEntry";

const hint = (url: string) => accessEntryHint(new URL(url));

it("classifies how the page was reached", () => {
  const loopback = hint("http://127.0.0.1:7681/settings");
  for (const url of [
    "http://[::1]:7681/settings",
    "http://localhost:7681/settings",
    "http://webmux.localhost:7681/settings",
  ])
    expect(hint(url), url).toBe(loopback);

  const quick = hint("https://abc-def.trycloudflare.com/settings");
  const https = hint("https://term.example.com/settings");
  const insecure = hint("http://192.168.1.5:7681/settings");
  expect(new Set([loopback, quick, https, insecure]).size).toBe(4);
  // A suffix match must not treat unrelated hostnames as loopback.
  expect(hint("http://notlocalhost:7681/")).toBe(insecure);
});
