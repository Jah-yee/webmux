import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

type Parsed = { title: string; body: string; tag: string; sessionId: string };
type Client = { url: string };
type SW = {
  parsePush(text: string): Parsed;
  clickPath(data: unknown): string;
  chooseClient(
    clients: Client[],
    origin: string,
    path: string,
  ): { client: Client | null; navigate: boolean };
};

// sw.js is a classic script served as-is; evaluate it outside a service
// worker scope so it registers no listeners and exposes its pure functions.
const sw = new Function(
  readFileSync(new URL("../public/sw.js", import.meta.url), "utf8") +
    "\nreturn { parsePush, clickPath, chooseClient };",
)() as SW;

const payload = (sessionId: string, navigate = "") =>
  JSON.stringify({
    web_push: 8030,
    notification: {
      title: "deploy",
      body: "需要回答",
      navigate,
      tag: "webmux-" + sessionId,
    },
    webmux: { v: 1, sessionId, kind: "question" },
  });

describe("push payload", () => {
  it("shows the session title, kind text and per-session tag", () => {
    expect(sw.parsePush(payload("abcdefgh"))).toEqual({
      title: "deploy",
      body: "需要回答",
      tag: "webmux-abcdefgh",
      sessionId: "abcdefgh",
    });
  });

  it.each(["not json", "", "null", "[]", '{"notification":7}'])(
    "falls back for %j",
    (text) => {
      expect(sw.parsePush(text)).toEqual({
        title: "WebMux",
        body: "有会话需要关注",
        tag: "webmux",
        sessionId: "",
      });
    },
  );
});

describe("notification click", () => {
  it("opens only this origin's session path", () => {
    expect(sw.clickPath(sw.parsePush(payload("abcdefgh")))).toBe("/s/abcdefgh");
    const hostile = sw.parsePush(
      payload("../../evil", "https://evil.example/"),
    );
    expect(sw.clickPath(hostile)).toBe("/");
    expect(sw.clickPath({ sessionId: "ABCDEFGH" })).toBe("/");
    expect(sw.clickPath(null)).toBe("/");
  });

  it("focuses the session window, else reuses a same-origin window", () => {
    const origin = "https://term.example.com";
    const list = { url: origin + "/" };
    const session = { url: origin + "/s/abcdefgh" };
    const other = { url: "https://other.example/s/abcdefgh" };
    expect(
      sw.chooseClient([list, session, other], origin, "/s/abcdefgh"),
    ).toEqual({ client: session, navigate: false });
    expect(sw.chooseClient([other, list], origin, "/s/abcdefgh")).toEqual({
      client: list,
      navigate: true,
    });
    expect(sw.chooseClient([other], origin, "/s/abcdefgh")).toEqual({
      client: null,
      navigate: false,
    });
  });
});
