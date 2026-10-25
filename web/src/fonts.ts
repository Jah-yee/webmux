const terminalFontFamily = "Maple Mono NF CN";

// The generic is sans-serif so a missing glyph cannot fall through to a serif
// CJK face such as SimSun. The two spellings resolve to the same faces; xterm
// only remeasures when the fontFamily string changes.
export const terminalFontFamilyCss = `"${terminalFontFamily}", sans-serif`;
export const terminalFontFamilyAlt = `"${terminalFontFamily}",sans-serif`;

export const terminalLineHeight = 1.15;

export async function loadTerminalFont(size: number): Promise<void> {
  const fonts = document.fonts;
  if (!fonts?.load) return;
  const family = `"${terminalFontFamily}"`;
  const pending = Promise.all(
    [`${size}px ${family}`, `700 ${size}px ${family}`].map((spec) =>
      fonts.load(spec, "A").catch(() => []),
    ),
  );
  await Promise.race([
    pending.then(() => undefined),
    new Promise<void>((resolve) => {
      window.setTimeout(resolve, 3000);
    }),
  ]);
}
