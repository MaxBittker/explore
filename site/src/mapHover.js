// Which grid image the pointer is over, shared between the grid (Block) and
// the minimap without re-rendering everything in between.

let hovered = null;
const listeners = new Set();

export function setHoveredBlock(id) {
    if (id === hovered) return;
    hovered = id;
    listeners.forEach((fn) => fn());
}

export function subscribeHoveredBlock(fn) {
    listeners.add(fn);
    return () => listeners.delete(fn);
}

export const getHoveredBlock = () => hovered;
