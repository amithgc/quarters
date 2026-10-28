// Draws the mascot, a little robot with a monitor for a head, in each mood
// the app shows: frontend/dist/mascot/<mood>.svg.
//
// Each file is the same robot with a different face and its own animation,
// written as plain CSS inside the SVG (so it also plays when the SVG is used
// as an <img>). The robot is built in nested layers so motions combine:
//
//   .move    where it is (jumps, weight shifts, shivers)
//   .squash  squash and stretch, anchored at its feet (breathing, landing)
//   .head    the head lagging behind the body, tilting, nodding
//   .ant     the antenna, springing after every movement
//   .arm-l / .arm-r   arms swinging from the shoulders
//   .look    where the eyes look;  .blink  the eyelids
//
// Loops have different lengths, so the movement never repeats exactly.
//
//   node tools/mascot/build.mjs
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const OUT = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "frontend", "dist", "mascot");

const C = {
  coral: "#E8825F", coralDark: "#C9653F", cream: "#F4EADD", creamEdge: "#DCCBB6", violet: "#9B86E0",
  screen: "#2A2233", glow: "#FFB59A", cheek: "#FF8FA3", ant: "#FF9B7A", drop: "#8EC9F0", spark: "#FFD27A",
};

// Easing: rising out of a crouch, falling under gravity, and settling.
const UP = "cubic-bezier(.2,.75,.35,1)";
const DOWN = "cubic-bezier(.6,0,.85,.35)";
const SNAP = "cubic-bezier(.2,1.6,.4,1)";

const stroke = (d, w = 3.2) => `<path d="${d}" fill="none" stroke="${C.glow}" stroke-width="${w}" stroke-linecap="round" stroke-linejoin="round"/>`;
const dot = (x, y, r, fill = C.glow, cls = "") => `<circle${cls ? ` class="${cls}"` : ""} cx="${x}" cy="${y}" r="${r}" fill="${fill}"/>`;
const eyes = (inner) => `<g class="look"><g class="blink">${inner}</g></g>`;
const star = (x, y, r, cls) =>
  `<path class="${cls}" d="M${x} ${y - r} Q${x} ${y} ${x + r} ${y} Q${x} ${y} ${x} ${y + r} Q${x} ${y} ${x - r} ${y} Q${x} ${y} ${x} ${y - r}Z" fill="${C.spark}"/>`;
const z = (x, y, s, cls) =>
  `<path class="${cls}" d="M${x} ${y} h${s} l-${s} ${s * 1.15} h${s}" fill="none" stroke="${C.violet}" stroke-width="${s / 3.2}" stroke-linecap="round" stroke-linejoin="round"/>`;

// A spring settling: angles in degrees for the antenna after a jolt.
const spring = (from, to, amp) => {
  const steps = [1, -0.7, 0.45, -0.25, 0.12, 0];
  return steps.map((k, i) => `${(from + ((to - from) * i) / (steps.length - 1)).toFixed(1)}% { transform: rotate(${(amp * k).toFixed(1)}deg); }`).join(" ");
};

const MOODS = {
  // your turn: bobs on its feet with a little hop, then waves at you
  happy: {
    face: `${eyes(`${stroke("M44 51 q5.5 -7 11 0")}${stroke("M65 51 q5.5 -7 11 0")}`)}
      <g class="mouth">${stroke("M51 58 q9 8 18 0")}</g>${dot(42, 58, 3, C.cheek, "cheek")}${dot(78, 58, 3, C.cheek, "cheek")}`,
    css: `
      .move { animation: hop 3.2s infinite; }
      @keyframes hop {
        0%, 10% { transform: translateY(0); animation-timing-function: ${UP}; }
        22% { transform: translateY(-8px); animation-timing-function: ${DOWN}; }
        33%, 100% { transform: translateY(0); } }
      .squash { animation: hop-squash 3.2s infinite; }
      @keyframes hop-squash {
        0% { transform: scale(1, 1); } 8% { transform: scale(1.07, .91); } 14% { transform: scale(.95, 1.07); }
        24% { transform: scale(1, 1); } 33% { transform: scale(1.09, .89); } 40% { transform: scale(.97, 1.03); }
        47%, 100% { transform: scale(1, 1); } }
      .shadow { animation: hop-shadow 3.2s infinite; }
      @keyframes hop-shadow { 0%, 10%, 33%, 100% { transform: scale(1); } 22% { transform: scale(.72); opacity: .1; } }
      .head { animation: hop-head 3.2s infinite; }
      @keyframes hop-head {
        0%, 8% { transform: translateY(0); } 14% { transform: translateY(2px); } 24% { transform: translateY(-1px); }
        34% { transform: translateY(3px); } 42% { transform: translateY(-.5px) rotate(-1deg); }
        55%, 92% { transform: translateY(0) rotate(4deg); } 100% { transform: translateY(0); } }
      .ant { animation: hop-ant 3.2s infinite; }
      @keyframes hop-ant { 0%, 12% { transform: rotate(0); } 16% { transform: rotate(8deg); } ${spring(33, 56, -15)} 100% { transform: rotate(0); } }
      .arm-r { animation: wave 3.2s infinite; }
      @keyframes wave {
        0%, 10% { transform: rotate(-8deg); } 22% { transform: rotate(-26deg); } 33% { transform: rotate(-4deg); }
        44% { transform: rotate(-128deg); animation-timing-function: ease-in-out; }
        52% { transform: rotate(-104deg); } 60% { transform: rotate(-132deg); } 68% { transform: rotate(-102deg); }
        76% { transform: rotate(-128deg); animation-timing-function: ${DOWN}; } 90%, 100% { transform: rotate(-8deg); } }
      .arm-l { animation: hop-arm 3.2s infinite; }
      @keyframes hop-arm { 0%, 10% { transform: rotate(8deg); } 22% { transform: rotate(28deg); } 33% { transform: rotate(2deg); } 40% { transform: rotate(12deg); } 50%, 100% { transform: rotate(8deg); } }
      .cheek { animation: blush 3.2s infinite; } @keyframes blush { 50%, 80% { opacity: 1; } 0%, 100% { opacity: .6; } }`,
  },

  // Claude is working: weight shifts, chin taps, eyes darting while it thinks
  thinking: {
    face: `${eyes(`${dot(51, 47, 4.2)}${dot(69, 47, 4.2)}`)}<g class="mouth">${stroke("M55 60 h10")}</g>`,
    extra: `${dot(101, 19, 2, C.violet, "d d1")}${dot(107, 12, 2.8, C.violet, "d d2")}${dot(114, 4.5, 3.6, C.violet, "d d3")}`,
    css: `
      .move { animation: sway 6.4s ease-in-out infinite; }
      @keyframes sway { 0%, 100% { transform: rotate(-1.6deg); } 50% { transform: rotate(1.8deg); } }
      .squash { animation: breathe 3.1s ease-in-out infinite; }
      .head { animation: ponder 6.4s ease-in-out infinite; }
      @keyframes ponder { 0%, 100% { transform: rotate(-4deg); } 45%, 60% { transform: rotate(5deg); } }
      .ant { animation: ponder-ant 6.4s ease-in-out infinite; }
      @keyframes ponder-ant { 0%, 100% { transform: rotate(5deg); } 50% { transform: rotate(-6deg); } }
      .ball { animation: compute 1.1s steps(2) infinite; }
      @keyframes compute { 50% { opacity: .45; } }
      .arm-r { animation: tap 6.4s infinite; }
      @keyframes tap {
        0%, 6%, 11%, 16%, 21%, 100% { transform: rotate(138deg); animation-timing-function: ${UP}; }
        8.5%, 13.5%, 18.5% { transform: rotate(147deg); animation-timing-function: ${DOWN}; }
        55%, 62%, 67% { transform: rotate(138deg); } 58.5%, 64.5% { transform: rotate(147deg); } }
      .look { animation: dart 6.4s infinite; }
      @keyframes dart {
        0%, 27% { transform: translate(-2.6px, -2.2px); } 30%, 54% { transform: translate(2.8px, -2.4px); }
        57%, 74% { transform: translate(.4px, .3px); } 77%, 100% { transform: translate(-2.6px, -2.2px); } }
      .mouth { animation: chew 6.4s infinite; }
      @keyframes chew { 0%, 27% { transform: translateX(-1.2px); } 31%, 54% { transform: translateX(1.4px); } 58%, 74% { transform: translateX(0) scaleX(.7); } 78%, 100% { transform: translateX(-1.2px); } }
      .d { opacity: 0; animation: rise 2.4s ${UP} infinite; } .d2 { animation-delay: .35s; } .d3 { animation-delay: .7s; }
      @keyframes rise { 0% { opacity: 0; transform: translateY(5px) scale(.4); } 25%, 60% { opacity: 1; transform: translateY(0) scale(1); } 100% { opacity: 0; transform: translateY(-3px) scale(1.1); } }`,
  },

  // no session open: breathes slowly, nods off, then snaps awake for a moment
  sleepy: {
    screen: "#221C29",
    face: `<g class="shut">${stroke("M45 51 q5 3.5 10 0")}${stroke("M65 51 q5 3.5 10 0")}</g>
      <g class="woke">${dot(51, 50, 3.6)}${dot(69, 50, 3.6)}</g>
      <circle class="snore" cx="60" cy="61" r="2.2" fill="none" stroke="${C.glow}" stroke-width="2"/>`,
    extra: `${z(97, 22, 7, "zz z1")}${z(104, 12, 5.5, "zz z2")}${z(110, 3, 4.5, "zz z3")}`,
    css: `
      .squash { animation: sleep-breathe 4.4s ease-in-out infinite; }
      @keyframes sleep-breathe { 0%, 100% { transform: scale(1, 1); } 45% { transform: scale(.985, 1.03); } }
      .head { animation: nod 7s infinite; }
      @keyframes nod {
        0% { transform: rotate(4deg) translateY(0); animation-timing-function: ease-in; }
        62% { transform: rotate(15deg) translateY(2.5px); } 66% { transform: rotate(17deg) translateY(3px); animation-timing-function: ${SNAP}; }
        70% { transform: rotate(-4deg) translateY(-1.5px); } 76% { transform: rotate(2deg) translateY(0); }
        84%, 100% { transform: rotate(4deg) translateY(0); } }
      .ant { animation: nod-ant 7s infinite; }
      @keyframes nod-ant { 0% { transform: rotate(4deg); } 64% { transform: rotate(14deg); } ${spring(68, 86, -18)} 100% { transform: rotate(4deg); } }
      .arm-l { animation: dangle-l 7s infinite; } .arm-r { animation: dangle-r 7s infinite; }
      @keyframes dangle-l { 0%, 64% { transform: rotate(4deg); } 70% { transform: rotate(18deg); } 76% { transform: rotate(-2deg); } 82% { transform: rotate(7deg); } 90%, 100% { transform: rotate(4deg); } }
      @keyframes dangle-r { 0%, 64% { transform: rotate(-4deg); } 70% { transform: rotate(-16deg); } 76% { transform: rotate(3deg); } 82% { transform: rotate(-6deg); } 90%, 100% { transform: rotate(-4deg); } }
      .shut { animation: shut 7s infinite; } .woke { opacity: 0; animation: woke 7s infinite; }
      @keyframes shut { 0%, 68%, 81%, 100% { opacity: 1; } 69%, 80% { opacity: 0; } }
      @keyframes woke { 0%, 68%, 81%, 100% { opacity: 0; } 69%, 80% { opacity: 1; } }
      .snore { animation: snore 4.4s ease-in-out infinite; } @keyframes snore { 45% { transform: scale(1.35); } }
      .snore { transform-origin: 60px 61px; }
      .screen { animation: dim 4.4s ease-in-out infinite; } @keyframes dim { 45% { opacity: .7; } }
      .zz { opacity: 0; animation: float 4.2s ease-out infinite; } .z2 { animation-delay: 1.4s; } .z3 { animation-delay: 2.8s; }
      @keyframes float {
        0% { opacity: 0; transform: translate(-6px, 8px) scale(.5) rotate(-10deg); } 20% { opacity: 1; }
        50% { transform: translate(-1px, 0) scale(.9) rotate(6deg); } 75% { opacity: .9; }
        100% { opacity: 0; transform: translate(3px, -7px) scale(1.1) rotate(-6deg); } }`,
  },

  // the session ended: slow sighs, eyes down, now and then a tear
  sad: {
    face: `${eyes(`${dot(51, 51, 3.8)}${dot(69, 51, 3.8)}`)}${stroke("M45 45 L55 41.5", 2.6)}${stroke("M75 45 L65 41.5", 2.6)}
      <g class="mouth">${stroke("M51 63 q9 -7 18 0")}</g><path class="tear" d="M50 55 q-2.4 3.6 0 5 q2.4 -1.4 0 -5z" fill="${C.drop}"/>`,
    css: `
      .squash { animation: sigh 7.5s infinite; }
      @keyframes sigh {
        0% { transform: scale(1, 1); animation-timing-function: ease-in-out; } 28% { transform: scale(.985, 1.035); }
        40% { transform: scale(.985, 1.035); animation-timing-function: ${DOWN}; }
        62% { transform: scale(1.025, .955); animation-timing-function: ease-in-out; } 100% { transform: scale(1, 1); } }
      .head { animation: sigh-head 7.5s infinite; }
      @keyframes sigh-head {
        0% { transform: translateY(2px) rotate(-3deg); animation-timing-function: ease-in-out; } 30%, 40% { transform: translateY(0) rotate(-1deg); }
        64% { transform: translateY(4.5px) rotate(-6deg); animation-timing-function: ease-in-out; } 100% { transform: translateY(2px) rotate(-3deg); } }
      .ant { animation: droop 7.5s ease-in-out infinite; }
      @keyframes droop { 0%, 100% { transform: rotate(12deg); } 35% { transform: rotate(6deg); } 66% { transform: rotate(18deg); } 74% { transform: rotate(14deg); } }
      .arm-l { animation: hang-l 7.5s ease-in-out infinite; } .arm-r { animation: hang-r 7.5s ease-in-out infinite; }
      @keyframes hang-l { 0%, 100% { transform: rotate(-3deg); } 35% { transform: rotate(4deg); } 64% { transform: rotate(-6deg); } 72% { transform: rotate(-2deg); } }
      @keyframes hang-r { 0%, 100% { transform: rotate(3deg); } 35% { transform: rotate(-4deg); } 64% { transform: rotate(6deg); } 72% { transform: rotate(2deg); } }
      .look { animation: downcast 7.5s ease-in-out infinite; }
      @keyframes downcast { 0%, 22%, 54%, 100% { transform: translate(0, 1.6px); } 30%, 46% { transform: translate(.5px, -1px); } }
      .tear { opacity: 0; animation: tear 7.5s infinite; transform-origin: 50px 58px; }
      @keyframes tear {
        0%, 44% { opacity: 0; transform: translateY(-2px) scale(.2); } 50% { opacity: 1; transform: translateY(0) scale(1); animation-timing-function: ${DOWN}; }
        68% { opacity: 1; transform: translateY(9px) scale(.9); } 72%, 100% { opacity: 0; transform: translateY(11px) scale(.6); } }`,
  },

  // a plan limit is nearly used up: jittery, wringing its hands, eyes darting, sweating
  nervous: {
    face: `${eyes(`${dot(51, 50, 4.8)}${dot(69, 50, 4.8)}<g class="pupils">${dot(52, 50.5, 1.9, C.screen)}${dot(70, 50.5, 1.9, C.screen)}</g>`)}
      ${stroke("M45 42 q5 -3 10 0", 2.4)}${stroke("M65 42 q5 -3 10 0", 2.4)}<g class="mouth">${stroke("M48 62 q2.75 -3 5.5 0 t5.5 0 t5.5 0 t5.5 0", 2.6)}</g>`,
    extra: `<path class="sweat" d="M15 30 q-4.5 6.5 0 9 q4.5 -2.5 0 -9z" fill="${C.drop}"/><path class="sweat s2" d="M104 34 q-3.4 5 0 7 q3.4 -2 0 -7z" fill="${C.drop}"/>`,
    css: `
      .move { animation: jitter .42s linear infinite; }
      @keyframes jitter {
        0% { transform: translate(0, 0) rotate(0); } 13% { transform: translate(.6px, -.2px) rotate(.4deg); }
        29% { transform: translate(-.4px, .2px) rotate(-.3deg); } 41% { transform: translate(.7px, 0) rotate(.2deg); }
        58% { transform: translate(-.6px, -.3px) rotate(-.5deg); } 72% { transform: translate(.3px, .2px) rotate(.3deg); }
        86% { transform: translate(-.7px, 0) rotate(-.2deg); } 100% { transform: translate(0, 0) rotate(0); } }
      .squash { animation: pant 1.3s ease-in-out infinite; } @keyframes pant { 50% { transform: scale(.99, 1.02); } }
      .head { animation: glance-head 2.6s infinite; }
      @keyframes glance-head { 0%, 30% { transform: rotate(-3deg); } 36%, 62% { transform: rotate(3deg); } 68%, 100% { transform: rotate(-3deg); } }
      .ant { animation: buzz .16s linear infinite; } @keyframes buzz { 50% { transform: rotate(4deg); } }
      .arm-l { animation: wring-l .55s ease-in-out infinite; } .arm-r { animation: wring-r .55s ease-in-out infinite; }
      @keyframes wring-l { 0%, 100% { transform: rotate(-26deg); } 50% { transform: rotate(-16deg); } }
      @keyframes wring-r { 0%, 100% { transform: rotate(16deg); } 50% { transform: rotate(26deg); } }
      .pupils { animation: dart 2.6s infinite; }
      @keyframes dart {
        0%, 14% { transform: translate(-2.2px, 0); } 17%, 30% { transform: translate(1.8px, .3px); } 33%, 42% { transform: translate(-1px, -.6px); }
        45%, 62% { transform: translate(2.3px, 0); } 65%, 82% { transform: translate(-2px, .4px); } 85%, 100% { transform: translate(-2.2px, 0); } }
      .mouth { animation: quiver .3s steps(2) infinite; } @keyframes quiver { 50% { transform: translateX(.6px); } }
      .sweat { opacity: 0; animation: sweat 2.8s infinite; transform-origin: 15px 34px; } .s2 { animation-delay: 1.5s; transform-origin: 104px 37px; }
      @keyframes sweat {
        0% { opacity: 0; transform: translateY(0) scale(.2); } 18% { opacity: 1; transform: translateY(0) scale(1); animation-timing-function: ${DOWN}; }
        70% { opacity: 1; transform: translateY(12px) scale(.95); } 80%, 100% { opacity: 0; transform: translateY(16px) scale(.7); } }`,
  },

  // welcome, or when you click it: crouches, jumps with both arms up, lands, sparkles
  excited: {
    face: `<g class="look"><g class="eyes-pop">${dot(51, 47, 5)}${dot(69, 47, 5)}</g>${dot(52.6, 45.4, 1.6, "#FFFFFF", "glint")}${dot(70.6, 45.4, 1.6, "#FFFFFF", "glint")}</g>
      <path class="mouth" d="M50 56 q10 13 20 0z" fill="${C.glow}"/>${dot(42, 57, 3, C.cheek)}${dot(78, 57, 3, C.cheek)}`,
    extra: `${star(14, 22, 5, "sp sp1")}${star(106, 16, 6, "sp sp2")}${star(108, 62, 4, "sp sp3")}${star(10, 66, 3.5, "sp sp4")}`,
    css: `
      .move { animation: jump 1.7s infinite; }
      @keyframes jump {
        0%, 16% { transform: translateY(0); animation-timing-function: ${UP}; }
        44% { transform: translateY(-17px); animation-timing-function: ${DOWN}; }
        70%, 100% { transform: translateY(0); } }
      .squash { animation: jump-squash 1.7s infinite; }
      @keyframes jump-squash {
        0% { transform: scale(1, 1); } 14% { transform: scale(1.11, .85); } 22% { transform: scale(.9, 1.12); }
        38%, 50% { transform: scale(.98, 1.02); } 66% { transform: scale(.94, 1.07); } 72% { transform: scale(1.13, .84); }
        82% { transform: scale(.97, 1.04); } 90%, 100% { transform: scale(1, 1); } }
      .shadow { animation: jump-shadow 1.7s infinite; }
      @keyframes jump-shadow { 0%, 16%, 70%, 100% { transform: scale(1); } 44% { transform: scale(.55); opacity: .08; } }
      .head { animation: jump-head 1.7s infinite; }
      @keyframes jump-head {
        0%, 12% { transform: translateY(0); } 16% { transform: translateY(2px); } 24% { transform: translateY(3px); } 40% { transform: translateY(-1.5px); }
        66% { transform: translateY(-2px); } 74% { transform: translateY(4px) rotate(2deg); } 84% { transform: translateY(-1px) rotate(-1deg); } 94%, 100% { transform: translateY(0); } }
      .ant { animation: jump-ant 1.7s infinite; }
      @keyframes jump-ant { 0%, 14% { transform: rotate(0); } 24% { transform: rotate(16deg); } 34% { transform: rotate(-8deg); } 44% { transform: rotate(4deg); } ${spring(70, 96, -20)} 100% { transform: rotate(0); } }
      .arm-l { animation: pump-l 1.7s infinite; } .arm-r { animation: pump-r 1.7s infinite; }
      @keyframes pump-l { 0% { transform: rotate(70deg); } 14% { transform: rotate(40deg); } 28% { transform: rotate(158deg); } 50% { transform: rotate(138deg); } 66% { transform: rotate(152deg); } 76% { transform: rotate(112deg); } 100% { transform: rotate(70deg); } }
      @keyframes pump-r { 0% { transform: rotate(-70deg); } 15% { transform: rotate(-40deg); } 29% { transform: rotate(-158deg); } 51% { transform: rotate(-138deg); } 67% { transform: rotate(-152deg); } 77% { transform: rotate(-112deg); } 100% { transform: rotate(-70deg); } }
      .eyes-pop { animation: pop 1.7s infinite; transform-origin: 60px 47px; }
      @keyframes pop { 0%, 12%, 30%, 100% { transform: scale(1); } 22% { transform: scale(1.18); } 72% { transform: scale(.9, 1.1); } }
      .mouth { animation: cheer 1.7s infinite; transform-origin: 60px 58px; }
      @keyframes cheer { 0%, 14% { transform: scale(.85, .7); } 26%, 60% { transform: scale(1.08, 1.15); } 74% { transform: scale(1, .8); } 100% { transform: scale(.85, .7); } }
      .glint { animation: glint .85s steps(2) infinite; } @keyframes glint { 50% { opacity: .35; } }
      .sp { opacity: 0; animation: sparkle 1.7s infinite; } .sp2 { animation-delay: .12s; } .sp3 { animation-delay: .3s; } .sp4 { animation-delay: .45s; }
      .sp1 { transform-origin: 14px 22px; } .sp2 { transform-origin: 106px 16px; } .sp3 { transform-origin: 108px 62px; } .sp4 { transform-origin: 10px 66px; }
      @keyframes sparkle { 0%, 30% { opacity: 0; transform: scale(0) rotate(0); } 44% { opacity: 1; transform: scale(1.15) rotate(45deg); } 62% { opacity: .9; transform: scale(.9) rotate(70deg); } 76%, 100% { opacity: 0; transform: scale(0) rotate(90deg); } }`,
  },
};

const BASE_CSS = `
  * { transform-box: view-box; }
  .move, .squash { transform-origin: 60px 128px; }
  .head { transform-origin: 60px 80px; }
  .ant { transform-origin: 60px 24px; }
  .arm-l { transform-origin: 36.5px 85px; transform: rotate(8deg); }
  .arm-r { transform-origin: 83.5px 85px; transform: rotate(-8deg); }
  .blink { transform-origin: 60px 50px; animation: blink 6.3s infinite; }
  @keyframes blink {
    0%, 41%, 45%, 86%, 89%, 92%, 100% { transform: scaleY(1); }
    43% { transform: scaleY(.08); } 87.5%, 90.5% { transform: scaleY(.08); } }
  .squash { animation: breathe 3.4s ease-in-out infinite; }
  @keyframes breathe { 50% { transform: scale(.99, 1.022); } }
  .shadow { fill: #000; opacity: .2; transform-origin: 60px 133px; }
  .ball { animation: glow 2.6s ease-in-out infinite; } @keyframes glow { 50% { opacity: .55; } }
  @media (prefers-color-scheme: light) { .shadow { opacity: .13; } }
  @media (prefers-reduced-motion: reduce) { * { animation: none !important; } }`;

function svg(mood, m) {
  const arms = `<g class="arm-l"><rect x="32" y="82" width="9" height="22" rx="4.5" fill="${C.coral}"/><circle cx="36.5" cy="106" r="5.5" fill="${C.cream}"/></g>
    <g class="arm-r"><rect x="79" y="82" width="9" height="22" rx="4.5" fill="${C.coral}"/><circle cx="83.5" cy="106" r="5.5" fill="${C.cream}"/></g>`;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-6 -20 132 160" role="img" aria-label="Quarters mascot, ${mood}">
<style>${BASE_CSS}${m.css}</style>
<ellipse class="shadow" cx="60" cy="133" rx="28" ry="4"/>
<g class="move"><g class="squash">
  <rect x="47" y="110" width="9" height="14" rx="3" fill="${C.coralDark}"/><rect x="64" y="110" width="9" height="14" rx="3" fill="${C.coralDark}"/>
  <rect x="43" y="121" width="16" height="7" rx="3.5" fill="${C.violet}"/><rect x="61" y="121" width="16" height="7" rx="3.5" fill="${C.violet}"/>
  <rect x="38" y="80" width="44" height="34" rx="12" fill="${C.coral}"/>
  <rect x="49" y="91" width="22" height="11" rx="4" fill="${C.coralDark}" opacity=".45"/>
  <circle cx="55" cy="96.5" r="2" fill="${C.cream}"/><circle cx="65" cy="96.5" r="2" fill="${C.violet}"/>
  <rect x="54" y="75" width="12" height="7" rx="2" fill="${C.coralDark}"/>
  <g class="head">
    <g class="ant"><path d="M60 24 V12" stroke="${C.coralDark}" stroke-width="3" stroke-linecap="round"/><circle class="ball" cx="60" cy="10" r="5" fill="${C.ant}"/></g>
    <rect x="14.5" y="40" width="7" height="18" rx="3.5" fill="${C.violet}"/><rect x="98.5" y="40" width="7" height="18" rx="3.5" fill="${C.violet}"/>
    <rect x="20" y="22" width="80" height="58" rx="14" fill="${C.cream}" stroke="${C.creamEdge}" stroke-width="2"/>
    <rect class="screen" x="29" y="30" width="62" height="42" rx="9" fill="${m.screen || C.screen}"/>
    ${m.face}
  </g>
  ${arms}
</g></g>
${m.extra || ""}
</svg>
`;
}

mkdirSync(OUT, { recursive: true });
for (const [mood, m] of Object.entries(MOODS)) {
  writeFileSync(join(OUT, `${mood}.svg`), svg(mood, m).replace(/\n\s+/g, "\n"));
  console.log(`${mood}.svg`);
}
