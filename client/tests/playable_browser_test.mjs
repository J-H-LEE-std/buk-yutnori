import assert from 'node:assert/strict';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { connect } from './devtools.mjs';

const [debuggerURL = 'http://localhost:9231', pageURL = 'http://localhost:8766/', evidence] = process.argv.slice(2);
const targets = await (await fetch(`${debuggerURL}/json/list`)).json();
const target = targets.find(t => t.type === 'page' && t.url.startsWith(pageURL));
assert(target, 'test page must be open');
const socket = await connect(target.webSocketDebuggerUrl);
let nextID = 1;
const requests = new Map();
socket.onMessage = text => {
  const message = JSON.parse(text);
  const request = requests.get(message.id);
  if (!request) return;
  requests.delete(message.id);
  if (message.error) request.reject(new Error(JSON.stringify(message.error))); else request.resolve(message.result);
};
const call = (method, params = {}) => new Promise((resolve,reject) => {
  const id = nextID++; requests.set(id,{resolve,reject}); socket.send(JSON.stringify({id,method,params}));
});
async function evaluate(expression) {
  const result = await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});
  if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
  return result.result.value;
}
async function wait(expression, timeout = 20000) {
  const deadline = Date.now() + timeout;
  do { if (await evaluate(expression)) return; await new Promise(r => setTimeout(r,100)); } while (Date.now()<deadline);
  throw new Error(`Timed out: ${expression}\n${await evaluate('document.body.innerText')}`);
}
async function click(selector) {
  const point = await evaluate(`(() => { const e=document.querySelector(${JSON.stringify(selector)}); if(!e || e.disabled || !e.getClientRects().length) throw new Error('not clickable: '+${JSON.stringify(selector)}); e.scrollIntoView({block:'center'}); const r=e.getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2}; })()`);
  await call('Input.dispatchMouseEvent',{type:'mousePressed',button:'left',clickCount:1,...point});
  await call('Input.dispatchMouseEvent',{type:'mouseReleased',button:'left',clickCount:1,...point});
}
async function fill(selector,text) {
  await click(selector);
  await evaluate(`document.querySelector(${JSON.stringify(selector)}).select()`);
  await call('Input.insertText',{text});
}
async function screenshot(name) {
  if (!evidence) return;
  mkdirSync(evidence,{recursive:true});
  const result = await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});
  writeFileSync(join(evidence,`${name}.png`),Buffer.from(result.data,'base64'));
}
try {
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1100,deviceScaleFactor:1,mobile:false});
  await call('Network.clearBrowserCookies');
  await call('Page.navigate',{url:pageURL});
  await wait('typeof wasmRuntimeReady !== "undefined" && wasmRuntimeReady && typeof BukScreens !== "undefined"');
  assert.equal(await evaluate('document.querySelector("main").dataset.screen'),'login');
  assert.equal(await evaluate('document.querySelector("#screen-game").getClientRects().length'),0);
  await screenshot('01-login');
  // Only the test binary accepts this credential. All subsequent operations
  // use the production HTTP handlers, session cookie, WebSocket and game rules.
  await evaluate(`handleGoogleCredential({credential:${JSON.stringify(`browser-test-player-${Date.now()}`)}})`);
  await wait('document.querySelector("main").dataset.screen === "lobby" && realtimeSocket?.readyState === WebSocket.OPEN');
  await click('#my-profile');
  await wait('!document.querySelector("#profile-save").disabled');
  await fill('#profile-nickname','브라우저검증');
  await click('#profile-save');
  await wait('!document.querySelector("#profile-save").disabled');
  await click('#profile-cancel');
  await screenshot('02-lobby');
  await fill('#room-title','혼자 CPU와 수동 검증');
  await click('#room-create');
  await wait('document.querySelector("main").dataset.screen === "room" && !document.querySelector("#room-add-cpu-b").disabled');
  assert.equal(await evaluate('document.querySelector("#screen-lobby").getClientRects().length'),0);
  await click('#room-add-cpu-b');
  await wait('document.querySelector("#room-members").textContent.includes("CPU")');
  await click('#room-ready');
  await wait('document.querySelector("#room-ready").textContent === "준비 취소"');
  await screenshot('03-room');
  await click('#room-start');
  await wait('document.querySelector("main").dataset.screen === "game" && !document.querySelector("#throw-yut").disabled',40000);
  assert.equal(await evaluate('document.querySelector("#screen-room").getClientRects().length'),0);
  await screenshot('04-game');
  await click('#throw-yut');
  await wait('document.querySelector("#latest-result").textContent.includes("내 결과")');
  assert.match(await evaluate('document.querySelector("#latest-result").textContent'), /도|개|걸|윷|모|북/);
  await screenshot('05-result');
  let moved = false;
  for (let step=0;step<35 && !moved;step++) {
    const action = await evaluate(`(() => {
      if(!throwYut.hidden && !throwYut.disabled && canSendStateChangingCommand()) return 'throw';
      if(document.querySelector('#move-candidates button:not(:disabled)')) return 'move';
      if(document.querySelector('#waiting-pieces button:not(:disabled)')) return 'waiting';
      if(document.querySelector('#piece-targets button:not(:disabled)')) return 'piece';
      return 'wait';
    })()`);
    if (action === 'throw') await click('#throw-yut');
    if (action === 'waiting') await click('#waiting-pieces button:not(:disabled)');
    if (action === 'piece') await click('#piece-targets button:not(:disabled)');
    if (action === 'move') {
      assert(await evaluate('document.querySelector("#move-paths polyline") !== null || confirmedRouteRequest !== null'),'server preview must be visible before moving');
      await screenshot('06-preview');
      await click('#move-candidates button:not(:disabled)');
    }
    await new Promise(r=>setTimeout(r,400));
    moved = await evaluate('document.querySelector("#piece-targets [data-piece-id^=A-]") !== null');
  }
  assert(moved,'a visible piece must reach the board through real UI actions');
  await screenshot('06-moved');
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});
  assert(await evaluate('document.documentElement.scrollWidth <= innerWidth'),'mobile game must not overflow horizontally');
  await screenshot('07-mobile');
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1100,deviceScaleFactor:1,mobile:false});
  await call('Page.reload',{ignoreCache:true});
  await new Promise(r=>setTimeout(r,500));
  await wait('document.querySelector("main").dataset.screen === "game" && canSendStateChangingCommand()',30000);
  await screenshot('08-reconnected');
  console.log('PLAYABLE_BROWSER_OK login -> lobby -> room -> CPU -> game -> throw result -> piece movement');
} finally { socket.close(); }
