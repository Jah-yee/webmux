// The same bytes drive the interactive PTY reproducer and browser regression.
// Four opaque horizontal bands make source cropping observable at pixel level.
const kittyBands =
  "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAICAIAAABRUclSAAAAGElEQVR4nGP4z8AARwwoHAYypP4jIRQOAOnSJ9k20jBFAAAAAElFTkSuQmCC";

export function kittyScrollFrame(step: number) {
  const upload = `\x1b_Ga=t,f=100,i=81,q=2;${kittyBands}\x1b\\`;
  const crop = step % 3;
  const sourceY = crop * 2;
  const sourceHeight = 8 - sourceY;
  // omp redraws text with ED0, deletes its old placement, then restores a
  // cropped placement above the saved cursor with ESC7/CUU/ESC8.
  return (
    (step === 0 ? upload : "") +
    "\x1b[H\r\x1b[J" +
    `Kitty scroll step ${step}: ${crop ? "cropped" : "full"}\r\n` +
    "\x1b_Ga=d,d=i,i=81,q=2\x1b\\" +
    "\x1b[10;1H\x1b7\x1b[8A" +
    `\x1b_Ga=p,i=81,p=1,C=1,q=2,c=8,r=${sourceHeight},y=${sourceY},h=${sourceHeight}\x1b\\` +
    "\x1b8\r\x1b[J" +
    "Up/down: scroll image | r: text redraw | q: quit"
  );
}

export function kittyTextRedraw(step: number) {
  return (
    "\x1b[H\r\x1b[J" +
    `Kitty redraw step ${step}: graphics must remain\r\n` +
    "\x1b[10;1HUp/down: scroll image | r: text redraw | q: quit"
  );
}
