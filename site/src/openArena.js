// Open an Are.na block. On desktop this is a small popup window instead of a
// tab: it's a real top-level window, so the viewer's Are.na login works there
// (an iframe wouldn't get it). Later blocks reuse the same popup.
export default function openArena(url) {
    const desktop = window.matchMedia("(hover: hover) and (pointer: fine)").matches && window.innerWidth >= 800;
    if (!desktop) {
        window.open(url, "_blank")?.focus();
        return;
    }
    // 2:3 (h:w), as large as fits: up to 60% of the browser width.
    const width = Math.round(Math.min(window.innerWidth * 0.6, (window.innerHeight - 64) * 1.5, 960));
    const height = Math.round(width * 2 / 3);
    // Near the top-right of the browser window, just below its toolbar.
    const chrome = window.outerHeight - window.innerHeight;
    // Clamped to the visible screen (the popup's own title/URL bar is ~80px).
    const s = window.screen;
    const minX = s.availLeft ?? 0, minY = s.availTop ?? 0;
    const left = Math.round(clamp(window.screenX + window.outerWidth - width - 16, minX, minX + s.availWidth - width));
    const top = Math.round(clamp(window.screenY + chrome + 48, minY, minY + s.availHeight - height - 80));
    const popup = window.open(url, "arena", `popup,width=${width},height=${height},left=${left},top=${top}`);
    if (popup) popup.focus();
    else window.open(url, "_blank"); // popup blocked: fall back to a tab
}

const clamp = (v, lo, hi) => Math.max(lo, Math.min(v, hi));
