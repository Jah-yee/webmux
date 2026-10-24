// Shared Tailwind classes for 44px (`11`) touch targets. The two conventions
// answer different questions and are not interchangeable.

// Narrow layouts: 44px below the `sm` breakpoint, 36px (`9`) from `sm` up.
export const narrowTouchSize = "size-11 sm:size-9";
export const narrowTouchHeight = "h-11 sm:h-9";
export const narrowTouchMinHeight = "min-h-11 sm:min-h-9";

// Coarse pointers: at least 44px tall whenever any coarse pointer is present,
// at every width.
export const coarseTouchMinHeight = "[@media(any-pointer:coarse)]:min-h-11";
