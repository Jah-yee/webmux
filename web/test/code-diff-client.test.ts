import { afterEach, expect, test, vi } from "vitest";
import { computeDiff } from "../src/code-diff";
import { calculateDiff } from "../src/code-diff-client";

class FakeWorker {
  static instances: FakeWorker[] = [];
  onmessage?: (event: { data: unknown }) => void;
  onerror?: (event: { preventDefault: () => void }) => void;
  onmessageerror?: () => void;
  postMessage = vi.fn();
  terminate = vi.fn();
  constructor() {
    FakeWorker.instances.push(this);
  }
  reply(snapshotId: string) {
    this.onmessage?.({
      data: { snapshotId, result: computeDiff("", "") },
    });
  }
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  FakeWorker.instances = [];
});

test("cancels obsolete work, validates the snapshot and terminates each Worker", async () => {
  vi.stubGlobal("Worker", FakeWorker);
  const first = new AbortController();
  const job = calculateDiff("old", "a", "b", first.signal);
  const rejected = expect(job).rejects.toMatchObject({ name: "AbortError" });
  const previous = FakeWorker.instances[0]!;
  first.abort();
  await rejected;
  const latest = calculateDiff("new", "c", "d", new AbortController().signal);
  const worker = FakeWorker.instances[1]!;
  previous.reply("old");
  worker.reply("new");
  expect(await latest).toEqual(computeDiff("", ""));
  expect(previous.terminate).toHaveBeenCalledTimes(1);
  expect(worker.terminate).toHaveBeenCalledTimes(1);
  const wrong = calculateDiff("expected", "", "", new AbortController().signal);
  FakeWorker.instances[2]!.reply("another snapshot");
  await expect(wrong).rejects.toThrow("快照不匹配");
});

test("reports a stalled or unavailable Worker and releases it", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("Worker", FakeWorker);
  const job = calculateDiff("s", "a", "b", new AbortController().signal);
  await vi.advanceTimersByTimeAsync(10000);
  expect(await job).toMatchObject({ kind: "limited" });
  expect(FakeWorker.instances[0]!.terminate).toHaveBeenCalledTimes(1);
  vi.stubGlobal(
    "Worker",
    class {
      constructor() {
        throw new Error("unavailable");
      }
    },
  );
  await expect(
    calculateDiff("s", "", "", new AbortController().signal),
  ).rejects.toThrow("无法启动");
  const aborted = new AbortController();
  aborted.abort();
  await expect(
    calculateDiff("s", "", "", aborted.signal),
  ).rejects.toMatchObject({ name: "AbortError" });
});
