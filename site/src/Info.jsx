import { Rnd } from 'react-rnd';
import click from '../public/block.wav';
import useSound from "use-sound";
import { ReactComponent as X } from "../public/x.svg";
import { useLocalStorage } from "usehooks-ts";

import attention from '../public/attention.gif';
import spiral2 from "../public/spiral3.png";

import Minimap from "./Minimap";

import "./info.css"

let challengeItems = [
  "Earth",
  "Death Metal Lettering",
  "Old Computer",
  "Something on Fire",
  "Galaxy / Nebula",
  "Pokemon/Yu-Gi-Oh/Magic",
  "Stained Glass Window",
  "Quilt",
  "Ancient Ceramics",
  "Complicated Diagram",
  "Obama Eating Ice Cream",
  "Crystal",
  "Fungus",
  "Salamander",
  "Porcelain",
  "Four Leaf Clover",
  "Half Moon",
  "Interspecies Friendship",
  "Microscopic Thing",
  "Cool Sword"
]
// map: the Minimap's props (minus size), shown while the panel is open.
export default function Info({ style, setInfoOpen, infoOpen, map }) {
  const [play] = useSound(click, { volume: 0.1 });
  const [seenItems, setSeenItems] = useLocalStorage('seenItems', []);


  if (!infoOpen) {
    return (<button
      onClick={() => {
        setInfoOpen(true)
        play()
      }}
    >
      <img width={24} src={spiral2} title="info" />
    </button>)
  }
  let w = Math.min(420, window.innerWidth - 70)
  return (
    <Rnd
      disableDragging={window.innerWidth < 800}
      enableResizing={false}
      cancel=".minimap"
      default={{
        x: -w + 4,
        y: -2,
        width: w,
      }}
    >
      <div className="info" style={style}>
        <span>
          <button className="close" onClick={() => {
            play()
            setInfoOpen(false)
          }}>
            <X width={24} height={24} title="close" fill="rgb(88, 88, 88)" stroke="rgb(88, 88, 88)" />
          </button>
          <img width={12} src={spiral2} title="home" style={{ position: 'absolute', left: 0, top: 0, transform: 'rotate(90deg)', filter: 'brightness(1.5)' }} />
          <img width={12} src={spiral2} title="home" style={{ position: 'absolute', left: 0, bottom: 0, filter: 'brightness(1.5)' }} />
          <img width={12} src={spiral2} title="home" style={{ position: 'absolute', right: -1, bottom: 0, transform: 'rotate(270deg)' }} />

          <h3> River is a visual connection engine</h3>
          Clear your mind and surf laterally though image space.
          <br />
          May contain NSFW content<img src={attention} width={16} height={16} id="atn"></img>

          <br />
          <br />
          Right click any image to open it on <a href="https://are.na?utm_source=river.maxbittker.com">Are.na</a> for source, full res, and human-curated connections.
          <br />
          <br />

        </span>
        {/* as wide as the panel's text: its width less margin, border, padding */}
        {map && <Minimap {...map} size={w - 40} />}
        <summary>Optional Challenge, find these items: </summary>
        <ul>
          {challengeItems.filter((item) => {
            if (w < 410) {
              return item.length < 19
            } return true
          }).map((item, i) => {
            let seen = seenItems.includes(item)
            return (<li
              key={item}
              style={{ textDecoration: seen ? 'line-through' : 'none' }}
              onClick={() => {
                if (seen) {
                  let newV = seenItems.filter((v) => v !== item)
                  setSeenItems([...newV])
                } else {
                  setSeenItems([...seenItems, item])

                }
              }}><input type='checkbox' readOnly checked={seen}></input>{item}</li>
            )
          })
          }
        </ul>
        <br />
        <span className='small'>
          Built by <a href="https://maxbittker.com">Max Bittker</a>, September 2023, powered by CLIP & pgvector
        </span>

      </div>
    </Rnd>)
}