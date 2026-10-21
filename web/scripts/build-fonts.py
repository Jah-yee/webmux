#!/usr/bin/env python3
"""Rebuild the self-hosted WebMux webfonts.

The app commits the generated woff2 files and web/src/fonts.css. Re-run this
only when upgrading Inter, Noto Sans SC, or Maple Mono.

Tools are intentionally outside the frontend lockfile (cn-font-split ships a
native library, and the web package allows only the esbuild install script):

  python3 -m pip install --user fonttools brotli
  # the script npm-installs cn-font-split into a temp directory

HarmonyOS Sans is not fetched: its license forbids modifying the files, so
unicode-range subsetting is not allowed. UI Chinese uses Noto Sans SC.
"""

from __future__ import annotations

import hashlib
import os
import re
import shutil
import subprocess
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PUBLIC = ROOT / "web" / "public" / "fonts"
CSS_OUT = ROOT / "web" / "src" / "fonts.css"
TERMINAL_CSS = ROOT / "web" / "src" / "fonts-terminal.css"
INDEX = ROOT / "web" / "index.html"
CACHE = Path(os.environ.get("WEBMUX_FONT_CACHE", "/tmp/webmux-fonts"))
SPLITTER = CACHE / "cn-font-split"
MAPLE_TAG = "v7.9"
NOTO_VERSION = "5.3.1"
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
# slices so a prompt or TUI does not pull a CJK pack, without joining the
# initial preload.
MAPLE_SLICES = (
    ("latin", "U+0000-00FF", True),
    ("latin-ext", "U+0100-024F", False),
    ("box", "U+2500-259F", False),
    ("powerline", "U+E0A0-E0D4", False),
)


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
            "python3",
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


def subset_remainder(src: Path, excluded: list[tuple[int, int]], dest: Path) -> None:
    from fontTools.ttLib import TTFont

    font = TTFont(src)
    keep = []
    for cp in font.getBestCmap() or []:
        if any(start <= cp <= end for start, end in excluded):
            continue
        keep.append(cp)
    font.close()
    listing = dest.with_suffix(".unicodes.txt")
    listing.write_text(",".join(f"U+{cp:04X}" for cp in keep))
    subprocess.check_call(
        [
            "python3",
            "-m",
            "fontTools.subset",
            str(src),
            f"--unicodes-file={listing}",
            "--layout-features-=liga,dlig,calt",
            f"--output-file={dest}",
        ]
    )


def ensure_splitter() -> None:
    if (SPLITTER / "node_modules" / "cn-font-split").exists():
        return
    SPLITTER.mkdir(parents=True, exist_ok=True)
    subprocess.check_call(["npm", "init", "-y"], cwd=SPLITTER)
    subprocess.check_call(["npm", "install", "cn-font-split@7.4.3"], cwd=SPLITTER)


def split_cjk(src: Path, out_dir: Path, family: str, weight: str) -> None:
    if out_dir.exists():
        shutil.rmtree(out_dir)
    out_dir.mkdir(parents=True)
    script = SPLITTER / "split-one.mjs"
    script.write_text(
        """
import { fontSplit } from "cn-font-split";
import fs from "fs";
const input = new Uint8Array(fs.readFileSync(process.argv[2]));
await fontSplit({
  input,
  outDir: process.argv[3],
  chunkSize: 64 * 1024,
  fontFeature: false,
  languageAreas: true,
  reduceMins: true,
  testHtml: false,
  reporter: false,
  renameOutputFont: "[hash:8].[ext]",
  silent: true,
  css: {
    fontFamily: process.argv[4],
    fontWeight: process.argv[5],
    fontStyle: "normal",
    fontDisplay: "block",
    localFamily: [],
    commentBase: false,
    commentNameTable: false,
    commentUnicodes: false,
    compress: false,
  },
});
""".lstrip()
    )
    env = os.environ.copy()
    env["NODE_PATH"] = str(SPLITTER / "node_modules")
    subprocess.check_call(
        ["node", str(script), str(src), str(out_dir), family, weight],
        cwd=SPLITTER,
        env=env,
    )


def faces_from_split(css_path: Path, prefix: str) -> list[str]:
    css = css_path.read_text()
    faces = []
    for block in re.findall(r"@font-face\s*\{(.*?)\}", css, flags=re.S):
        src = re.search(r'url\("\./([^"]+\.woff2)"\)', block)
        urange = re.search(r"unicode-range:\s*([^;]+);", block)
        weight = re.search(r"font-weight:\s*([^;]+);", block)
        family = re.search(r'font-family:\s*"([^"]+)"', block)
        if not (src and urange and weight and family):
            raise SystemExit(f"unparsed face in {css_path}")
        data = (css_path.parent / src.group(1)).read_bytes()
        filename = write_font(data, prefix)
        faces.append(
            emit_face(family.group(1), weight.group(1).strip(), filename, urange.group(1).strip())
        )
    if not faces:
        raise SystemExit(f"no faces in {css_path}")
    return faces


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
    download(
        "https://github.com/notofonts/latin-greek-cyrillic/raw/main/OFL.txt",
        CACHE / "OFL-Noto.txt",
    )
    for name in ("latin-wght-normal.woff2", "latin-ext-wght-normal.woff2"):
        download(
            f"https://cdn.jsdelivr.net/fontsource/fonts/inter:vf@{INTER_VERSION}/{name}",
            CACHE / "inter" / name,
        )
    noto_dir = CACHE / "noto"
    for weight in ("400", "700"):
        css_path = noto_dir / f"{weight}.css"
        download(
            f"https://cdn.jsdelivr.net/npm/@fontsource/noto-sans-sc@{NOTO_VERSION}/{weight}.css",
            css_path,
        )
        for filename in re.findall(r"files/(noto-sans-sc-[^)]+\.woff2)", css_path.read_text()):
            download(
                f"https://cdn.jsdelivr.net/npm/@fontsource/noto-sans-sc@{NOTO_VERSION}/files/{filename}",
                noto_dir / "files" / filename,
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


def build_noto() -> list[str]:
    faces = []
    for weight, css_weight in (("400", "100 500"), ("700", "600 900")):
        css = (CACHE / "noto" / f"{weight}.css").read_text()
        for block in re.findall(r"@font-face\s*\{([^}]+)\}", css):
            src = re.search(r"url\(\./files/([^)]+\.woff2)\)", block)
            urange = re.search(r"unicode-range:\s*([^;]+);", block)
            if not (src and urange):
                raise SystemExit("unparsed Noto face")
            data = (CACHE / "noto" / "files" / src.group(1)).read_bytes()
            filename = write_font(data, f"noto-{weight}")
            faces.append(emit_face("Noto Sans SC", css_weight, filename, urange.group(1).strip()))
    return faces


def build_maple() -> tuple[list[str], list[str], list[str]]:
    ensure_splitter()
    preloads: list[str] = []
    static_faces: list[str] = []
    split_faces: list[str] = []
    excluded = []
    for _name, unicodes, _preload in MAPLE_SLICES:
        start, end = unicodes.removeprefix("U+").split("-")
        excluded.append((int(start, 16), int(end, 16)))
    for label, src_name, weight, prefix in (
        ("regular", "MapleMono-NF-CN-Regular.ttf", "100 500", "maple-400"),
        ("bold", "MapleMono-NF-CN-Bold.ttf", "600 900", "maple-700"),
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
            if preload and label == "regular":
                preloads.append(filename)
            if preload and label == "bold":
                preloads.append(filename)
        rest = CACHE / "built" / f"{prefix}-rest.ttf"
        subset_remainder(src, excluded, rest)
        split_dir = CACHE / "built" / f"{prefix}-split"
        split_cjk(rest, split_dir, "Maple Mono NF CN", weight)
        split_faces.extend(faces_from_split(split_dir / "result.css", prefix))
    return static_faces, split_faces, preloads


def write_licenses() -> None:
    licenses = PUBLIC / "licenses"
    licenses.mkdir(parents=True)
    shutil.copy(CACHE / "OFL-Inter.txt", licenses / "OFL-Inter.txt")
    shutil.copy(CACHE / "OFL-Noto.txt", licenses / "OFL-NotoSansSC.txt")
    shutil.copy(CACHE / "maple" / "LICENSE.txt", licenses / "OFL-MapleMono.txt")
    (licenses / "NOTICE.txt").write_text(
        """WebMux self-hosted fonts

Inter (Latin UI) — SIL Open Font License 1.1. See OFL-Inter.txt.
Maple Mono NF CN v7.9 (terminal, code, Nerd Font icons, Simplified Chinese) —
SIL Open Font License 1.1. See OFL-MapleMono.txt. Ligature features are
removed so xterm cell widths stay stable.
Noto Sans SC 5.3.1 (Chinese UI, weights 400 and 700) — SIL Open Font License
1.1. See OFL-NotoSansSC.txt.

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
    noto = build_noto()
    maple_static, maple_split, maple_preloads = build_maple()
    write_licenses()
    header = (
        "/* Generated by web/scripts/build-fonts.py. Latin slices are preloaded;\n"
        "   CJK and Nerd Font slices load through unicode-range. */\n"
    )
    CSS_OUT.write_text(header + "\n".join(inter + noto + maple_static))
    TERMINAL_CSS.write_text(header + "\n".join(maple_split))
    write_preloads([inter_preload, *maple_preloads])
    total = sum(path.stat().st_size for path in PUBLIC.rglob("*.woff2"))
    print(f"woff2 files {len(list(PUBLIC.rglob('*.woff2')))} bytes {total}")
    print("preload", [inter_preload, *maple_preloads])


if __name__ == "__main__":
    main()
