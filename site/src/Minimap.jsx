import { useState, useEffect, useRef, useCallback, useSyncExternalStore } from "react";
import { API_BASE } from "./config";
import { subscribeHoveredBlock, getHoveredBlock, setHoveredBlock } from "./mapHover";
import "./minimap.css";

// A map of the whole collection, seen through a fisheye lens. The collection
// is cut into ~1500 cells with fixed positions (a 2D layout of every image);
// the server says where the current block or search is on it (minimap.go),
// and the lens centers there: the surroundings are magnified, the rest of
// the world is squeezed toward the edges. Nothing ever changes place, so moving
// to a similar image just slides the view a little. On home the lens is off
// and the whole map shows.
//
// A flat layout can't keep everything that is similar side by side, so the
// cells that really are most similar to the current place are inked darker
// wherever they lie. The path walked this session is drawn through the map,
// and clicking a dot goes to an image there. Hovering an image in the grid
// points out where it is on the map. It lives in the info panel.

const INK = "rgb(88, 88, 88)";
const PAD = 7; // px between the panel edge and the edge of the map
const HOVER_REACH = 9; // px
const MOVE_MS = 600;
const TRAIL_SHOWN = 12;
const TRAIL_MAX = 40;

// A lens is {x, y, zoom}: its center on the map and how much the map is
// magnified there, in panel half-widths per map unit. Each axis is bent on
// its own, and each side of the center separately, so that the edge of the
// world on that side lands on the edge of the panel: the map always fills
// the panel, however far off-center the lens sits. A side with span s draws
// distance d at (b+1)t / (bt+1), t = d/s, with b = zoom*s - 1: the scale is
// `zoom` at the center on every side and falls off smoothly toward the edge.
// A side too short to need bending (zoom*s <= 1) is drawn straight.
const ZOOM = 6;
const lensAt = (place) => ({ x: place.x, y: place.y, zoom: ZOOM });

function bend(d, before, after, zoom) {
    const span = d < 0 ? before : after;
    const a = Math.abs(d);
    const b = zoom * span - 1;
    if (b <= 0) return Math.sign(d) * Math.min(zoom * a, 1);
    const t = Math.min(a / span, 1);
    return Math.sign(d) * (((b + 1) * t) / (b * t + 1));
}

// Map point -> offset from the panel's center in half-widths across and
// half-heights down, plus how far out that is (0 center, 1 edge). box is the
// world's extent; tall is the panel's height over its width, which the
// vertical zoom makes up for so the center is magnified the same both ways.
function project(lens, box, tall, x, y) {
    const px = bend(x - lens.x, lens.x - box.x0, box.x1 - lens.x, lens.zoom);
    const py = bend(y - lens.y, lens.y - box.y0, box.y1 - lens.y, lens.zoom / tall);
    return [px, py, Math.max(Math.abs(px), Math.abs(py))];
}

// The trail (places visited, oldest first) survives reloads within a tab,
// like the history it mirrors.
function loadTrail() {
    try {
        const saved = JSON.parse(sessionStorage.getItem("mapTrail")) || [];
        return saved.filter((p) => typeof p.x === "number");
    } catch (e) {
        return [];
    }
}
let trailStore = loadTrail();
function pushTrail(key, place) {
    const last = trailStore[trailStore.length - 1];
    if (last?.key === key) return trailStore;
    trailStore = [...trailStore, { key, x: place.x, y: place.y }].slice(-TRAIL_MAX);
    try {
        sessionStorage.setItem("mapTrail", JSON.stringify(trailStore));
    } catch (e) {
        // Private mode: the trail just won't survive a reload.
    }
    return trailStore;
}

let cellData = null;
function loadCells() {
    cellData = cellData || fetch("/minimap.json")
        .then((res) => {
            if (!res.ok) throw new Error("minimap " + res.status);
            return res.json();
        })
        .then(({ cells }) => {
            const count = cells.length / 3;
            let most = 1;
            for (let c = 0; c < count; c++) most = Math.max(most, cells[3 * c + 2]);
            // The world's extent: the box holding all but the outermost 0.5%
            // of images on each side. A few specks lie far out at sea; framing
            // them would leave the panel mostly empty, so they're drawn at
            // the edge instead.
            let total = 0;
            for (let c = 0; c < count; c++) total += cells[3 * c + 2];
            const edge = (axis, share) => {
                const order = Array.from({ length: count }, (_, c) => c)
                    .sort((a, b) => cells[3 * a + axis] - cells[3 * b + axis]);
                let seen = 0;
                for (const c of order) {
                    seen += cells[3 * c + 2];
                    if (seen >= share * total) return cells[3 * c + axis] / 1000;
                }
            };
            const box = { x0: edge(0, 0.005), x1: edge(0, 0.995), y0: edge(1, 0.005), y1: edge(1, 0.995) };
            return {
                count,
                box,
                // The panel takes the world's shape, so the whole map fits it.
                tall: (box.y1 - box.y0) / (box.x1 - box.x0),
                // The lens that shows the whole map flat.
                whole: {
                    x: (box.x0 + box.x1) / 2,
                    y: (box.y0 + box.y1) / 2,
                    zoom: 2 / (box.x1 - box.x0),
                },
                x: (c) => cells[3 * c] / 1000,
                y: (c) => cells[3 * c + 1] / 1000,
                // 0..1, by area: a dot's area tracks how many images the cell holds
                weight: (c) => Math.sqrt(cells[3 * c + 2] / most),
            };
        });
    return cellData;
}

// Where the current page is on the map: {key, x, y, near}. Null on home and
// until the answer for this page arrives.
function usePlace(id, search) {
    const key = id ? "id:" + id : search?.vector ? search.key : null;
    const vector = id ? null : search?.vector;
    const [place, setPlace] = useState(null);
    useEffect(() => {
        if (!key) return;
        let stale = false;
        const request = vector
            ? fetch(API_BASE + "/api/map/place", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ vector }),
            })
            : fetch(API_BASE + "/api/map/place?id=" + encodeURIComponent(id));
        request
            .then((res) => (res.ok ? res.json() : null))
            .then((p) => {
                if (!stale && p) setPlace({ key, ...p });
            })
            .catch(() => { });
        return () => {
            stale = true;
        };
    }, [key]);
    return place?.key === key ? place : null;
}

// Where the grid image under the pointer is: {id, x, y, near}, or null. Waits
// for the pointer to rest a moment, so sweeping across the grid doesn't ask
// about every image on the way; answers are kept for the session.
const PEEK_DELAY_MS = 70;
const peeks = new Map();
function usePeek() {
    const id = useSyncExternalStore(subscribeHoveredBlock, getHoveredBlock);
    const [peek, setPeek] = useState(null);
    useEffect(() => {
        if (id == null) return;
        if (peeks.has(id)) {
            setPeek(peeks.get(id));
            return;
        }
        let stale = false;
        const timer = setTimeout(() => {
            fetch(API_BASE + "/api/map/place?id=" + id)
                .then((res) => (res.ok ? res.json() : null))
                .then((p) => {
                    if (!p) return;
                    peeks.set(id, { id, ...p });
                    if (!stale) setPeek(peeks.get(id));
                })
                .catch(() => { });
        }, PEEK_DELAY_MS);
        return () => {
            stale = true;
            clearTimeout(timer);
        };
    }, [id]);
    return peek?.id === id ? peek : null;
}

const ease = (t) => (t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2);

export default function Minimap({ id, search, size, onJump, play }) {
    const [cells, setCells] = useState(null);
    const [trail, setTrail] = useState(trailStore);
    const [hover, setHover] = useState(null); // cell index under the pointer
    const [jumping, setJumping] = useState(false);
    const canvas = useRef(null);
    const lensNow = useRef(null); // the lens as drawn, mid-move included
    const place = usePlace(id, search);
    // Between pages, hold the last place rather than flashing the whole map.
    const pending = !!(id || search) && !place;
    const held = useRef(null);
    if (place) held.current = place;
    else if (!pending) held.current = null;
    const at = held.current;
    const peek = usePeek();
    // The grid is replaced on navigation without a pointerleave.
    useEffect(() => setHoveredBlock(null), [id, search?.key]);

    useEffect(() => {
        loadCells().then(setCells).catch(() => { });
    }, []);
    useEffect(() => {
        if (place) setTrail(pushTrail(place.key, place));
    }, [place]);

    // size is the panel's width; its height follows the world's shape.
    const tall = cells?.tall ?? 1;
    const height = Math.round(size * tall);
    const midX = size / 2, midY = height / 2;
    const reachX = midX - PAD, reachY = midY - PAD;
    const ratio = reachY / reachX;

    useEffect(() => {
        const el = canvas.current;
        if (!el || !cells) return;
        const dpr = window.devicePixelRatio || 1;
        el.width = size * dpr;
        el.height = height * dpr;
        const ctx = el.getContext("2d");
        ctx.scale(dpr, dpr);

        const near = new Set(at?.near);

        const draw = (lens) => {
            const spot = (x, y) => {
                const [px, py, r] = project(lens, cells.box, ratio, x, y);
                return [midX + px * reachX, midY + py * reachY, r];
            };
            ctx.clearRect(0, 0, size, height);
            ctx.fillStyle = INK;
            // Dots are sized for a 220px map and scale with a larger one.
            const dotScale = Math.max(1, size / 220);
            // Dots swell with the space around the center.
            const grow = Math.max(0, (lens.zoom - cells.whole.zoom) / (ZOOM - cells.whole.zoom));
            for (const pass of ["far", "near"]) {
                ctx.globalAlpha = pass === "far" ? 0.32 : 0.9;
                ctx.beginPath();
                for (let c = 0; c < cells.count; c++) {
                    if (near.has(c) !== (pass === "near")) continue;
                    const [x, y, out] = spot(cells.x(c), cells.y(c));
                    const r = dotScale * (0.55 + 1.5 * cells.weight(c)) * (1 + 1.3 * grow * (1 - out) * (1 - out));
                    ctx.moveTo(x + r, y);
                    ctx.arc(x, y, r, 0, 2 * Math.PI);
                }
                ctx.fill();
            }

            // The path walked, fading with age.
            ctx.strokeStyle = INK;
            ctx.lineWidth = 1;
            ctx.lineJoin = "round";
            const from = Math.max(trail.length - TRAIL_SHOWN, 1);
            for (let i = from; i < trail.length; i++) {
                const [x0, y0] = spot(trail[i - 1].x, trail[i - 1].y);
                const [x1, y1] = spot(trail[i].x, trail[i].y);
                ctx.globalAlpha = 0.15 + 0.7 * ((i - from + 1) / (trail.length - from));
                ctx.beginPath();
                ctx.moveTo(x0, y0);
                ctx.lineTo(x1, y1);
                ctx.stroke();
            }

            ctx.globalAlpha = 1;
        };

        // Slide the lens from where it is to the new place.
        const to = at ? lensAt(at) : cells.whole;
        const from = lensNow.current || to;
        const same = ["x", "y", "zoom"].every((k) => from[k] === to[k]);
        if (same || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
            lensNow.current = to;
            draw(to);
            return;
        }
        const start = performance.now();
        let raf = 0;
        const step = (t) => {
            const k = ease(Math.min((t - start) / MOVE_MS, 1));
            const lens = {};
            for (const key of ["x", "y", "zoom"]) lens[key] = from[key] + (to[key] - from[key]) * k;
            lensNow.current = k < 1 ? lens : to;
            draw(lensNow.current);
            if (k < 1) raf = requestAnimationFrame(step);
        };
        raf = requestAnimationFrame(step);
        return () => cancelAnimationFrame(raf);
    }, [cells, at, size, height, trail]);

    // Hit testing and the hover ring use the lens at rest.
    const lens = at ? lensAt(at) : cells?.whole;
    const spotAt = (x, y) => {
        const [px, py] = project(lens, cells.box, ratio, x, y);
        return [midX + px * reachX, midY + py * reachY];
    };
    const spotOf = (c) => spotAt(cells.x(c), cells.y(c));
    const cellAt = (e) => {
        if (!cells) return null;
        const box = canvas.current.getBoundingClientRect();
        const x = e.clientX - box.left, y = e.clientY - box.top;
        let best = null, bestD = HOVER_REACH * HOVER_REACH;
        for (let c = 0; c < cells.count; c++) {
            const [sx, sy] = spotOf(c);
            const d = (sx - x) * (sx - x) + (sy - y) * (sy - y);
            if (d < bestD) best = c, bestD = d;
        }
        return best;
    };

    const jump = useCallback((cell) => {
        if (cell == null || jumping) return;
        setJumping(true);
        play?.();
        fetch(API_BASE + "/api/map/jump?cell=" + cell)
            .then((res) => (res.ok ? res.json() : null))
            .then((image) => image && onJump(image.Id))
            .catch(() => { })
            .finally(() => setJumping(false));
    }, [jumping, onJump, play]);

    return (
        <div className={"minimap" + (jumping ? " jumping" : "")} style={{ width: size }}>
            <div className="minimap-world" style={{ width: size, height }}>
                <canvas
                    ref={canvas}
                    style={{ width: size, height, cursor: hover != null ? "pointer" : "default" }}
                    onPointerMove={(e) => e.pointerType === "mouse" && setHover(cellAt(e))}
                    onPointerLeave={() => setHover(null)}
                    onClick={(e) => jump(cellAt(e))}
                />
                {cells && hover != null && (
                    <div className="minimap-ring" style={{ left: spotOf(hover)[0], top: spotOf(hover)[1] }} />
                )}
                {cells && peek && (() => {
                    const [x, y] = spotAt(peek.x, peek.y);
                    return <>
                        {at && <div className="minimap-peek-line" style={{
                            left: midX, top: midY,
                            width: Math.hypot(x - midX, y - midY),
                            transform: `rotate(${Math.atan2(y - midY, x - midX)}rad)`,
                        }} />}
                        <div className="minimap-peek" style={{ left: x, top: y }} />
                    </>;
                })()}
                {at && <div className="minimap-here" style={{ left: midX, top: midY }} />}
            </div>
        </div>
    );
}
