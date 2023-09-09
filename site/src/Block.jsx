
import { ArrowTopRightIcon } from "@radix-ui/react-icons";
import { Link } from "react-router-dom";

import useSound from "use-sound";

let last_hit = 0;
export default function Block({ item, i, index, cWidth, setOffset, setLastItem, play, setSearchParams }) {


    let aspectRatio = item.Width / item.Height;
    let w = cWidth;
    let h = cWidth / aspectRatio;

    return (
        <Link
            to={'?id=' + item.Id}
            className={"img-container"}
            // onTouchStart={(e) => { e.preventDefault(); e.stopPropagation(); }}
            onClick={(e) => {
                // e.preventDefault();
                setOffset(0);
                setLastItem({ ...item, x: index, y: i });
                // setSearchParams({
                //     id: item.Id,
                // });
                // play()
            }}
            onContextMenu={(e) => {
                // e.preventDefault();
                // let url = "https://www.are.na/block/" + item.Id;
                // window.open(url, "_blank").focus();
            }}
        >
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
            <button className="corner-button" title="View on Are.na">
                <ArrowTopRightIcon
                    onClick={() => {
                        let url = "https://www.are.na/block/" + item.Id;
                        window.open(url, "_blank").focus();
                    }}
                />
            </button>
        </Link>
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
