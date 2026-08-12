// Vite type declarations — declares module specifiers for static asset imports
// (.png, .jpg, .svg, .webm) so TypeScript treats them as `string`.
//
// Without this, npx tsc complains "Cannot find module './*.png'".
/// <reference types="vite/client" />
