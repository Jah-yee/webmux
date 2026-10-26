// A DPR change need not resize the CSS box; re-arm the query for each new DPR.
export function observeDevicePixelRatio(changed: () => void) {
  let disposed = false;
  const query = () =>
    matchMedia(`(resolution: ${window.devicePixelRatio || 1}dppx)`);
  let resolution = query();
  const update = () => {
    if (disposed) return;
    resolution.removeEventListener("change", update);
    resolution = query();
    resolution.addEventListener("change", update);
    changed();
  };
  resolution.addEventListener("change", update);
  return () => {
    disposed = true;
    resolution.removeEventListener("change", update);
  };
}
