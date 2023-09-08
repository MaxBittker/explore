import "./App.css";
import { useState, useEffect, useMemo } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import VisibilitySensor from "react-visibility-sensor";
import { ArrowTopRightIcon } from "@radix-ui/react-icons";
import seedrandom from "seedrandom";
import { useWindowSize, useLocalStorage } from "usehooks-ts";


import useSound from "use-sound";
import block from "../public/blockshort.wav";
import spiral from "../public/spiral.svg";
import back from "../public/back.svg";
import Info from "./Info";
// import castanet from '../public/castanet.wav';
// import click from '../public/click.wav';
// import hit from '../public/hit.wav';

let last_hit = 0;
let count = 10;

export default function App() {
  // fetch data from api
  const { width, height } = useWindowSize();
  const [lastItem, setLastItem] = useState(null);
  const [offset, setOffset] = useState(0);
  const [infoOpen, setInfoOpen] = useLocalStorage(true);
  const [data, setData] = useState([]);
  const [images, setImages] = useState([]);
  const [searchParams, setSearchParams] = useSearchParams();
  const id = searchParams.get("id");
  const navigate = useNavigate();

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [play] = useSound(block, { volume: 0.1 });

  let columns = Math.round(width / 225);
  columns = Math.max(columns, 3);
  columns = Math.min(columns, 9);

  useEffect(() => {
    setLoading(true);
    fetch(
      "https://explore.maxbittker.com/neighbors?" +
      new URLSearchParams({
        id: id,
        offset: offset,
        limit: 6 * columns
      }),
    )
      .then((res) => res.json())
      .then((newData) => {
        let newImages = newData.images;
        const minDiff = 0.0001; // Define the minimum difference to consider. Adjust as needed.
        if (newImages[0].Distance) {
          newImages = newImages.reduce((acc, item) => {
            if (acc.length === 0) {
              return acc.concat(item);
            }
            const lastItem = acc[acc.length - 1];
            const diff = Math.abs(lastItem.Distance - item.Distance);
            if (diff < minDiff) {
              // console.log(item.Thumb);
              return acc;
            } else {
              return acc.concat(item);
            }
          }, []);
        }

        setData(newImages);
        shuffleArray(newImages);
        console.log(offset);
        count = 10;
        if (offset > 0) {
          setData([...data, ...newImages]);
          let newColumns = collateListBalanced(
            newImages,
            columns,
            lastItem,
            images,
          );
          setImages(newColumns);

          // setImages([...images, ...newImages]);
        } else {
          // console.log(newImages);

          // let {x, y} = lastItem ;
          setData(newImages);

          let oldColumns = images.map((c, i) => {
            if (i === lastItem?.x) {
              return c.slice(0, lastItem.y + 1);
            } else {
              return [];
            }
          });
          let newColumns = collateListBalanced(
            newImages,
            columns,
            lastItem,
            oldColumns,
          );
          setImages(newColumns);
          // let images = newData.images.map((item, i) => {
          //   let img = new Image();
          //   img.src = item.Thumb;
          //   item.img = img;
          //   img.onload = () => {
          //     // setData((data) => {
          //     //   data[i] = item;
          //     //   return [...data]
          //     // });
          //   };
          //   return item;
          // });
        }
        setError(null);
      })
      .catch((err) => {
        console.error(err);
        setError(err);
      })
      .finally(() => setLoading(false));

    return () => {
      setLoading(false);
    };
  }, [id, offset, columns]);


  let cWidth = Math.round(width / columns);

  return (
    <main>
      <div className="button-row">
        <button
          onClick={() => {
            setOffset(0);
            setLastItem(null);
            navigate(-1)
          }}
        >
          <img src={back} title="back" />
        </button>
        <button
          onClick={() => {
            setOffset(0);
            setSearchParams({});
          }}
        >
          <img src={spiral} title="spiral" />
        </button>

        <Info
          infoOpen={infoOpen}
          setInfoOpen={setInfoOpen}
        ></Info>
      </div>
      <div style={{ display: "flex" }}>
        {images.map((list, index) => {
          return (
            <div
              key={index}
              style={{ display: "flex", flexDirection: "column" }}
            >
              {list.map((item, i) => {
                let aspectRatio = item.Width / item.Height;
                let w = cWidth;
                let h = cWidth / aspectRatio;

                return (
                  <div
                    key={i}
                    className={"img-container"}
                    onClick={(e) => {
                      e.preventDefault();
                      setOffset(0);
                      setLastItem({ ...item, x: index, y: i });
                      setSearchParams({
                        id: item.Id,
                      });
                      window.scrollTo(0, 0);
                      // play()
                    }}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      let url = "https://www.are.na/block/" + item.Id;
                      window.open(url, "_blank").focus();
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
                        if (count > 0 && t0 - last_hit > 30) {
                          count--;
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
                  </div>
                );
              })}

              {index == 0 && (
                <VisibilitySensor
                  onChange={(isVisible) => {
                    console.log(isVisible, loading, error);
                    if (!loading && !error && isVisible) {
                      setOffset(data?.length ?? 0);
                    }
                  }}
                >
                  <div>...</div>
                </VisibilitySensor>
              )}
            </div>
          );
        })}
      </div>
    </main>
  );
}

function aspectRatio(Item) {
  return Item.Width / Item.Height;
}

function collateListBalanced(newItems, columns, lastItem, oldColumns) {
  lastItem = lastItem ?? {};
  let skipList = {};
  let heights = Array(columns).fill(0);
  if (oldColumns) {
    heights = heights.map(
      (h, i) =>
        oldColumns[i]?.reduce((a, b) => {
          skipList[b.Id] = true;
          return a + b?.Height / b?.Width;
        }, 0) ?? 0,
    );
  }

  let result = Array.from({ length: columns }, () => []);
  if (oldColumns) {
    oldColumns.forEach((list, i) => {
      result[i] = list;
    });
  }
  let list = newItems.filter((item) => !skipList[item.Id]);
  let i = 0;
  while (i < list.length) {
    let item = list[i];
    let minHeight = Math.min(...heights); // Find the smallest height
    let index = heights.findIndex((height) => height === minHeight); // first column w smallest height
    let x = index;
    let y = result[index].length;
    i++;
    let aspectRatio = item.Height / item.Width;
    result[index].push(item); // Push this item into the column with the smallest height
    heights[index] += aspectRatio; // Increase the total height of this column
  }

  return result;
}

function shuffleArray(array) {
  let rng = seedrandom(77);
  for (let i = array.length - 1; i > 0; i--) {
    const j = Math.floor(rng() * (i + 1));
    [array[i], array[j]] = [array[j], array[i]];
  }
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
