
import { useState, useCallback, useEffect } from "react";
import { Link } from "react-router-dom";
import { ArrowTopRightIcon, ExclamationTriangleIcon, CopyIcon, MinusCircledIcon, StarIcon, Link2Icon } from "@radix-ui/react-icons";
import copy from 'clipboard-copy'
import * as ContextMenu from '@radix-ui/react-context-menu';
import { ReactComponent as ArenaLogo } from "../public/arena.svg";
let last_hit = 0;

function flagBlock(id) {
    let [flaggedAs, setFlaggedAs] = useState(undefined)
    let flag = useCallback((id, flag) => {
        fetch("https://river.maxbittker.com/api/flag", {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
            body: JSON.stringify({ id, flag }),
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
export default function Block({ item, i, index, cWidth, reset, setLastItem, play, setSearchParams }) {


    let aspectRatio = item.Width / item.Height;
    let w = cWidth;
    let h = cWidth / aspectRatio;

    let [appliedFlag, flag] = flagBlock(item.Id)

    return (
        <ContextMenu.Root>
            <ContextMenu.Trigger className="ContextMenuTrigger">
                <Link
                    to={'?id=' + item.Id}
                    className={"img-container"}
                    onClick={(e) => {
                        // e.preventDefault();
                        reset(0);
                        setLastItem({ ...item, x: index, y: i });
                        // setSearchParams({
                        //     id: item.Id,
                        // });
                        // play()
                    }}
                >
                    {appliedFlag && <div className="flag-thanks">   Marked as {appliedFlag}! Thank you
                    </div>}
                    <img
                        width={w}
                        height={h}
                        style={{
                            width: w + "px",
                            height: h + "px",
                        }}
                        key={i}
                        src={item.Thumb}
                        onLoad={(e) => {
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
                        window.open(url, "_blank").focus();
                    }}
                        className="ContextMenuItem">
                        Open in Are.na <div className="RightSlot">
                            <ArenaLogo />
                        </div>
                    </ContextMenu.Item>
                    <ContextMenu.Separator className="ContextMenuSeparator" />

                    <ContextMenu.Item onSelect={() => {
                        let url = "https://river.maxbittker.com/?id=" + item.Id;
                        window.open(url, "_blank").focus();
                    }}
                        className="ContextMenuItem">
                        Open in new tab <div className="RightSlot">  <ArrowTopRightIcon /></div>
                    </ContextMenu.Item>

                    <ContextMenu.Separator className="ContextMenuSeparator" />
                    <ContextMenu.Item className="ContextMenuItem" onSelect={() => {
                        copy("https://river.maxbittker.com/?id=" + item.Id)
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
