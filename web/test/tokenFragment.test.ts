import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { consumeTokenFragment, stored } from "../src/api";

describe("consumeTokenFragment", () => {
  let storage: Map<string, string>;
  let address: URL;
  let replaced: string[];
  const state = { from: "router" };

  function open(url: string) {
    address = new URL(url);
  }

  beforeEach(() => {
    storage = new Map([["webmux.token", "old"]]);
    replaced = [];
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
    });
    vi.stubGlobal("location", {
      get hash() {
        return address.hash;
      },
      get pathname() {
        return address.pathname;
      },
      get search() {
        return address.search;
      },
    });
    vi.stubGlobal("history", {
      state,
      replaceState(data: unknown, _unused: string, url: string) {
        expect(data).toBe(state);
        replaced.push(url);
        open(new URL(url, address).href);
      },
    });
  });

  afterEach(() => vi.unstubAllGlobals());

  it("stores the decoded token and strips only the fragment", () => {
    open("https://host/s/abcdefgh?view=1#token=a%20b%26c%23d");
    expect(consumeTokenFragment()).toBe(true);
    expect(stored("token")).toBe("a b&c#d");
    expect(replaced).toEqual(["/s/abcdefgh?view=1"]);
    expect(address.hash).toBe("");
  });

  it("leaves unrelated fragments untouched", () => {
    open("https://host/settings#quick-commands");
    expect(consumeTokenFragment()).toBe(false);
    expect(stored("token")).toBe("old");
    expect(replaced).toEqual([]);
  });

  it("strips a malformed token without storing it", () => {
    open("https://host/#token=%E0%A4%A");
    expect(consumeTokenFragment()).toBe(false);
    expect(stored("token")).toBe("old");
    expect(replaced).toEqual(["/"]);
  });
});
