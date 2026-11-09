import { API_BASE } from "./config";

// Impression and click beacons for block_stats. An impression is a loaded
// thumbnail at least half on screen, counted once per page view (until
// resetImpressions). Ids are sent with sendBeacon (survives unloads, posts
// text/plain so there's no CORS preflight) when the page is hidden (tab
// switch, app switch, close), and at most once a FLUSH_MS while browsing, so
// a tab killed without a hide event (iOS, in-app browsers) loses at most a
// minute. Idle tabs send nothing; a short visit costs one small request.

const FLUSH_MS = 60000;
const MAX_BATCH = 500;

let seen = new Set();
let impressions = [];
let clicks = [];

function flush() {
    if (impressions.length === 0 && clicks.length === 0) return;
    const body = JSON.stringify({ i: impressions, c: clicks });
    impressions = [];
    clicks = [];
    try {
        navigator.sendBeacon(API_BASE + "/api/stats", body);
    } catch (e) {
        // Stats are best effort.
    }
}

function queued() {
    if (impressions.length + clicks.length >= MAX_BATCH) flush();
}

const observer = typeof IntersectionObserver === "undefined" ? null :
    new IntersectionObserver((entries) => {
        for (const entry of entries) {
            if (!entry.isIntersecting) continue;
            const id = Number(entry.target.dataset.bid);
            observer.unobserve(entry.target);
            if (!id || seen.has(id)) continue;
            seen.add(id);
            impressions.push(id);
        }
        queued();
    }, { threshold: 0.5 });

// observeImpression watches el (carrying data-bid) until it's half on
// screen. Returns the cleanup.
export function observeImpression(el) {
    if (!observer || !el || seen.has(Number(el.dataset.bid))) return () => { };
    observer.observe(el);
    return () => observer.unobserve(el);
}

export function recordClick(id) {
    clicks.push(id);
    queued();
}

// A new page view (anchor) counts every block afresh.
export function resetImpressions() {
    seen = new Set();
}

if (typeof window !== "undefined") {
    setInterval(flush, FLUSH_MS);
    document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "hidden") flush();
    });
    window.addEventListener("pagehide", flush);
}
