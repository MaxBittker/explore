// Public site origin, used for share links.
export const SITE_URL = "https://river.maxbittker.com";

// Backend origin. Defaults to production; set VITE_API_BASE (e.g. in
// site/.env.local) to http://localhost:6000 to develop against a local server.
export const API_BASE = import.meta.env.VITE_API_BASE || SITE_URL;
