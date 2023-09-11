import { useState } from 'react';
import { Rnd } from 'react-rnd';
import click from '../public/block.wav';
import useSound from "use-sound";
import home from '../public/info.svg';
import x from '../public/x.svg';
import attention from '../public/attention.gif';
import river from '../public/animriver.gif';
import { ArrowTopRightIcon } from "@radix-ui/react-icons";
import "./info.css"

export default function Info({ style, setInfoOpen, infoOpen }) {
  const [play] = useSound(click, { volume: 0.1 });


  if (!infoOpen) {
    return (<button
      onClick={() => {
        setInfoOpen(true)
        play()
      }}
    >
      <img width={24} src={home} title="home" />
    </button>)
  }
  return (
    <Rnd
      disableDragging={window.innerWidth < 800}
      enableResizing={false}
      default={{
        x: -318,
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
            <img width={24} src={x} alt="home" />
          </button>
          {/* <img draggable={false} src={river} style={{ width: "100%", height: "10px" }}></img> */}
          River is a visual connection engine.
          <br />
          Clear your mind and surf laterally though image space.
          <br />

          <img src={attention} id="atn"></img>
          May contain NSFW content.
          <br />
          <br />
          <b>Right click any image to open on <a href="https://are.na?utm_source=river.maxbittker.com">Are.na</a>, for full size version and human-curated connections.</b>
          <br />
          <br />
          Consider connecting your finds to my public channel, <a href="https://www.are.na/max-bittker/found-in-the-river">found in the river</a>.
          <br />
          <br />
        </span>
        {/* <details>
          <summary>Checkpoints</summary>
          <ul>
            <li>Fungus</li>
            <li>Sword</li>
          </ul>
        </details> */}
        <span className='small'>
          Powered by CLIP & pgvector, on a small VPS
        </span>
        <span className='small'>
          Built by <a href="https://maxbittker.com">Max Bittker</a>, September 2023
        </span>
        <span className='small'>
          Open for consulting and collaboration this fall/winter
        </span>

      </div>
    </Rnd>)
}