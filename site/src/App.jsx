import "./App.css";
import { useState, useEffect, useMemo } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import VisibilitySensor from "react-visibility-sensor";
import seedrandom from "seedrandom";
import { useWindowSize, useLocalStorage } from "usehooks-ts";
import useSound from "use-sound";
import spiral from "../public/spiral.svg";
import back from "../public/back.svg";
import Info from "./Info";
import Block from "./Block";
import block from "../public/blockshort.wav";

window.count = 10;
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
  columns = clamp(columns, 3, 9)

  useEffect(() => {
    if (loading) return
    setLoading(true);
    console.log(offset)
    fetch(
      "https://river.maxbittker.com/neighbors?" +
      new URLSearchParams({
        id: id,
        offset: offset,
        limit: 8 * columns
      }),
    )
      .then((res) => res.json())
      .then((newData) => {
        let newImages = newData.images;
        if (!newImages) {
          return
        }

        shuffleArray(newImages);
        window.count = 10;
        if (offset > 0) {
          setData([...data, ...newImages]);
          let newColumns = collateListBalanced(
            newImages,
            columns,
            lastItem,
            images,
          );
          setImages(newColumns);
        } else {
          setData(newImages);

          let oldColumns = images.map((c, i) => {
            if (i === lastItem?.x) {
              if (window.scrollY > 0) {
                lastItem.y = 0;
                setLastItem({ ...lastItem })
                return [lastItem]
              } else {
                return c.slice(0, lastItem.y + 1);
              }
            } else {
              return [];
            }
          });
          if (offset === 0) {
            window.scrollTo(0, 0);
          }
          let newColumns = collateListBalanced(
            newImages,
            columns,
            lastItem,
            oldColumns,
          );
          setImages(newColumns);
        }
        setError(null);
      })
      .catch((err) => {
        console.error(err);
        setError(err);
      })
      .finally(() => setLoading(false));

    return () => {
      // setLoading(false);
    };
  }, [id, offset, columns]);


  let cWidth = width / columns;

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
      </div>
      <div className="button-row right">
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
              {list.map((item, i) => <Block
                key={item.Id}
                item={item} i={i} index={index}
                cWidth={cWidth}
                setLastItem={setLastItem}
                setOffset={setOffset}
                setSearchParams={setSearchParams}
                play={play} />)}
              <VisibilitySensor
                onChange={(isVisible) => {
                  if (isVisible && !loading && !error) {
                    setOffset((data?.length ?? 0));
                  }
                }}
              >
                {({ isVisible }) => {
                  if (isVisible && !loading && !error) {
                    setOffset((data?.length ?? 0));
                  }
                  return <div className="bottom">∎∎∎</div>
                }
                }
              </VisibilitySensor>
            </div>
          );
        })}
      </div>
    </main>
  );
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

const clamp = (val, min, max) => Math.min(Math.max(val, min), max)
