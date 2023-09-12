import "./App.css";
import './menuStyles.css';

import { useState, useEffect, useMemo } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import VisibilitySensor from "react-visibility-sensor";
import { useWindowSize, useLocalStorage } from "usehooks-ts";
import useSound from "use-sound";
import Info from "./Info";
import Block from "./Block";
import block from "../public/blockshort.wav";
import useGetData from "./getData";
import { ReactComponent as Back } from "../public/back.svg";
import { ReactComponent as Home } from "../public/home.svg";

window.count = 10;
export default function App() {
  // fetch data from api
  let { width, height } = useWindowSize();
  width = width || window.innerWidth
  const [infoOpen, setInfoOpen] = useLocalStorage('infoOpen', true);
  const [searchParams, setSearchParams] = useSearchParams();
  const id = searchParams.get("id");
  const navigate = useNavigate();

  const [play] = useSound(block, { volume: 0.1 });

  let colCount = Math.round(width / 225);
  colCount = clamp(colCount, 3, 9)
  const { columns, loading, error, setLastItem, reset, loadMore } = useGetData(id, colCount);


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
      </div>
      <div className="button-row right">
        <Info
          infoOpen={infoOpen}
          setInfoOpen={setInfoOpen}
        ></Info>
      </div>

      <div style={{ display: "flex" }}>
        {columns.map((list, index) => {
          return (
            <div
              key={index}
              style={{ display: "flex", flexDirection: "column" }}
            >
              {list.map((item, i) => <Block
                key={i}
                item={item} i={i} index={index}
                cWidth={cWidth}
                setLastItem={setLastItem}
                reset={reset}
                play={play} />)}
              <VisibilitySensor
                onChange={(isVisible) => {
                  if (isVisible && !loading && !error) {
                    loadMore()
                  }
                }}
              >
                {({ isVisible }) => {
                  if (isVisible && !loading && !error) {
                    loadMore()
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

const clamp = (val, min, max) => Math.min(Math.max(val, min), max)
