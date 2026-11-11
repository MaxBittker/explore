import "./App.css";
import './menuStyles.css';

import { useState, useCallback, useEffect, useRef } from "react";
import { useSearchParams, useNavigate, useLocation } from "react-router-dom";
import { useWindowSize, useLocalStorage } from "usehooks-ts";
import useSound from "use-sound";
import Info from "./Info";
import Block from "./Block";
import block from "../public/blockshort.wav";
import useGetData from "./getData";
import { resetImpressions } from "./stats";
import Search from "./Search";
import { useTextVector, embedImage } from "./searchApi";
import { ReactComponent as Back } from "../public/back.svg";
import { ReactComponent as Home } from "../public/home.svg";

window.count = 10;

// Start loading the next page once the shortest column's content ends within
// this many screens below the viewport, so it's in before anyone gets there.
const PREFETCH_SCREENS = 2.5;
// Placeholder tiles below every column (heights in column widths). They look
// like images still loading, and give a fast flick room to keep its momentum
// instead of stopping dead at the end of the document.
const GHOSTS = [1.25, 0.8, 1.0, 1.4, 0.75, 1.1, 0.9];
const GHOSTS_PER_COLUMN = 4;

export default function App() {
  // fetch data from api
  let { width, height } = useWindowSize();
  width = width || window.innerWidth
  const [infoOpen, setInfoOpen] = useLocalStorage('infoOpen', true);
  const [searchParams, setSearchParams] = useSearchParams();
  const id = searchParams.get("id");
  const q = id ? null : searchParams.get("q");
  const imgKey = id || q ? null : searchParams.get("img");
  const navigate = useNavigate();
  const location = useLocation();

  // Text searches live in the URL (?q=); image searches can't, so their
  // vector rides along in history state under a random ?img= key.
  const imageQuery = imgKey && location.state?.imageQuery?.key === imgKey ? location.state.imageQuery : null;
  const textQuery = useTextVector(q);
  const [imagePending, setImagePending] = useState(false);
  const [imageError, setImageError] = useState(null);
  let search = null;
  if (q) search = { key: "q:" + q, vector: textQuery.vector };
  else if (imageQuery) search = { key: "img:" + imgKey, vector: imageQuery.vector };

  useEffect(() => {
    // ?img= without its history state (e.g. after a reload): nothing to show.
    if (imgKey && !imageQuery) setSearchParams({}, { replace: true });
  }, [imgKey, imageQuery]);

  const [play] = useSound(block, { volume: 0.1 });

  // Each page (block, search or home) counts its impressions afresh.
  useEffect(() => resetImpressions(), [id, q, imgKey]);

  let colCount = Math.round(width / 225);
  colCount = clamp(colCount, 3, 9)
  const { columns, loading, error, setLastItem, reset, reroll, loadMore, retry } = useGetData(id, colCount, search);

  // Each column's tail marks where its content ends.
  const tails = useRef([]);
  const checkRef = useRef();
  checkRef.current = () => {
    if (loading || error) return;
    const tops = tails.current.slice(0, columns.length).filter(Boolean)
      .map((el) => el.getBoundingClientRect().top);
    if (tops.length === 0) return;
    if (Math.min(...tops) < window.innerHeight * (1 + PREFETCH_SCREENS)) loadMore();
  };
  useEffect(() => {
    let raf = 0;
    const onScroll = () => {
      if (raf) return;
      raf = requestAnimationFrame(() => {
        raf = 0;
        checkRef.current();
      });
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    return () => {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
      cancelAnimationFrame(raf);
    };
  }, []);
  // A page that lands short of the threshold (or a finished retry) should
  // keep loading without waiting for the next scroll event.
  useEffect(() => checkRef.current(), [columns, loading, error]);

  const searchText = useCallback((text) => {
    setImageError(null);
    if (text === q && textQuery.error) {
      textQuery.retry();
      return;
    }
    if (text === q) return;
    reset();
    setLastItem(null);
    setSearchParams({ q: text });
  }, [q, textQuery, reset, setLastItem, setSearchParams]);

  const searchImage = useCallback(async (file) => {
    setImageError(null);
    setImagePending(true);
    try {
      const { vector, thumb } = await embedImage(file);
      const key = Math.random().toString(36).slice(2, 8);
      reset();
      setLastItem(null);
      navigate("/?img=" + key, { state: { imageQuery: { key, vector, thumb } } });
    } catch (e) {
      setImageError(e.message);
    } finally {
      setImagePending(false);
    }
  }, [reset, setLastItem, navigate]);


  let cWidth = width / colCount;
  return (
    <main>
      <div className="button-row left">
        <button
          onClick={() => {
            reset();
            setLastItem(null);
            navigate(-1)
          }}
        >
          <Back width={24} height={24} title="back" fill="rgb(88, 88, 88)" stroke="rgb(88, 88, 88)" />
        </button>
        <button
          onClick={() => {
            reset();
            setLastItem(null);
            setSearchParams({});
          }}
        >
          <Home width={24} height={24} title="home" fill="rgb(88, 88, 88)" stroke="rgb(88, 88, 88)" />
        </button>
        <Search
          q={q}
          imageThumb={imageQuery?.thumb}
          pending={imagePending || (!!q && !textQuery.vector && !textQuery.error)}
          error={imageError || textQuery.error}
          onText={searchText}
          onImage={searchImage}
          onClear={() => {
            reset();
            setLastItem(null);
            setSearchParams({});
          }}
        />
      </div>
      <div className="button-row right">
        <Info
          infoOpen={infoOpen}
          setInfoOpen={setInfoOpen}
          map={{
            id,
            search,
            play,
            onJump: (blockId) => {
              reset();
              setLastItem(null);
              navigate("/?id=" + blockId);
            },
          }}
        ></Info>
      </div>

      <div style={{ display: "flex" }}>
        {columns.map((list, index) => {
          return (
            <div
              key={index}
              style={{ display: "flex", flexDirection: "column" }}
            >
              {list.map((item, i) => item.spacer
                ? <div key={i} style={{ width: cWidth, height: item.units * cWidth, flexShrink: 0 }} />
                : <Block
                  key={i}
                  item={item} i={i} index={index}
                  cWidth={cWidth}
                  setLastItem={setLastItem}
                  reset={String(item.Id) === id ? reroll : reset}
                  track={String(item.Id) !== id}
                  play={play} />)}
              <div ref={(el) => (tails.current[index] = el)}>
                {!error && Array.from({ length: GHOSTS_PER_COLUMN }, (_, k) => (
                  <div
                    key={k}
                    className="ghost"
                    style={{ width: cWidth, height: GHOSTS[(index * 3 + k) % GHOSTS.length] * cWidth }}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </div>
      {error && (
        <button className="load-error" onClick={retry}>
          ∎∎∎ couldn't load more, retry
        </button>
      )}
    </main>
  );
}

const clamp = (val, min, max) => Math.min(Math.max(val, min), max)
