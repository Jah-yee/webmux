import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { expect, expectConnected, test } from "./fixtures";

// A stand-in agent: it reports a question over the Warp cli-agent protocol,
// then keeps redrawing a spinner exactly like omp does while it waits.
const agent = String.raw`
printf '\033]777;notify;warp://cli-agent;{"v":1,"agent":"e2e","event":"question_asked"}\007'
( while :; do printf '\r| waiting'; sleep 0.1; printf '\r/ waiting'; sleep 0.1; done ) &
stty -echo -icanon min 1
dd bs=1 count=1 >/dev/null 2>&1
printf '\007'
sleep 1.5
printf '\007'
wait
`;

test("agent attention survives spinner output and clears on user input", async ({
  page,
  request,
  workDir,
}) => {
  const created = await request.post("/api/sessions", {
    data: {
      cwd: workDir,
      cmd: ["/bin/bash", "-c", agent],
      name: "E2E agent",
    },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  const row = page.locator(`[data-session-id="${id}"]`);

  await page.goto("/");
  await expect(row).toContainText("需要回答");
  // Well past the 2-second output window that used to report "running".
  await page.waitForTimeout(2500);
  await expect(row).toContainText("需要回答");
  await expect(row).toHaveAttribute("data-status", "waiting");
  const info = await (await request.get(`/api/sessions/${id}`)).json();
  expect(info.attention).toMatchObject({ kind: "question", source: "agent" });

  await page.goto(`/s/${id}`);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("y");
  // The bell rung right after the keystroke is ignored ...
  await expect
    .poll(
      async () =>
        (await (await request.get(`/api/sessions/${id}`)).json()).attention,
      { intervals: [50] },
    )
    .toBeNull();
  // ... while the bell after 1.5 s of quiet raises attention again.
  await page.goto("/");
  await expect(row).toContainText("响铃提醒");
  expect(
    (await (await request.get(`/api/sessions/${id}`)).json()).attention,
  ).toMatchObject({ kind: "bell", source: "terminal" });
});

// omp queries terminal modes and sizes at startup. Opening the page replays
// those queries into xterm.js, whose automatic replies reach the PTY; they
// must not count as the user answering.
const queryingAgent = String.raw`
printf '\033[?2026$p\033[18t\033[14t\033[16t\033[c\033[6n'
printf '\033]777;notify;warp://cli-agent;{"v":1,"agent":"e2e","event":"question_asked"}\007'
stty -echo -icanon min 1
( while :; do printf '\r| waiting'; sleep 0.1; printf '\r/ waiting'; sleep 0.1; done ) &
cat > replies.bin
`;

test("opening a session does not clear attention through terminal replies", async ({
  page,
  request,
  workDir,
}) => {
  const created = await request.post("/api/sessions", {
    data: {
      cwd: workDir,
      cmd: ["/bin/bash", "-c", queryingAgent],
      name: "E2E querying agent",
    },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  const attention = async () =>
    (await (await request.get(`/api/sessions/${id}`)).json()).attention;
  await expect.poll(attention).toMatchObject({ kind: "question" });

  for (const visit of [1, 2]) {
    await page.goto(`/s/${id}`);
    await expectConnected(page);
    await expect(page.locator(".restore-overlay")).toBeHidden();
    // Give the replayed queries' replies time to reach the PTY.
    await expect
      .poll(() => readFile(join(workDir, "replies.bin"), "latin1"), {
        message: `visit ${visit} replies`,
      })
      .toMatch(/\$y/);
    await page.waitForTimeout(800);
    expect(await attention(), `visit ${visit}`).toMatchObject({
      kind: "question",
      source: "agent",
    });
  }
});

// A stand-in for an OSC 7501 agent such as Claude Code: it asks whether the
// terminal speaks the protocol before any browser is attached, then reports
// its status while it keeps redrawing a spinner. The test advances it by
// creating step files in its working directory.
const programStatusReply =
  "\x1b]7501;?:states=idle,working,done,blocked,error:kinds=permission,question,auth\x1b\\";
const statusAgent = String.raw`
stty raw -echo
printf '\033]7501;?\007'
reply=$(dd bs=1 count=${programStatusReply.length} 2>/dev/null)
case "$reply" in *'7501;?:states='*) printf 'PROBE-OK\r\n' ;; *) printf 'PROBE-MISSING\r\n' ;; esac
printf '\033]7501;state=blocked:kind=permission:app=e2e\007'
( while :; do printf '\r| busy'; sleep 0.1; printf '\r/ busy'; sleep 0.1; done ) &
until [ -e step1 ]; do sleep 0.05; done
printf '\033]7501;state=working:app=e2e\007'
until [ -e step2 ]; do sleep 0.05; done
# Assembled at run time so the command line itself does not carry it.
msg="c2Vj$(printf cmV0)"
printf '\033]7501;state=done:app=e2e:msg=%s\007' "$msg"

cat > /dev/null
`;

test("program status reports drive attention and status", async ({
  page,
  request,
  workDir,
}) => {
  const created = await request.post("/api/sessions", {
    data: {
      cwd: workDir,
      cmd: ["/bin/bash", "-c", statusAgent],
      name: "E2E status agent",
    },
  });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  const info = async () =>
    (await request.get(`/api/sessions/${id}`)).json() as Promise<{
      status: string;
      attention: { kind: string; source: string } | null;
    }>;
  const row = page.locator(`[data-session-id="${id}"]`);

  await expect.poll(info).toMatchObject({
    status: "waiting",
    attention: { kind: "permission", source: "agent" },
  });
  await page.goto("/");
  await expect(row).toContainText("等待授权");
  await expect(row).toHaveAttribute("data-status", "waiting");

  await writeFile(join(workDir, "step1"), "");
  await expect.poll(info).toMatchObject({ status: "running", attention: null });
  // A working record keeps the session running however the output looks.
  await page.waitForTimeout(2500);
  expect(await info()).toMatchObject({ status: "running", attention: null });

  await writeFile(join(workDir, "step2"), "");
  await expect(row).toContainText("本轮完成");
  await expect(row).toHaveAttribute("data-status", "waiting");
  const reported = await (await request.get(`/api/sessions/${id}`)).text();
  expect(reported).not.toContain("c2VjcmV0");

  await page.goto(`/s/${id}`);
  await expect(page.locator(".restore-overlay")).toBeHidden();
  await expect(page.locator(".xterm-rows")).toContainText("PROBE-OK");
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("y");
  // Seen, the finished turn is idle even while the spinner keeps drawing.
  await expect.poll(info).toMatchObject({ status: "idle", attention: null });
});

// The last line looks like a prompt and the program is blocked reading the
// terminal: the heuristics call that waiting, unless the program already said
// it reports its own waiting through structured events.
const promptLike = (structured: boolean) => String.raw`
${structured ? `printf '\\033]777;notify;warp://cli-agent;{"v":1,"event":"prompt_submit"}\\007'` : ""}
printf 'Continue? '
stty -echo -icanon min 1
dd bs=1 count=1 > /dev/null 2>&1
`;

test("structured agents do not trigger the waiting heuristics", async ({
  request,
  workDir,
}) => {
  const ids: Record<string, string> = {};
  for (const structured of [true, false]) {
    const created = await request.post("/api/sessions", {
      data: {
        cwd: workDir,
        cmd: ["/bin/bash", "-c", promptLike(structured)],
        name: structured ? "E2E structured" : "E2E plain",
      },
    });
    expect(created.status()).toBe(201);
    ids[String(structured)] = (await created.json()).id;
  }
  const status = async (structured: boolean) =>
    (
      await (
        await request.get(`/api/sessions/${ids[String(structured)]}`)
      ).json()
    ).status;
  // Past the 2-second output window: only the heuristics decide now.
  await expect.poll(() => status(false)).toBe("waiting");
  expect(await status(true)).toBe("running");
});
