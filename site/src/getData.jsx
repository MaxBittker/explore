import { useState, useEffect, useCallback } from "react";
import seedrandom from "seedrandom";
import { API_BASE } from "./config";

export default function useGetData(id, colCount, search) {
    const [lastItem, setLastItem] = useState(null);
    // offset is the cursor for the current request, in the server's paging
    // units (result rank, or page number on home). It only ever advances to a
    // nextOffset the server reported: pages are deduped server- and client-
    // side, so counting the images we got back drifts behind and re-requests
    // pages we already have.
    const [offset, setOffset] = useState(0);
    const [nextOffset, setNextOffset] = useState(0);
    const [retries, setRetries] = useState(0);

    const [images, setImages] = useState([]);
    const [freshImages, setFreshImages] = useState([]);
    const [columns, setColumns] = useState([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);

    // search ({key, vector}) only applies on pages without an id. Its vector
    // is null while the query is still being embedded.
    const searchKey = id ? null : search?.key ?? null;
    const searchVector = searchKey ? search.vector : null;
    const searchPending = !!searchKey && !searchVector;
    const anchor = id ? "id:" + id : searchKey || "home";
    const home = !id && !searchKey;

    // When an id or search runs out of results we "wander" on from the last
    // image. Wander state is tagged with the anchor it belongs to, so it
    // can't leak into the next page.
    const [wander, setWander] = useState(null);
    const wanderId = wander?.anchor === anchor ? wander.id : null;
    const offsetOffset = wander?.anchor === anchor ? wander.offset : 0;

    // Clicking the image you're already on deals a fresh page: the grid
    // restarts at the first rank we haven't fetched yet. baseOffset is where
    // the current grid started (0 unless rerolled), tagged with its anchor
    // like wander.
    const [base, setBase] = useState(null);
    const baseOffset = base?.anchor === anchor ? base.offset : 0;

    let reset = useCallback(() => {
        setOffset(0)
        setNextOffset(0)
        setImages([])
        setError(null)
    }, []);

    // The UI resets before it navigates, but browser back/forward doesn't go
    // through it: start the new anchor over rather than paging into it from a
    // stale cursor (the fetch already fired with it is superseded).
    useEffect(() => {
        setWander(null);
        setBase(null);
        if (offset !== 0 || images.length > 0) {
            reset();
            setLastItem(null);
            setRetries((r) => r + 1);
        }
    }, [anchor]);

    useEffect(() => {
        if (searchPending) return;
        setLoading(true);
        let stale = false;
        const limit = 6 * colCount;
        const requestOffset = Math.max(offset - offsetOffset, 0);
        let request;
        if (searchKey && !wanderId) {
            request = fetch(API_BASE + "/vectorNeighbors", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ vector: searchVector, limit, offset: requestOffset }),
            });
        } else {
            request = fetch(API_BASE + "/neighbors?" +
                new URLSearchParams({
                    id: wanderId || id,
                    limit,
                    offset: requestOffset,
                }));
        }
        request
            .then((res) => {
                if (!res.ok) throw new Error("neighbors " + res.status);
                return res.json();
            })
            .then((newData) => {
                if (stale) return;
                const raw = newData?.images || [];
                // Servers older than nextOffset paged by limit (or by 1 on home).
                const next = offsetOffset +
                    (newData?.nextOffset ?? requestOffset + (home ? 1 : limit));
                const newImages = newOnly(raw, images);
                setError(null);

                if (newImages.length == 0) {
                    if (raw.length == 0 && !home) {
                        console.log("END!")
                        if (images.length === 0) {
                            // A reroll ran off the end: wrap around to the top.
                            if (baseOffset > 0) {
                                setBase(null);
                                setWander(null);
                                setNextOffset(0);
                                setOffset(0);
                            }
                            // Otherwise nothing to wander from: no results at all.
                            return
                        }
                        setWander({ anchor, id: images[images.length - 1].Id, offset })
                    } else {
                        // A page of nothing new: skip past it.
                        setNextOffset(next)
                        setOffset(next)
                    }
                    return
                }
                // Keep search hits in rank order; shuffle neighbor pages.
                if (id || wanderId) {
                    shuffleArray(newImages);
                }
                window.count = 10;

                setNextOffset(next);
                setImages([...images, ...newImages]);
                setFreshImages([...freshImages, ...newImages])
            })
            .catch((e) => {
                if (!stale) setError(e);
            })
            .finally(() => {
                if (!stale) setLoading(false);
            });

        return () => {
            stale = true;
            setLoading(false);
        };
    }, [id, offset, wanderId, colCount, searchKey, searchVector, retries]);

    useEffect(() => {
        if (freshImages.length === 0) return
        let remnantColumns = columns;
        if (offset === baseOffset) {
            remnantColumns = columns.map((c, i) => {
                if (i !== lastItem?.x) {
                    return []
                }
                // The clicked image stays put (or moves to the top if we were
                // scrolled) and grows to two columns wide, overhanging its
                // right neighbor, or its left one from the last column.
                const { x, y, hero, heroDir, clip, ...item } = lastItem;
                const clicked = {
                    ...item,
                    hero: true,
                    heroDir: x + 1 < colCount ? "right" : "left",
                };
                if (window.scrollY > 0) return [clicked];
                const above = c.slice(0, y)
                    .filter((it) => !it.spacer)
                    .map(({ hero, heroDir, ...it }) => it);
                return [...above, clicked];
            });
            window.scrollTo(0, 0);
        }
        let newColumns = collateListBalanced(
            freshImages,
            colCount,
            remnantColumns,
        );
        setFreshImages([])
        setColumns(newColumns)

    }, [id, searchKey, freshImages, columns, offset, baseOffset, colCount, lastItem])

    let loadMore = useCallback(() => {
        setOffset(nextOffset)
    }, [nextOffset]);

    let reroll = useCallback(() => {
        setBase({ anchor, offset: nextOffset });
        setOffset(nextOffset);
        setImages([]);
        setError(null);
        // Refetch even if the cursor didn't move (a page still in flight).
        setRetries((r) => r + 1);
    }, [anchor, nextOffset]);

    let retry = useCallback(() => {
        setError(null)
        setRetries((r) => r + 1)
    }, []);

    // Report a pending search as loading so infinite scroll waits for it.
    return { columns, loading: loading || searchPending, error, setLastItem, reset, reroll, loadMore, retry };
}

// A column entry's height in units of column width. Besides images, columns
// hold spacers (the hole a hero overhangs) and images clipped to fit above one.
export function unitsOf(item) {
    if (item.spacer) return item.units;
    if (item.clip != null) return item.clip;
    return (item.Height / item.Width) * (item.hero ? 2 : 1);
}

// Don't clip an image down to a sliver to fill above a hero; leave a gap.
const MIN_CLIP = 0.25;

// A hero overhangs a neighbor column, which needs a hole cut for it: images
// down to the hero's top, the last one clipped, then a spacer. Returns the
// hole still to be cut, if any.
function pendingHole(columns) {
    for (let i = 0; i < columns.length; i++) {
        let top = 0;
        for (const item of columns[i]) {
            if (item.hero) {
                const col = item.heroDir === "left" ? i - 1 : i + 1;
                if (col < 0 || col >= columns.length) return null;
                if (columns[col].some((it) => it.spacer)) return null;
                return { col, top, bottom: top + unitsOf(item) };
            }
            top += unitsOf(item);
        }
    }
    return null;
}

function collateListBalanced(newItems, columns, oldColumns) {
    let skipList = {};

    let heights = Array(columns).fill(0);
    if (oldColumns) {
        heights = heights.map(
            (h, i) =>
                oldColumns[i]?.reduce((a, b) => {
                    if (b.Id) skipList[b.Id] = true;
                    return a + unitsOf(b);
                }, 0) ?? 0,
        );
    }

    let result = Array.from({ length: columns }, () => []);
    if (oldColumns) {
        oldColumns.forEach((list, i) => {
            if (i >= columns) return
            result[i] = [...list];
        });
    }
    let hole = pendingHole(result);
    let list = newItems.filter((item) => !skipList[item.Id]);
    let i = 0;
    while (i < list.length) {
        let item = list[i];
        let minHeight = Math.min(...heights); // Find the smallest height
        let index = heights.findIndex((height) => height === minHeight); // first column w smallest height
        let aspectRatio = item.Height / item.Width;
        if (hole && index === hole.col) {
            const gap = hole.top - heights[index];
            if (gap < MIN_CLIP || aspectRatio >= gap) {
                if (gap >= MIN_CLIP) {
                    result[index].push({ ...item, clip: gap });
                    heights[index] = hole.top;
                    i++;
                }
                const units = Math.max(hole.bottom - heights[index], 0);
                result[index].push({ spacer: true, units });
                heights[index] += units;
                hole = null;
                continue;
            }
        }
        i++;
        result[index].push(item); // Push this item into the column with the smallest height
        heights[index] += aspectRatio; // Increase the total height of this column
    }

    return result;
}
function newOnly(news, old) {
    let skipList = {};
    old.forEach((item) => {
        skipList[item.Id] = true;
    });
    return news.filter((item) => !skipList[item.Id]);
}

function shuffleArray(array) {
    let rng = seedrandom(77);
    for (let i = array.length - 1; i > 0; i--) {
        const j = Math.floor(rng() * (i + 1));
        [array[i], array[j]] = [array[j], array[i]];
    }
}
