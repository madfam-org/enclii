package frameworks

// Inline SVG icons. These are deliberately minimal monochrome marks so
// they inherit text color via currentColor, render at any size, and
// don't depend on external assets at build or runtime.
//
// Rationale for going minimal instead of pixel-perfect brand marks:
//   - Inlining reduces first-paint latency (no network fetch).
//   - Monochrome simplifies light/dark theming.
//   - Simplified geometry stays within the viewBox=0 0 24 24 grid.
//
// If a pixel-perfect brand is needed, the downstream consumer can
// override by mapping the slug to a CDN asset — the catalog Framework
// still returns the structured data they need.

const iconNextJS = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 2a8 8 0 016.18 13.08L10 7H8v10h2V9.5l9 10.12A8 8 0 1112 4zm4 5h2v6h-2z"/></svg>`

const iconRemix = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M4 4h10c3 0 5 2 5 4.5s-2 4-4.5 4.5H8v-1.5h6.5c1 0 2-.6 2-2S15.5 7.5 14.5 7.5H6v10H4V4zm10 10h4l2 6h-3l-2-5h-5v5H8v-6h6z"/></svg>`

const iconSvelteKit = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M20 7.5c-.8-2-3-3.3-5.3-3.3-1.1 0-2.2.3-3.1.8L6.9 8c-2 1.2-2.7 3.7-1.7 5.7.3.6.7 1.1 1.2 1.4-.2.7-.3 1.4-.2 2.1.1 1.5.9 2.9 2.2 3.7.9.6 2 .9 3.1.9.3 0 .6 0 .9-.1 1-.1 2-.5 2.8-1l4.6-3c2-1.2 2.7-3.7 1.7-5.7-.3-.6-.7-1.1-1.2-1.4.2-1 .1-2-.3-2.9zM10.4 18.7c-1 .3-2-.1-2.7-.9-.7-.9-.8-2.1-.3-3.1l.1-.2.1-.3.2.2 2 1.4c.4.3.9.3 1.3.1l4.5-2.9-.5 2.3-.1.3-4.6 3zm8-8.5c-.2.3-.4.6-.8.8l-4.5 2.9c-.4.3-.9.3-1.3.1l-2.8-1.9c-.4-.3-.7-.7-.8-1.2-.2-.9.2-1.8 1-2.3L13.7 6c.4-.3.9-.3 1.3-.1l2.8 1.9c.4.3.7.7.8 1.2.2.4.1.8-.2 1.2z"/></svg>`

const iconNuxt = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M13.7 4.5c-.8-1.3-2.7-1.3-3.5 0L3.3 16c-.8 1.3.1 3 1.7 3h4V17H6.8l5.2-9 2 3.5L11.2 17h2.5l1.3-2.3L17.6 19H22c1.6 0 2.5-1.7 1.7-3l-3.9-6.8c-.8-1.3-2.7-1.3-3.5 0l-.8 1.4-2.8-6.1z"/></svg>`

const iconAstro = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M12 2l5 18-5-3-5 3L12 2zm0 5.5L9.5 16 12 14.5 14.5 16 12 7.5z"/></svg>`

const iconNestJS = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M8.5 2.5c-1.5.3-2.6 1-3.4 2.1.7-.2 1.5-.2 2.1.1.6.3 1 .7 1.4 1.2.3.5.5 1.1.5 1.7 0 .7-.2 1.3-.6 1.8-.4.5-.9.9-1.5 1.1l.4.4c1 1 2.2 1.6 3.4 1.8-.8 2.5-1.3 5-1.5 7.6-.1 1 .5 1.9 1.5 2.1 1 .2 2-.4 2.3-1.4l.3-1.1c.4-1.2 1-2.4 1.7-3.5.8-1.2 1.7-2.3 2.8-3.2-.3-.3-.5-.6-.7-1-.3.2-.7.4-1.1.5-1.2.3-2.3-.3-2.6-1.5-.3-1.2.4-2.3 1.5-2.6.5-.1 1-.1 1.5 0-.3-.8-.8-1.5-1.4-2.1-1.5-1.4-3.5-2.1-5.6-2z"/></svg>`

const iconVite = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M2 4h20L12 22 2 4zm4 1.5L12 17l6-11.5H6z"/></svg>`

const iconReact = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><circle cx="12" cy="12" r="2" fill="currentColor"/><ellipse cx="12" cy="12" rx="9" ry="3.5" stroke="currentColor" stroke-width="1.3" fill="none"/><ellipse cx="12" cy="12" rx="9" ry="3.5" stroke="currentColor" stroke-width="1.3" fill="none" transform="rotate(60 12 12)"/><ellipse cx="12" cy="12" rx="9" ry="3.5" stroke="currentColor" stroke-width="1.3" fill="none" transform="rotate(120 12 12)"/></svg>`

const iconVue = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M2 4h4l6 10L18 4h4L12 22 2 4zm5.5 0h2L12 8l2.5-4h2L12 11.5 7.5 4z"/></svg>`

const iconAngular = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M12 2l9 3.2-1.4 12.3L12 22l-7.6-4.5L3 5.2 12 2zm0 2.3L5.3 6.7l1.1 9.8L12 19.8l5.6-3.3 1.1-9.8L12 4.3zM8.2 15.5L12 6.8l3.8 8.7h-2l-.7-1.8h-2.2l-.7 1.8h-2zm3-3.5h1.6L12 9.8l-.8 2.2z"/></svg>`

const iconExpress = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M2 7h2.4l3.4 5 3.4-5h2.3l-4.6 6.5 4.9 7h-2.4l-3.6-5.4-3.6 5.4H1.5l4.9-7L2 7zm14 0c3 0 5 2 5 5v1H16.5c.2 2 1.4 3.2 3 3.2 1.1 0 2-.5 2.5-1.5h1.5c-.6 1.9-2.3 3.3-4.5 3.3-3 0-5-2.2-5-5.5S13 7 16 7zm0 1.5c-1.6 0-2.8 1-3.2 2.6h6.4c-.3-1.6-1.5-2.6-3.2-2.6z"/></svg>`

const iconFastify = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M3 4h18v3H11l-1 3h10v3H9l-1 3h12v4H3V4z"/></svg>`

const iconDjango = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M7 3h3v14.3c-1.7.4-2.8.4-4 0-1.9-.7-2.7-2.3-2.7-4.5 0-2 .8-3.5 2.4-4.3 1-.4 2.3-.5 3.3-.2V3zm0 7.2c-.5-.1-1-.1-1.5.1-.9.4-1.3 1.3-1.3 2.6 0 1.4.4 2.2 1.3 2.6.4.2.9.2 1.5.1v-5.4zm5-4h3v14h-3V6.2zm0-3.2h3v2.5h-3V3z"/></svg>`

const iconFastAPI = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M12 2a10 10 0 100 20 10 10 0 000-20zm-1 4h3l-1 6h2l-4 8 1-6H9l2-8z"/></svg>`

const iconFlask = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M10 2h4v2h-1v3.2l5 9.6c.9 1.7-.3 3.7-2.2 3.7H8.2c-2 0-3.1-2-2.2-3.7l5-9.6V4h-1V2zm1 5.5L6.6 15h10.8L13 7.5V4h-2v3.5z"/></svg>`

const iconRails = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M3 6h18l-9 15L3 6zm4.5 1.5L12 16.5l4.5-9h-9z"/></svg>`

const iconPhoenix = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M12 2c-4 3-6 6-6 9 0 2 1 4 3 5-1 1-1 2 0 3 1 1 3 2 3 2s-1-2 0-3 3 0 3 0-2-1-2-3 2-3 4-3c0 0-1-2-3-3s-2-3-2-3c3 1 5 2 6 3 0-2-2-5-6-7z"/></svg>`

const iconGo = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M2 11h4v1.5H2V11zm2-3h4v1.5H4V8zm2-3h4v1.5H6V5zm5-1c3.5 0 6.5 2.5 7 6 0 .5-.4 1-1 1h-2c-.6 0-1-.5-1-1 0-1.5-1.5-2.5-3-2.5-2 0-3.5 1.5-3.5 3.5s1.5 3.5 3.5 3.5c1 0 2-.5 2.5-1.5h-2v-2h4.5c.3 0 .5.2.5.5v3.5c-1 2-3 3-5.5 3-3.5 0-6.5-3-6.5-6.5S8 4 12 4z"/></svg>`

const iconRust = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M12 2l2.5 2 3-.5 1 3 2.5 2-1.5 3 .5 3-3 1-2 2.5-3-1-3 1-2-2.5-3-1 .5-3L2 11l2.5-2 1-3 3 .5L11 4zm0 4c-3.3 0-6 2.7-6 6s2.7 6 6 6 6-2.7 6-6-2.7-6-6-6zm0 2h3c1 0 1.5.5 1.5 1.5S16 11 15 11h-1v2l1 2h-2l-1-2h-1v2h-2V8h3zm-1 1.5V10h1.5c.3 0 .5-.2.5-.5s-.2-.5-.5-.5H11z"/></svg>`

const iconDocker = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M3 10h2v2H3v-2zm3 0h2v2H6v-2zm3 0h2v2H9v-2zm3 0h2v2h-2v-2zm-6-3h2v2H6V7zm3 0h2v2H9V7zm3 0h2v2h-2V7zm0-3h2v2h-2V4zM2 13h18c0 2-1 4-3 5-2 1-5 1-8 1s-6 0-8-2c-1-1 0-3 1-4z"/></svg>`

const iconStatic = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M4 4h16c1 0 2 1 2 2v12c0 1-1 2-2 2H4c-1 0-2-1-2-2V6c0-1 1-2 2-2zm0 4v10h16V8H4zm2 2h5v2H6v-2zm0 3h5v2H6v-2zm7-3h5v5h-5v-5z"/></svg>`

const iconUnknown = `<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path fill="currentColor" d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 2a8 8 0 110 16 8 8 0 010-16zm-1 4c2 0 3.5 1.1 3.5 3 0 1.4-.8 2.1-2 2.6-.8.3-1 .6-1 1.2V15h-2v-.4c0-1.2.5-2 1.6-2.4.8-.3 1.2-.6 1.2-1.3 0-.7-.5-1.1-1.3-1.1-.9 0-1.4.5-1.5 1.3H8.3c0-1.7 1.2-2.8 3.2-2.8zM10.8 16.5h2v2h-2v-2z"/></svg>`
