import { useState } from 'react';
import { Rnd } from 'react-rnd';
import click from '../public/block.wav';
import useSound from "use-sound";
import home from '../public/home.svg';
import x from '../public/x.svg';
export default function Info({ style, setInfoOpen, infoOpen }) {
  const [play] = useSound(click, { volume: 0.1 });


  if (!infoOpen) {
    return (<button
      onClick={() => {
        console.log(infoOpen)
        setInfoOpen(true)
        play()
      }}
    >
      <img src={home} title="home" />
    </button>)
  }
  return (
    <Rnd
      disableDragging={window.innerWidth < 800}
      enableResizing={false}
      default={{
        x: 55,
        y: -1,
        width: 320,
        height: 200,
      }}
    >
      <div className="info" style={style}>
        <span>
          <button className="close" onClick={() => {
            play()

            setInfoOpen(false)
          }}>
            <img src={x} alt="home" />
          </button>

          <img src="public/attention.gif" id="atn"></img>
          This is a visual connection engine. It helps you explore laterally though image space.
          <br />
          <br />
          Images curated by <a href="https://are.na">Are.na</a> community.
          Right click for source.
          <br />
          <br />
        </span>

        <span>
          Built by <a href="https://maxbittker.com">Max Bittker</a>, Aug 2023.
        </span>
      </div>
    </Rnd>)
}