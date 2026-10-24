import { describe, expect, test } from "vitest";
import { fullscreenOffered, popoutOffered } from "../src/capabilities";

describe("fullscreenOffered", () => {
  test.each([
    // iPad, desktop browsers and their web apps
    { supported: true, standalone: false, offered: true },
    { supported: true, standalone: true, offered: true },
    // iPhone Safari tab: kept, to suggest adding to the Home Screen
    { supported: false, standalone: false, offered: true },
    // iPhone Home Screen app: nothing to offer
    { supported: false, standalone: true, offered: false },
  ])(
    "supported=$supported standalone=$standalone → $offered",
    ({ offered, ...platform }) => {
      expect(fullscreenOffered(platform)).toBe(offered);
    },
  );
});

describe("popoutOffered", () => {
  test.each([
    // Document Picture-in-Picture never covers the opener.
    {
      documentPictureInPicture: true,
      finePointer: false,
      touch: true,
      standalone: true,
      offered: true,
    },
    // Desktop browser tab and desktop Safari web app
    {
      documentPictureInPicture: false,
      finePointer: true,
      touch: false,
      standalone: false,
      offered: true,
    },
    {
      documentPictureInPicture: false,
      finePointer: true,
      touch: false,
      standalone: true,
      offered: true,
    },
    // iPad with a trackpad: Safari tab keeps it, the Home Screen app does not
    {
      documentPictureInPicture: false,
      finePointer: true,
      touch: true,
      standalone: false,
      offered: true,
    },
    {
      documentPictureInPicture: false,
      finePointer: true,
      touch: true,
      standalone: true,
      offered: false,
    },
    // Phones, in a tab or a Home Screen app
    {
      documentPictureInPicture: false,
      finePointer: false,
      touch: true,
      standalone: false,
      offered: false,
    },
    {
      documentPictureInPicture: false,
      finePointer: false,
      touch: true,
      standalone: true,
      offered: false,
    },
  ])(
    "pip=$documentPictureInPicture fine=$finePointer touch=$touch standalone=$standalone → $offered",
    ({ offered, ...platform }) => {
      expect(popoutOffered(platform)).toBe(offered);
    },
  );
});
