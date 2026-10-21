#!/usr/bin/env python3
"""Rebuild the self-hosted WebMux webfonts.

The app commits the generated woff2 files and web/src/fonts.css. Re-run this
only when upgrading Inter or Maple Mono.

Tools are intentionally outside the frontend lockfile:

  python3 -m pip install --user fonttools brotli

HarmonyOS Sans is not fetched: its license forbids modifying the files, so
unicode-range subsetting is not allowed. CJK text uses system sans-serif fonts.
"""

from __future__ import annotations

import hashlib
import os
import re
import shutil
import subprocess
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PUBLIC = ROOT / "web" / "public" / "fonts"
CSS_OUT = ROOT / "web" / "src" / "fonts.css"
TERMINAL_CSS = ROOT / "web" / "src" / "fonts-terminal.css"
INDEX = ROOT / "web" / "index.html"
CACHE = Path(os.environ.get("WEBMUX_FONT_CACHE", "/tmp/webmux-fonts"))
MAPLE_TAG = "v7.9"
INTER_VERSION = "5.2.8"

INTER_LATIN = (
    "U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,"
    "U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,"
    "U+2215,U+FEFF,U+FFFD"
)
INTER_LATIN_EXT = (
    "U+0100-02BA,U+02BD-02C5,U+02C7-02CC,U+02CE-02D7,U+02DD-02FF,U+0304,"
    "U+0308,U+0329,U+1D00-1DBF,U+1E00-1E9F,U+1EF2-1EFF,U+2020,U+20A0-20AB,"
    "U+20AD-20C0,U+2113,U+2C60-2C7F,U+A720-A7FF"
)

# Latin-1 is the preload slice. Box drawing and Powerline stay in their own
# slices so a prompt or TUI does not pull the full symbol font, without
# joining the initial preload.
MAPLE_SLICES = (
    ("latin", "U+0000-00FF", True),
    ("latin-ext", "U+0100-024F", False),
    ("box", "U+2500-259F", False),
    ("powerline", "U+E0A0-E0D4", False),
)


# CJK blocks fall back to the device's sans-serif fonts. Keep all other Maple
# glyphs, including combining marks, box drawing, Powerline and Nerd Font PUA.
CJK_RANGES = [
    (0x1100, 0x11FF),
    (0x2E80, 0x9FFF),
    (0xA960, 0xA97F),
    (0xAC00, 0xD7FF),
    (0xF900, 0xFAFF),
    (0xFE10, 0xFE1F),
    (0xFE30, 0xFE6F),
    (0xFF00, 0xFFEF),
    (0x16FE0, 0x18DFF),
    (0x1AFF0, 0x1B2FF),
    (0x1F200, 0x1F2FF),
    (0x20000, 0x3FFFF),
]


def download(url: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    if dest.exists() and dest.stat().st_size > 0:
        return
    print(f"download {dest.name}")
    urllib.request.urlretrieve(url, dest)


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()[:8]


def emit_face(family: str, weight: str, filename: str, unicode_range: str) -> str:
    # UI fonts must not reflow controls after a slow download. xterm explicitly
    # loads its Latin faces before measuring the terminal grid.
    return (
        "@font-face {\n"
        f'  font-family: "{family}";\n'
        "  font-style: normal;\n"
        f"  font-weight: {weight};\n"
        f"  font-display: {'block' if family == 'Maple Mono NF CN' else 'optional'};\n"
        f'  src: url("/fonts/{filename}") format("woff2");\n'
        f"  unicode-range: {unicode_range};\n"
        "}\n"
    )


def write_font(data: bytes, prefix: str) -> str:
    name = f"{prefix}-{digest(data)}.woff2"
    (PUBLIC / name).write_bytes(data)
    return name


def unicode_range_from_font(path: Path) -> str:
    from fontTools.ttLib import TTFont

    font = TTFont(path)
    cps = sorted(font.getBestCmap() or [])
    font.close()
    if not cps:
        raise SystemExit(f"{path} has no cmap")
    ranges: list[tuple[int, int]] = []
    start = prev = cps[0]
    for cp in cps[1:]:
        if cp == prev + 1:
            prev = cp
            continue
        ranges.append((start, prev))
        start = prev = cp
    ranges.append((start, prev))
    parts = []
    for start, end in ranges:
        parts.append(f"U+{start:X}" if start == end else f"U+{start:X}-{end:X}")
    return ",".join(parts)


def subset(src: Path, unicodes: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    subprocess.check_call(
        [
            sys.executable,
            "-m",
            "fontTools.subset",
            str(src),
            f"--unicodes={unicodes}",
            "--flavor=woff2",
            "--layout-features-=liga,dlig,calt",
            "--ignore-missing-unicodes",
            f"--output-file={dest}",
        ]
    )


def subset_remainder(
    src: Path, excluded: list[tuple[int, int]], shared_icons: set[int], dest: Path
) -> None:
    from fontTools.ttLib import TTFont

    font = TTFont(src)
    keep = []
    for cp in font.getBestCmap() or []:
        if cp in shared_icons or any(start <= cp <= end for start, end in excluded):
            continue
        keep.append(cp)
    font.close()
    listing = dest.with_suffix(".unicodes.txt")
    listing.write_text(",".join(f"U+{cp:04X}" for cp in keep))
    subprocess.check_call(
        [
            sys.executable,
            "-m",
            "fontTools.subset",
            str(src),
            f"--unicodes-file={listing}",
            "--flavor=woff2",
            "--layout-features-=liga,dlig,calt",
            f"--output-file={dest}",
        ]
    )


def fetch_sources() -> None:
    download(
        f"https://github.com/subframe7536/maple-font/releases/download/{MAPLE_TAG}/MapleMono-NF-CN.zip",
        CACHE / "MapleMono-NF-CN.zip",
    )
    maple = CACHE / "maple"
    regular = maple / "MapleMono-NF-CN-Regular.ttf"
    if not regular.exists():
        subprocess.check_call(
            [
                "unzip",
                "-qo",
                str(CACHE / "MapleMono-NF-CN.zip"),
                "MapleMono-NF-CN-Regular.ttf",
                "MapleMono-NF-CN-Bold.ttf",
                "LICENSE.txt",
                "-d",
                str(maple),
            ]
        )
    download(
        "https://github.com/rsms/inter/raw/master/LICENSE.txt",
        CACHE / "OFL-Inter.txt",
    )
    for name in ("latin-wght-normal.woff2", "latin-ext-wght-normal.woff2"):
        download(
            f"https://cdn.jsdelivr.net/fontsource/fonts/inter:vf@{INTER_VERSION}/{name}",
            CACHE / "inter" / name,
        )


def build_inter() -> tuple[list[str], str]:
    faces = []
    preload = ""
    for kind, urange, preload_this in (
        ("latin", INTER_LATIN, True),
        ("latin-ext", INTER_LATIN_EXT, False),
    ):
        data = (CACHE / "inter" / f"{kind}-wght-normal.woff2").read_bytes()
        filename = write_font(data, f"inter-{kind}")
        faces.append(emit_face("Inter", "100 900", filename, urange))
        if preload_this:
            preload = filename
    return faces, preload


def shared_icon_codepoints(regular: Path, bold: Path) -> set[int]:
    from fontTools.ttLib import TTFont
    from fontTools.pens.recordingPen import DecomposingRecordingPen

    fonts = [TTFont(regular), TTFont(bold)]
    cmaps = [font.getBestCmap() for font in fonts]
    glyphs = [font.getGlyphSet() for font in fonts]

    def hinted(font: TTFont, name: str) -> bool:
        glyph = font["glyf"][name]
        program = getattr(glyph, "program", None)
        if program and program.getBytecode():
            return True
        return glyph.isComposite() and any(
            hinted(font, component.glyphName) for component in glyph.components
        )

    shared = set()
    for cp in cmaps[0].keys() & cmaps[1].keys():
        # Powerline has its own small, immediately available slices. Only
        # deduplicate PUA icons with identical outlines AND horizontal metrics.
        if not (0xE000 <= cp <= 0xF8FF or 0xF0000 <= cp <= 0x10FFFD):
            continue
        if 0xE0A0 <= cp <= 0xE0D4:
            continue
        # The weights have different CVT tables. Keep hinted glyphs separate
        # even when their unhinted outlines match.
        if any(hinted(fonts[i], cmaps[i][cp]) for i in range(2)):
            continue
        pens = [DecomposingRecordingPen(glyph_set) for glyph_set in glyphs]
        for i in range(2):
            glyphs[i][cmaps[i][cp]].draw(pens[i])
        if (
            pens[0].value == pens[1].value
            and fonts[0]["hmtx"][cmaps[0][cp]] == fonts[1]["hmtx"][cmaps[1][cp]]
        ):
            shared.add(cp)
    for font in fonts:
        font.close()
    return shared


def build_maple() -> tuple[list[str], list[str], list[str]]:
    preloads: list[str] = []
    static_faces: list[str] = []
    symbol_faces: list[str] = []
    regular = CACHE / "maple" / "MapleMono-NF-CN-Regular.ttf"
    bold = CACHE / "maple" / "MapleMono-NF-CN-Bold.ttf"
    shared_icons = shared_icon_codepoints(regular, bold)
    icons = CACHE / "built" / "maple-icons.woff2"
    subset(regular, ",".join(f"U+{cp:X}" for cp in sorted(shared_icons)), icons)
    filename = write_font(icons.read_bytes(), "maple-icons")
    # Unicode slices form a composite face only when their descriptors match.
    # A single 100-900 icon face would shadow the 100-500/600-900 Latin faces
    # during font matching, sending Latin glyphs to the proportional fallback.
    for weight in ("100 500", "600 900"):
        symbol_faces.append(
            emit_face("Maple Mono NF CN", weight, filename, unicode_range_from_font(icons))
        )
    print("shared icons", len(shared_icons))
    excluded = list(CJK_RANGES)
    for _name, unicodes, _preload in MAPLE_SLICES:
        start, end = unicodes.removeprefix("U+").split("-")
        excluded.append((int(start, 16), int(end, 16)))
    for src_name, weight, prefix in (
        ("MapleMono-NF-CN-Regular.ttf", "100 500", "maple-400"),
        ("MapleMono-NF-CN-Bold.ttf", "600 900", "maple-700"),
    ):
        src = CACHE / "maple" / src_name
        for name, unicodes, preload in MAPLE_SLICES:
            dest = CACHE / "built" / f"{prefix}-{name}.woff2"
            subset(src, unicodes, dest)
            data = dest.read_bytes()
            filename = write_font(data, f"{prefix}-{name}")
            static_faces.append(
                emit_face(
                    "Maple Mono NF CN",
                    weight,
                    filename,
                    unicode_range_from_font(dest),
                )
            )
            if preload:
                preloads.append(filename)
        rest = CACHE / "built" / f"{prefix}-symbols.woff2"
        subset_remainder(src, excluded, shared_icons, rest)
        filename = write_font(rest.read_bytes(), f"{prefix}-symbols")
        symbol_faces.append(
            emit_face("Maple Mono NF CN", weight, filename, unicode_range_from_font(rest))
        )
    return static_faces, symbol_faces, preloads


def write_licenses() -> None:
    licenses = PUBLIC / "licenses"
    licenses.mkdir(parents=True)
    shutil.copy(CACHE / "OFL-Inter.txt", licenses / "OFL-Inter.txt")
    shutil.copy(CACHE / "maple" / "LICENSE.txt", licenses / "OFL-MapleMono.txt")
    (licenses / "NOTICE.txt").write_text(
        """WebMux self-hosted fonts

Inter (Latin UI) — SIL Open Font License 1.1. See OFL-Inter.txt.
Maple Mono NF CN v7.9 (terminal, code and Nerd Font icons) — SIL Open Font
License 1.1. See OFL-MapleMono.txt. CJK glyphs and ligature features are
removed; CJK text uses system sans-serif fonts.

HarmonyOS Sans SC is not included. Its license forbids modifications, which
rules out the unicode-range slices this app serves, and it does not allow
redistributing the font files on their own.
"""
    )


def write_preloads(filenames: list[str]) -> None:
    links = []
    for filename in filenames:
        links.append(
            "    <link\n"
            '      rel="preload"\n'
            f'      href="/fonts/{filename}"\n'
            '      as="font"\n'
            '      type="font/woff2"\n'
            '      crossorigin="anonymous"\n'
            "    />"
        )
    block = "<!-- font-preload:start -->\n" + "\n".join(links) + "\n    <!-- font-preload:end -->"
    html = INDEX.read_text()
    html, count = re.subn(
        r"<!-- font-preload:start -->.*?<!-- font-preload:end -->",
        block,
        html,
        count=1,
        flags=re.S,
    )
    if count != 1:
        raise SystemExit("index.html is missing font-preload markers")
    INDEX.write_text(html)


def main() -> None:
    fetch_sources()
    if PUBLIC.exists():
        shutil.rmtree(PUBLIC)
    PUBLIC.mkdir(parents=True)
    inter, inter_preload = build_inter()
    maple_static, maple_symbols, maple_preloads = build_maple()
    write_licenses()
    header = (
        "/* Generated by web/scripts/build-fonts.py. Latin slices are preloaded;\n"
        "   Symbols load through unicode-range; CJK uses system fonts. */\n"
    )
    CSS_OUT.write_text(header + "\n".join(inter + maple_static))
    TERMINAL_CSS.write_text(header + "\n".join(maple_symbols))
    write_preloads([inter_preload, *maple_preloads])
    total = sum(path.stat().st_size for path in PUBLIC.rglob("*.woff2"))
    print(f"woff2 files {len(list(PUBLIC.rglob('*.woff2')))} bytes {total}")
    print("preload", [inter_preload, *maple_preloads])


if __name__ == "__main__":
    main()
