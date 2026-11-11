
import { useState, useCallback, useEffect, useRef } from "react";
import { Link } from "react-router-dom";
import { ArrowTopRightIcon, ExclamationTriangleIcon, CopyIcon, MinusCircledIcon, StarIcon, Link2Icon, MagicWandIcon } from "@radix-ui/react-icons";
import copy from 'clipboard-copy'
import * as ContextMenu from '@radix-ui/react-context-menu';
import { ReactComponent as ArenaLogo } from "../public/arena.svg";
import { API_BASE, SITE_URL } from "./config";
import openArena from "./openArena";
import { observeImpression, recordClick } from "./stats";
import { setHoveredBlock } from "./mapHover";
let last_hit = 0;

function flagBlock(id) {
    let [flaggedAs, setFlaggedAs] = useState(undefined)
    let flag = useCallback((id, flag) => {
        fetch(API_BASE + "/api/flag", {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
            body: JSON.stringify({ id, flag, pw: localStorage.getItem("pw") }),
        }).then((res) => res.json())
            .then((res) => {
                setFlaggedAs(res?.flag)
            })
    }, []);
    useEffect(() => {
        setFlaggedAs(undefined)
    }, [id])

    return [flaggedAs, flag]
}
// track: count impressions and clicks for this tile (false for the block
// the page is about, which isn't a recommendation).
export default function Block({ item, i, index, cWidth, reset, setLastItem, play, track }) {


    let aspectRatio = item.Width / item.Height;
    // The clicked image (hero) is two columns wide, overhanging its neighbor
    // column, which leaves a hole for it. An image above that hole is clipped
    // to fit (item.clip, in column widths) and shows only its top.
    let w = item.hero ? cWidth * 2 : cWidth;
    let h = w / aspectRatio;
    let boxH = item.clip != null ? item.clip * cWidth : h;

    let [appliedFlag, flag] = flagBlock(item.Id)
    // Fade in over the placeholder grey once loaded. Keyed by src so a reused
    // element doesn't count the previous image.
    let [loadedSrc, setLoadedSrc] = useState(null);
    let loaded = loadedSrc === item.Thumb;
    let imgEl = useRef(null);
    let imgRef = useCallback((el) => {
        imgEl.current = el;
        if (el?.complete && el.naturalWidth) setLoadedSrc(el.getAttribute("src"));
    }, []);
    // An impression needs the pixels: start watching once the image loads.
    useEffect(() => {
        if (track && loaded) return observeImpression(imgEl.current);
    }, [track, loaded, item.Id]);

    return (
        <ContextMenu.Root>
            <ContextMenu.Trigger
                className={"ContextMenuTrigger" + (item.hero ? " hero" : "")}
                style={item.hero ? { width: cWidth, height: h } : undefined}
            >
                <Link
                    to={'?id=' + item.Id}
                    className={"img-container"}
                    style={{
                        width: w,
                        height: boxH,
                        marginLeft: item.hero && item.heroDir === "left" ? -cWidth : 0,
                    }}
                    onPointerEnter={(e) => e.pointerType === "mouse" && setHoveredBlock(item.Id)}
                    onPointerLeave={() => setHoveredBlock(null)}
                    onClick={(e) => {
                        if (track) recordClick(item.Id);
                        if (e.ctrlKey || e.metaKey) return
                        reset(0);
                        const { hero, heroDir, clip, ...plain } = item;
                        setLastItem({ ...plain, x: index, y: i });
                    }}
                >
                    {appliedFlag && <div className="flag-thanks">   Marked as {appliedFlag}! Thank you
                    </div>}
                    <img
                        ref={imgRef}
                        data-bid={item.Id}
                        width={w}
                        height={h}
                        className={loaded ? "loaded" : undefined}
                        style={{
                            width: w + "px",
                            height: h + "px",
                        }}
                        key={i}
                        src={item.Thumb}
                        decoding="async"
                        onLoad={(e) => {
                            setLoadedSrc(e.target.getAttribute("src"));
                            if (!elementIsVisibleInViewport(e.target)) {
                                return;
                            }
                            let t0 = performance.now();
                            if (window.count > 0 && t0 - last_hit > 30) {
                                window.count--;
                                play({
                                    playbackRate: Math.random() * 0.2 + 0.6 + i * 0.1,
                                });
                                last_hit = t0;
                            }
                        }}
                    ></img>

                </Link>
            </ContextMenu.Trigger>

            <ContextMenu.Portal>
                <ContextMenu.Content className="ContextMenuContent" sideOffset={5} align="end">
                    <ContextMenu.Item onSelect={() => {
                        let url = "https://www.are.na/block/" + item.Id;
                        openArena(url);
                    }}
                        className="ContextMenuItem">
                        Open in Are.na <div className="RightSlot">
                            <ArenaLogo />
                        </div>
                    </ContextMenu.Item>
                    <ContextMenu.Separator className="ContextMenuSeparator" />

                    <ContextMenu.Item onSelect={() => {
                        let url = "https://www.are.na/block/" + item.Id;
                        window.open(url, "_blank")?.focus();
                    }}
                        className="ContextMenuItem">
                        Open in new tab <div className="RightSlot">  <ArrowTopRightIcon /></div>
                    </ContextMenu.Item>

                    <ContextMenu.Separator className="ContextMenuSeparator" />
                    <ContextMenu.Item className="ContextMenuItem" onSelect={() => {
                        copy(SITE_URL + "/?id=" + item.Id)
                    }}>
                        Copy Link <div className="RightSlot"><Link2Icon /></div>
                    </ContextMenu.Item>

                    <ContextMenu.Separator className="ContextMenuSeparator" />
                    {appliedFlag ?
                        <ContextMenu.Item className="ContextMenuItem Secondary" onSelect={() => flag(item.Id, "duplicate")} >
                            Marked as {appliedFlag}! Thank you
                        </ContextMenu.Item> : (
                            <><ContextMenu.Item className="ContextMenuItem Secondary" onSelect={() => flag(item.Id, "cool")}  >
                                Interesting <div className="RightSlot"><StarIcon /></div>
                            </ContextMenu.Item>
                                <ContextMenu.Separator className="ContextMenuSeparator" />

                                <ContextMenu.Item className="ContextMenuItem Secondary" onSelect={() => flag(item.Id, "nsfw")} >
                                    Flag NSFW <div className="RightSlot"><ExclamationTriangleIcon /></div>
                                </ContextMenu.Item>

                                <ContextMenu.Item className="ContextMenuItem Secondary" onSelect={() => flag(item.Id, "ai")} >
                                    Flag AI <div className="RightSlot"><MagicWandIcon /></div>
                                </ContextMenu.Item>

                                <ContextMenu.Item className="ContextMenuItem Secondary" onSelect={() => flag(item.Id, "duplicate")} >
                                    Flag Duplicate <div className="RightSlot"><CopyIcon /></div>
                                </ContextMenu.Item>
                                <ContextMenu.Item className="ContextMenuItem Secondary" onSelect={() => flag(item.Id, "boring")} >
                                    Flag Boring <div className="RightSlot"><MinusCircledIcon /></div>
                                </ContextMenu.Item>
                            </>)}
                </ContextMenu.Content>
            </ContextMenu.Portal>
        </ContextMenu.Root >
    );
}

const elementIsVisibleInViewport = (el, partiallyVisible = false) => {
    const { top, left, bottom, right } = el.getBoundingClientRect();
    const { innerHeight, innerWidth } = window;
    return partiallyVisible
        ? ((top > 0 && top < innerHeight) ||
            (bottom > 0 && bottom < innerHeight)) &&
        ((left > 0 && left < innerWidth) || (right > 0 && right < innerWidth))
        : top >= 0 && left >= 0 && bottom <= innerHeight && right <= innerWidth;
};
