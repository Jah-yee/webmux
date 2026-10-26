// Run in a WebMux session: node web/test/fixtures/kitty-scroll-demo.mjs
import { kittyScrollFrame, kittyTextRedraw } from "./kitty-scroll.ts";

let step = 0;
process.stdin.setRawMode(true);
process.stdin.resume();
process.stdout.write("\x1b[2J\x1b[H" + kittyScrollFrame(step));
process.stdin.on("data", (data) => {
  const key = data.toString();
  if (key.includes("q") || key.includes("\x03")) {
    process.stdout.write("\x1b_Ga=d,d=I,i=81,q=2\x1b\\\x1b[2J\x1b[H");
    process.stdin.setRawMode(false);
    process.exit(0);
  }
  if (key === "r") {
    process.stdout.write(kittyTextRedraw(step));
    return;
  }
  if (key.includes("\x1b[A")) step = Math.max(0, step - 1);
  else if (key.includes("\x1b[B") || key === " ") step++;
  else return;
  process.stdout.write(kittyScrollFrame(step));
});
